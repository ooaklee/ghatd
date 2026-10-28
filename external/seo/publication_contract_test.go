package seo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
)

// This fixture exposes the same stored rows to both administrative and XML
// operations, so the contract test can detect inappropriate list filtering.
type publicationContractRepository struct{ publicRepositoryStub }

func (r *publicationContractRepository) GetSitemapItems(context.Context, *GetSitemapItemsRequest) ([]SitemapItem, error) {
	return append([]SitemapItem(nil), r.items...), nil
}

func (r *publicationContractRepository) GetTotalSitemapItems(context.Context, *GetSitemapItemsRequest) (int64, error) {
	return int64(len(r.items)), nil
}

func TestPublicationContractGeneratedOutputsUseOneInstant(t *testing.T) {
	for _, mode := range []string{"direct XML", "administrative export", "live response"} {
		t.Run(mode, func(t *testing.T) {
			boundary := time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC)
			now, clockCalls := boundary.Add(-time.Nanosecond), 0
			items := []SitemapItem{publicItem("/legacy"), publicItem("/scheduled-a"), publicItem("/scheduled-b")}
			items[1].PublishedAt = boundary.Format(time.RFC3339Nano)
			items[2].PublishedAt = "2027-01-05T10:00:00+01:00"
			repository := &publicationContractRepository{publicRepositoryStub: publicRepositoryStub{items: items}}
			service := NewService(&NewServiceRequest{
				SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: PublicSitemapModeDatabase,
				Now: func() time.Time { clockCalls++; instant := now; now = now.Add(time.Nanosecond); return instant },
			})
			// This is a stateful boundary lifecycle: each request must recalculate
			// eligibility while all rows in that output share one captured instant.
			for _, step := range []struct {
				name string
				at   time.Time
				want []string
			}{
				{name: "one nanosecond before", at: boundary.Add(-time.Nanosecond), want: []string{"https://example.com/legacy"}},
				{name: "exact boundary", at: boundary, want: []string{"https://example.com/legacy", "https://example.com/scheduled-a", "https://example.com/scheduled-b"}},
				{name: "after boundary", at: boundary.Add(time.Nanosecond), want: []string{"https://example.com/legacy", "https://example.com/scheduled-a", "https://example.com/scheduled-b"}},
			} {
				t.Run(step.name, func(t *testing.T) {
					now, clockCalls = step.at, 0
					var content []byte
					switch mode {
					case "direct XML":
						xml, err := service.GenerateXML(repository.items)
						require.NoError(t, err)
						content = []byte(xml)
					case "administrative export":
						result, err := service.GenerateSitemap(t.Context(), nil)
						require.NoError(t, err)
						require.Equal(t, len(step.want), result.Total)
						content = []byte(result.XML)
					case "live response":
						result, err := service.GetPublicSitemap(t.Context(), nil)
						require.NoError(t, err)
						require.Equal(t, "no-store", result.CacheControl)
						content = result.Content
					}
					require.Equal(t, 1, clockCalls, "one output must not straddle a publication boundary")
					require.ElementsMatch(t, step.want, publicXMLLocations(t, content))
					require.NotContains(t, string(content), "published_at")
					admin, err := service.GetSitemapItems(t.Context(), &GetSitemapItemsRequest{})
					require.NoError(t, err)
					require.EqualValues(t, 3, admin.Total)
					require.Equal(t, items, admin.SitemapItems, "admin lists retain scheduled rows and metadata")
					require.Equal(t, 1, clockCalls, "administrative listing does not evaluate eligibility")
				})
			}
		})
	}
}

func TestPublicationContractInvalidStoredDatesPreserveExportsAndFailPublicly(t *testing.T) {
	for _, value := range []string{"private-publication-diagnostic", "2027-02-30T12:00:00Z", "0001-01-01T00:00:00Z", "2027-01-05"} {
		t.Run(value, func(t *testing.T) {
			item := publicItem("/scheduled")
			item.PublishedAt = value
			repository := &publicRepositoryStub{items: []SitemapItem{publicItem("/legacy"), item}}
			root := t.TempDir()
			filename := filepath.Join(root, "sitemap.xml")
			require.NoError(t, os.WriteFile(filename, []byte("PREVIOUS-EXPORT"), 0o600))
			service := NewService(&NewServiceRequest{
				SitemapRepository: repository, FrontendDomain: "https://example.com", PublicSitemapMode: PublicSitemapModeDatabase,
				WritableRootPath: root, FileSystem: fstest.MapFS{"sitemap.xml": &fstest.MapFile{Data: []byte("EMBEDDED-FALLBACK")}},
				Now: func() time.Time { return time.Date(2027, 1, 5, 9, 0, 0, 0, time.UTC) },
			})
			export, err := service.GenerateSitemap(t.Context(), &GenerateSitemapRequest{SaveToPaths: []string{"sitemap.xml"}})
			require.Error(t, err)
			require.Nil(t, export)
			stored, err := os.ReadFile(filename)
			require.NoError(t, err)
			require.Equal(t, "PREVIOUS-EXPORT", string(stored), "invalid data cannot replace a previously valid export")
			response := httptest.NewRecorder()
			NewHandler(service, nil).GetSitemap(response, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
			require.Equal(t, http.StatusServiceUnavailable, response.Code)
			require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
			require.JSONEq(t, `{"errors":[{"title":"Service Unavailable","detail":"The sitemap is temporarily unavailable","status":"503","code":"SEO0-011"}]}`, response.Body.String())
			require.NotContains(t, response.Body.String(), value)
			require.NotContains(t, response.Body.String(), "PREVIOUS-EXPORT")
			require.NotContains(t, response.Body.String(), "EMBEDDED-FALLBACK")
		})
	}
}
