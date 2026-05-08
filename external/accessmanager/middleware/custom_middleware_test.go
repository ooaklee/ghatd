package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/reply/v2"
)

func TestBuildCustomMeEndpointErrorMap_EmptyBaseMaps(t *testing.T) {
	result := BuildCustomMeEndpointErrorMap(nil)

	if len(result) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(result))
	}

	override := result[0]
	item, ok := override[accessmanager.ErrUnauthorizedUnableToAttainRequestorID]
	if !ok {
		t.Fatal("expected override key in result")
	}
	if item.StatusCode != http.StatusAccepted {
		t.Errorf("expected StatusCode 202, got %d", item.StatusCode)
	}
	if item.Title != "Unauthorized" {
		t.Errorf("expected Title 'Unauthorized', got %s", item.Title)
	}
	if item.Code != "AM00-013" {
		t.Errorf("expected Code 'AM00-013', got %s", item.Code)
	}
}

func TestBuildCustomMeEndpointErrorMap_OverrideIsLastLayer(t *testing.T) {
	baseMap := reply.ErrorManifest{
		errors.New("base_key"): {Title: "Base", StatusCode: 400},
	}
	anotherMap := reply.ErrorManifest{
		errors.New("another_key"): {Title: "Another", StatusCode: 401},
	}

	result := BuildCustomMeEndpointErrorMap([]reply.ErrorManifest{baseMap, anotherMap})

	if len(result) != 3 {
		t.Fatalf("expected 3 manifests (2 base + 1 override), got %d", len(result))
	}

	last := result[len(result)-1]
	if _, ok := last[accessmanager.ErrUnauthorizedUnableToAttainRequestorID]; !ok {
		t.Fatal("expected override manifest to be the last element")
	}
}

func TestBuildCustomMeEndpointErrorMap_BaseMapsPreserved(t *testing.T) {
	baseKey := errors.New("base_key")
	baseMap := reply.ErrorManifest{
		baseKey: {Title: "Base Title", StatusCode: 400, Code: "BASE-001"},
	}

	result := BuildCustomMeEndpointErrorMap([]reply.ErrorManifest{baseMap})

	first := result[0]
	item, ok := first[baseKey]
	if !ok {
		t.Fatal("expected base key to be preserved")
	}
	if item.Title != "Base Title" {
		t.Errorf("expected Title 'Base Title', got %s", item.Title)
	}
	if item.StatusCode != 400 {
		t.Errorf("expected StatusCode 400, got %d", item.StatusCode)
	}
}

func TestBuildCustomMeEndpointErrorMap_OverrideWinsOnSameKey(t *testing.T) {
	baseMap := reply.ErrorManifest{
		accessmanager.ErrUnauthorizedUnableToAttainRequestorID: {
			Title:      "Unauthorized (default)",
			StatusCode: http.StatusUnauthorized,
			Code:       "AM00-013",
		},
	}

	result := BuildCustomMeEndpointErrorMap([]reply.ErrorManifest{baseMap})

	if len(result) != 2 {
		t.Fatalf("expected 2 manifests (1 base + 1 override), got %d", len(result))
	}

	last := result[len(result)-1]
	item := last[accessmanager.ErrUnauthorizedUnableToAttainRequestorID]
	if item.StatusCode != http.StatusAccepted {
		t.Errorf("expected StatusCode 202 from override, got %d", item.StatusCode)
	}
}

func TestBuildCustomMeEndpointErrorMap_NilSlice(t *testing.T) {
	var nilMaps []reply.ErrorManifest
	result := BuildCustomMeEndpointErrorMap(nilMaps)

	if len(result) != 1 {
		t.Fatalf("expected 1 manifest for nil input, got %d", len(result))
	}
}

func TestCustomMeEndpointValidApiTokenOrJWTMiddleware_ReturnsValidMiddleware(t *testing.T) {
	middleware := createTestMiddleware(&mockAccessManagerService{})
	customMaps := BuildCustomMeEndpointErrorMap(nil)
	middlewareFunc := middleware.CustomMeEndpointValidApiTokenOrJWTMiddleware(customMaps)

	if middlewareFunc == nil {
		t.Fatal("expected non-nil middleware function")
	}

	handler := createTestHandler()
	wrapped := middlewareFunc(handler)

	if wrapped == nil {
		t.Fatal("expected non-nil wrapped handler")
	}
}

func TestCustomMeEndpointValidApiTokenOrJWTMiddleware_UsesCustomErrorMaps(t *testing.T) {
	middleware := createTestMiddleware(&mockAccessManagerService{})
	customMaps := BuildCustomMeEndpointErrorMap(nil)
	middlewareFunc := middleware.CustomMeEndpointValidApiTokenOrJWTMiddleware(customMaps)

	handler := createTestHandler()
	wrapped := middlewareFunc(handler)

	req := httptest.NewRequest("GET", "/api/v1/ums/me", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("expected StatusAccepted 202 for unauthenticated /me, got %d", w.Code)
	}
}

func TestCustomMeEndpointValidApiTokenOrJWTMiddleware_WithValidTokenProceeds(t *testing.T) {
	userID := "test-user-456"
	mockService := &mockAccessManagerService{
		middlewareValidAPITokenRequiredFunc: func(r *http.Request) (*accessmanager.MiddlewareAuthedUserResponse, error) {
			return mockAuthedResp(userID, "ACTIVE", []string{"USER"}), nil
		},
	}

	middleware := createTestMiddleware(mockService)
	customMaps := BuildCustomMeEndpointErrorMap(nil)
	middlewareFunc := middleware.CustomMeEndpointValidApiTokenOrJWTMiddleware(customMaps)

	handler := createTestHandler()
	wrapped := middlewareFunc(handler)

	req := httptest.NewRequest("GET", "/api/v1/ums/me", nil)
	req.Header.Set("X-Api-Token", "valid-token")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected StatusOK 200, got %d", w.Code)
	}
}

func TestCustomMeEndpointValidApiTokenOrJWTMiddleware_DefaultErrorsAre401(t *testing.T) {
	middleware := createTestMiddleware(&mockAccessManagerService{})
	defaultMaps := []reply.ErrorManifest{
		{
			accessmanager.ErrUnauthorizedUnableToAttainRequestorID: {
				Title:      "Unauthorized",
				StatusCode: http.StatusUnauthorized,
				Code:       "AM00-013",
			},
		},
	}
	middlewareFunc := middleware.CustomMeEndpointValidApiTokenOrJWTMiddleware(defaultMaps)

	handler := createTestHandler()
	wrapped := middlewareFunc(handler)

	req := httptest.NewRequest("GET", "/api/v1/ums/me", nil)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected StatusUnauthorized 401, got %d", w.Code)
	}
}
