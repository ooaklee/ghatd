package seo

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// sitemapHandler expected methods for valid sitemap handler.
type sitemapHandler interface {
	CreateSitemapItem(w http.ResponseWriter, r *http.Request)
	DeleteEntriesWithUriRegex(w http.ResponseWriter, r *http.Request)
	DownloadSitemapByPath(w http.ResponseWriter, r *http.Request)
	GenerateSitemap(w http.ResponseWriter, r *http.Request)
	GetSitemap(w http.ResponseWriter, r *http.Request)
	GetSitemapItems(w http.ResponseWriter, r *http.Request)
	MassSitemapItemCreationByBatch(w http.ResponseWriter, r *http.Request)
	UpdateSitemapItemByUri(w http.ResponseWriter, r *http.Request)
}

// AttachRoutesRequest holds everything needed to attach SEO routes.
type AttachRoutesRequest struct {
	Router              *router.Router
	Handler             sitemapHandler
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches sitemap handler routes to the router.
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	httpRouter.HandleFunc(PublicSitemapPath, request.Handler.GetSitemap).Methods(http.MethodGet, http.MethodOptions)

	adminRoutes := httpRouter.PathPrefix(APISEOPrefix).Subrouter()
	adminRoutes.HandleFunc("/sitemap-items", request.Handler.CreateSitemapItem).Methods(http.MethodPost, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap-items", request.Handler.GetSitemapItems).Methods(http.MethodGet, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap-items", request.Handler.UpdateSitemapItemByUri).Methods(http.MethodPatch, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap-items", request.Handler.DeleteEntriesWithUriRegex).Methods(http.MethodDelete, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap-items/batch", request.Handler.MassSitemapItemCreationByBatch).Methods(http.MethodPost, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap.xml/generate", request.Handler.GenerateSitemap).Methods(http.MethodPost, http.MethodOptions)
	adminRoutes.HandleFunc("/sitemap.xml/download", request.Handler.DownloadSitemapByPath).Methods(http.MethodGet, http.MethodOptions)

	if request.AdminOnlyMiddleware != nil {
		adminRoutes.Use(request.AdminOnlyMiddleware)
	}
}
