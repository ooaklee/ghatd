package commsconversation

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOwnerWrappingIsIdempotent(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			original := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(204) })
			once := requireOwner(original)
			twice := requireOwner(once)
			require.Same(t, once, twice)
			r := httptest.NewRequest(method, "/", nil).WithContext(ownerContext("owner"))
			r.Header.Set(OwnerHeader, "owner")
			w := httptest.NewRecorder()
			twice.ServeHTTP(w, r)
			require.Equal(t, 204, w.Code)
			require.Equal(t, 1, calls)
		})
	}
}

func TestStableWireErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		code   string
		status int
	}{
		{"HOST_COMMS_OWNER_REQUIRED", 428}, {"HOST_COMMS_OWNER_INVALID", 400},
		{"HOST_COMMS_OWNER_CHANGED", 412}, {"HOST_COMMS_OWNER_SESSION_REQUIRED", 401},
		{"HOST_COMMS_VOTE_INVALID", 400}, {"HOST_COMMS_VOTE_UNAVAILABLE", 503},
	} {
		t.Run(tc.code, func(t *testing.T) {
			matches := 0
			for _, def := range ownerErrors() {
				if def.Code == tc.code {
					matches++
					require.Equal(t, tc.status, def.StatusCode)
				}
			}
			for _, def := range voteErrors() {
				if def.Code == tc.code {
					matches++
					require.Equal(t, tc.status, def.StatusCode)
				}
			}
			require.Equal(t, 1, matches)
		})
	}
}
