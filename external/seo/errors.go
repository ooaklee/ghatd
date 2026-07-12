package seo

import "errors"

var (
	// ErrSitemapItemError is returned for general sitemap item failures.
	ErrSitemapItemError = errors.New("sitemap-item-request-is-invalid")

	// ErrSitemapItemDatabaseError is returned when sitemap persistence fails.
	ErrSitemapItemDatabaseError = errors.New("sitemap-item-database-operation-failed")

	// ErrSitemapItemResourceNotFound is returned when the requested sitemap item cannot be found.
	ErrSitemapItemResourceNotFound = errors.New("sitemap-item-resource-not-found")

	// ErrSitemapItemURIIsRequired is returned when a sitemap item URI is missing.
	ErrSitemapItemURIIsRequired = errors.New("sitemap-item-uri-is-required")

	// ErrSitemapItemInvalidURI is returned when a sitemap item URI is malformed.
	ErrSitemapItemInvalidURI = errors.New("sitemap-item-uri-is-invalid")

	// ErrSitemapItemInvalidChangeFrequency is returned when change_frequency is not supported.
	ErrSitemapItemInvalidChangeFrequency = errors.New("sitemap-item-change-frequency-is-invalid")

	// ErrSitemapItemInvalidPriority is returned when priority is outside sitemap bounds.
	ErrSitemapItemInvalidPriority = errors.New("sitemap-item-priority-is-invalid")

	// ErrSitemapItemInvalidLastMod is returned when last_mod cannot be parsed.
	ErrSitemapItemInvalidLastMod = errors.New("sitemap-item-last-mod-is-invalid")

	// ErrSitemapPathIsInvalid is returned when a sitemap file path is unsafe.
	ErrSitemapPathIsInvalid = errors.New("sitemap-path-is-invalid")

	// ErrSitemapFrontendDomainIsRequired is returned when URL generation has no frontend domain.
	ErrSitemapFrontendDomainIsRequired = errors.New("sitemap-frontend-domain-is-required")
)
