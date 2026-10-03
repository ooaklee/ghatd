package accessmanager_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// emailChangeDelivery captures only this fixture's outgoing verification proof.
type emailChangeDelivery struct {
	*connectionMail
	proof string
}

func (m *emailChangeDelivery) SendVerificationEmail(_ context.Context, r *emailmanager.SendVerificationEmailRequest) error {
	m.proof = r.Token
	return nil
}

// emailChangeRequest traverses the real route and cookie authentication without
// publishing identity from a test helper. Each caller supplies its own session.
func emailChangeRequest(f *connectionFixture, target, email string, tokens *auth.TokenDetails) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{"email": email})
	req := httptest.NewRequest(http.MethodPatch, "https://app.example/api/v1/ams/users/"+target+"/email", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: "access", Value: tokens.AccessToken})
	req.AddCookie(&http.Cookie{Name: "refresh", Value: tokens.RefreshToken})
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

// emailChangeAudit installs the production audit service/repository against this
// case's disposable database; it does not replace the fixture's mail adapter.
func emailChangeAudit(t *testing.T, f *connectionFixture) {
	t.Helper()
	store, err := repository.NewMongoDbRepositoryFromDatabase(f.db, nil)
	require.NoError(t, err)
	f.service.AuditService = audit.NewService(audit.NewRepository(store))
}

// Native domain failures must survive the production handler's manifest order.
// In particular Access Manager's established duplicate-email override is retained.
func TestEmailChangeLiveHTTPErrorMaps(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{
		{"duplicate email", http.StatusConflict, "OAuthEmailConflict"},
		{"missing target", http.StatusNotFound, "USV2-020"},
		{"suspended target", http.StatusBadRequest, "USV2-003"},
		{"missing index", http.StatusServiceUnavailable, "USV2-036"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newConnectionFixture(t)
			emailChangeAudit(t, f)
			mail := &emailChangeDelivery{connectionMail: f.mail}
			f.service.EmailManager = mail
			actor, tokens := f.newAccount(t, "operator@example.test", "operator-subject")
			_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": actor.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
			require.NoError(t, err)
			target, email := f.account.ID, "changed@example.test"
			switch tc.name {
			case "duplicate email":
				email = actor.Email
			case "missing target":
				target = "00000000-0000-4000-8000-000000000099"
			case "suspended target":
				_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": target}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
				require.NoError(t, err)
			case "missing index":
				err = f.db.Collection("users").Indexes().DropOne(f.ctx, "idx_users_email")
				require.NoError(t, err)
			}
			var before, after bson.M
			require.NoError(t, f.db.Collection("users").FindOne(f.ctx, bson.M{"_id": f.account.ID}).Decode(&before))
			w := emailChangeRequest(f, target, email, tokens)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			var response struct {
				Errors []struct {
					Code string `json:"code"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Len(t, response.Errors, 1)
			require.Equal(t, tc.code, response.Errors[0].Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, w.Result().Cookies())
			require.Empty(t, mail.proof)
			require.Nil(t, mail.custom)
			require.NotContains(t, w.Body.String(), "@")
			require.NoError(t, f.db.Collection("users").FindOne(f.ctx, bson.M{"_id": f.account.ID}).Decode(&after))
			require.Equal(t, before, after, "rejected change must not mutate the target")
			count, err := f.db.Collection(audit.AuditCollection).CountDocuments(f.ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count)
			_, err = f.service.AuthenticateSession(f.ctx, tokens.AccessToken)
			require.NoError(t, err, "target rejection must preserve the administrator session")
		})
	}
}

// Consecutive changes invalidate unconsumed proofs whether the intermediate
// mailbox was already verified or remained provisional. Each case owns stores.
func TestEmailChangeLiveRepeatedRevision(t *testing.T) {
	for _, mode := range []string{"intermediate provisional", "intermediate verified"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectionFixture(t)
			mail := &emailChangeDelivery{connectionMail: f.mail}
			f.service.EmailManager = mail
			actor, tokens := f.newAccount(t, "operator@example.test", "operator-subject")
			_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": actor.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
			require.NoError(t, err)
			w := emailChangeRequest(f, f.account.ID, "first@example.test", tokens)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if mode == "intermediate verified" {
				_, err = f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: mail.proof})
				require.NoError(t, err)
			}
			intermediate := f.current(t)
			// Mint a separate, unconsumed proof so failure cannot be attributed to
			// having consumed the delivery proof when activating the first address.
			proof, err := f.auth.CreateEmailVerificationToken(f.ctx, intermediate)
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.StoreToken(f.ctx, proof.EmailVerificationUUID, intermediate.ID, proof.EvTTL))
			w = emailChangeRequest(f, f.account.ID, "second@example.test", tokens)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			current := f.current(t)
			require.Equal(t, "second@example.test", current.Email)
			require.Equal(t, f.account.EmailRevision+2, current.EmailRevision)
			require.False(t, current.Verification.EmailVerified)
			_, err = f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: proof.EmailVerificationToken})
			require.Error(t, err)
			require.False(t, f.current(t).Verification.EmailVerified)
			_, err = f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: mail.proof})
			require.NoError(t, err)
			require.True(t, f.current(t).Verification.EmailVerified)
			_, err = f.service.AuthenticateSession(f.ctx, tokens.AccessToken)
			require.NoError(t, err)
		})
	}
}

func TestEmailChangeLiveCredentialRevocationIntegration(t *testing.T) {
	for _, mode := range []string{"self", "admin", "self cleanup unavailable", "admin cleanup unavailable"} {
		t.Run(mode, func(t *testing.T) {
			f := newConnectionFixture(t)
			emailChangeAudit(t, f)
			mail := &emailChangeDelivery{connectionMail: f.mail}
			f.service.EmailManager = mail
			actor, actorTokens := f.account, f.tokens
			if strings.HasPrefix(mode, "admin") {
				actor, actorTokens = f.newAccount(t, "admin@example.test", "admin-subject")
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": actor.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
				require.NoError(t, err)
			}
			login, err := f.auth.CreateInitalToken(f.ctx, f.account)
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.StoreToken(f.ctx, login.EphemeralUUID, f.account.ID, login.EtTTL))
			verify, err := f.auth.CreateEmailVerificationToken(f.ctx, f.account)
			require.NoError(t, err)
			require.NoError(t, f.ephemeral.StoreToken(f.ctx, verify.EmailVerificationUUID, f.account.ID, verify.EvTTL))
			if strings.Contains(mode, "unavailable") {
				f.service.EphemeralStore = failedDisconnectCleanup{f.ephemeral}
			}
			w := emailChangeRequest(f, f.account.ID, "changed@example.test", actorTokens)
			require.Equal(t, 200, w.Code, w.Body.String())
			var response struct {
				Data accessmanager.UpdateUserEmailResponse `json:"data"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.True(t, response.Data.Changed)
			require.True(t, response.Data.AuditRecorded)
			require.Equal(t, !strings.Contains(mode, "unavailable"), response.Data.SessionCleanupComplete)
			cursor, err := f.db.Collection(audit.AuditCollection).Find(f.ctx, bson.M{})
			require.NoError(t, err)
			var events []bson.M
			require.NoError(t, cursor.All(f.ctx, &events))
			require.Len(t, events, 1)
			require.Equal(t, actor.ID, events[0]["actor_id"])
			require.Equal(t, f.account.ID, events[0]["target_id"])
			require.Equal(t, string(audit.UserAccountChangeEmail), events[0]["action"])
			require.Equal(t, bson.D{{Key: "email_revision", Value: f.account.EmailRevision + 1}}, events[0]["details"])
			persisted, err := json.Marshal(events)
			require.NoError(t, err)
			require.NotContains(t, string(persisted), "@", "persisted audit must not contain either mailbox")
			require.NotContains(t, string(persisted), mail.proof)
			require.NotEmpty(t, mail.proof)
			current := f.current(t)
			require.Equal(t, "changed@example.test", current.Email)
			require.Equal(t, f.account.EmailRevision+1, current.EmailRevision)
			require.False(t, current.Verification.EmailVerified)
			if strings.HasPrefix(mode, "admin") {
				require.Empty(t, w.Result().Cookies())
				_, err = f.service.AuthenticateSession(f.ctx, actorTokens.AccessToken)
				require.NoError(t, err)
			} else {
				require.Len(t, w.Result().Cookies(), 4)
			}
			// Restore ACTIVE without reducing the revision, to prove rejection is
			// not just the provisional-status gate or successful cache deletion.
			_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": "ACTIVE"}})
			require.NoError(t, err)
			_, err = f.service.AuthenticateSession(f.ctx, f.tokens.AccessToken)
			require.Error(t, err)
			_, err = f.service.RefreshToken(f.ctx, &accessmanager.RefreshTokenRequest{RefreshToken: f.tokens.RefreshToken})
			require.Error(t, err)
			_, err = f.service.LoginUser(f.ctx, &accessmanager.LoginUserRequest{Token: login.EphemeralToken})
			require.Error(t, err)
			_, err = f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: verify.EmailVerificationToken})
			require.Error(t, err)
			// The freshly minted proof targets the new revision and can activate
			// that address; provider identities remain intact throughout.
			_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": "PROVISIONED"}})
			require.NoError(t, err)
			_, err = f.service.ValidateEmailVerificationCode(f.ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: mail.proof})
			require.NoError(t, err)
			current = f.current(t)
			require.True(t, current.Verification.EmailVerified)
			require.Equal(t, f.account.OAuthIdentityKeys, current.OAuthIdentityKeys)
			// Ordinary profile replacement must not restore the previous email.
			_, err = f.users.UpdateUser(f.ctx, &user.UpdateUserRequest{User: f.account})
			require.Equal(t, user.ErrEmailChangeRequired, err)
		})
	}
}
