// Package routecontracts_test verifies actual framework attachment dispatch.
// The readable fixture preserves pre-migration paths, methods and handler order;
// access labels describe the corresponding trusted authentication adapters.
package routecontracts_test

import (
	"context"
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/vision"
	"github.com/ooaklee/ghatd/internal/blueprint"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/domain_routes.json
var domainContractJSON []byte

// routeContract is reviewed expected API behavior, not live registry output.
// Update it deliberately when an endpoint changes; never regenerate during tests.
type routeContract struct {
	Domain     string
	Path       string
	Methods    []string
	Operation  string
	Access     router.AccessMode
	Proof      string
	Middleware string
}

// contracts decodes fresh case-independent expectations. The one discarded
// legacy registration was an unreachable duplicate GET /ums/users; the service
// still performs its group-filtered member versus administrator projection.
func contracts(t *testing.T) []routeContract {
	t.Helper()
	var all []routeContract
	require.NoError(t, json.Unmarshal(domainContractJSON, &all))
	require.Len(t, all, 182)
	return all
}

// domainContracts preserves registration order, including overlapping OPTIONS.
func domainContracts(all []routeContract, domain string) []routeContract {
	var selected []routeContract
	for _, c := range all {
		if c.Domain == domain {
			selected = append(selected, c)
		}
	}
	return selected
}

// attach uses named adapter factories so changing which request field a route
// consumes is visible to the test. These are trusted fixture adapters, not JWT
// verifiers; live credential lifecycle tests reside with Access Manager.
func attach(domain string, r *router.Router, h *routeRecorder, mw func(string) mux.MiddlewareFunc) {
	switch domain {
	case "billingmanager":
		billingmanager.AttachRoutes(&billingmanager.AttachRoutesRequest{Router: r, Handler: h, MiddlewareActiveValidApiTokenOrJWTMiddleware: mw("MiddlewareActiveValidApiTokenOrJWTMiddleware")})
	case "contentmanager":
		contentmanager.AttachRoutes(&contentmanager.AttachRoutesRequest{Router: r, Handler: h, MiddlewareAdminApiTokenOrJwtRequired: mw("MiddlewareAdminApiTokenOrJwtRequired"), RateLimitOrActiveMiddleware: mw("RateLimitOrActiveMiddleware")})
	case "group":
		group.AttachRoutes(&group.AttachRoutesRequest{Router: r, Handler: h, AdminOnlyMiddleware: mw("AdminOnlyMiddleware")})
	case "policy":
		policy.AttachRoutes(&policy.AttachRoutesRequest{Router: r, Handler: h})
	case "pricer":
		pricer.AttachRoutes(&pricer.AttachRoutesRequest{Router: r, Handler: h, AdminOnlyMiddleware: mw("AdminOnlyMiddleware")})
	case "user":
		user.AttachRoutes(&user.AttachRoutesRequest{Router: r, Handler: h, AdminOnlyMiddleware: mw("AdminOnlyMiddleware")})
	case "usermanager":
		usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{Router: r, Handler: h, RateLimitOrActiveMiddleware: mw("RateLimitOrActiveMiddleware"), ActiveValidApiTokenOrJWTMiddleware: mw("ActiveValidApiTokenOrJWTMiddleware"), ValidApiTokenOrJWTMiddleware: mw("ValidApiTokenOrJWTMiddleware"), CustomMeEndpointValidApiTokenOrJWTMiddleware: mw("CustomMeEndpointValidApiTokenOrJWTMiddleware"), AdminOnlyMiddleware: mw("AdminOnlyMiddleware"), AdminApiTokenOrJWTMiddleware: mw("AdminApiTokenOrJWTMiddleware")})
	case "vision":
		vision.AttachRoutes(&vision.AttachRoutesRequest{Router: r, Handler: h, AdminOnlyMiddleware: mw("AdminOnlyMiddleware"), AuthenticatedMiddleware: mw("AuthenticatedMiddleware")})
	case "blueprint":
		blueprint.AttachRoutes(&blueprint.AttachRoutesRequest{Router: r, Handler: h, AdminOnlyMiddleware: mw("AdminOnlyMiddleware"), AuthenticatedMiddleware: mw("AuthenticatedMiddleware")})
	default:
		panic("unknown route fixture domain")
	}
}

// concretePath asks Mux to fill templates, including bounded regex variables.
func concretePath(t *testing.T, c routeContract) string {
	t.Helper()
	r := mux.NewRouter().NewRoute().Path(c.Path)
	names, err := r.GetVarNames()
	require.NoError(t, err)
	var pairs []string
	for _, name := range names {
		value := "00000000-0000-4000-8000-000000000001"
		if name == "slug" {
			value = "sample-plan"
		}
		pairs = append(pairs, name, value)
	}
	u, err := r.URL(pairs...)
	require.NoError(t, err)
	return u.String()
}

// firstMatch models documented Mux first-match semantics from the independent
// fixture, not from live inventory. OPTIONS may reach an earlier declaration.
func firstMatch(t *testing.T, records []routeContract, request *http.Request) routeContract {
	t.Helper()
	for _, c := range records {
		r := mux.NewRouter().NewRoute().Path(c.Path).Methods(c.Methods...)
		if r.Match(request, &mux.RouteMatch{}) {
			return c
		}
	}
	t.Fatalf("fixture has no match for %s %s", request.Method, request.URL.Path)
	return routeContract{}
}

// assertInventory checks complete ordered contracts, not just route counts.
func assertInventory(t *testing.T, r *router.Router, records []routeContract) {
	t.Helper()
	got := r.RouteInventory()
	require.Len(t, got, len(records))
	for i, want := range records {
		require.Equal(t, want.Path, got[i].Path)
		require.Equal(t, want.Methods, got[i].Methods)
		require.Equal(t, want.Operation, got[i].Operation)
		require.Equal(t, want.Access, got[i].Access)
		require.Equal(t, router.RoutePolicy{Proof: want.Proof}, got[i].Policy)
	}
}

func TestDomainRouteDispatchAndPolicyOrder(t *testing.T) {
	all := contracts(t)
	for _, c := range all {
		for _, method := range c.Methods {
			for _, mode := range []string{"allowed", "middleware-denied", "policy-denied", "missing-middleware"} {
				t.Run(c.Operation+"/"+method+"/"+mode, func(t *testing.T) {
					records := domainContracts(all, c.Domain)
					request := httptest.NewRequest(method, concretePath(t, c), nil)
					want := firstMatch(t, records, request)
					var events []string
					r := router.NewRouter(nil, nil)
					require.NoError(t, r.SetRouteAuthorizer(func(_ context.Context, _ *http.Request, d router.RouteDefinition) error {
						events = append(events, "policy")
						require.Equal(t, want.Operation, d.Operation)
						require.Equal(t, want.Path, d.Path)
						if mode == "policy-denied" {
							return router.ErrRouteDenied
						}
						return nil
					}))
					attach(c.Domain, r, &routeRecorder{events: &events}, func(name string) mux.MiddlewareFunc {
						if mode == "missing-middleware" {
							return nil
						}
						return func(next http.Handler) http.Handler {
							return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
								events = append(events, "middleware:"+name)
								if mode == "middleware-denied" {
									w.WriteHeader(http.StatusUnauthorized)
									return
								}
								next.ServeHTTP(w, r)
							})
						}
					})
					// Nil optional adapters select documented stricter fallbacks;
					// other missing adapters still invalidate the entire registry.
					if mode == "missing-middleware" && c.Domain == "usermanager" {
						for i := range records {
							if records[i].Access == router.ProfileOptional {
								records[i].Access = router.SessionOrAPI
							}
							if records[i].Access == router.AdminSessionOrAPI {
								records[i].Access = router.AdminSession
							}
						}
					}
					assertInventory(t, r, records)
					missing := mode == "missing-middleware" && c.Domain != "policy"
					if missing {
						require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
					} else {
						require.NoError(t, r.ValidateRoutePolicies())
					}
					response := httptest.NewRecorder()
					r.GetRouter().ServeHTTP(response, request)
					var expected []string
					status := http.StatusNoContent
					if missing {
						status = http.StatusServiceUnavailable
						require.Contains(t, response.Body.String(), "ROUTE_CONFIGURATION")
					} else {
						if want.Middleware != "" {
							expected = append(expected, "middleware:"+want.Middleware)
						}
						if mode == "middleware-denied" && want.Middleware != "" {
							status = http.StatusUnauthorized
						} else {
							expected = append(expected, "policy")
							if mode == "policy-denied" {
								status = http.StatusForbidden
								require.Contains(t, response.Body.String(), "ROUTE_DENIED")
							} else {
								expected = append(expected, "handler:"+strings.SplitN(want.Operation, ".", 2)[1])
							}
						}
					}
					require.Equal(t, status, response.Code, response.Body.String())
					require.Equal(t, expected, events)
				})
			}
		}
	}
}
