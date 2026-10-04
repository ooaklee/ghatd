package starter

import (
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestAttachDefaultRoutesCommsVotingOptIn(t *testing.T) {
	for _, tc := range []struct {
		name          string
		enabled, skip bool
		count         int
	}{
		{"disabled", false, false, 0},
		{"enabled", true, false, 5},
		{"skipped manager", true, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			handlers, err := NewHandlers(validHandlersRequest(t))
			require.NoError(t, err)
			middleware, err := NewMiddleware(validMiddlewareRequest(t))
			require.NoError(t, err)
			req := &AttachDefaultRoutesRequest{Router: r, Stack: &Stack{Handlers: handlers, Middleware: middleware}, EnableCommsVoting: tc.enabled}
			if tc.skip {
				req.Skip = []RouteGroup{RouteGroupUserManager}
			}
			require.NoError(t, AttachDefaultRoutes(req))
			count := 0
			for _, def := range r.RouteInventory() {
				if strings.HasPrefix(def.Operation, "commsconversation.") {
					count++
					require.Equal(t, router.AdminSession, def.Access)
				}
			}
			require.Equal(t, tc.count, count)
		})
	}
}
