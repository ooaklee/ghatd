package seo

const (
	defaultIdentityTagSystem = "system"

	// APIV1Base is the start of the V1 API URI.
	APIV1Base = "/api/v1"

	// APISEOPrefix is the base URI prefix for SEO administration routes.
	APISEOPrefix = APIV1Base + "/seo"

	// PublicSitemapPath is the public route used by search engines.
	PublicSitemapPath = "/sitemap.xml"

	// SitemapItemsCollection is the MongoDB collection name for sitemap records.
	SitemapItemsCollection = "sitemap_items"
)

const (
	// ChangeFrequencyAlways is used for pages that change constantly.
	ChangeFrequencyAlways ChangeFrequency = "always"

	// ChangeFrequencyHourly is used for fast-moving pages.
	ChangeFrequencyHourly ChangeFrequency = "hourly"

	// ChangeFrequencyDaily is used for regularly updated pages.
	ChangeFrequencyDaily ChangeFrequency = "daily"

	// ChangeFrequencyWeekly is used for standard blogs or catalogues.
	ChangeFrequencyWeekly ChangeFrequency = "weekly"

	// ChangeFrequencyMonthly is used for static pages that occasionally change.
	ChangeFrequencyMonthly ChangeFrequency = "monthly"

	// ChangeFrequencyYearly is used for stable static pages.
	ChangeFrequencyYearly ChangeFrequency = "yearly"

	// ChangeFrequencyNever is used for pages that should not change.
	ChangeFrequencyNever ChangeFrequency = "never"
)

const (
	defaultSitemapPath          = "sitemap.xml"
	defaultSitemapWritableRoot  = "/tmp"
	defaultSitemapPriority      = 0.5
	defaultSitemapFilePerm      = 0o644
	defaultSitemapDirectoryPerm = 0o755
	maxSitemapURIRegexLength    = 256
)
