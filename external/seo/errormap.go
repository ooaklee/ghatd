package seo

import "github.com/ooaklee/reply/v2"

// SitemapErrorMap maps SEO errors to HTTP responses.
var SitemapErrorMap = reply.ErrorManifest{
	ErrSitemapItemError:                  {Title: "Bad Request", Detail: "Sitemap request is invalid", StatusCode: 400},
	ErrSitemapItemDatabaseError:          {Title: "Internal Server Error", Detail: "Unable to complete sitemap database operation", StatusCode: 500},
	ErrSitemapItemResourceNotFound:       {Title: "Sitemap item not found", StatusCode: 404},
	ErrSitemapItemURIIsRequired:          {Title: "Bad Request", Detail: "Sitemap item URI is required", StatusCode: 400},
	ErrSitemapItemInvalidURI:             {Title: "Bad Request", Detail: "Sitemap item URI is invalid", StatusCode: 400},
	ErrSitemapItemInvalidChangeFrequency: {Title: "Bad Request", Detail: "Sitemap item change frequency is invalid", StatusCode: 400},
	ErrSitemapItemInvalidPriority:        {Title: "Bad Request", Detail: "Sitemap item priority is invalid", StatusCode: 400},
	ErrSitemapItemInvalidLastMod:         {Title: "Bad Request", Detail: "Sitemap item last_mod is invalid", StatusCode: 400},
	ErrSitemapPathIsInvalid:              {Title: "Bad Request", Detail: "Sitemap path is invalid", StatusCode: 400},
	ErrSitemapFrontendDomainIsRequired:   {Title: "Bad Request", Detail: "Sitemap frontend domain is required", StatusCode: 400},
}
