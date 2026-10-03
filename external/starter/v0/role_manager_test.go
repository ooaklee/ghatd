package starter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/stretchr/testify/require"
)

func TestStarterWiresRoleManager(t *testing.T) {
	for _, name := range []string{"add", "remove"} {
		t.Run(name, func(t *testing.T) {
			req := validHandlersRequest(t)
			h, err := NewHandlers(req)
			require.NoError(t, err)
			require.Same(t, req.Services.UserManager, h.User.RoleManager)
			// A trusted actor alone cannot bypass the actual session authorizer.
			ctx := helpers.TransitAuthenticatedWith(helpers.TransitWith(context.Background(), "actor"), true)
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"role":"ADMIN"}`)).WithContext(ctx)
			r = mux.SetURLVars(r, map[string]string{"userID": "target"})
			w := httptest.NewRecorder()
			if name == "add" {
				h.User.AddUserRole(w, r)
			} else {
				h.User.RemoveUserRole(w, r)
			}
			require.Equal(t, 401, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		})
	}
}
