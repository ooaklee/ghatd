package seo

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// sitemapHandler expected methods for valid sitemap handler.
type sitemapHandler interface {
	// CreateSitemapItem serves the HTTP endpoint for sitemap item creation, writing
	// a 201 data response or an error response after delegating to the service's
	// create-if-absent logic.
	CreateSitemapItem(w http.ResponseWriter, r *http.Request)
	// DeleteEntriesWithUriRegex serves the HTTP endpoint that deletes sitemap
	// entries whose URI matches a request-supplied regex, writing a data or error
	// response from the service result.
	DeleteEntriesWithUriRegex(w http.ResponseWriter, r *http.Request)
	// DownloadSitemapByPath serves the admin HTTP endpoint that downloads the
	// sitemap file at the requested path.
	DownloadSitemapByPath(w http.ResponseWriter, r *http.Request)
	// GenerateSitemap serves the HTTP endpoint for XML generation with optional
	// file saves, writing a data or error response from the service result.
	GenerateSitemap(w http.ResponseWriter, r *http.Request)
	// GetSitemap serves the public HTTP endpoint returning the sitemap file, per
	// the sitemapHandler contract.
	GetSitemap(w http.ResponseWriter, r *http.Request)
	// GetSitemapItems serves the HTTP endpoint listing sitemap items, writing a
	// data or error response from the service result.
	GetSitemapItems(w http.ResponseWriter, r *http.Request)
	// MassSitemapItemCreationByBatch serves HTTP batch sitemap item creation,
	// mapping the request and delegating to the service; the implementation creates
	// items sequentially with optional override, responding 201 with the
	// created/updated/skipped summary.
	MassSitemapItemCreationByBatch(w http.ResponseWriter, r *http.Request)
	// UpdateSitemapItemByUri serves HTTP updates of sitemap items by URI,
	// delegating to the service which patches mutable fields and validates before
	// persisting, responding 200 with the updated item.
	UpdateSitemapItemByUri(w http.ResponseWriter, r *http.Request)
}

// AttachRoutesRequest holds everything needed to attach SEO routes.
type AttachRoutesRequest struct {
	// Router records the public sitemap and protected administration inventory.
	Router *router.Router
	// Handler owns sitemap operations; route metadata does not authorize storage.
	Handler sitemapHandler
	// AdminOnlyMiddleware must authenticate and authorize an administrator.
	// Missing middleware invalidates the registry rather than exposing these routes.
	AdminOnlyMiddleware mux.MiddlewareFunc
	// AdminAccess declares the supplied middleware's credential boundary. Empty
	// means AdminSession; AdminSessionOrAPI explicitly permits the host's admin
	// API-token adapter. Any other value invalidates registration. This metadata
	// never substitutes for enforcing middleware.
	AdminAccess router.AccessMode
}

// AttachRoutes preserves sitemap paths, methods and registration order while
// registering their access modes. Hosts must check ValidateRoutePolicies after
// attaching all routes and before serving; an invalid registry fails closed.
func AttachRoutes(request *AttachRoutesRequest) {
	public := request.Router.NewRouteGroup(PublicSitemapPath, router.Public, nil)
	public.Handle(router.RouteDefinition{Path: "", Operation: "seo.GetSitemap", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetSitemap)

	access := request.AdminAccess
	switch access {
	case "":
		access = router.AdminSession
	case router.AdminSession, router.AdminSessionOrAPI:
	default:
		access = "" // Unknown mode triggers the registry's closed backstop.
	}
	adminRoutes := request.Router.NewRouteGroup(APISEOPrefix, access, request.AdminOnlyMiddleware)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap-items", Operation: "seo.CreateSitemapItem", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateSitemapItem)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap-items", Operation: "seo.GetSitemapItems", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetSitemapItems)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap-items", Operation: "seo.UpdateSitemapItemByUri", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateSitemapItemByUri)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap-items", Operation: "seo.DeleteEntriesWithUriRegex", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteEntriesWithUriRegex)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap-items/batch", Operation: "seo.MassSitemapItemCreationByBatch", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.MassSitemapItemCreationByBatch)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap.xml/generate", Operation: "seo.GenerateSitemap", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.GenerateSitemap)
	adminRoutes.Handle(router.RouteDefinition{Path: "/sitemap.xml/download", Operation: "seo.DownloadSitemapByPath", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.DownloadSitemapByPath)
}
