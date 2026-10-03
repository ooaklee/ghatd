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
	// APIVisionV1Prefix is the base URI for vision domain routes.
	APIVisionV1Prefix = common.ApiV1UriPrefix + "/visions"
)

// AttachRoutesRequest holds dependencies needed to attach vision routes.
type AttachRoutesRequest struct {
	// Router receives ordered descriptors and must be validated before serving.
	Router *router.Router
	// Handler owns domain authorization and persistence after HTTP admission.
	Handler visionHandler
	// AdminOnlyMiddleware verifies an active administrator session.
	AdminOnlyMiddleware mux.MiddlewareFunc
	// AuthenticatedMiddleware verifies a user session for interaction routes.
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches admin management and authenticated interaction routes.
func AttachRoutes(request *AttachRoutesRequest) {

	adminRoutes := request.Router.NewRouteGroup(APIVisionV1Prefix, router.AdminSession, request.AdminOnlyMiddleware)
	adminRoutes.Handle(router.RouteDefinition{Path: "/config", Operation: "vision.GetVisionConfig", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisionConfig)
	adminRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}", Operation: "vision.UpdateVision", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateVision)
	adminRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/status", Operation: "vision.UpdateVisionStatus", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateVisionStatus)
	adminRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}", Operation: "vision.DeleteVision", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteVision)

	authenticatedRoutes := request.Router.NewRouteGroup(APIVisionV1Prefix, router.Session, request.AuthenticatedMiddleware)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "", Operation: "vision.CreateVision", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateVision)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "", Operation: "vision.GetVisions", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisions)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}", Operation: "vision.GetVisionByNanoID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetVisionByNanoID)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/votes", Operation: "vision.SetVisionVote", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.SetVisionVote)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/votes", Operation: "vision.RemoveVisionVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveVisionVote)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/comments", Operation: "vision.AddVisionComment", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AddVisionComment)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/comments/{commentID}/votes", Operation: "vision.SetVisionCommentVote", Methods: []string{http.MethodPut, http.MethodOptions}}, request.Handler.SetVisionCommentVote)
	authenticatedRoutes.Handle(router.RouteDefinition{Path: "/{visionNanoID}/comments/{commentID}/votes", Operation: "vision.RemoveVisionCommentVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveVisionCommentVote)

}
