package seo

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type fakeSitemapRepository struct {
	items map[string]SitemapItem
}

func newFakeSitemapRepository(items []SitemapItem) *fakeSitemapRepository {
	repo := &fakeSitemapRepository{items: map[string]SitemapItem{}}
	for _, item := range items {
		item.URI = normaliseSitemapURI(item.URI)
		repo.items[item.URI] = item
	}
	return repo
}

func (f *fakeSitemapRepository) CreateSitemapItem(_ context.Context, item *SitemapItem) (*SitemapItem, error) {
	f.items[item.URI] = *item
	return item, nil
}

func (f *fakeSitemapRepository) DeleteSitemapItemsByURIs(_ context.Context, uris []string) error {
	for _, uri := range uris {
		delete(f.items, uri)
	}
	return nil
}

func (f *fakeSitemapRepository) GetAllSitemapItems(_ context.Context) ([]SitemapItem, error) {
	items := make([]SitemapItem, 0, len(f.items))
	for _, item := range f.items {
		items = append(items, item)
	}
	return items, nil
}

func (f *fakeSitemapRepository) GetSitemapItemByURI(_ context.Context, uri string) (*SitemapItem, error) {
	item, ok := f.items[normaliseSitemapURI(uri)]
	if !ok {
		return nil, ErrSitemapItemResourceNotFound
	}
	return &item, nil
}

func (f *fakeSitemapRepository) GetLatestSitemapItemByURIRegex(_ context.Context, uriRegex string) (*SitemapItem, error) {
	matcher, err := regexp.Compile(uriRegex)
	if err != nil {
		return nil, err
	}

	var latest *SitemapItem
	for _, item := range f.items {
		item := item
		if !matcher.MatchString(item.URI) {
			continue
		}
		if latest == nil || item.LastMod > latest.LastMod {
			latest = &item
		}
	}
	if latest == nil {
		return nil, ErrSitemapItemResourceNotFound
	}

	return latest, nil
}

func (f *fakeSitemapRepository) GetSitemapItems(_ context.Context, _ *GetSitemapItemsRequest) ([]SitemapItem, error) {
	return f.GetAllSitemapItems(context.Background())
}

func (f *fakeSitemapRepository) GetTotalSitemapItems(_ context.Context, _ *GetSitemapItemsRequest) (int64, error) {
	return int64(len(f.items)), nil
}

func (f *fakeSitemapRepository) UpdateSitemapItemByURI(_ context.Context, item *SitemapItem) (*SitemapItem, error) {
	f.items[item.URI] = *item
	return item, nil
}

func (f *fakeSitemapRepository) UpsertSitemapItemByURI(_ context.Context, item *SitemapItem) (*SitemapItem, bool, error) {
	_, exists := f.items[item.URI]
	f.items[item.URI] = *item
	return item, !exists, nil
}

func TestGenerateXMLBuildsSafeSitemap(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository(nil),
		FrontendDomain:    "https://example.com/",
	})

	got, err := service.GenerateXML([]SitemapItem{
		{
			URI:             "/blog?ref=one&next=two",
			LastMod:         "2026-02-21T11:00:00",
			Priority:        0.8,
			ChangeFrequency: ChangeFrequencyWeekly,
		},
	})
	if err != nil {
		t.Fatalf("GenerateXML() error = %v", err)
	}

	if !strings.Contains(got, `<loc>https://example.com/blog?ref=one&amp;next=two</loc>`) {
		t.Fatalf("GenerateXML() did not XML-escape and join loc correctly:\n%s", got)
	}
	if !strings.Contains(got, `<lastmod>2026-02-21T11:00:00+00:00</lastmod>`) {
		t.Fatalf("GenerateXML() missing formatted lastmod:\n%s", got)
	}
}

func TestGenerateXMLSkipsExcludedAndDuplicateURIs(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository(nil),
		FrontendDomain:    "https://example.com",
	})

	got, err := service.GenerateXML([]SitemapItem{
		{URI: "/pricing", LastMod: "2026-02-21T11:00:00", Priority: 0.9, ChangeFrequency: ChangeFrequencyMonthly},
		{URI: "/admin", LastMod: "2026-02-21T11:00:00", Priority: 0.5, ChangeFrequency: ChangeFrequencyWeekly},
		{URI: "/app/private", LastMod: "2026-02-21T11:00:00", Priority: 0.4, ChangeFrequency: ChangeFrequencyWeekly},
		{URI: "/pricing", LastMod: "2026-02-21T11:00:00", Priority: 0.9, ChangeFrequency: ChangeFrequencyMonthly},
		{URI: "/faq", LastMod: "2026-02-21T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyMonthly},
	})
	if err != nil {
		t.Fatalf("GenerateXML() error = %v", err)
	}

	if strings.Contains(got, "/admin") || strings.Contains(got, "/app/private") {
		t.Fatalf("GenerateXML() included non-indexable routes:\n%s", got)
	}
	if count := strings.Count(got, "<loc>"); count != 2 {
		t.Fatalf("GenerateXML() loc count = %d, want 2:\n%s", count, got)
	}
	if strings.Index(got, "https://example.com/faq") > strings.Index(got, "https://example.com/pricing") {
		t.Fatalf("GenerateXML() URLs should be deterministic and sorted:\n%s", got)
	}
}

func TestGenerateSitemapSavesWithSafePath(t *testing.T) {
	tempDir := t.TempDir()
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository([]SitemapItem{
			{
				URI:             "/policy/terms",
				LastMod:         "2026-02-21T11:00:00",
				Priority:        0.8,
				ChangeFrequency: ChangeFrequencyMonthly,
			},
		}),
		FrontendDomain:   "https://example.com",
		WritableRootPath: tempDir,
	})

	response, err := service.GenerateSitemap(context.Background(), &GenerateSitemapRequest{SaveToPaths: []string{"nested/sitemap.xml"}})
	if err != nil {
		t.Fatalf("GenerateSitemap() error = %v", err)
	}
	if len(response.SavedPaths) != 1 {
		t.Fatalf("GenerateSitemap() saved paths = %v, want one", response.SavedPaths)
	}

	if _, err := os.Stat(filepath.Join(tempDir, "nested", "sitemap.xml")); err != nil {
		t.Fatalf("expected sitemap file to be written: %v", err)
	}
}

func TestGenerateSitemapReportsGeneratedURLCount(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository([]SitemapItem{
			{URI: "/pricing", LastMod: "2026-02-21T11:00:00", Priority: 0.9, ChangeFrequency: ChangeFrequencyMonthly},
			{URI: "/app/private", LastMod: "2026-02-21T11:00:00", Priority: 0.4, ChangeFrequency: ChangeFrequencyWeekly},
		}),
		FrontendDomain: "https://example.com",
	})

	response, err := service.GenerateSitemap(context.Background(), &GenerateSitemapRequest{})
	if err != nil {
		t.Fatalf("GenerateSitemap() error = %v", err)
	}

	if response.Total != 1 {
		t.Fatalf("GenerateSitemap() total = %d, want generated URL count 1", response.Total)
	}
}

func TestDownloadSitemapByPathRejectsTraversal(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository(nil),
		WritableRootPath:  t.TempDir(),
	})

	_, err := service.DownloadSitemapByPath(context.Background(), &DownloadSitemapByPathRequest{Path: "../secret.xml"})
	if !errors.Is(err, ErrSitemapPathIsInvalid) {
		t.Fatalf("DownloadSitemapByPath() error = %v, want ErrSitemapPathIsInvalid", err)
	}
}

func TestDeleteEntriesWithUriRegexUsesGoRegexAgainstExactDeletes(t *testing.T) {
	repo := newFakeSitemapRepository([]SitemapItem{
		{URI: "/blog/one", LastMod: "2026-02-21T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyWeekly},
		{URI: "/policy/terms", LastMod: "2026-02-21T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyMonthly},
	})
	service := NewService(&NewServiceRequest{SitemapRepository: repo, FrontendDomain: "https://example.com"})

	response, err := service.DeleteEntriesWithUriRegex(context.Background(), &DeleteEntriesWithURIRegexRequest{URIRegex: `^/blog/`})
	if err != nil {
		t.Fatalf("DeleteEntriesWithUriRegex() error = %v", err)
	}
	if response.Deleted != 1 {
		t.Fatalf("DeleteEntriesWithUriRegex() deleted = %d, want 1", response.Deleted)
	}
	if _, ok := repo.items["/policy/terms"]; !ok {
		t.Fatal("non-matching URI should remain")
	}
}

func TestDeleteEntriesWithUriRegexRejectsLongPattern(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository(nil),
		FrontendDomain:    "https://example.com",
	})

	_, err := service.DeleteEntriesWithUriRegex(context.Background(), &DeleteEntriesWithURIRegexRequest{
		URIRegex: strings.Repeat("a", maxSitemapURIRegexLength+1),
	})
	if !errors.Is(err, ErrSitemapItemInvalidURI) {
		t.Fatalf("DeleteEntriesWithUriRegex() error = %v, want ErrSitemapItemInvalidURI", err)
	}
}

func TestGetLatestSitemapItemByURIRegexReturnsNewestMatch(t *testing.T) {
	service := NewService(&NewServiceRequest{
		SitemapRepository: newFakeSitemapRepository([]SitemapItem{
			{URI: "/blog/one", LastMod: "2026-02-21T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyWeekly},
			{URI: "/blog/two", LastMod: "2026-02-22T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyWeekly},
			{URI: "/policy/terms", LastMod: "2026-03-01T11:00:00", Priority: 0.8, ChangeFrequency: ChangeFrequencyMonthly},
		}),
		FrontendDomain: "https://example.com",
	})

	response, err := service.GetLatestSitemapItemByURIRegex(context.Background(), &GetLatestSitemapItemByURIRegexRequest{
		URIRegex: `^/blog/`,
	})
	if err != nil {
		t.Fatalf("GetLatestSitemapItemByURIRegex() error = %v", err)
	}
	if response.SitemapItem == nil || response.SitemapItem.URI != "/blog/two" {
		t.Fatalf("latest sitemap item = %+v, want latest article URI", response.SitemapItem)
	}
}

func TestMassSitemapItemCreationByBatchOverride(t *testing.T) {
	repo := newFakeSitemapRepository([]SitemapItem{
		{URI: "/policy/terms", LastMod: "2026-02-21T11:00:00", Priority: 0.5, ChangeFrequency: ChangeFrequencyMonthly},
	})
	service := NewService(&NewServiceRequest{SitemapRepository: repo, FrontendDomain: "https://example.com"})
	priority := 0.9

	response, err := service.MassSitemapItemCreationByBatch(context.Background(), &MassSitemapItemCreationByBatchRequest{
		OverrideIfExists: true,
		Items: []CreateSitemapItemRequest{
			{URI: "/policy/terms", LastMod: "2026-02-22T11:00:00", Priority: &priority, ChangeFrequency: ChangeFrequencyWeekly},
		},
	})
	if err != nil {
		t.Fatalf("MassSitemapItemCreationByBatch() error = %v", err)
	}
	if response.Updated != 1 || response.Created != 0 {
		t.Fatalf("MassSitemapItemCreationByBatch() created=%d updated=%d, want created=0 updated=1", response.Created, response.Updated)
	}
	if got := repo.items["/policy/terms"].Priority; got != priority {
		t.Fatalf("stored priority = %v, want %v", got, priority)
	}
}
