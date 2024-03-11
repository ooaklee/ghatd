package rememberer

import (
	"fmt"
	"net/http"

	"github.com/ooaklee/ghatd/internal/common"
	"github.com/ooaklee/ghatd/internal/router"
)

// remembererHandler expected methods for valid rememberer handler
type remembererHandler interface {
	DeleteWord(w http.ResponseWriter, r *http.Request)
	CreateWord(w http.ResponseWriter, r *http.Request)
	GetWords(w http.ResponseWriter, r *http.Request)
	GetWordById(w http.ResponseWriter, r *http.Request)
}

const (
	// ApiRemembererPrefix base URI prefix for all rememberer routes
	ApiRemembererPrefix = common.ApiV1UriPrefix + "/rememberer"

	// ApiWordsVariable URI variable used to get actions words
	ApiWordsVariable = "/words"
)

var (
	// ApiRemembererWordIdVariable URI variable used to get rememberer Id out of URI
	ApiRemembererWordIdVariable = fmt.Sprintf("/{%s}", RemembererWordURIVariableId)

	// ApiRemembererSpecificWordIdUriPath the URI path for actioning a specific word
	ApiRemembererSpecificWordIdUriPath = fmt.Sprintf("%s%s", ApiWordsVariable, ApiRemembererWordIdVariable)
)

// AttachRoutesRequest holds everything needed to attach rememberer
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by Api
	Router *router.Router

	// Handler valid rememberer handler
	Handler remembererHandler
}

// AttachRoutes attaches rememberer handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	remembererRoutes := httpRouter.PathPrefix(ApiRemembererPrefix).Subrouter()

	remembererRoutes.HandleFunc(ApiWordsVariable, request.Handler.GetWords).Methods(http.MethodGet, http.MethodOptions)
	remembererRoutes.HandleFunc(ApiWordsVariable, request.Handler.CreateWord).Methods(http.MethodPost, http.MethodOptions)
	remembererRoutes.HandleFunc(ApiRemembererSpecificWordIdUriPath, request.Handler.GetWordById).Methods(http.MethodGet, http.MethodOptions)
	remembererRoutes.HandleFunc(ApiRemembererSpecificWordIdUriPath, request.Handler.DeleteWord).Methods(http.MethodDelete, http.MethodOptions)

}
