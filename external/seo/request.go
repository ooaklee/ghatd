package seo

// CreateSitemapItemRequest holds data needed to create a sitemap item.
type CreateSitemapItemRequest struct {
	URI             string          `json:"uri"`
	LastMod         string          `json:"last_mod"`
	Priority        *float64        `json:"priority,omitempty"`
	ChangeFrequency ChangeFrequency `json:"change_frequency"`
	CreatedByID     string          `json:"created_by_id,omitempty"`
}

// UpdateSitemapItemRequest holds data needed to update a sitemap item by URI.
type UpdateSitemapItemRequest struct {
	URI             string           `json:"uri"`
	LastMod         string           `json:"last_mod,omitempty"`
	Priority        *float64         `json:"priority,omitempty"`
	ChangeFrequency *ChangeFrequency `json:"change_frequency,omitempty"`
	UpdatedByID     string           `json:"updated_by_id,omitempty"`
}

// GetSitemapItemsRequest holds filters for listing sitemap items.
type GetSitemapItemsRequest struct {
	Query    string `query:"query"`
	Page     int64  `query:"page"`
	PageSize int64  `query:"page_size"`
}

// GetSitemapItemByURIRequest holds data needed to retrieve a sitemap item.
type GetSitemapItemByURIRequest struct {
	URI string `query:"uri"`
}

// GetLatestSitemapItemByURIRegexRequest holds data needed to retrieve the latest sitemap item matching a URI regex.
type GetLatestSitemapItemByURIRegexRequest struct {
	URIRegex string `query:"uri_regex"`
}

// DeleteEntriesWithURIRegexRequest holds a safe Go regex used to delete matching sitemap item URIs.
type DeleteEntriesWithURIRegexRequest struct {
	URIRegex string `query:"uri_regex"`
}

// GenerateSitemapRequest holds options for generating sitemap XML.
type GenerateSitemapRequest struct {
	SaveToPaths []string `json:"save_to_paths" query:"save_to_paths"`
}

// DownloadSitemapByPathRequest holds options for downloading a sitemap file.
type DownloadSitemapByPathRequest struct {
	Path string `query:"path"`
}

// MassSitemapItemCreationByBatchRequest holds batch creation data.
type MassSitemapItemCreationByBatchRequest struct {
	Items            []CreateSitemapItemRequest `json:"items"`
	OverrideIfExists bool                       `json:"override_if_exists"`
}
