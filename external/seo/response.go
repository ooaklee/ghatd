package seo

// SitemapItemResponse wraps a sitemap item.
type SitemapItemResponse struct {
	SitemapItem *SitemapItem `json:"sitemap_item"`
	Created     bool         `json:"created,omitempty"`
	Updated     bool         `json:"updated,omitempty"`
}

// GetSitemapItemsResponse wraps sitemap item list results.
type GetSitemapItemsResponse struct {
	SitemapItems []SitemapItem `json:"sitemap_items"`
	Total        int64         `json:"total"`
}

// DeleteEntriesWithURIRegexResponse describes regex delete results.
type DeleteEntriesWithURIRegexResponse struct {
	Deleted int64    `json:"deleted"`
	URIs    []string `json:"uris"`
}

// GenerateSitemapResponse describes generated XML and any saved files.
type GenerateSitemapResponse struct {
	XML        string   `json:"xml"`
	SavedPaths []string `json:"saved_paths,omitempty"`
	Total      int      `json:"total"`
}

// DownloadSitemapByPathResponse wraps downloaded sitemap bytes.
type DownloadSitemapByPathResponse struct {
	Path        string
	FileName    string
	ContentType string
	Content     []byte
}

// MassSitemapItemCreationByBatchResponse describes batch creation results.
type MassSitemapItemCreationByBatchResponse struct {
	SitemapItems []SitemapItem `json:"sitemap_items"`
	Created      int           `json:"created"`
	Updated      int           `json:"updated"`
	Skipped      int           `json:"skipped"`
}
