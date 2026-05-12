package blueprint

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

type blueprintTestValidator struct {
	err error
}

func (v blueprintTestValidator) Validate(s interface{}) error {
	return v.err
}

func TestMapRequestToCreateBlueprintRequest(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		userID     string
		validator  blueprintValidator
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
			wantErr: ErrBlueprintInvalidPayload,
		},
		{
			name:      "FAILURE - validation failure",
			body:      `{"name":"Starter API","kind":"Service"}`,
			validator: blueprintTestValidator{err: errors.New("invalid")},
			wantErr:   ErrBlueprintInvalidPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/blueprints", bytes.NewBufferString(tt.body))
			if tt.userID != "" {
				req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), tt.userID))
			}

			got, err := MapRequestToCreateBlueprintRequest(req, tt.validator)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MapRequestToCreateBlueprintRequest() error = %v, want %v", err, tt.wantErr)
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

func TestMapRequestToGetBlueprintsRequest(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints?query=starter&kind=Service&status=Active&page=2&page_size=5", nil)

	got, err := MapRequestToGetBlueprintsRequest(req, nil)
	if err != nil {
		t.Fatalf("MapRequestToGetBlueprintsRequest() error = %v", err)
	}
	if got.Query != "starter" || got.Kind != "Service" || got.Status != "Active" || got.Page != 2 || got.PageSize != 5 {
		t.Fatalf("mapped request = %+v, want decoded query params", got)
	}
}

func TestMapRequestToGetBlueprintByIDRequest(t *testing.T) {
	tests := []struct {
		name        string
		blueprintID string
		userID      string
		wantErr     error
	}{
		{name: "SUCCESS - maps path and context user", blueprintID: "bp-1", userID: "user-1"},
		{name: "FAILURE - missing path ID", userID: "user-1", wantErr: ErrBlueprintIDIsRequired},
		{name: "FAILURE - missing context user", blueprintID: "bp-1", wantErr: ErrBlueprintUserIDIsRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/blueprints/bp-1", nil)
			if tt.blueprintID != "" {
				req = mux.SetURLVars(req, map[string]string{BlueprintURIVariableID: tt.blueprintID})
			}
			if tt.userID != "" {
				req = req.WithContext(accessmanagerhelpers.TransitWith(req.Context(), tt.userID))
			}

			got, err := MapRequestToGetBlueprintByIDRequest(req, nil)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("MapRequestToGetBlueprintByIDRequest() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got.ID != tt.blueprintID || got.UserID != tt.userID {
				t.Fatalf("mapped request = %+v, want id=%s user=%s", got, tt.blueprintID, tt.userID)
			}
		})
	}
}
