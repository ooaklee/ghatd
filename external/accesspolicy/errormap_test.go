package accesspolicy

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

func TestPolicyErrorResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"denied", ErrDenied, 403, "ACP0-001"}, {"configuration", ErrConfiguration, 503, "ACP0-002"},
		{"conflict", ErrConflict, 409, "ACP0-003"}, {"limit", ErrLimitReached, 429, "ACP0-004"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, variant := range []string{"direct", "wrapped", "host override", "unknown sibling"} {
				t.Run(variant, func(t *testing.T) {
					err, status, code := tc.err, tc.status, tc.code
					maps := []reply.ErrorManifest{AccessPolicyErrorMap}
					switch variant {
					case "wrapped":
						err = fmt.Errorf("private diagnostic: %w", err)
					case "host override":
						maps = append(maps, reply.ErrorManifest{tc.err: {StatusCode: 422, Code: "HOST_POLICY", Detail: "Host policy response"}})
						status = 422
						code = "HOST_POLICY"
					case "unknown sibling":
						err = errors.Join(err, errors.New("private diagnostic"))
						status = 500
						code = ""
					}
					response := httptest.NewRecorder()
					require.NoError(t, errormanifest.WriteHTTPError(response, err, maps))
					require.Equal(t, status, response.Code)
					require.NotContains(t, response.Body.String(), "private diagnostic")
					if code != "" {
						require.Contains(t, response.Body.String(), code)
					} else {
						require.NotContains(t, response.Body.String(), tc.code)
					}
				})
			}
		})
	}
}
