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

func TestStarterWiresStatusManager(t *testing.T) {
	for _, bulk := range []bool{false, true} {
		name := "single"
		if bulk {
			name = "bulk"
		}
		t.Run(name, func(t *testing.T) {
			request := validHandlersRequest(t)
			h, err := NewHandlers(request)
			require.NoError(t, err)
			require.Same(t, request.Services.UserManager, h.User.StatusManager)
			// A trusted identity without session evidence reaches the actual
			// authorizer and fails 401, not unwired 503 or domain persistence.
			ctx := helpers.TransitAuthenticatedWith(helpers.TransitWith(context.Background(), "actor"), true)
			body := `{"desired_status":"SUSPENDED"}`
			if bulk {
				body = `{"ids":["target"],"desired_status":"SUSPENDED"}`
			}
			r := httptest.NewRequest(http.MethodPatch, "/", strings.NewReader(body)).WithContext(ctx)
			r = mux.SetURLVars(r, map[string]string{"userID": "target"})
			w := httptest.NewRecorder()
			if bulk {
				h.User.BulkUpdateUsersStatus(w, r)
			} else {
				h.User.UpdateUserStatus(w, r)
			}
			require.Equal(t, 401, w.Code, w.Body.String())
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		})
	}
}
