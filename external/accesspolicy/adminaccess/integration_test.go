package adminaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// capturedMail retains synthetic proof in this test process only; production
// uses the existing email manager and never returns a code through HTTP.
type capturedMail struct {
	code string
	fail bool
}

func (m *capturedMail) SendCustomEmail(_ context.Context, r *emailmanager.SendCustomEmailRequest) error {
	if m.fail {
		return fmt.Errorf("private delivery diagnostic")
	}
	m.code = regexp.MustCompile(`<strong>([0-9A-F]{12})</strong>`).FindStringSubmatch(r.EmailBody)[1]
	return nil
}

type bridgeFixture struct {
	ctx            context.Context
	db             *mongo.Database
	redis          *redis.Client
	auth           *auth.Service
	runtime        *ephemeral.RedisRuntime
	actor          *user.UniversalUser
	tokens         *auth.TokenDetails
	bridge         *Bridge
	routes         http.Handler
	mail           *capturedMail
	page, reviewID string
}

// newBridgeFixture owns one randomly named Mongo database and Redis namespace.
// Cleanup targets only those generated identities; shared development data stays intact.
func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	uri, addr := os.Getenv("GHATD_TEST_MONGO_URI"), os.Getenv("GHATD_TEST_REDIS_ADDR")
	if uri == "" || addr == "" {
		t.Skip("set GHATD_TEST_MONGO_URI and GHATD_TEST_REDIS_ADDR for isolated signed-session integration")
	}
	ctx := context.Background()
	namespace := "admin_access_test_" + strings.ReplaceAll(toolbox.GenerateUuidV4(), "-", "")
	h, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, namespace))
	require.NoError(t, err)
	core := repository.NewMongoDbRepositoryWithDefaults(h, namespace)
	db, err := core.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Drop(ctx)); require.NoError(t, h.Close(ctx)) })
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	users := user.NewService(user.NewRepository(core), nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	runtime, err := ephemeral.NewRedisRuntime(ctx, &ephemeral.NewRedisRuntimeRequest{Options: &redis.Options{Addr: addr}, Component: namespace, Environment: "test"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close(ctx)) })
	created, err := users.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: namespace}, Email: "operator@example.test"})
	require.NoError(t, err)
	_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": created.User.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}, "verification.email_verified": true}})
	require.NoError(t, err)
	signing := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "synthetic-access-test-only", RefreshTokenSecret: "synthetic-refresh-test-only"})
	tokens, err := signing.CreateTokenWithAuthenticationTime(ctx, created.User, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.NoError(t, runtime.Store.CreateAuth(ctx, created.User.ID, tokens))
	t.Cleanup(func() { require.NoError(t, runtime.Store.DeleteAllTokenExceptedSpecified(ctx, created.User.ID, nil)) })
	authority := accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: users, AuthService: signing, EphemeralStore: runtime.Store})
	authorize, err := authority.PolicyManagementAuthorizer("examplehost")
	require.NoError(t, err)
	policy, err := accesspolicy.NewMongoStore(ctx, core)
	require.NoError(t, err)
	require.NoError(t, policy.Initialize(ctx))
	tokenRepo := apitoken.NewRepository(core)
	require.NoError(t, tokenRepo.InitializeTokenInventory(ctx))
	manager, err := accesspolicymanager.NewService(accesspolicymanager.Config{System: "examplehost", Store: policy, Authorize: authorize, Users: user.NewRepository(core), Inventory: tokenRepo})
	require.NoError(t, err)
	suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{Service: authority, EphemeralStore: runtime.Store, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
	require.NoError(t, err)
	store, err := NewRedisStore(runtime.Client, namespace)
	require.NoError(t, err)
	t.Cleanup(func() {
		keys, err := runtime.Client.Keys(store.prefix + "*").Result()
		require.NoError(t, err)
		if len(keys) > 0 {
			require.NoError(t, runtime.Client.Del(keys...).Err())
		}
	})
	mail := &capturedMail{}
	b, err := New(Config{Origin: "https://app.example.test", System: "examplehost", Environment: "local", CookieName: "access", Window: 5 * time.Minute, Store: store, Manager: manager, Authorize: authorize, BearerSession: suite.BearerSession, Email: mail})
	require.NoError(t, err)
	routes := router.NewRouter(nil, nil)
	policyService, err := accesspolicy.NewService(policy, nil)
	require.NoError(t, err)
	guard, err := middleware.NewRoutePolicyGuard("examplehost", policyService, nil)
	require.NoError(t, err)
	require.NoError(t, guard.Install(routes))
	require.NoError(t, b.Attach(routes))
	return &bridgeFixture{ctx: ctx, db: db, redis: runtime.Client, auth: signing, runtime: runtime, actor: created.User, tokens: tokens, bridge: b, routes: routes.GetRouter(), mail: mail}
}

func (f *bridgeFixture) request(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, BasePath+path, strings.NewReader(body))
	r.Header.Set("Origin", "https://app.example.test")
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Sec-Fetch-Dest", "empty")
	r.Header.Set("X-Admin-Access", "1")
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
	if f.page != "" {
		r.Header.Set(ContextHeader, f.page)
	}
	if f.reviewID != "" {
		r.Header.Set(ReviewHeader, f.reviewID)
	}
	return r
}
func (f *bridgeFixture) call(r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.routes.ServeHTTP(w, r)
	return w
}
func (f *bridgeFixture) start(t *testing.T) {
	t.Helper()
	w := f.call(f.request("POST", "/session", ""))
	require.Equal(t, 200, w.Code, w.Body.String())
	var body struct {
		Data struct {
			Context string `json:"context"`
		}
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	f.page = body.Data.Context
	require.True(t, opaqueID.MatchString(f.page))
}

const limitsJSON = `{"permanent":2,"ephemeral":1,"minimum_ttl":60,"maximum_ttl":3600,"ttl_increment":60}`

func (f *bridgeFixture) preview(t *testing.T) {
	t.Helper()
	w := f.call(f.request("POST", "/users/"+f.actor.ID+"/token-limits/preview", limitsJSON))
	require.Equal(t, 200, w.Code, w.Body.String())
	f.reviewID = w.Header().Get(ReviewHeader)
	require.True(t, opaqueID.MatchString(f.reviewID))
	require.Equal(t, `"0"`, w.Header().Get("ETag"))
	require.NotContains(t, w.Body.String(), f.tokens.AccessToken)
}
func (f *bridgeFixture) approve(t *testing.T) {
	t.Helper()
	w := f.call(f.request("POST", "/challenge", ""))
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), f.mail.code)
	w = f.call(f.request("POST", "/confirm", `{"code":"`+f.mail.code+`"}`))
	require.Equal(t, 200, w.Code, w.Body.String())
}
func (f *bridgeFixture) apply() *httptest.ResponseRecorder {
	r := f.request("PUT", "/users/"+f.actor.ID+"/token-limits", limitsJSON)
	r.Header.Set("If-Match", `"0"`)
	return f.call(r)
}

func TestBrowserBoundaryWithRealSignedSession(t *testing.T) {
	f := newBridgeFixture(t)
	for _, tc := range []struct {
		name, header, value, action string
		status                      int
	}{
		{"missing origin", "Origin", "", "delete", 403}, {"null origin", "Origin", "null", "set", 403}, {"cross origin", "Origin", "https://evil.example", "set", 403},
		{"duplicate origin", "Origin", "https://app.example.test", "add", 403}, {"same site is not same origin", "Sec-Fetch-Site", "same-site", "set", 403}, {"missing metadata", "Sec-Fetch-Site", "", "delete", 403},
		{"navigation", "Sec-Fetch-Dest", "document", "set", 403}, {"simple form", "X-Admin-Access", "", "delete", 403}, {"duplicate gate", "X-Admin-Access", "1", "add", 403},
		{"explicit bearer rejected", "Authorization", "Bearer ignored", "set", 403}, {"API header rejected", "X-Api-Token", "ignored", "set", 403},
		{"missing cookie", "Cookie", "", "delete", 403}, {"duplicate cookie", "Cookie", "access=other", "add", 403}, {"duplicate auth name", "Cookie", "access=other; access=again", "set", 401},
		{"forged cookie", "Cookie", "access=forged", "set", 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := f.request("POST", "/session", "")
			switch tc.action {
			case "delete":
				r.Header.Del(tc.header)
			case "add":
				r.Header.Add(tc.header, tc.value)
			default:
				r.Header.Set(tc.header, tc.value)
			}
			w := f.call(r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, w.Header().Values("Set-Cookie"))
		})
	}
	f.start(t)
	require.NotEmpty(t, f.page)
}

func TestReviewedCommandBoundaries(t *testing.T) {
	for _, scenario := range []string{"happy path and replay", "not verified", "changed proposal", "changed target", "changed revision", "duplicate context", "missing context", "another session", "revoked session", "demoted", "unverified email", "email revision changed", "expired", "cancelled", "code attempts", "delivery failure", "concurrent apply"} {
		t.Run(scenario, func(t *testing.T) {
			f := newBridgeFixture(t)
			f.start(t)
			f.preview(t)
			if scenario == "delivery failure" {
				f.mail.fail = true
				w := f.call(f.request("POST", "/challenge", ""))
				require.Equal(t, 503, w.Code)
				require.NotContains(t, w.Body.String(), "private delivery")
				require.Equal(t, 428, f.apply().Code)
				return
			}
			if scenario == "code attempts" {
				w := f.call(f.request("POST", "/challenge", ""))
				require.Equal(t, 200, w.Code)
				for range 5 {
					w = f.call(f.request("POST", "/confirm", `{"code":"000000000000"}`))
					require.Equal(t, 422, w.Code)
				}
				w = f.call(f.request("POST", "/confirm", `{"code":"`+f.mail.code+`"}`))
				require.Equal(t, 428, w.Code)
				return
			}
			if scenario != "not verified" {
				f.approve(t)
			}
			r := f.request("PUT", "/users/"+f.actor.ID+"/token-limits", limitsJSON)
			r.Header.Set("If-Match", `"0"`)
			want := 428
			switch scenario {
			case "happy path and replay":
				want = 200
			case "changed proposal":
				r = f.request("PUT", "/users/"+f.actor.ID+"/token-limits", strings.Replace(limitsJSON, `"permanent":2`, `"permanent":3`, 1))
				r.Header.Set("If-Match", `"0"`)
			case "changed target":
				r.URL.Path = BasePath + "/users/another/token-limits"
			case "changed revision":
				r.Header.Set("If-Match", `"1"`)
			case "duplicate context":
				r.Header.Add(ContextHeader, f.page)
			case "missing context":
				r.Header.Del(ContextHeader)
			case "another session":
				token, err := f.auth.CreateTokenWithAuthenticationTime(f.ctx, f.actor, time.Now())
				require.NoError(t, err)
				require.NoError(t, f.runtime.Store.CreateAuth(f.ctx, f.actor.ID, token))
				r.Header.Set("Cookie", "access="+token.AccessToken)
			case "demoted":
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.actor.ID}, bson.M{"$set": bson.M{"roles": bson.A{}}})
				require.NoError(t, err)
				want = 403
			case "unverified email":
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.actor.ID}, bson.M{"$set": bson.M{"verification.email_verified": false}})
				require.NoError(t, err)
				want = 403
			case "revoked session":
				require.NoError(t, f.runtime.Store.DeleteAllTokenExceptedSpecified(f.ctx, f.actor.ID, nil))
				want = 401
			case "email revision changed":
				_, err := f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.actor.ID}, bson.M{"$inc": bson.M{"email_revision": 1}})
				require.NoError(t, err)
				want = 401
			case "expired":
				require.NoError(t, f.redis.HSet(f.bridge.config.Store.prefix+f.page, "expires", 1).Err())
			case "cancelled":
				require.Equal(t, 200, f.call(f.request("POST", "/cancel", "")).Code)
			case "concurrent apply":
				var wg sync.WaitGroup
				results := make(chan int, 8)
				for range 8 {
					wg.Go(func() { results <- f.apply().Code })
				}
				wg.Wait()
				close(results)
				success := 0
				for status := range results {
					if status == 200 {
						success++
					} else {
						require.Equal(t, 428, status)
					}
				}
				require.Equal(t, 1, success)
				return
			}
			w := f.call(r)
			require.Equal(t, want, w.Code, w.Body.String())
			require.Empty(t, w.Header().Values("Set-Cookie"))
			require.NotContains(t, w.Body.String(), f.tokens.AccessToken)
			if want == 200 {
				require.Equal(t, `"1"`, w.Header().Get("ETag"))
				require.Equal(t, 428, f.apply().Code)
			}
		})
	}
}
