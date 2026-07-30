package vision

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestVisionErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "SUCCESS - generic vision error", err: ErrVisionError, wantStatus: http.StatusBadRequest, wantCode: "BLP0-001"},
		{name: "SUCCESS - missing name", err: ErrVisionNameIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-002"},
		{name: "SUCCESS - missing kind", err: ErrVisionKindIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-003"},
		{name: "SUCCESS - missing ID", err: ErrVisionIDIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-004"},
		{name: "SUCCESS - not found", err: ErrVisionResourceNotFound, wantStatus: http.StatusNotFound, wantCode: "BLP0-005"},
		{name: "SUCCESS - missing user ID", err: ErrVisionUserIDIsRequired, wantStatus: http.StatusUnauthorized, wantCode: "BLP0-006"},
		{name: "SUCCESS - invalid payload", err: ErrVisionInvalidPayload, wantStatus: http.StatusBadRequest, wantCode: "BLP0-007"},
		{name: "SUCCESS - invalid query", err: ErrVisionInvalidQueryParam, wantStatus: http.StatusBadRequest, wantCode: "BLP0-008"},
		{name: "SUCCESS - database error", err: ErrVisionDatabaseError, wantStatus: http.StatusInternalServerError, wantCode: "BLP0-009"},
		{name: "SUCCESS - registration key missing", err: ErrVisionRegistrationKeyMissing, wantStatus: http.StatusBadRequest, wantCode: "BLP0-010"},
		{name: "SUCCESS - registration conflict", err: ErrVisionRegistrationConflict, wantStatus: http.StatusConflict, wantCode: "BLP0-011"},
		{name: "SUCCESS - registration not found", err: ErrVisionRegistrationNotFound, wantStatus: http.StatusNotFound, wantCode: "BLP0-012"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler := NewHandler(nil, nil)

			if err := handler.getBaseResponseHandler().NewHTTPErrorResponse(recorder, tt.err); err != nil {
				t.Fatalf("NewHTTPErrorResponse() error = %v", err)
			}

			if recorder.Code != tt.wantStatus {
				t.Fatalf("HTTP status = %d, want %d", recorder.Code, tt.wantStatus)
			}

			var response struct {
				Errors []struct {
					Title  string `json:"title"`
					Detail string `json:"detail"`
					Status string `json:"status"`
					Code   string `json:"code"`
				} `json:"errors"`
			}
			if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
				t.Fatalf("Decode() error = %v", err)
			}
			if len(response.Errors) != 1 {
				t.Fatalf("errors length = %d, want 1", len(response.Errors))
			}

			got := response.Errors[0]
			if got.Status != strconv.Itoa(tt.wantStatus) {
				t.Fatalf("response error status = %q, want %q", got.Status, strconv.Itoa(tt.wantStatus))
			}
			if got.Code != tt.wantCode {
				t.Fatalf("response error code = %s, want %s", got.Code, tt.wantCode)
			}
			if got.Title == "" || got.Detail == "" {
				t.Fatalf("response error should include title and detail: %+v", got)
			}
		})
	}
}
