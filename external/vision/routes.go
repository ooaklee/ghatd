package vision

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

type visionHandler interface {
	CreateVision(w http.ResponseWriter, r *http.Request)
	GetVisionByNanoID(w http.ResponseWriter, r *http.Request)
	GetVisions(w http.ResponseWriter, r *http.Request)
	UpdateVision(w http.ResponseWriter, r *http.Request)
	UpdateVisionStatus(w http.ResponseWriter, r *http.Request)
	SetVisionVote(w http.ResponseWriter, r *http.Request)
	RemoveVisionVote(w http.ResponseWriter, r *http.Request)
	AddVisionComment(w http.ResponseWriter, r *http.Request)
	SetVisionCommentVote(w http.ResponseWriter, r *http.Request)
	RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request)
	DeleteVision(w http.ResponseWriter, r *http.Request)
	GetVisionConfig(w http.ResponseWriter, r *http.Request)
}

const (
	// APIVisionV1Prefix is the base URI for raw vision routes.
	APIVisionV1Prefix = common.ApiV1UriPrefix + "/visions"
)

// AttachRoutesRequest holds dependencies needed to attach vision routes.
type AttachRoutesRequest struct {
	Router                  *router.Router
	Handler                 visionHandler
	AdminOnlyMiddleware     mux.MiddlewareFunc
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches admin management and authenticated interaction routes.
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	adminRoutes := httpRouter.PathPrefix(APIVisionV1Prefix).Subrouter()
	adminRoutes.HandleFunc("/config", request.Handler.GetVisionConfig).Methods(http.MethodGet, http.MethodOptions)
	adminRoutes.HandleFunc("/{visionNanoID}", request.Handler.UpdateVision).Methods(http.MethodPatch, http.MethodOptions)
	adminRoutes.HandleFunc("/{visionNanoID}/status", request.Handler.UpdateVisionStatus).Methods(http.MethodPatch, http.MethodOptions)
	adminRoutes.HandleFunc("/{visionNanoID}", request.Handler.DeleteVision).Methods(http.MethodDelete, http.MethodOptions)
	if request.AdminOnlyMiddleware != nil {
		adminRoutes.Use(request.AdminOnlyMiddleware)
	}

	authenticatedRoutes := httpRouter.PathPrefix(APIVisionV1Prefix).Subrouter()
	authenticatedRoutes.HandleFunc("", request.Handler.CreateVision).Methods(http.MethodPost, http.MethodOptions)
	authenticatedRoutes.HandleFunc("", request.Handler.GetVisions).Methods(http.MethodGet, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}", request.Handler.GetVisionByNanoID).Methods(http.MethodGet, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}/votes", request.Handler.SetVisionVote).Methods(http.MethodPut, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}/votes", request.Handler.RemoveVisionVote).Methods(http.MethodDelete, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}/comments", request.Handler.AddVisionComment).Methods(http.MethodPost, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}/comments/{commentID}/votes", request.Handler.SetVisionCommentVote).Methods(http.MethodPut, http.MethodOptions)
	authenticatedRoutes.HandleFunc("/{visionNanoID}/comments/{commentID}/votes", request.Handler.RemoveVisionCommentVote).Methods(http.MethodDelete, http.MethodOptions)
	if request.AuthenticatedMiddleware != nil {
		authenticatedRoutes.Use(request.AuthenticatedMiddleware)
	}
}
