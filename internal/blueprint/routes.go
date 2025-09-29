package blueprint

import (
	"fmt"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
)

// blueprintHandler expected methods for valid blueprint handler
type blueprintHandler interface{}

const (
	// ApiBlueprintPrefix base URI prefix for all blueprint routes
	ApiBlueprintPrefix = common.ApiV1UriPrefix + "/blueprint"
)

var (
	// ApiBlueprintIdVariable URI variable used to get blueprint Id out of URI
	ApiBlueprintIdVariable = fmt.Sprintf("/{%s}", BlueprintURIVariableId)
)

// AttachRoutesRequest holds everything needed to attach blueprint
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid blueprint handler
	Handler blueprintHandler
}

// AttachRoutes attaches blueprint handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	blueprintRoutes := httpRouter.PathPrefix(ApiBlueprintPrefix).Subrouter()

	//nolint Remove when implemented route, to stop error
	if blueprintRoutes.KeepContext {
		fmt.Print("Remove me...")
	}

}
