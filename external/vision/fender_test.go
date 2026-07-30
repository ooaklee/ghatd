package vision

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type visionTestValidator struct {
	err error
}

func (v visionTestValidator) Validate(s interface{}) error {
	return v.err
}

func TestMapRequestToCreateVisionRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		userID     string
		validator  visionValidator
		wantErr    error
		wantName   string
		wantKind   string
		wantUserID string
	}{
		{
			name:       "SUCCESS - maps body and context user",
			body:       `{"name":"Starter API","kind":"Service","description":"demo"}`,
			userID:     "user-1",
			wantName:   "Starter API",
			wantKind:   "Service",
			wantUserID: "user-1",
		},
		{
			name:    "FAILURE - invalid body",
			body:    `{`,
			wantErr: ErrVisionInvalidPayload,
		},
		{
			name:      "FAILURE - validation failure",
			body:      `{"name":"Starter API","kind":"Service"}`,
			validator: visionTestValidator{err: errors.New("invalid")},
			wantErr:   ErrVisionInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/visions", bytes.NewBufferString(tt.body))
			if tt.userID != "" {
				req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), tt.userID))
			}

			got, err := MapRequestToCreateVisionRequest(req, tt.validator)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MapRequestToCreateVisionRequest() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.Name != tt.wantName || got.Kind != tt.wantKind || got.CreatedByUserID != tt.wantUserID {
				t.Fatalf("mapped request = %+v, want name=%s kind=%s user=%s", got, tt.wantName, tt.wantKind, tt.wantUserID)
			}
		})
	}
}

func TestMapRequestToGetVisionsRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/visions?query=starter&kind=Service&status=Active&page=2&page_size=5", nil)

	got, err := MapRequestToGetVisionsRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToGetVisionsRequest() error = %v", err)
	}
	if got.Query != "starter" || got.Kind != "Service" || got.Status != "Active" || got.Page != 2 || got.PageSize != 5 {
		t.Fatalf("mapped request = %+v, want decoded query params", got)
	}
}

func TestMapRequestToGetVisionByIDRequest(t *testing.T) {
	tests := []struct {
		name     string
		visionID string
		userID   string
		wantErr  error
	}{
		{name: "SUCCESS - maps path and context user", visionID: "bp-1", userID: "user-1"},
		{name: "FAILURE - missing path ID", userID: "user-1", wantErr: ErrVisionIDIsRequired},
		{name: "FAILURE - missing context user", visionID: "bp-1", wantErr: ErrVisionUserIDIsRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/visions/bp-1", nil)
			if tt.visionID != "" {
				req = mux.SetURLVars(req, map[string]string{VisionURIVariableID: tt.visionID})
			}
			if tt.userID != "" {
				req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), tt.userID))
			}

			got, err := MapRequestToGetVisionByIDRequest(req, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MapRequestToGetVisionByIDRequest() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.ID != tt.visionID || got.UserID != tt.userID {
				t.Fatalf("mapped request = %+v, want id=%s user=%s", got, tt.visionID, tt.userID)
			}
		})
	}
}
