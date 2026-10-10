package vision

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// visionHandler is the HTTP handler surface the route registrar depends on, one
// method per vision endpoint.
type visionHandler interface {
	// CreateVision serves the vision creation HTTP endpoint, mapping the request
	// and responding with the created vision or an error response.
	CreateVision(w http.ResponseWriter, r *http.Request)
	// GetVisionByNanoID serves the HTTP endpoint that fetches a single vision by
	// its public NanoID, responding with the vision or an error response.
	GetVisionByNanoID(w http.ResponseWriter, r *http.Request)
	// GetVisions serves the vision listing endpoint of the visionHandler route
	// contract; the handler implementation maps the HTTP request, calls the service
	// for a filtered page with metadata, and writes the results to w.
	GetVisions(w http.ResponseWriter, r *http.Request)
	// UpdateVision serves the vision descriptive-update endpoint of the
	// visionHandler route contract; the handler implementation maps the request,
	// delegates to the service, and writes the updated vision to w.
	UpdateVision(w http.ResponseWriter, r *http.Request)
	// UpdateVisionStatus serves the roadmap status transition endpoint of the
	// visionHandler route contract; the handler implementation maps the request,
	// delegates validation and persistence to the service, and writes the updated
	// vision to w.
	UpdateVisionStatus(w http.ResponseWriter, r *http.Request)
	// SetVisionVote serves the endpoint of the visionHandler route contract that
	// sets or changes the requestor's vote on a vision; the handler implementation
	// maps the request, delegates to the service, and writes the updated vision to
	// w.
	SetVisionVote(w http.ResponseWriter, r *http.Request)
	// RemoveVisionVote serves the endpoint of the visionHandler route contract that
	// removes the requestor's vote on a vision; the handler implementation maps the
	// request, delegates to the service, and writes the updated vision to w.
	RemoveVisionVote(w http.ResponseWriter, r *http.Request)
	// AddVisionComment serves the endpoint of the visionHandler route contract that
	// appends a comment to a vision; the handler implementation maps the request,
	// delegates to the service, and writes a created response to w.
	AddVisionComment(w http.ResponseWriter, r *http.Request)
	// SetVisionCommentVote serves the endpoint of the visionHandler route contract
	// that sets or changes the requestor's vote on a vision comment; the handler
	// implementation maps the request, delegates to the service, and writes the
	// updated vision to w.
	SetVisionCommentVote(w http.ResponseWriter, r *http.Request)
	// RemoveVisionCommentVote serves the endpoint of the visionHandler route
	// contract that removes the requestor's vote on a vision comment; the handler
	// implementation maps the request, delegates to the service, and writes the
	// updated vision to w.
	RemoveVisionCommentVote(w http.ResponseWriter, r *http.Request)
	// DeleteVision serves the vision deletion endpoint of the visionHandler route
	// contract; the handler implementation maps the request, delegates
	// NanoID-addressed deletion to the service, and writes the deletion response to
	// w.
	DeleteVision(w http.ResponseWriter, r *http.Request)
	// GetVisionConfig serves the endpoint of the visionHandler route contract
	// returning client-safe vision configuration; the handler implementation calls
	// the service and writes the capabilities payload to w.
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
