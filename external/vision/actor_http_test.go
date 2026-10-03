package vision

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// serveVisionMutation exercises the real handler-to-service response boundary.
// Fixture identity is published explicitly; authentication cryptography and
// route admission are covered by their dedicated contract suites.
func serveVisionMutation(h *Handler, op string, w http.ResponseWriter, r *http.Request) {
	switch op {
	case "create":
		h.CreateVision(w, r)
	case "update":
		h.UpdateVision(w, r)
	case "status":
		h.UpdateVisionStatus(w, r)
	case "vote":
		h.SetVisionVote(w, r)
	case "remove vote":
		h.RemoveVisionVote(w, r)
	case "comment":
		h.AddVisionComment(w, r)
	case "comment vote":
		h.SetVisionCommentVote(w, r)
	case "remove comment vote":
		h.RemoveVisionCommentVote(w, r)
	case "delete":
		h.DeleteVision(w, r)
	default:
		panic("unknown vision operation")
	}
}

func TestVisionMutationHTTPErrorMapping(t *testing.T) {
	for _, op := range visionMutationNames {
		for _, tc := range []struct {
			name     string
			failure  error
			status   int
			code     string
			override bool
		}{
			{"mapped", ErrVisionInvalidStatusTransition, 409, "VIS0-013", false},
			{"wrapped", fmt.Errorf("private storage detail: %w", ErrVisionInvalidStatusTransition), 409, "VIS0-013", false},
			{"unknown", errors.New("private storage detail"), 500, "", false},
			{"mixed", errors.Join(ErrVisionResourceNotFound, errors.New("private storage detail")), 500, "", false},
			{"unavailable", ErrVisionUnavailable, 503, "VIS0-018", false},
			{"host override", fmt.Errorf("private storage detail: %w", ErrVisionUnavailable), 502, "HOST-VISION", true},
			{"cancelled", context.Canceled, 500, "", false},
			{"deadline", context.DeadlineExceeded, 500, "", false},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := newActorVisionStore()
				port.writeError = tc.failure
				var maps []reply.ErrorManifest
				if tc.override {
					maps = []reply.ErrorManifest{{ErrVisionUnavailable: {Title: "Unavailable", Detail: "Host mapping", StatusCode: 502, Code: "HOST-VISION"}}}
				}
				h := NewHandler(mustVisionService(t, port), validator.NewValidator(), maps...)
				r := httptest.NewRequest("POST", "/", strings.NewReader(strings.ReplaceAll(visionActorPayload, `"parent_comment_id":"comment-0"`, `"parent_comment_id":"comment-1"`))).WithContext(authenticatedActor(context.Background(), "caller"))
				r = mux.SetURLVars(r, map[string]string{VisionURIVariableNanoID: "vision-1", VisionURIVariableCommentID: "comment-1"})
				w := httptest.NewRecorder()
				serveVisionMutation(h, op, w, r)
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.NotContains(t, w.Body.String(), "private storage detail")
				if tc.code != "" {
					require.Contains(t, w.Body.String(), tc.code)
				}
				require.Equal(t, 1, port.writes)
				if op != "delete" {
					require.Equal(t, "caller", port.actor)
				}
			})
		}
	}
}

func TestVisionMutationHTTPRequiresAuthentication(t *testing.T) {
	for _, op := range visionMutationNames {
		t.Run(op, func(t *testing.T) {
			port := newActorVisionStore()
			h := NewHandler(mustVisionService(t, port), validator.NewValidator())
			r := httptest.NewRequest("POST", "/", strings.NewReader(visionActorPayload))
			r = mux.SetURLVars(r, map[string]string{VisionURIVariableNanoID: "vision-1", VisionURIVariableCommentID: "comment-1"})
			w := httptest.NewRecorder()
			serveVisionMutation(h, op, w, r)
			require.Equal(t, 401, w.Code)
			require.Contains(t, w.Body.String(), "VIS0-006")
			require.Zero(t, port.reads)
			require.Zero(t, port.writes)
		})
	}
}
