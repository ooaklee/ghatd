package usermanager

import (
	"net/http/httptest"
	"testing"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

func TestSetVisionReadCacheHeadersForPublicRequest(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/ums/visions", nil)
	response := httptest.NewRecorder()

	setVisionReadCacheHeaders(response, request)

	if got := response.Header().Get("Cache-Control"); got != "public, max-age=60, s-maxage=300" {
		t.Fatalf("Cache-Control = %q", got)
	}
	if got := response.Header().Get("Vary"); got != "Cookie, Authorization" {
		t.Fatalf("Vary = %q", got)
	}
}

func TestSetVisionReadCacheHeadersForAuthenticatedRequest(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/v1/ums/visions", nil)
	request = request.WithContext(accessmanagerhelpers.TransitAuthenticatedWith(request.Context(), true))
	response := httptest.NewRecorder()

	setVisionReadCacheHeaders(response, request)

	if got := response.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q", got)
	}
}
