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

	GetLatestPostsByType(w http.ResponseWriter, r *http.Request)
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

	// MiddlewareValidApiTokenOrJWTMiddleware middleware used to lock endpoints down to valid users only
	MiddlewareValidApiTokenOrJWTMiddleware mux.MiddlewareFunc
}

// AttachRoutes handles attaching contentManager routes to router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	contentManagerAdminOnlyRoutes := httpRouter.PathPrefix("/api/v1/cms").Subrouter()
	contentManagerAdminOnlyRoutes.HandleFunc("/posts", request.Handler.CreatePost).Methods(http.MethodPost, http.MethodOptions)
	contentManagerAdminOnlyRoutes.HandleFunc("/posts/{postId}", request.Handler.UpdatePostById).Methods(http.MethodPatch, http.MethodOptions)
	contentManagerAdminOnlyRoutes.HandleFunc("/posts/{postId}", request.Handler.DeletePostById).Methods(http.MethodDelete, http.MethodOptions)
	contentManagerAdminOnlyRoutes.HandleFunc("/posts/{postId}/restore", request.Handler.RestorePostById).Methods(http.MethodPatch, http.MethodOptions)
	contentManagerAdminOnlyRoutes.Use(request.MiddlewareAdminApiTokenOrJwtRequired)

	contentManagerValidUserOnlyRoutes := httpRouter.PathPrefix("/api/v1/cms").Subrouter()
	contentManagerValidUserOnlyRoutes.Use(request.MiddlewareValidApiTokenOrJWTMiddleware)

	contentManagerOpenRoutes := httpRouter.PathPrefix("/api/v1/cms").Subrouter()
	contentManagerOpenRoutes.HandleFunc("/changelog", request.Handler.GetChangelogItems).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/changelog/{urlFriendlyId}", request.Handler.GetChangelogItemByUrlFriendlyId).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/glossary", request.Handler.GetGlossaryItems).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/faq", request.Handler.GetFaqItems).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/articles", request.Handler.GetArticles).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/articles/{urlFriendlyId}", request.Handler.GetArticleItemByUrlFriendlyId).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.HandleFunc("/latest", request.Handler.GetLatestPostsByType).Methods(http.MethodGet, http.MethodOptions)
	contentManagerOpenRoutes.Use(request.RateLimitOrActiveMiddleware)
}
