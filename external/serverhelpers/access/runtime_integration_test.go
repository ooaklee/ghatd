package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/router"
	starter "github.com/ooaklee/ghatd/external/starter/v0"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// policySessionFixture substitutes signature/session storage only; the host
// exercises the real bearer adapter, current-user checks, manager and database.
type policySessionFixture struct {
	accessmanager.AuthService
	revoked bool
}

func (f *policySessionFixture) ExtractTokenMetadata(_ context.Context, r *http.Request) (*auth.TokenAccessDetails, error) {
	if r.Header.Get("Authorization") != "Bearer host-test-session" {
		return nil, auth.ErrUnauthorized
	}
	return &auth.TokenAccessDetails{UserID: "operator", AccessUUID: "live", TokenUse: auth.TokenUseAccess}, nil
}

type policySessionStore struct {
	accessmanager.EphemeralStore
	fixture *policySessionFixture
}

func (s *policySessionStore) FetchAuth(context.Context, ephemeral.TokenDetailsAccess) (string, error) {
	if s.fixture.revoked {
		return "", ephemeral.ErrAuthNotFound
	}
	return "operator", nil
}

// policyIssuanceContext models the selected owner's separately verified session
// for issuance after administrative provisioning. It is not the operator's
// session and is not a cryptographic authentication test.
func policyIssuanceContext(ctx context.Context) context.Context {
	ctx = accesshelpers.TransitWith(ctx, "selected")
	ctx = accesshelpers.TransitAuthenticatedWith(ctx, true)
	return accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "selected", AccessUUID: "test-session", TokenUse: auth.TokenUseAccess, IsAuthorized: true})
}

// policyHostCore limits cleanup to a generated case-owned database and retains
// client lifecycle ownership, just like the application's managed Mongo runtime.
func policyHostCore(t *testing.T) (context.Context, *repository.MongoDbRepository) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	name := "ghatd_composition_policy_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	manager, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	core := repository.NewMongoDbRepositoryWithDefaults(manager, name)
	db, err := core.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, manager.Close(cleanup))
	})
	return ctx, core
}

func TestPolicyCompositionProvisioning(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		status        int
	}{
		{"explicit bearer provisions selected user", "", 200},
		{"cookie alone cannot provision", "cookie", 401},
		{"API credential cannot provision", "api", 401},
		{"revoked session", "revoked", 401},
		{"demoted administrator", "demoted", 403},
		{"disabled administrator", "disabled", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, core := policyHostCore(t)
			routes := router.NewRouter(nil, nil)
			runtime, err := NewRuntime(ctx, RuntimeConfig{System: "fixture", Core: core, Routes: routes})
			require.NoError(t, err)
			repos, err := starter.NewRepositories(&starter.NewRepositoriesRequest{Core: core})
			require.NoError(t, err)
			require.NoError(t, repos.APIToken.InitializeTokenInventory(ctx))
			operator := &userv2.UniversalUser{ID: "operator", NanoID: "operator", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}
			_, err = repos.User.CreateUser(ctx, operator)
			require.NoError(t, err)
			_, err = repos.User.CreateUser(ctx, &userv2.UniversalUser{ID: "selected", NanoID: "selected", Status: userv2.AccountStatusKeyActive})
			require.NoError(t, err)
			fixture := &policySessionFixture{}
			manager := accessmanager.NewService(&accessmanager.NewServiceRequest{AuthService: fixture, EphemeralStore: &policySessionStore{fixture: fixture}, UserService: &userv2.Service{UserRepository: repos.User}, ApiTokenService: apitoken.NewService(repos.APIToken), TokenPolicy: accesspolicy.TokenPolicy{Service: runtime.Service(), System: "fixture"}})
			mw := amiddleware.NewMiddleware(&amiddleware.NewMiddlewareRequest{Service: manager})
			stack := &starter.Stack{Repositories: repos, Services: &starter.Services{AccessManager: manager}, Middleware: &starter.Middleware{AccessManager: &amiddleware.Suite{BearerSession: mw.BearerSessionRequired}}}
			require.NoError(t, runtime.AttachManagement(routes, ManagementConfig{Sessions: stack.Services.AccessManager, Users: stack.Repositories.User, Inventory: stack.Repositories.APIToken, BearerSession: stack.Middleware.AccessManager.BearerSession, Capabilities: true}))
			require.NoError(t, routes.ValidateRoutePolicies())
			require.Len(t, routes.RouteInventory(), 4)
			_, err = manager.CreateUserAPIToken(policyIssuanceContext(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
			// Missing grants retain the policy domain's denial identity; the
			// HTTP error map, rather than the manager, selects its public status.
			require.ErrorIs(t, err, accesspolicy.ErrDenied)
			switch tc.variant {
			case "revoked":
				fixture.revoked = true
			case "demoted":
				operator.Roles = nil
			case "disabled":
				operator.Status = userv2.AccountStatusKeySuspended
			}
			if tc.variant == "demoted" || tc.variant == "disabled" {
				_, err = repos.User.UpdateUser(ctx, operator)
				require.NoError(t, err)
			}
			r := httptest.NewRequest("PUT", accesspolicymanager.BasePath+"/users/selected/token-limits", strings.NewReader(`{"permanent":1,"ephemeral":0,"minimum_ttl":0,"maximum_ttl":0,"ttl_increment":0}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("If-Match", `"0"`)
			if tc.variant == "cookie" {
				r.AddCookie(&http.Cookie{Name: "__aauth", Value: "host-test-session"})
			} else {
				r.Header.Set("Authorization", "Bearer host-test-session")
			}
			if tc.variant == "api" {
				r.Header.Set("X-Api-Token", "unselected-api")
			}
			w := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Empty(t, w.Header().Values("Set-Cookie"))
			db, err := core.GetDatabase(ctx, "")
			require.NoError(t, err)
			count, err := db.Collection(apitoken.InventoryFenceCollection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			if tc.status == 200 {
				require.Equal(t, int64(1), count)
				// A grant does not turn an unverified owner ID into session authority.
				_, err = manager.CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
				require.ErrorIs(t, err, accessmanager.ErrUnauthorizedUnableToAttainRequestorID)
				got, err := manager.CreateUserAPIToken(policyIssuanceContext(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
				require.NoError(t, err)
				require.NotEmpty(t, got.UserAPIToken.Value)
				_, err = manager.CreateUserAPIToken(policyIssuanceContext(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
				require.ErrorIs(t, err, accessmanager.ErrPermanentAPITokenLimitReached)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestPolicyCompositionStartupBoundaries(t *testing.T) {
	for _, variant := range []string{"nil context", "nil core", "nil router", "guard frozen"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			core := &repository.MongoDbRepository{}
			routes := router.NewRouter(nil, nil)
			switch variant {
			case "nil context":
				ctx = nil
			case "nil core":
				core = nil
			case "nil router":
				routes = nil
			case "guard frozen":
				ctx, core = policyHostCore(t)
				routes.NewRouteGroup("/already-attached", router.Public, nil)
			}
			got, err := NewRuntime(ctx, RuntimeConfig{System: "fixture", Core: core, Routes: routes})
			require.Error(t, err)
			require.Nil(t, got)
		})
	}
}
