package response

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ooaklee/reply"
)

const (
	ErrKeyResourceNotFound = "DefaultResourceNotFound"
)

var defaultErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyResourceNotFound: {Title: "Resource not found.", StatusCode: 404},
}

// GetResourceNotFoundError returns default 404 response
func GetResourceNotFoundError(w http.ResponseWriter, r *http.Request) {
	replier := reply.NewReplier(append([]reply.ErrorManifest{}, defaultErrorMap))

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		//nolint will set up default fallback later
		replier.NewHTTPErrorResponse(w, errors.New(ErrKeyResourceNotFound))
		return
	}

	http.Error(w, "Not Found", http.StatusNotFound)
}

// GetDefault200Response returns default 200 response to be used
// in cases such as Healthchecks.
//
// TODO: Consider swapping out for https://github.com/etherlabsio/healthcheck at
// a later date
func GetDefault200Response(w http.ResponseWriter, r *http.Request) {
	replier := reply.NewReplier([]reply.ErrorManifest{})

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		//nolint will set up default fallback later
		replier.NewHTTPBlankResponse(w, 200)
		return
	}

	w.WriteHeader(200)
	w.Write([]byte("OK"))
}
