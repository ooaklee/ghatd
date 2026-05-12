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
