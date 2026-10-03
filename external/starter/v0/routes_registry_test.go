package starter

import (
	"net/http"
	"testing"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestAttachDefaultRoutesValidatesExistingRegistry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invalid bool
	}{
		{"valid host descriptor", false},
		{"missing host middleware", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			mode := router.Public
			if tc.invalid {
				mode = router.Session
			}
			r.NewRouteGroup("/host", mode, nil).Handle(router.RouteDefinition{Path: "", Operation: "host.read", Methods: []string{http.MethodGet}}, http.NotFound)
			err := AttachDefaultRoutes(&AttachDefaultRoutesRequest{
				Router: r,
				Stack:  &Stack{Handlers: &Handlers{Policy: validHandlers(t).Policy}},
				Skip:   []RouteGroup{RouteGroupPricer, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager, RouteGroupVision},
			})
			if tc.invalid {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
