package router_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

// routeFailure tests structural classification without permitting diagnostics
// to be formatted. A nil receiver must never have Is or Unwrap invoked.
type routeFailure struct {
	// cause is the single wrapped error, including malformed cycles or typed nils.
	cause error
	// match identifies an explicit adapter classification, when present.
	match error
	// ambiguous claims every manifest identity to exercise strict rejection.
	ambiguous bool
}

// Error fails the test if the response path formats private diagnostics.
func (*routeFailure) Error() string { panic("private diagnostic must not be formatted") }

// Unwrap exposes only the fixture's single cause; typed nil receivers are unsafe.
func (e *routeFailure) Unwrap() error { return e.cause }

// Is models an adapter's explicit classification without searching its cause.
func (e *routeFailure) Is(target error) bool { return e.ambiguous || target == e.match }

// routeUncomparableFailure is an unknown error that cannot be a Go map key.
type routeUncomparableFailure []string

// Error makes accidental fallback text formatting observable as a test failure.
func (routeUncomparableFailure) Error() string { panic("private diagnostic must not be formatted") }

// routeContextKey keeps the test's private request context distinct from public headers.
type routeContextKey struct{}

// TestPolicyErrorStructuralBoundaries exercises the actual registered route,
// proving invalid failure shapes cannot panic, publish diagnostics or dispatch.
func TestPolicyErrorStructuralBoundaries(t *testing.T) {
	t.Parallel()
	var typedNil *routeFailure
	cycle := &routeFailure{}
	cycle.cause = cycle
	chain := func(nodes int) error {
		var err error = router.ErrRouteDenied
		for i := 1; i < nodes; i++ {
			err = &routeFailure{cause: err}
		}
		return err
	}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"direct denial", router.ErrRouteDenied, 403, "ROUTE_DENIED"},
		{"opaque wrapper", &routeFailure{cause: router.ErrRouteDenied}, 403, "ROUTE_DENIED"},
		{"explicit adapter classification", &routeFailure{match: router.ErrRouteLimitReached}, 429, "ROUTE_LIMIT_REACHED"},
		{"ambiguous adapter", &routeFailure{ambiguous: true}, 503, "ROUTE_UNAVAILABLE"},
		{"conflicting chain classifications", &routeFailure{match: router.ErrRouteDenied, cause: router.ErrRouteLimitReached}, 503, "ROUTE_UNAVAILABLE"},
		{"typed nil", typedNil, 503, "ROUTE_UNAVAILABLE"},
		{"wrapped typed nil", &routeFailure{cause: typedNil}, 503, "ROUTE_UNAVAILABLE"},
		{"nil slice error", routeUncomparableFailure(nil), 503, "ROUTE_UNAVAILABLE"},
		{"uncomparable error", routeUncomparableFailure{"private"}, 503, "ROUTE_UNAVAILABLE"},
		{"cycle", cycle, 503, "ROUTE_UNAVAILABLE"},
		{"64 node boundary", chain(64), 403, "ROUTE_DENIED"},
		{"65 node boundary", chain(65), 503, "ROUTE_UNAVAILABLE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := router.NewRouter(nil, nil)
			called := false
			require.NoError(t, r.SetRouteAuthorizer(func(ctx context.Context, request *http.Request, _ router.RouteDefinition) error {
				require.Equal(t, "private request context", ctx.Value(routeContextKey{}))
				require.Equal(t, ctx, request.Context())
				return tc.err
			}))
			r.NewRouteGroup("/api", router.Session, func(next http.Handler) http.Handler { return next }).Handle(
				router.RouteDefinition{Path: "/items", Operation: "items.read", Methods: []string{http.MethodGet}},
				func(w http.ResponseWriter, _ *http.Request) { called = true; w.WriteHeader(http.StatusNoContent) },
			)
			require.NoError(t, r.ValidateRoutePolicies())
			request := httptest.NewRequest(http.MethodGet, "/api/items", nil)
			request = request.WithContext(context.WithValue(request.Context(), routeContextKey{}, "private request context"))
			recorder := httptest.NewRecorder()
			require.NotPanics(t, func() { r.GetRouter().ServeHTTP(recorder, request) })
			require.False(t, called, "no failed authorizer may dispatch the domain handler")
			require.Equal(t, tc.status, recorder.Code)
			var envelope struct {
				Errors []struct {
					Code string `json:"code"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.Len(t, envelope.Errors, 1)
			require.Equal(t, tc.code, envelope.Errors[0].Code)
			require.NotContains(t, recorder.Body.String(), "private")
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.Equal(t, "nosniff", recorder.Header().Get("X-Content-Type-Options"))
		})
	}
}
