package seo

import (
	"context"
	"encoding/xml"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
)

type publicRepositoryStub struct {
	SitemapRepository
	items []SitemapItem
	err   error
	calls int
}

func (r *publicRepositoryStub) GetAllSitemapItems(context.Context) ([]SitemapItem, error) {
	r.calls++
	return r.items, r.err
}

func publicItem(uri string) SitemapItem {
	return SitemapItem{URI: uri, LastMod: "2026-10-28T12:00:00", Priority: 0.7, ChangeFrequency: ChangeFrequencyWeekly}
}

func publicXMLLocations(t *testing.T, content []byte) []string {
	t.Helper()
	var document struct {
		XMLName xml.Name `xml:"urlset"`
		URLs    []struct {
			Loc string `xml:"loc"`
		} `xml:"url"`
	}
	require.NoError(t, xml.Unmarshal(content, &document))
	require.Equal(t, "http://www.sitemaps.org/schemas/sitemap/0.9", document.XMLName.Space)
	locations := []string{}
	for _, entry := range document.URLs {
		locations = append(locations, entry.Loc)
	}
	return locations
}

func TestPublicSitemapSourceModes(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mode      PublicSitemapMode
		requested string
		database  bool
	}{
		{name: "zero value preserves default file"},
		{name: "explicit file with safe selector", mode: PublicSitemapModeFile, requested: "alternate.xml"},
		{name: "database reads storage", mode: PublicSitemapModeDatabase, database: true},
		{name: "database ignores file selector", mode: PublicSitemapModeDatabase, requested: "alternate.xml", database: true},
		{name: "database never follows unsafe file selector", mode: PublicSitemapModeDatabase, requested: "../private.xml", database: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/current")}}
			filterCalls := 0
			service := NewService(&NewServiceRequest{
				SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: tc.mode,
				FileSystem: fstest.MapFS{
					"assets/default.xml":   &fstest.MapFile{Data: []byte("EMBEDDED-DEFAULT")},
					"assets/alternate.xml": &fstest.MapFile{Data: []byte("EMBEDDED-ALTERNATE")},
				},
				EmbeddedPathPrefix: "assets", DefaultPath: "default.xml", WritableRootPath: t.TempDir(),
				PublicSitemapItemsFilter: func(_ context.Context, items []SitemapItem) ([]SitemapItem, error) {
					filterCalls++
					return items, nil
				},
			})
			response, err := service.GetPublicSitemap(context.Background(), &DownloadSitemapByPathRequest{Path: tc.requested})
			require.NoError(t, err)
			if tc.database {
				require.Equal(t, []string{"https://example.com/current"}, publicXMLLocations(t, response.Content))
				require.Equal(t, "no-store", response.CacheControl)
				require.Equal(t, 1, repository.calls)
				require.Equal(t, 1, filterCalls)
			} else {
				want := "EMBEDDED-DEFAULT"
				if tc.requested != "" {
					want = "EMBEDDED-ALTERNATE"
				}
				require.Equal(t, want, string(response.Content))
				require.Empty(t, response.CacheControl)
				require.Zero(t, repository.calls)
				require.Zero(t, filterCalls)
			}
		})
	}
}

func TestPublicSitemapFilterAndRenderer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter string
		want   []string
	}{
		{name: "no policy uses shared exclusions and deduplication", want: []string{"https://example.com/first", "https://example.com/second"}},
		{name: "host policy limits candidates", filter: "first", want: []string{"https://example.com/first"}},
		{name: "empty policy result is authoritative", filter: "empty", want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/second"), publicItem("/first"), publicItem("/first"), publicItem("/admin/private")}}
			service := NewService(&NewServiceRequest{SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: PublicSitemapModeDatabase})
			if tc.filter != "" {
				service.PublicSitemapItemsFilter = func(ctx context.Context, items []SitemapItem) ([]SitemapItem, error) {
					require.NoError(t, ctx.Err())
					items[0].URI = "/changed-in-filter"
					if tc.filter == "empty" {
						return nil, nil
					}
					return items[1:2], nil
				}
			}
			response, err := service.GetPublicSitemap(context.Background(), nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, publicXMLLocations(t, response.Content))
			require.Equal(t, "/second", repository.items[0].URI, "filter must not mutate the repository's backing slice")
		})
	}
}

func TestPublicSitemapCurrentStateAcrossServiceInstances(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fresh bool
	}{
		{name: "same service"},
		{name: "fresh service using shared repository", fresh: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/before")}}
			newService := func() *Service {
				return NewService(&NewServiceRequest{SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: PublicSitemapModeDatabase})
			}
			service := newService()
			first, err := service.GetPublicSitemap(context.Background(), nil)
			require.NoError(t, err)
			require.Equal(t, []string{"https://example.com/before"}, publicXMLLocations(t, first.Content))
			repository.items = []SitemapItem{publicItem("/after")}
			if tc.fresh {
				service = newService()
			}
			second, err := service.GetPublicSitemap(context.Background(), nil)
			require.NoError(t, err)
			require.Equal(t, []string{"https://example.com/after"}, publicXMLLocations(t, second.Content))
			require.Equal(t, 2, repository.calls)
		})
	}
}

func TestPublicSitemapFailuresNeverReadFallbackFiles(t *testing.T) {
	for _, failure := range []string{"repository", "filter", "render", "missing repository", "unknown mode", "cancelled context", "nil context"} {
		t.Run(failure, func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/current")}}
			root := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(root, "sitemap.xml"), []byte("STALE-DISK"), 0o600))
			service := NewService(&NewServiceRequest{SitemapRepository: repository, FrontendDomain: "https://example.com",
				PublicSitemapMode: PublicSitemapModeDatabase, WritableRootPath: root,
				FileSystem: fstest.MapFS{"sitemap.xml": &fstest.MapFile{Data: []byte("STALE-EMBEDDED")}},
			})
			ctx := context.Background()
			cause := errors.New("private-database-diagnostic")
			switch failure {
			case "repository":
				repository.err = cause
			case "filter":
				service.PublicSitemapItemsFilter = func(context.Context, []SitemapItem) ([]SitemapItem, error) { return nil, cause }
			case "render":
				repository.items[0].Priority = 9
			case "missing repository":
				service.SitemapRepository = nil
			case "unknown mode":
				service.PublicSitemapMode = PublicSitemapMode(99)
			case "cancelled context":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "nil context":
				ctx = nil
			}
			response, err := service.GetPublicSitemap(ctx, nil)
			require.Nil(t, response)
			require.ErrorIs(t, err, ErrPublicSitemapUnavailable)
			if failure == "repository" || failure == "filter" {
				require.ErrorIs(t, err, cause, "service callers retain diagnostic causes")
			}
		})
	}
}

func TestPublicSitemapModeLeavesAdminExportsUnchanged(t *testing.T) {
	for _, mode := range []PublicSitemapMode{PublicSitemapModeFile, PublicSitemapModeDatabase} {
		t.Run(map[PublicSitemapMode]string{PublicSitemapModeFile: "file mode", PublicSitemapModeDatabase: "database mode"}[mode], func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/current")}}
			filterCalls := 0
			service := NewService(&NewServiceRequest{SitemapRepository: repository, FrontendDomain: "https://example.com",
				PublicSitemapMode: mode, WritableRootPath: t.TempDir(),
				PublicSitemapItemsFilter: func(context.Context, []SitemapItem) ([]SitemapItem, error) { filterCalls++; return nil, nil },
			})
			export, err := service.GenerateSitemap(context.Background(), &GenerateSitemapRequest{SaveToPaths: []string{"manual.xml"}})
			require.NoError(t, err)
			require.Equal(t, []string{"manual.xml"}, export.SavedPaths)
			require.Equal(t, []string{"https://example.com/current"}, publicXMLLocations(t, []byte(export.XML)))
			download, err := service.DownloadSitemapByPath(context.Background(), &DownloadSitemapByPathRequest{Path: "manual.xml"})
			require.NoError(t, err)
			require.Equal(t, export.XML, string(download.Content))
			require.Empty(t, download.CacheControl)
			require.Zero(t, filterCalls)
		})
	}
}

type legacyPublicSitemapService struct {
	sitemapService
	requested string
}

func (s *legacyPublicSitemapService) DownloadSitemapByPath(_ context.Context, req *DownloadSitemapByPathRequest) (*DownloadSitemapByPathResponse, error) {
	s.requested = req.Path
	return &DownloadSitemapByPathResponse{Content: []byte("LEGACY-CUSTOM-FILE"), ContentType: "application/xml"}, nil
}

func TestPublicSitemapHandlerCompatibilityAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		legacy, file bool
		failure      string
		want         int
	}{
		{name: "legacy custom service", legacy: true, want: http.StatusOK},
		{name: "default file service keeps path selector", file: true, want: http.StatusOK},
		{name: "database public response", want: http.StatusOK},
		{name: "unregistered repository cause", failure: "repository unknown", want: http.StatusServiceUnavailable},
		{name: "registered repository cause", failure: "repository known", want: http.StatusServiceUnavailable},
		{name: "registered render cause", failure: "render", want: http.StatusServiceUnavailable},
		{name: "filter foreign cause", failure: "filter", want: http.StatusServiceUnavailable},
		{name: "filter cancellation", failure: "cancel", want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/current")}}
			service := NewService(&NewServiceRequest{SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: PublicSitemapModeDatabase,
				WritableRootPath: t.TempDir(), FileSystem: fstest.MapFS{"alternate.xml": &fstest.MapFile{Data: []byte("ALTERNATE-FILE")}},
			})
			if tc.file {
				service.PublicSitemapMode = PublicSitemapModeFile
			}
			switch tc.failure {
			case "repository unknown":
				repository.err = errors.New("private-credential-internal-diagnostic")
			case "repository known":
				repository.err = ErrSitemapItemDatabaseError
			case "render":
				repository.items[0].Priority = 5
			case "filter":
				service.PublicSitemapItemsFilter = func(context.Context, []SitemapItem) ([]SitemapItem, error) { return nil, os.ErrPermission }
			case "cancel":
				service.PublicSitemapItemsFilter = func(context.Context, []SitemapItem) ([]SitemapItem, error) { return nil, context.Canceled }
			}
			legacy := &legacyPublicSitemapService{}
			handler := NewHandler(service, nil)
			if tc.legacy {
				handler = NewHandler(legacy, nil)
			}
			response := httptest.NewRecorder()
			handler.GetSitemap(response, httptest.NewRequest(http.MethodGet, "/sitemap.xml?path=alternate.xml", nil))
			require.Equal(t, tc.want, response.Code, response.Body.String())
			require.NotContains(t, response.Body.String(), "private-credential-internal-diagnostic")
			if tc.legacy {
				require.Equal(t, "alternate.xml", legacy.requested)
				require.Equal(t, "LEGACY-CUSTOM-FILE", response.Body.String())
			} else if tc.file {
				require.Equal(t, "ALTERNATE-FILE", response.Body.String())
			} else {
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				if tc.failure != "" {
					require.JSONEq(t, `{"errors":[{"title":"Service Unavailable","detail":"The sitemap is temporarily unavailable","status":"503","code":"SEO0-011"}]}`, response.Body.String())
				} else {
					require.Equal(t, []string{"https://example.com/current"}, publicXMLLocations(t, response.Body.Bytes()))
				}
			}
		})
	}
}

func TestNeverCachePublicSitemap(t *testing.T) {
	for _, uri := range []string{"/sitemap.xml", "/sitemap.xml?path=old.xml", "/sitemap.xml?refresh=1", "/sitemap.xml/other", "/pricing"} {
		t.Run(uri, func(t *testing.T) {
			cacheCalls, liveCalls := 0, 0
			cache := mux.MiddlewareFunc(func(http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { cacheCalls++; _, _ = w.Write([]byte("STALE")) })
			})
			handler := NeverCachePublicSitemap(cache)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				liveCalls++
				w.WriteHeader(http.StatusServiceUnavailable)
			}))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, uri, nil))
			if uri == PublicSitemapPath || strings.HasPrefix(uri, PublicSitemapPath+"?") {
				require.Zero(t, cacheCalls)
				require.Equal(t, 1, liveCalls)
				require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
				require.Equal(t, http.StatusServiceUnavailable, response.Code)
			} else {
				require.Equal(t, 1, cacheCalls)
				require.Zero(t, liveCalls)
			}
		})
	}
}
