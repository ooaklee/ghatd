package seo

import "errors"

var (
	// ErrPublicSitemapUnavailable wraps failures to read, filter or render the
	// live public sitemap. HTTP responses omit the diagnostic cause.
	ErrPublicSitemapUnavailable = errors.New("public-sitemap-unavailable")

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

	// ErrSitemapItemInvalidPublishedAt is returned when a supplied or stored
	// published_at value is malformed or a zero timestamp.
	ErrSitemapItemInvalidPublishedAt = errors.New("sitemap-item-published-at-is-invalid")

	// ErrSitemapItemPublicationScheduleUnsupported is returned when an explicit
	// publication mutation (set, clear or schedule-preserving override) cannot
	// be honoured atomically by the configured repository. Mutations fail
	// closed instead of risking overwriting a known scheduled state.
	ErrSitemapItemPublicationScheduleUnsupported = errors.New("sitemap-item-publication-schedule-unsupported")

	// ErrSitemapPathIsInvalid is returned when a sitemap file path is unsafe.
	ErrSitemapPathIsInvalid = errors.New("sitemap-path-is-invalid")

	// ErrSitemapFrontendDomainIsRequired is returned when URL generation has no frontend domain.
	ErrSitemapFrontendDomainIsRequired = errors.New("sitemap-frontend-domain-is-required")
)
