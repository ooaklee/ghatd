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
	// UpdatePostById serves the HTTP request to update a post by its ID, mapping
	// and validating the request before delegating to the service and writing the
	// updated post response.
	UpdatePostById(w http.ResponseWriter, r *http.Request)
	// DeletePostById serves the HTTP request to delete a post by its ID, mapping
	// and validating the request before delegating to the service and writing a
	// no-content response.
	DeletePostById(w http.ResponseWriter, r *http.Request)
	// RestorePostById serves the HTTP request to restore a previously deleted post
	// by its ID, delegating to the service and writing the restored post response.
	RestorePostById(w http.ResponseWriter, r *http.Request)

	// GetChangelogItems serves the HTTP request for changelog posts, delegating to
	// the service and writing the matching posts, optionally including metadata.
	GetChangelogItems(w http.ResponseWriter, r *http.Request)
	// GetChangelogItemByUrlFriendlyId serves the HTTP request to fetch a single
	// changelog item by its URL-friendly identifier, delegating to the service and
	// writing the post.
	GetChangelogItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request)

	// GetGlossaryItems serves the HTTP request for glossary posts, delegating to
	// the service and writing the matching posts, optionally including metadata.
	GetGlossaryItems(w http.ResponseWriter, r *http.Request)
	// GetFaqItems serves the HTTP request for FAQ posts, delegating to the service
	// and writing the matching posts, optionally including metadata.
	GetFaqItems(w http.ResponseWriter, r *http.Request)
	// GetArticles serves the HTTP request for article posts, delegating to the
	// service and writing the matching posts, optionally including metadata.
	GetArticles(w http.ResponseWriter, r *http.Request)
	// GetArticleItemByUrlFriendlyId serves the HTTP request to fetch a single
	// article by its URL-friendly identifier, delegating to the service and writing
	// the post.
	GetArticleItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request)

	// GetArticleSitemapItems writes sitemap-ready article URL entries.
	GetArticleSitemapItems(w http.ResponseWriter, r *http.Request)

	// GetLatestPostsByType serves the HTTP request for the latest post overviews of
	// a given type, delegating to the service and writing the overviews.
	GetLatestPostsByType(w http.ResponseWriter, r *http.Request)
	// GetLatestNotificationOverviews serves the HTTP request for the latest
	// notification overviews, delegating to the service and writing the overviews
	// or an empty list.
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
