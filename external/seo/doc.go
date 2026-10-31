// Package seo manages independent sitemap entries, XML rendering, protected
// administration routes and public sitemap delivery. Optional PublishedAt dates
// gate generated output using one evaluation instant; undated entries remain
// immediately eligible. CMS changes do not synchronize SEO entries.
//
// Publication-aware repositories implement SitemapPublicationRepository for
// atomic metadata/date writes: nil preserves the date, while an explicit empty
// string clears it. Legacy adapters remain supported for unscheduled entries.
// SitemapPublicationSource adds bounded, read-only ID keyset inventory for shared
// due eligibility; PublicationTime uses the same strict gate parser as rendering.
//
// Public delivery defaults to file snapshots with optional embedded fallback;
// PublicSitemapModeDatabase renders current shared storage on each request.
// Hosts own crawler/build restrictions through PublicSitemapItemsFilter and
// should wrap response caching with NeverCachePublicSitemap for live delivery.
// Administrative generation shares publication gating but not the host filter;
// downloads read saved snapshots. Invalid stored dates fail generation, and live
// failures return a generic 503 without stale-file fallback.
package seo
