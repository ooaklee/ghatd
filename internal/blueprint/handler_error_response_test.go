package blueprint

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestBlueprintHandlerErrorBoundary verifies the reference handler's response
// and handler-owned logs use only mapped identities or an opaque fallback.
func TestBlueprintHandlerErrorBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"direct", ErrBlueprintDatabaseError, 503, "HOST-DATABASE"},
		{"wrapped", fmt.Errorf("private diagnostic: %w", ErrBlueprintDatabaseError), 503, "HOST-DATABASE"},
		{"joined validation", errors.Join(ErrBlueprintNameIsRequired, ErrBlueprintKindIsRequired), 400, "BLP0-002"},
		{"unknown", errors.New("private diagnostic"), 500, ""},
		{"mixed", errors.Join(ErrBlueprintNameIsRequired, errors.New("private diagnostic")), 500, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints", nil)
			req = req.WithContext(logger.TransitWith(req.Context(), zap.New(core)))
			h := NewHandler(&mockBlueprintHTTPService{getFunc: func(context.Context, *GetBlueprintsRequest) (*GetBlueprintsResponse, error) { return nil, tc.err }}, nil,
				reply.ErrorManifest{ErrBlueprintDatabaseError: {StatusCode: 503, Code: "HOST-DATABASE"}})
			rec := httptest.NewRecorder()
			h.GetBlueprints(rec, req)
			require.Equal(t, tc.status, rec.Code)
			if tc.code != "" {
				require.Contains(t, rec.Body.String(), tc.code)
			}
			require.NotContains(t, rec.Body.String(), "private diagnostic")
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.ContextMap()), "private diagnostic")
			}
		})
	}
}
