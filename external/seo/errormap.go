package seo

import "github.com/ooaklee/reply/v2"

// SitemapErrorMap maps SEO errors to HTTP responses.
var SitemapErrorMap = reply.ErrorManifest{
	ErrSitemapItemError:                  {Title: "Bad Request", Detail: "Sitemap request is invalid", StatusCode: 400, Code: "SEO0-001"},
	ErrSitemapItemDatabaseError:          {Title: "Internal Server Error", Detail: "Unable to complete sitemap database operation", StatusCode: 500, Code: "SEO0-002"},
	ErrSitemapItemResourceNotFound:       {Title: "Sitemap item not found", StatusCode: 404, Code: "SEO0-003"},
	ErrSitemapItemURIIsRequired:          {Title: "Bad Request", Detail: "Sitemap item URI is required", StatusCode: 400, Code: "SEO0-004"},
	ErrSitemapItemInvalidURI:             {Title: "Bad Request", Detail: "Sitemap item URI is invalid", StatusCode: 400, Code: "SEO0-005"},
	ErrSitemapItemInvalidChangeFrequency: {Title: "Bad Request", Detail: "Sitemap item change frequency is invalid", StatusCode: 400, Code: "SEO0-006"},
	ErrSitemapItemInvalidPriority:        {Title: "Bad Request", Detail: "Sitemap item priority is invalid", StatusCode: 400, Code: "SEO0-007"},
	ErrSitemapItemInvalidLastMod:         {Title: "Bad Request", Detail: "Sitemap item last_mod is invalid", StatusCode: 400, Code: "SEO0-008"},
	ErrSitemapPathIsInvalid:              {Title: "Bad Request", Detail: "Sitemap path is invalid", StatusCode: 400, Code: "SEO0-009"},
	ErrSitemapFrontendDomainIsRequired:   {Title: "Bad Request", Detail: "Sitemap frontend domain is required", StatusCode: 400, Code: "SEO0-010"},
}
