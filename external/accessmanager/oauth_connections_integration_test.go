package accessmanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/repository"
	helpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type connectionMail struct {
	custom *emailmanager.SendCustomEmailRequest
	login  *emailmanager.SendLoginEmailRequest
	fail   bool
}

type failedDisconnectCleanup struct{ accessmanager.EphemeralStore }

func (failedDisconnectCleanup) DeleteAllTokenExceptedSpecified(context.Context, string, []string) error {
	return errors.New("fixture session cleanup unavailable")
}

func (m *connectionMail) SendCustomEmail(_ context.Context, r *emailmanager.SendCustomEmailRequest) error {
	m.custom = r
	if m.fail {
		return errors.New("fixture send failed")
	}
	return nil
}
func (m *connectionMail) SendLoginEmail(_ context.Context, r *emailmanager.SendLoginEmailRequest) error {
	m.login = r
	return nil
}
func (m *connectionMail) SendVerificationEmail(context.Context, *emailmanager.SendVerificationEmailRequest) error {
	return nil
}

type connectionFixture struct {
	ctx       context.Context
	users     *user.Service
	service   *accessmanager.Service
	auth      *auth.Service
	ephemeral *ephemeral.Client
	db        *mongo.Database
	redis     *redis.Client
	mail      *connectionMail
	handler   *accessmanager.Handler
	router    http.Handler
	account   *user.UniversalUser
	tokens    *auth.TokenDetails
	namespace string
}

func newConnectionFixture(t *testing.T) *connectionFixture {
	t.Helper()
	mongoURI, redisAddr := os.Getenv("GHATD_TEST_MONGO_URI"), os.Getenv("GHATD_TEST_REDIS_ADDR")
	if mongoURI == "" || redisAddr == "" {
		t.Skip("set GHATD_TEST_MONGO_URI and GHATD_TEST_REDIS_ADDR for isolated connection lifecycle integration")
	}
	f := &connectionFixture{ctx: context.Background(), mail: &connectionMail{}, namespace: "disconnect_" + toolbox.GenerateUuidV4()}
	handler, err := helpers.NewHandler(helpers.DefaultConfig(mongoURI, f.namespace))
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close(f.ctx) })
	store := repository.NewMongoDbRepositoryWithDefaults(handler, f.namespace)
	f.db, err = store.GetDatabase(f.ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.db.Drop(f.ctx) })
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(f.ctx, f.db))
	f.users = user.NewService(user.NewRepository(store), nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	runtime, err := ephemeral.NewRedisRuntime(f.ctx, &ephemeral.NewRedisRuntimeRequest{Options: &redis.Options{Addr: redisAddr}, Component: f.namespace, Environment: "test"})
	require.NoError(t, err)
	f.redis = runtime.Client
	f.ephemeral = runtime.Store
	t.Cleanup(func() { _ = runtime.Close(f.ctx) })
	f.auth = auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "fixture-access", RefreshTokenSecret: "fixture-refresh"})
	f.service = accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: f.users, AuthService: f.auth, EphemeralStore: runtime.Store, AuditService: oauthAuditStub{}, EmailManager: f.mail})
	require.NoError(t, f.service.ConfigureOAuthConnections(accessmanager.OAuthConnectionsConfig{Origin: "https://app.example", Store: oauth.NewRedisDisconnectChallengeStore(runtime.Client, f.namespace)}))
	f.handler = accessmanager.NewHandler(&accessmanager.NewHandlerRequest{Service: f.service, Validator: validator.NewValidator(), ErrorMaps: []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap}, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", OAuthOrigin: "https://app.example"})
	routes := router.NewRouter(nil, nil)
	accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{Router: routes, Handler: f.handler})
	f.router = routes.GetRouter()
	f.account, f.tokens = f.newAccount(t, "original@example.test", "subject")
	return f
}
func (f *connectionFixture) newAccount(t *testing.T, email, subject string) (*user.UniversalUser, *auth.TokenDetails) {
	t.Helper()
	created, err := f.users.CreateOAuthUser(f.ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: subject}, Email: email})
	require.NoError(t, err)
	tokens, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, created.User, time.Now())
	require.NoError(t, err)
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, created.User.ID, tokens))
	return created.User, tokens
}
func (f *connectionFixture) serve(method, path, body, origin, contentType string, tokens *auth.TokenDetails) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "https://app.example/api/v1/ams/oauth/connections"+path, strings.NewReader(body))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", contentType)
	if tokens != nil {
		req.AddCookie(&http.Cookie{Name: "access", Value: tokens.AccessToken})
		req.AddCookie(&http.Cookie{Name: "refresh", Value: tokens.RefreshToken})
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}
func (f *connectionFixture) request(method, path, body string) *httptest.ResponseRecorder {
	return f.serve(method, path, body, "https://app.example", "application/json", f.tokens)
}
func (f *connectionFixture) current(t *testing.T) *user.UniversalUser {
	t.Helper()
	r, err := f.users.GetUserByID(f.ctx, &user.GetUserByIDRequest{ID: f.account.ID})
	require.NoError(t, err)
	return r.User
}
func (f *connectionFixture) start(t *testing.T, email string) (string, string, string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"email": email})
	r := f.request("POST", "/google/disconnect", string(raw))
	require.Equal(t, 202, r.Code, r.Body.String())
	var response struct {
		Data accessmanager.OAuthDisconnectStartResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(r.Body.Bytes(), &response))
	matches := regexp.MustCompile(`<strong>([A-Z0-9]{8})</strong>`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, matches, 2)
	linkMatches := regexp.MustCompile(`href="([^"]+)"`).FindStringSubmatch(f.mail.custom.EmailBody)
	require.Len(t, linkMatches, 2)
	link, err := url.Parse(html.UnescapeString(linkMatches[1]))
	require.NoError(t, err)
	fragment, err := url.ParseQuery(link.Fragment)
	require.NoError(t, err)
	require.Equal(t, "google", fragment.Get("oauth_disconnect"))
	require.Equal(t, response.Data.ChallengeID, fragment.Get("challenge_id"))
	require.Empty(t, link.RawQuery)
	return response.Data.ChallengeID, matches[1], fragment.Get("token")
}
func confirmBody(id, code, token string) string {
	value, _ := json.Marshal(accessmanager.OAuthDisconnectConfirmRequest{ChallengeID: id, Code: code, Token: token})
	return string(value)
}

func TestOAuthConnectionsEmailFallbackLifecycle(t *testing.T) {
	f := newConnectionFixture(t)
	r := f.request("GET", "", "")
	require.Equal(t, 200, r.Code, r.Body.String())
	require.Contains(t, r.Body.String(), `"connected":["google"]`)
	require.NotContains(t, r.Body.String(), "subject")
	require.NotContains(t, r.Body.String(), "linked_at")
	stale := f.current(t)
	oldToken, err := f.service.CreateInitalLoginToken(f.ctx, f.account, false, "/app")
	require.NoError(t, err)
	id, code, _ := f.start(t, "replacement@example.test")
	require.Equal(t, "original@example.test", f.current(t).Email)
	require.Len(t, f.current(t).OAuthIdentities, 1)
	require.Equal(t, 400, f.request("POST", "/google/disconnect/confirm", confirmBody(id, "WRONG123", "")).Code)
	result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
	require.Equal(t, 200, result.Code, result.Body.String())
	oldSession := *f.tokens
	f.tokens = &auth.TokenDetails{}
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == "access" {
			f.tokens.AccessToken = cookie.Value
		}
		if cookie.Name == "refresh" {
			f.tokens.RefreshToken = cookie.Value
		}
	}
	require.NotEmpty(t, f.tokens.AccessToken)
	// Simulate an overlapping old login restoring its Redis session after
	// cleanup: signed revision binding must still reject access and refresh.
	require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, &oldSession))
	require.Equal(t, 401, f.serve("GET", "", "", "", "", &oldSession).Code)
	oldRequest := httptest.NewRequest("GET", "/private", nil)
	oldRequest.Header.Set("Authorization", "Bearer "+oldSession.AccessToken)
	_, err = f.service.MiddlewareJWTRequired(oldRequest)
	require.Error(t, err)
	_, err = f.service.MiddlewareActiveJWTRequired(oldRequest)
	require.Error(t, err)
	_, refreshErr := f.service.RefreshToken(f.ctx, &accessmanager.RefreshTokenRequest{RefreshToken: oldSession.RefreshToken})
	require.Error(t, refreshErr)
	current := f.current(t)
	require.Equal(t, stale.ID, current.ID)
	require.Equal(t, "replacement@example.test", current.Email)
	require.Empty(t, current.OAuthIdentities)
	require.True(t, current.Verification.EmailVerified)
	require.NoError(t, current.Validate())
	var stored bson.M
	require.NoError(t, f.db.Collection("users").FindOne(f.ctx, bson.M{"_id": current.ID}).Decode(&stored))
	require.NotContains(t, stored, "oauth_identity_keys")
	require.Equal(t, 400, f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code)
	_, err = f.service.LoginUser(f.ctx, &accessmanager.LoginUserRequest{Token: oldToken})
	require.Error(t, err)
	stale.PersonalInfo.FirstName = "Old snapshot"
	_, err = f.users.UpdateUser(f.ctx, &user.UpdateUserRequest{User: stale})
	require.Error(t, err)
	require.Equal(t, current.Email, f.current(t).Email)
	_, err = f.service.CreateInitalLoginToken(f.ctx, current, false, "/new-login")
	require.NoError(t, err)
	require.Equal(t, current.Email, f.mail.login.Email)
	loggedIn, err := f.service.LoginUser(f.ctx, &accessmanager.LoginUserRequest{Code: f.mail.login.Code})
	require.NoError(t, err)
	details, err := f.auth.ExtractAccessTokenMetadataByString(f.ctx, loggedIn.AccessToken)
	require.NoError(t, err)
	require.Equal(t, current.ID, details.UserID)
	// Even a callback that passed the session check before disconnect cannot
	// attach its identity later using the old revision. Fresh linking still works.
	_, err = f.users.LinkOAuthIdentityAtRevision(f.ctx, current.ID, &stale.OAuthIdentities[0], stale.EmailRevision)
	require.Error(t, err)
	require.Empty(t, f.current(t).OAuthIdentities)
	_, err = f.users.LinkOAuthIdentityAtRevision(f.ctx, current.ID, &stale.OAuthIdentities[0], current.EmailRevision)
	require.NoError(t, err)
	_, err = f.users.LinkOAuthIdentityAtRevision(f.ctx, current.ID, &stale.OAuthIdentities[0], stale.EmailRevision)
	require.Error(t, err, "idempotent-link lookup must also check the revision")
	// A second last-provider removal must not collide on the sparse unique index.
	f.account, f.tokens = f.newAccount(t, "second@example.test", "second-subject")
	id, _, token := f.start(t, f.account.Email)
	result = f.request("POST", "/google/disconnect/confirm", confirmBody(id, "", token))
	require.Equal(t, 200, result.Code, result.Body.String())
}

func TestOAuthConnectionsSecurityBoundaries(t *testing.T) {
	t.Run("session failure after commit reports the completed change", func(t *testing.T) {
		f := newConnectionFixture(t)
		id, code, _ := f.start(t, "verified@example.test")
		f.service.EphemeralStore = failedDisconnectCleanup{f.ephemeral}
		result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
		require.Equal(t, 503, result.Code, result.Body.String())
		require.Contains(t, result.Body.String(), "OAuthDisconnectSessionRequired")
		require.Equal(t, "verified@example.test", f.current(t).Email)
		require.Empty(t, f.current(t).OAuthIdentities)
		require.Equal(t, 401, f.request("GET", "", "").Code)
	})
	t.Run("session and CSRF", func(t *testing.T) {
		f := newConnectionFixture(t)
		require.Equal(t, 401, f.serve("GET", "", "", "", "", nil).Code)
		for _, origin := range []string{"", "https://attacker.example", "null"} {
			require.Equal(t, 403, f.serve("POST", "/google/disconnect", `{}`, origin, "application/json", f.tokens).Code)
		}
		require.Equal(t, 400, f.serve("POST", "/google/disconnect", `{}`, "https://app.example", "text/plain", f.tokens).Code)
		require.Equal(t, 400, f.request("POST", "/google/disconnect", `{"email":"a@example.test","user_id":"other"}`).Code)
		require.Contains(t, []int{404, 405}, f.request("GET", "/google/disconnect/confirm", "").Code)
		stale, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now().Add(-6*time.Minute))
		require.NoError(t, err)
		require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, stale))
		require.Equal(t, 401, f.serve("POST", "/google/disconnect", `{}`, "https://app.example", "application/json", stale).Code)
		_, err = f.ephemeral.DeleteAuth(f.ctx, toolbox.CombinedUuidFormat(f.account.ID, f.tokens.AccessUUID))
		require.NoError(t, err)
		require.Equal(t, 401, f.request("GET", "", "").Code)
	})
	t.Run("duplicate email never merges", func(t *testing.T) {
		f := newConnectionFixture(t)
		other, _ := f.newAccount(t, "taken@example.test", "taken")
		id, code, _ := f.start(t, other.Email)
		r := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
		require.Equal(t, 409, r.Code, r.Body.String())
		require.Equal(t, f.account.Email, f.current(t).Email)
		require.Len(t, f.current(t).OAuthIdentities, 1)
	})
	t.Run("wrong session and attempt lockout", func(t *testing.T) {
		f := newConnectionFixture(t)
		id, code, _ := f.start(t, f.account.Email)
		another, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.account, time.Now())
		require.NoError(t, err)
		require.NoError(t, f.ephemeral.CreateAuth(f.ctx, f.account.ID, another))
		require.Equal(t, 400, f.serve("POST", "/google/disconnect/confirm", confirmBody(id, code, ""), "https://app.example", "application/json", another).Code)
		for i := 0; i < 4; i++ {
			require.Equal(t, 400, f.request("POST", "/google/disconnect/confirm", confirmBody(id, "WRONG123", "")).Code)
		}
		require.Equal(t, 423, f.request("POST", "/google/disconnect/confirm", confirmBody(id, "WRONG123", "")).Code)
		require.Equal(t, 423, f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code)
		require.Len(t, f.current(t).OAuthIdentities, 1)
		require.Equal(t, 429, f.request("POST", "/google/disconnect", `{}`).Code)
	})
	t.Run("failed send leaves no proof", func(t *testing.T) {
		f := newConnectionFixture(t)
		f.mail.fail = true
		r := f.request("POST", "/google/disconnect", `{}`)
		require.Equal(t, 503, r.Code, r.Body.String())
		require.Len(t, f.current(t).OAuthIdentities, 1)
		keys, err := f.redis.Keys("oauth:disconnect:" + f.namespace + ":*").Result()
		require.NoError(t, err)
		require.Len(t, keys, 1)
		require.Contains(t, keys[0], ":cooldown:")
	})
	t.Run("expired proof cannot remove provider", func(t *testing.T) {
		f := newConnectionFixture(t)
		id, code, _ := f.start(t, f.account.Email)
		require.NoError(t, f.redis.PExpire("oauth:disconnect:"+f.namespace+":"+id, time.Millisecond).Err())
		time.Sleep(10 * time.Millisecond)
		require.Equal(t, 400, f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code)
		require.Len(t, f.current(t).OAuthIdentities, 1)
	})
	t.Run("disconnect all identities for one provider, preserve the other", func(t *testing.T) {
		f := newConnectionFixture(t)
		_, err := f.users.LinkOAuthIdentity(f.ctx, f.account.ID, &user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: "second-google"})
		require.NoError(t, err)
		_, err = f.users.LinkOAuthIdentity(f.ctx, f.account.ID, &user.OAuthIdentity{Provider: "apple", Issuer: oauth.AppleIssuer, Subject: "apple"})
		require.NoError(t, err)
		id, code, _ := f.start(t, f.account.Email)
		require.Equal(t, 200, f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code)
		after := f.current(t)
		require.Len(t, after.OAuthIdentities, 1)
		require.Equal(t, "apple", after.OAuthIdentities[0].Provider)
	})
	t.Run("concurrent redemption has one winner", func(t *testing.T) {
		f := newConnectionFixture(t)
		id, code, _ := f.start(t, f.account.Email)
		var wg sync.WaitGroup
		statuses := make(chan int, 12)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses <- f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, "")).Code
			}()
		}
		wg.Wait()
		close(statuses)
		wins := 0
		for status := range statuses {
			if status == 200 {
				wins++
			} else {
				require.Contains(t, []int{400, 401}, status)
			}
		}
		require.Equal(t, 1, wins)
	})
	t.Run("account changes invalidate pending proof", func(t *testing.T) {
		for _, change := range []string{"email", "relink", "new-identity", "restricted"} {
			t.Run(change, func(t *testing.T) {
				f := newConnectionFixture(t)
				id, code, _ := f.start(t, f.account.Email)
				switch change {
				case "email":
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"email": "changed@example.test"}})
					require.NoError(t, err)
				case "relink":
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"oauth_identities.0.linked_at": time.Now().Add(time.Second)}})
					require.NoError(t, err)
				case "new-identity":
					_, err := f.users.LinkOAuthIdentity(f.ctx, f.account.ID, &user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: "additional"})
					require.NoError(t, err)
				case "restricted":
					_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
					require.NoError(t, err)
				}
				result := f.request("POST", "/google/disconnect/confirm", confirmBody(id, code, ""))
				require.Contains(t, []int{409, 403}, result.Code, result.Body.String())
				require.NotEmpty(t, f.current(t).OAuthIdentities)
			})
		}
	})
}
