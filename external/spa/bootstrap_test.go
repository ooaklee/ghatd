package spa

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ooaklee/ghatd/external/router"
)

func TestNewBootstrap(t *testing.T) {
	tests := []struct {
		name    string
		req     *BootstrapRequest
		wantErr string
	}{
		{
			name: "SUCCESS - builds router and handler",
			req: &BootstrapRequest{
				EmbeddedContent: fstest.MapFS{
					"dist/index.html": {Data: []byte("<h1>hello</h1>")},
				},
			},
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "spa/bootstrap-nil-request",
		},
		{
			name:    "FAILURE - missing embedded content",
			req:     &BootstrapRequest{},
			wantErr: "spa/bootstrap-missing-embedded-content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewBootstrap(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewBootstrap() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewBootstrap() error = %v", err)
			}
			if got == nil || got.Router == nil || got.Handler == nil {
				t.Fatalf("NewBootstrap() = %+v, want router and handler", got)
			}
		})
	}
}

func TestBootstrapAttachRoutes(t *testing.T) {
	bootstrap, err := NewBootstrap(&BootstrapRequest{
		EmbeddedContent: fstest.MapFS{
			"dist/index.html": {Data: []byte("<h1>hello</h1>")},
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}

	if err := bootstrap.AttachRoutes(); err != nil {
		t.Fatalf("AttachRoutes() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	bootstrap.Router.GetRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestBootstrapAttachRoutesServesWebManifest(t *testing.T) {
	bootstrap, err := NewBootstrap(&BootstrapRequest{
		EmbeddedContent: fstest.MapFS{
			"dist/index.html":           {Data: []byte("<h1>hello</h1>")},
			"dist/manifest.webmanifest": {Data: []byte(`{"name":"Example"}`)},
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}

	if err := bootstrap.AttachRoutes(); err != nil {
		t.Fatalf("AttachRoutes() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/manifest.webmanifest", nil)
	rec := httptest.NewRecorder()

	bootstrap.Router.GetRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != `{"name":"Example"}` {
		t.Fatalf("body = %q, want manifest body", body)
	}
	if contentType := rec.Header().Get("Content-Type"); contentType != "application/manifest+json" {
		t.Fatalf("Content-Type = %q, want application/manifest+json", contentType)
	}
}

func TestBootstrapAttachRoutesServesBypassedFileName(t *testing.T) {
	bootstrap, err := NewBootstrap(&BootstrapRequest{
		EmbeddedContent: fstest.MapFS{
			"dist/index.html":           {Data: []byte("<h1>index</h1>")},
			"dist/beacon-example.html":  {Data: []byte("<h1>beacon</h1>")},
			"dist/another-example.html": {Data: []byte("<h1>another</h1>")},
		},
		HandleUpdatePathToIndexFunc: NewHandleUpdatePathToIndex(
			BypassWithFileName("beacon-example.html"),
		),
	})
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}

	if err := bootstrap.AttachRoutes(); err != nil {
		t.Fatalf("AttachRoutes() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/beacon-example.html", nil)
	rec := httptest.NewRecorder()

	bootstrap.Router.GetRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "<h1>beacon</h1>" {
		t.Fatalf("body = %q, want beacon body", body)
	}

	req = httptest.NewRequest(http.MethodGet, "/another-example.html", nil)
	rec = httptest.NewRecorder()

	bootstrap.Router.GetRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "<h1>index</h1>" {
		t.Fatalf("body = %q, want index body", body)
	}
}

func TestBootstrapAttachRoutesServesServiceWorkerWithNoCacheHeaders(t *testing.T) {
	bootstrap, err := NewBootstrap(&BootstrapRequest{
		EmbeddedContent: fstest.MapFS{
			"dist/index.html":        {Data: []byte("<h1>hello</h1>")},
			"dist/service-worker.js": {Data: []byte("self.addEventListener('install', () => {})")},
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}

	if err := bootstrap.AttachRoutes(); err != nil {
		t.Fatalf("AttachRoutes() error = %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/service-worker.js", nil)
	rec := httptest.NewRecorder()

	bootstrap.Router.GetRouter().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if body := rec.Body.String(); body != "self.addEventListener('install', () => {})" {
		t.Fatalf("body = %q, want service worker body", body)
	}
	if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != serviceWorkerCacheControl {
		t.Fatalf("Cache-Control = %q, want %q", cacheControl, serviceWorkerCacheControl)
	}
	if pragma := rec.Header().Get("Pragma"); pragma != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", pragma)
	}
	if expires := rec.Header().Get("Expires"); expires != "0" {
		t.Fatalf("Expires = %q, want 0", expires)
	}
}

func TestBootstrapAttachRoutesDoesNotSetNoCacheHeadersForOrdinaryAssets(t *testing.T) {
	bootstrap, err := NewBootstrap(&BootstrapRequest{
		EmbeddedContent: fstest.MapFS{
			"dist/index.html":       {Data: []byte("<h1>hello</h1>")},
			"dist/assets/app.js":    {Data: []byte("console.log('hello')")},
			"dist/assets/app.css":   {Data: []byte("body{}")},
			"dist/site.webmanifest": {Data: []byte(`{"name":"Example"}`)},
		},
	})
	if err != nil {
		t.Fatalf("NewBootstrap() error = %v", err)
	}

	if err := bootstrap.AttachRoutes(); err != nil {
		t.Fatalf("AttachRoutes() error = %v", err)
	}

	for _, targetPath := range []string{"/assets/app.js", "/assets/app.css", "/site.webmanifest"} {
		req := httptest.NewRequest(http.MethodGet, targetPath, nil)
		rec := httptest.NewRecorder()

		bootstrap.Router.GetRouter().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", targetPath, rec.Code, http.StatusOK)
		}
		if cacheControl := rec.Header().Get("Cache-Control"); cacheControl != "" {
			t.Fatalf("%s Cache-Control = %q, want empty", targetPath, cacheControl)
		}
		if pragma := rec.Header().Get("Pragma"); pragma != "" {
			t.Fatalf("%s Pragma = %q, want empty", targetPath, pragma)
		}
	}
}

func TestBootstrapAttachRoutesErrors(t *testing.T) {
	tests := []struct {
		name    string
		target  *Bootstrap
		wantErr string
	}{
		{name: "FAILURE - nil bootstrap", target: nil, wantErr: "spa/bootstrap-nil"},
		{name: "FAILURE - missing router", target: &Bootstrap{}, wantErr: "spa/bootstrap-missing-router"},
		{name: "FAILURE - missing file system", target: &Bootstrap{Router: router.NewRouter(nil, nil)}, wantErr: "spa/bootstrap-missing-file-system"},
		{
			name: "FAILURE - missing dist directory",
			target: &Bootstrap{
				Router:                        router.NewRouter(nil, nil),
				spaFileSystem:                 fstest.MapFS{"index.html": {Data: []byte("<h1>hello</h1>")}},
				handleUpdatePathToIndexFunc:   NewHandleUpdatePathToIndex(),
				embeddedContentFilePathPrefix: "",
			},
			wantErr: "spa/attach-routes-file-system",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.target.AttachRoutes()
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("AttachRoutes() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}
