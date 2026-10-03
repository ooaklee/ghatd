package starter

import (
	"context"
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestAttachDefaultRoutesHandleOptIn(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		enabled, guard, skip bool
		count                int
		invalid              bool
	}{
		{"disabled", false, false, false, 0, false},
		{"enabled", true, true, false, 3, false},
		{"missing evaluator", true, false, false, 3, true},
		{"skipped manager", true, false, true, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			if tc.guard {
				require.NoError(t, r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return nil }))
			}
			handlers, err := NewHandlers(validHandlersRequest(t))
			require.NoError(t, err)
			middleware, err := NewMiddleware(validMiddlewareRequest(t))
			require.NoError(t, err)
			req := &AttachDefaultRoutesRequest{Router: r, Stack: &Stack{Handlers: handlers, Middleware: middleware}, EnableUserHandles: tc.enabled}
			if tc.skip {
				req.Skip = []RouteGroup{RouteGroupUserManager}
			}
			err = AttachDefaultRoutes(req)
			if tc.invalid {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
			} else {
				require.NoError(t, err)
			}
			count := 0
			for _, def := range r.RouteInventory() {
				if def.Operation == "usermanager.GetMyHandle" || def.Operation == "usermanager.ValidateMyHandle" || def.Operation == "usermanager.UpdateMyHandle" {
					count++
					require.Equal(t, router.ActiveSession, def.Access)
					require.Equal(t, def.Operation == "usermanager.UpdateMyHandle", def.Policy.RevisionRequired)
				}
			}
			require.Equal(t, tc.count, count)
		})
	}
}
