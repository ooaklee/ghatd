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

func TestNewHandleUpdatePathToIndexBypassesNamedFileButRewritesOtherHtml(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		BypassWithFileName("beacon-example.html"),
	)

	beaconReq := httptest.NewRequest("GET", "/beacon-example.html", nil)
	beaconGot := updatePath(beaconReq)
	if beaconGot.URL.Path != "/beacon-example.html" {
		t.Fatalf("path = %q, want /beacon-example.html", beaconGot.URL.Path)
	}

	anotherReq := httptest.NewRequest("GET", "/another.html", nil)
	anotherGot := updatePath(anotherReq)
	if anotherGot.URL.Path != "/" {
		t.Fatalf("path = %q, want /", anotherGot.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexBypassesNamedFileFromNestedRoute(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		BypassWithFileName("/public/beacon-example.html"),
	)

	req := httptest.NewRequest("GET", "/nested/beacon-example.html", nil)
	got := updatePath(req)
	if got.URL.Path != "/nested/beacon-example.html" {
		t.Fatalf("path = %q, want /nested/beacon-example.html", got.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexFileNameBypassesWhenExtensionIgnored(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		IgnoreFileExtension(".js"),
		BypassWithFileName("beacon-loader.js"),
	)

	loaderReq := httptest.NewRequest("GET", "/beacon-loader.js", nil)
	loaderGot := updatePath(loaderReq)
	if loaderGot.URL.Path != "/beacon-loader.js" {
		t.Fatalf("path = %q, want /beacon-loader.js", loaderGot.URL.Path)
	}

	otherJsReq := httptest.NewRequest("GET", "/other.js", nil)
	otherJsGot := updatePath(otherJsReq)
	if otherJsGot.URL.Path != "/" {
		t.Fatalf("path = %q, want /", otherJsGot.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexIgnoreFileNameRewritesNamedFile(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		BypassWithFileName("beacon-example.html"),
		IgnoreFileName("beacon-example.html"),
	)

	req := httptest.NewRequest("GET", "/beacon-example.html", nil)
	got := updatePath(req)
	if got.URL.Path != "/" {
		t.Fatalf("path = %q, want /", got.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexIgnoreFileNameOverridesExtensionBypass(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		IgnoreFileName("beacon-loader.js"),
	)

	req := httptest.NewRequest("GET", "/beacon-loader.js", nil)
	got := updatePath(req)
	if got.URL.Path != "/" {
		t.Fatalf("path = %q, want /", got.URL.Path)
	}
}

func TestNewHandleUpdatePathToIndexLaterBypassFileNameOverridesIgnoreFileName(t *testing.T) {
	updatePath := NewHandleUpdatePathToIndex(
		IgnoreFileName("beacon-loader.js"),
		BypassWithFileName("beacon-loader.js"),
	)

	req := httptest.NewRequest("GET", "/beacon-loader.js", nil)
	got := updatePath(req)
	if got.URL.Path != "/beacon-loader.js" {
		t.Fatalf("path = %q, want /beacon-loader.js", got.URL.Path)
	}
}
