package spa

import (
	"net/http/httptest"
	"testing"
)

func TestNewHandleUpdatePathToIndexDefaultWebManifestBypass(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex()
	req := httptest.NewRequest("GET", "/manifest.webmanifest", nil)

	got := updatePath(req)

	if got.URL.Path != "/manifest.webmanifest" {
		t.Fatalf("path = %q, want /manifest.webmanifest", got.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexRewritesSpaRoute(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex()
	req := httptest.NewRequest("GET", "/app/plan", nil)

	got := updatePath(req)

	if got.URL.Path != "/" {
		t.Fatalf("path = %q, want /", got.URL.Path)
	}
}
