package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// These fixtures validate composition and denial before domain calls; successful
// provisioning and live-session changes use the isolated Mongo suite.
func managementFixture(t *testing.T) (*Runtime, ManagementConfig) {
	t.Helper()
	store := &accesspolicy.MongoStore{}
	policy, err := accesspolicy.NewService(store, nil)
	require.NoError(t, err)
	sessions := accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: &userv2.Service{}, EphemeralStore: &policySessionStore{fixture: &policySessionFixture{}}})
	return &Runtime{system: "fixture", store: store, service: policy}, ManagementConfig{Sessions: sessions, Users: &userv2.Repository{}, Inventory: &apitoken.Repository{}, BearerSession: func(next http.Handler) http.Handler { return next }}
}

func TestManagementValidatesBeforeAttachingRoutes(t *testing.T) {
	for _, name := range []string{"nil runtime", "nil routes", "nil sessions", "nil users", "typed nil users", "nil inventory", "typed nil inventory", "nil middleware", "nil redis", "short window", "long window", "production delivery disabled", "invalid origin", "missing sender", "typed nil sender"} {
		t.Run(name, func(t *testing.T) {
			runtime, cfg := managementFixture(t)
			routes := router.NewRouter(nil, nil)
			original := routes
			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
			t.Cleanup(func() { _ = client.Close() })
			cfg.AdminAccess = &AdminAccessConfig{Origin: "http://localhost:4000", Environment: "local", CookieName: "__auth", RedisNamespace: "fixture:local", Window: time.Minute, Redis: client, Email: &emailmanager.EmailManager{}}
			switch name {
			case "nil runtime":
				runtime = nil
			case "nil routes":
				routes = nil
			case "nil sessions":
				cfg.Sessions = nil
			case "nil users":
				cfg.Users = nil
			case "typed nil users":
				cfg.Users = (*userv2.Repository)(nil)
			case "typed nil inventory":
				cfg.Inventory = (*apitoken.Repository)(nil)
			case "typed nil sender":
				cfg.AdminAccess.Email = (*emailmanager.EmailManager)(nil)
			case "nil inventory":
				cfg.Inventory = nil
			case "nil middleware":
				cfg.BearerSession = nil
			case "nil redis":
				cfg.AdminAccess.Redis = nil
			case "short window":
				cfg.AdminAccess.Window = time.Minute - time.Nanosecond
			case "long window":
				cfg.AdminAccess.Window = 5*time.Minute + time.Nanosecond
			case "production delivery disabled":
				cfg.AdminAccess.Environment = "production"
			case "invalid origin":
				cfg.AdminAccess.Origin = "https://user:secret-canary@example.test"
			case "missing sender":
				cfg.AdminAccess.Email = nil
			}
			err := runtime.AttachManagement(routes, cfg)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "secret-canary")
			require.Empty(t, original.RouteInventory())
		})
	}
}

func TestManagementCompositionKeepsFeatureChoicesAndRejectsForgedBearerAdapter(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		capabilities, bridge bool
		count                int
	}{{"token only", false, false, 2}, {"capabilities", true, false, 4}, {"browser bridge", false, true, 8}, {"all", true, true, 10}} {
		t.Run(tc.name, func(t *testing.T) {
			runtime, cfg := managementFixture(t)
			cfg.Capabilities = tc.capabilities
			routes := router.NewRouter(nil, nil)
			guard, err := accessmiddleware.NewRoutePolicyGuard("fixture", runtime.Service(), nil)
			require.NoError(t, err)
			require.NoError(t, guard.Install(routes))
			if tc.bridge {
				client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
				t.Cleanup(func() { _ = client.Close() })
				cfg.AdminAccess = &AdminAccessConfig{Origin: "http://localhost:4000", Environment: "local", CookieName: "__auth", RedisNamespace: "fixture:local", Window: time.Minute, Redis: client, Email: &emailmanager.EmailManager{}}
			}
			require.NoError(t, runtime.AttachManagement(routes, cfg))
			require.Len(t, routes.RouteInventory(), tc.count)
			// A host-supplied pass-through middleware is never a bearer identity.
			for _, header := range []string{"", "Bearer forged-session"} {
				r := httptest.NewRequest(http.MethodPut, accesspolicymanager.BasePath+"/users/selected/token-limits", nil)
				r.Header.Set("Authorization", header)
				r.AddCookie(&http.Cookie{Name: "__auth", Value: "forged-session"})
				w := httptest.NewRecorder()
				routes.GetRouter().ServeHTTP(w, r)
				require.Equal(t, http.StatusUnauthorized, w.Code)
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestCanceledRuntimeDoesNotTouchDependencies(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout bool
	}{{"cancelled", false}, {"expired", true}} {
		t.Run(tc.name, func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if tc.timeout {
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			cancel()
			runtime, err := NewRuntime(ctx, RuntimeConfig{Core: &repository.MongoDbRepository{}, Routes: router.NewRouter(nil, nil)})
			require.ErrorIs(t, err, ctx.Err())
			require.Nil(t, runtime)
			require.Nil(t, runtime.Service())
		})
	}
}
