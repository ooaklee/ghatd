// Package seo manages persisted sitemap entries, XML rendering, protected
// administration routes and public sitemap delivery. Public delivery defaults
// to safe file reads with optional embedded fallback; PublicSitemapModeDatabase
// opts into current shared storage and an optional PublicSitemapItemsFilter.
// Hosts own publication policy and should wrap response caching with
// NeverCachePublicSitemap when enabling live delivery. Administrative exports
// remain independent of the public source and host filter.
package seo
