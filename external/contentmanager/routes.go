package contentmanager

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// contentManagerHandler holds methods for contentManager handler
type contentManagerHandler interface {
	// GetPosts(w http.ResponseWriter, r *http.Request)
	CreatePost(w http.ResponseWriter, r *http.Request)
	UpdatePostById(w http.ResponseWriter, r *http.Request)
	DeletePostById(w http.ResponseWriter, r *http.Request)
	RestorePostById(w http.ResponseWriter, r *http.Request)

	GetChangelogItems(w http.ResponseWriter, r *http.Request)
	GetChangelogItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request)

	GetGlossaryItems(w http.ResponseWriter, r *http.Request)
	GetFaqItems(w http.ResponseWriter, r *http.Request)
	GetArticles(w http.ResponseWriter, r *http.Request)
	GetArticleItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request)

	// GetArticleSitemapItems writes sitemap-ready article URL entries.
	GetArticleSitemapItems(w http.ResponseWriter, r *http.Request)

	GetLatestPostsByType(w http.ResponseWriter, r *http.Request)
	GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request)
}

// AttachRoutesRequest holds everything needed to attach contentManager
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid contentManager handler
	Handler contentManagerHandler

	// MiddlewareAdminApiTokenOrJwtRequired middleware used to lock endpoints down to admin only
	MiddlewareAdminApiTokenOrJwtRequired mux.MiddlewareFunc

	// RateLimitOrActiveMiddleware middleware used to open endpoints up (with rate limite) or active users only
	RateLimitOrActiveMiddleware mux.MiddlewareFunc

	// MiddlewareValidApiTokenOrJWTMiddleware is retained for source compatibility;
	// no routes in this attachment consume it.
	MiddlewareValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers administrative writes and optional-active reads.
// Both authentication adapters are required. Validate the registry before
// serving; an absent adapter fails closed, including on public read branches.
func AttachRoutes(request *AttachRoutesRequest) {

	contentManagerAdminOnlyRoutes := request.Router.NewRouteGroup("/api/v1/cms", router.AdminSessionOrAPI, request.MiddlewareAdminApiTokenOrJwtRequired)
	contentManagerAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/posts", Operation: "contentmanager.CreatePost", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreatePost)
	contentManagerAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/posts/{postId}", Operation: "contentmanager.UpdatePostById", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdatePostById)
	contentManagerAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/posts/{postId}", Operation: "contentmanager.DeletePostById", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeletePostById)
	contentManagerAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/posts/{postId}/restore", Operation: "contentmanager.RestorePostById", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.RestorePostById)
	contentManagerAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/seo/posts/articles/sitemap-items", Operation: "contentmanager.GetArticleSitemapItems", Methods: []string{http.MethodGet, http.MethodPost, http.MethodOptions}}, request.Handler.GetArticleSitemapItems)

	contentManagerOpenRoutes := request.Router.NewRouteGroup("/api/v1/cms", router.OptionalActive, request.RateLimitOrActiveMiddleware)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/changelog", Operation: "contentmanager.GetChangelogItems", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetChangelogItems)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/changelog/{urlFriendlyId}", Operation: "contentmanager.GetChangelogItemByUrlFriendlyId", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetChangelogItemByUrlFriendlyId)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/glossary", Operation: "contentmanager.GetGlossaryItems", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetGlossaryItems)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/faq", Operation: "contentmanager.GetFaqItems", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetFaqItems)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/articles", Operation: "contentmanager.GetArticles", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetArticles)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/articles/{urlFriendlyId}", Operation: "contentmanager.GetArticleItemByUrlFriendlyId", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetArticleItemByUrlFriendlyId)
	contentManagerOpenRoutes.Handle(router.RouteDefinition{Path: "/latest", Operation: "contentmanager.GetLatestNotificationOverviews", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetLatestNotificationOverviews)

}
