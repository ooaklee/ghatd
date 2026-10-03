package accessmanager

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/oauth"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// oauthAuthorityClaims supplies already verified metadata; cryptographic and
// persistent-store coverage lives in login_proof_integration_test.go.
type oauthAuthorityClaims struct {
	AuthService
	details *auth.TokenAccessDetails
}

func (a oauthAuthorityClaims) ExtractAccessTokenMetadataByString(context.Context, string) (*auth.TokenAccessDetails, error) {
	return a.details, nil
}

type oauthAuthorityStore struct {
	EphemeralStore
	owner string
	err   error
}

func (s oauthAuthorityStore) FetchAuth(context.Context, ephemeral.TokenDetailsAccess) (string, error) {
	return s.owner, s.err
}

type oauthAuthorityUsers struct {
	UserService
	account *user.UniversalUser
	err     error
}

func (s oauthAuthorityUsers) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{User: s.account}, s.err
}

func TestOAuthAuthorityAtInitiationAndCallback(t *testing.T) {
	outage := errors.New("private adapter diagnostic")
	for _, tc := range []struct {
		name string
		want error
	}{
		{"accepted", nil},
		{"legacy type unbound", nil},
		{"changed type", ErrOAuthReauthenticationRequired},
		{"changed revision", ErrOAuthReauthenticationRequired},
		{"wrong owner", ErrSessionVerificationUnavailable},
		{"absent session", ErrOAuthReauthenticationRequired},
		{"cache outage", outage},
		{"joined absence and outage", outage},
		{"account outage", outage},
		{"wrong account", ErrOAuthReauthenticationRequired},
		{"nil account", ErrOAuthReauthenticationRequired},
		{"restricted account", user.ErrOAuthRestricted},
		{"unverified account", user.ErrOAuthRestricted},
		{"old login", ErrOAuthReauthenticationRequired},
		{"unknown login time", ErrOAuthReauthenticationRequired},
		{"future login", ErrOAuthReauthenticationRequired},
		{"cancelled", context.Canceled},
	} {
		for _, phase := range []string{"initiation", "callback"} {
			t.Run(phase+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				account := user.NewUserFactory(nil).CreateUser("member@example.test")
				account.ID, account.Type, account.Status = "member", "person", user.AccountStatusKeyActive
				account.VerifyEmail()
				details := &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", UserType: "person", TokenUse: auth.TokenUseAccess, IsAuthorized: true, AuthenticationTime: time.Now().Add(-time.Minute)}
				store := oauthAuthorityStore{owner: "member"}
				users := oauthAuthorityUsers{account: account}
				switch tc.name {
				case "legacy type unbound":
					details.UserType = ""
				case "changed type":
					account.Type = "service"
				case "changed revision":
					account.EmailRevision++
				case "wrong owner":
					store.owner = "other"
				case "absent session":
					store.err = ephemeral.ErrAuthNotFound
				case "cache outage":
					store.err = outage
				case "joined absence and outage":
					store.err = errors.Join(ephemeral.ErrAuthNotFound, outage)
				case "account outage":
					users.err = outage
				case "wrong account":
					account.ID = "other"
				case "nil account":
					users.account = nil
				case "restricted account":
					account.Status = user.AccountStatusKeyProvisioned
				case "unverified account":
					account.Verification.EmailVerified = false
				case "old login":
					details.AuthenticationTime = time.Now().Add(-6 * time.Minute)
				case "unknown login time":
					details.AuthenticationTime = time.Time{}
				case "future login":
					details.AuthenticationTime = time.Now().Add(time.Minute)
				case "cancelled":
					cancel()
				}
				s := &Service{AuthService: oauthAuthorityClaims{details: details}, EphemeralStore: store, UserService: users}
				var err error
				if phase == "initiation" {
					_, _, err = s.connectionAccount(ctx, "signed-session", true)
				} else {
					err = s.validateOAuthLinkProof(ctx, &oauth.LinkProof{UserID: details.UserID, AccessUUID: details.AccessUUID, AuthenticationTime: details.AuthenticationTime, UserType: details.UserType, EmailRevision: details.EmailRevision})
				}
				require.ErrorIs(t, err, tc.want)
			})
		}
	}
}

func TestRecentOAuthAuthenticationBoundary(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{"now", now, true},
		{"exactly five minutes", now.Add(-5 * time.Minute), true},
		{"one nanosecond too old", now.Add(-5*time.Minute - time.Nanosecond), false},
		{"one nanosecond in future", now.Add(time.Nanosecond), false},
		{"missing", time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, recentOAuthAuthentication(tc.at, now)) })
	}
}
