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

// AttachRoutes registers public policy documents in the shared inventory.
// Public routes still participate in registry validation and any authorizer;
// hosts must validate after all attachments and before serving.
func AttachRoutes(request *AttachRoutesRequest) {

	policyRoutes := request.Router.NewRouteGroup("/api/v1/policies", router.Public, nil)
	policyRoutes.Handle(router.RouteDefinition{Path: "", Operation: "policy.GetPolicies", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPolicies)
	policyRoutes.Handle(router.RouteDefinition{Path: "/{policyName}", Operation: "policy.GetPolicyByName", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetPolicyByName)

}
