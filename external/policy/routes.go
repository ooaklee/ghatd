package policy

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/router"
)

// policyHandler expected methods for valid policy handler
type policyHandler interface {
	GetPolicies(w http.ResponseWriter, r *http.Request)
	GetPolicyByName(w http.ResponseWriter, r *http.Request)
}

// AttachRoutesRequest holds everything needed to attach policy
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid policy handler
	Handler policyHandler
}

// AttachRoutes attaches policy handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	policyRoutes := httpRouter.PathPrefix("/api/v1/policies").Subrouter()
	policyRoutes.HandleFunc("", request.Handler.GetPolicies).Methods(http.MethodGet, http.MethodOptions)
	policyRoutes.HandleFunc("/{policyName}", request.Handler.GetPolicyByName).Methods(http.MethodGet, http.MethodOptions)

}
