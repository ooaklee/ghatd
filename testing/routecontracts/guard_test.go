package routecontracts_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// unexpectedPolicyStore rejects invented grant or budget requirements. Migrated
// routes retain their middleware boundary without implicitly granting new scopes.
type unexpectedPolicyStore struct{ t *testing.T }

func (s unexpectedPolicyStore) Authorize(context.Context, accesspolicy.Subject, []string, []string) error {
	s.t.Error("middleware-only route must not look up grants")
	return accesspolicy.ErrDenied
}

func (s unexpectedPolicyStore) ConsumeAuthorized(context.Context, accesspolicy.Consumption) (accesspolicy.Usage, error) {
	s.t.Error("middleware-only route must not consume a budget")
	return accesspolicy.Usage{}, accesspolicy.ErrDenied
}

// fixtureIdentity uses the production context publisher after a simulated
// credential verifier. It tests guard/route composition, not JWT cryptography.
func fixtureIdentity(t *testing.T, name string) context.Context {
	t.Helper()
	if name == "anonymous" {
		return context.Background()
	}
	u := &user.UniversalUser{ID: "member", Type: "person", Status: user.AccountStatusKeyActive}
	if strings.HasPrefix(name, "admin") {
		u.Roles = []string{user.UserRoleAdmin}
	}
	if name == "inactive-session" {
		u.Status = "DISABLED"
	}
	result := &accessmanager.MiddlewareAuthedUserResponse{UserID: u.ID, User: u, Authenticated: true}
	if strings.HasSuffix(name, "api") {
		result.APIToken = &apitoken.CredentialDetails{UserID: u.ID, TokenID: "credential"}
	} else {
		result.Token = &auth.TokenAccessDetails{UserID: u.ID, AccessUUID: "session", UserType: u.Type}
	}
	ctx, err := middleware.ContextWithAuthentication(context.Background(), result)
	require.NoError(t, err)
	return ctx
}

func TestAttachedRoutesWithPolicyGuard(t *testing.T) {
	// Status columns are anonymous, member session, admin session, member API,
	// admin API and inactive session. Each case exercises a real attachment.
	for _, tc := range []struct {
		operation string
		omit      string
		want      [6]int
	}{
		{"policy.GetPolicies", "", [6]int{204, 204, 204, 204, 204, 204}},
		{"billingmanager.ProcessBillingProviderWebhooks", "", [6]int{204, 204, 204, 204, 204, 204}},
		{"contentmanager.GetArticles", "", [6]int{204, 204, 204, 204, 204, 403}},
		{"usermanager.GetUserProfile", "", [6]int{204, 204, 204, 204, 204, 204}},
		{"usermanager.GetUserProfile", "CustomMeEndpointValidApiTokenOrJWTMiddleware", [6]int{401, 204, 204, 204, 204, 204}},
		{"usermanager.GetUserMicroProfile", "", [6]int{401, 204, 204, 204, 204, 204}},
		{"usermanager.GetGroupsConfig", "", [6]int{401, 204, 204, 204, 204, 403}},
		{"group.GetGroups", "", [6]int{401, 403, 204, 401, 401, 403}},
		{"contentmanager.CreatePost", "", [6]int{401, 403, 204, 403, 204, 403}},
		{"blueprint.GetBlueprints", "", [6]int{401, 204, 204, 401, 401, 204}},
		{"usermanager.NotifyUsers", "", [6]int{401, 403, 204, 403, 204, 403}},
		{"usermanager.NotifyUsers", "AdminApiTokenOrJWTMiddleware", [6]int{401, 403, 204, 401, 401, 403}},
	} {
		for index, identity := range []string{"anonymous", "member-session", "admin-session", "member-api", "admin-api", "inactive-session"} {
			t.Run(tc.operation+"/"+tc.omit+"/"+identity, func(t *testing.T) {
				var selected *routeContract
				for _, c := range contracts(t) {
					if c.Operation == tc.operation {
						copy := c
						selected = &copy
						break
					}
				}
				require.NotNil(t, selected, "choose an actual baseline endpoint")
				var events []string
				r := router.NewRouter(nil, nil)
				guard, err := middleware.NewRoutePolicyGuard("service", unexpectedPolicyStore{t}, nil)
				require.NoError(t, err)
				require.NoError(t, guard.Install(r))
				attach(selected.Domain, r, &routeRecorder{events: &events}, func(name string) mux.MiddlewareFunc {
					if name == tc.omit {
						return nil
					}
					return func(next http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
							next.ServeHTTP(w, req.WithContext(fixtureIdentity(t, identity)))
						})
					}
				})
				require.NoError(t, r.ValidateRoutePolicies())
				response := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(response, httptest.NewRequest(selected.Methods[0], concretePath(t, *selected), nil))
				require.Equal(t, tc.want[index], response.Code, response.Body.String())
				if tc.want[index] == 204 {
					require.Equal(t, []string{"handler:" + strings.SplitN(tc.operation, ".", 2)[1]}, events)
				} else {
					require.Empty(t, events)
				}
			})
		}
	}
}

func TestPricerPathConstraints(t *testing.T) {
	for _, tc := range []struct {
		name, value, handler string
		status               int
	}{
		{"uuid", "00000000-0000-4000-8000-000000000001", "GetPricePlanByID", 204},
		{"slug", "sample-plan", "GetPricePlanBySlug", 204},
		{"non-hex length36 is slug", strings.Repeat("z", 36), "GetPricePlanBySlug", 204},
		{"leading hyphen invalid", "-sample", "", 404},
		{"punctuation invalid", "sample.plan", "", 404},
	} {
		for _, method := range []string{http.MethodGet, http.MethodOptions} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				var events []string
				r := router.NewRouter(nil, nil)
				attach("pricer", r, &routeRecorder{events: &events}, func(string) mux.MiddlewareFunc { return func(h http.Handler) http.Handler { return h } })
				require.NoError(t, r.ValidateRoutePolicies())
				w := httptest.NewRecorder()
				r.GetRouter().ServeHTTP(w, httptest.NewRequest(method, "/api/v1/pricing/plans/"+tc.value, nil))
				status, handler := tc.status, tc.handler
				// Legacy OPTIONS falls through both read regexes to the earlier
				// unconstrained update declaration. This migration preserves it.
				if method == http.MethodOptions && status == 404 {
					status, handler = 204, "UpdatePricePlan"
				}
				require.Equal(t, status, w.Code)
				if handler != "" {
					require.Equal(t, []string{"handler:" + handler}, events)
				} else {
					require.Empty(t, events)
				}
			})
		}
	}
}
