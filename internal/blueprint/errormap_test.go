package blueprint

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestBlueprintErrorResponses(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{name: "SUCCESS - generic blueprint error", err: ErrBlueprintError, wantStatus: http.StatusBadRequest, wantCode: "BLP0-001"},
		{name: "SUCCESS - missing name", err: ErrBlueprintNameIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-002"},
		{name: "SUCCESS - missing kind", err: ErrBlueprintKindIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-003"},
		{name: "SUCCESS - missing ID", err: ErrBlueprintIDIsRequired, wantStatus: http.StatusBadRequest, wantCode: "BLP0-004"},
		{name: "SUCCESS - not found", err: ErrBlueprintResourceNotFound, wantStatus: http.StatusNotFound, wantCode: "BLP0-005"},
		{name: "SUCCESS - missing user ID", err: ErrBlueprintUserIDIsRequired, wantStatus: http.StatusUnauthorized, wantCode: "BLP0-006"},
		{name: "SUCCESS - invalid payload", err: ErrBlueprintInvalidPayload, wantStatus: http.StatusBadRequest, wantCode: "BLP0-007"},
		{name: "SUCCESS - invalid query", err: ErrBlueprintInvalidQueryParam, wantStatus: http.StatusBadRequest, wantCode: "BLP0-008"},
		{name: "SUCCESS - database error", err: ErrBlueprintDatabaseError, wantStatus: http.StatusInternalServerError, wantCode: "BLP0-009"},
		{name: "SUCCESS - registration key missing", err: ErrBlueprintRegistrationKeyMissing, wantStatus: http.StatusBadRequest, wantCode: "BLP0-010"},
		{name: "SUCCESS - registration conflict", err: ErrBlueprintRegistrationConflict, wantStatus: http.StatusConflict, wantCode: "BLP0-011"},
		{name: "SUCCESS - registration not found", err: ErrBlueprintRegistrationNotFound, wantStatus: http.StatusNotFound, wantCode: "BLP0-012"},
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
