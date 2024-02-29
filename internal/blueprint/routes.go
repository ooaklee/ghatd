package blueprint

import (
	"fmt"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/router"
)

// blueprintHandler expected methods for valid blueprint handler
type blueprintHandler interface{}

const (
	// ApiV1Base the start of the V1 Api's URI
	ApiV1Base = "/v1"

	// ApiBlueprintPrefix base URI prefix for all blueprint routes
	ApiBlueprintPrefix = ApiV1Base + "/blueprint"
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
