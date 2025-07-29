package spa

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"github.com/ooaklee/reply"
)

// Handler manages request for spa
type Handler struct {
	embeddedFileSystem            fs.FS
	embeddedContentFilePathPrefix string
	spaUpdatePathToIndexFunc      func(r *http.Request) *http.Request
}

// NewSpaHandlerRequest is the request needed to create a spa handler
type NewSpaHandlerRequest struct {
	// EmbeddedContent the embedded content to serve
	EmbeddedContent fs.FS
	// EmbeddedContentFilePathPrefix the prefix used to access the embedded files
	EmbeddedContentFilePathPrefix string

	// HandleUpdatePathToIndexFunc is the function that handles updating
	// request path that should be sent to the / path
	HandleUpdatePathToIndexFunc func(r *http.Request) *http.Request
}

// NewSpaHandler creates and returns a new Handler for serving web application content
// using the provided embedded file system and content file path prefix.
func NewSpaHandler(request *NewSpaHandlerRequest) *Handler {
	return &Handler{
		embeddedFileSystem:            request.EmbeddedContent,
		embeddedContentFilePathPrefix: request.EmbeddedContentFilePathPrefix,
		spaUpdatePathToIndexFunc:      request.HandleUpdatePathToIndexFunc,
	}
}

// GetResourceNotFoundError handles requests for non-existent resources by either returning a JSON error response
// or serving the default index page from the embedded dist directory. For JSON requests, it returns a 404 error,
// while for other content types, it serves the root index page from the static assets.
func (h *Handler) GetResourceNotFoundError(w http.ResponseWriter, r *http.Request) {
	replier := reply.NewReplier(append([]reply.ErrorManifest{}, defaultErrorMap))

	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		//nolint will set up default fallback later
		replier.NewHTTPErrorResponse(w, errors.New(ErrKeyResourceNotFound))
		return
	}

	// Create filesystem only holding dist dir assets
	distDirFS, err := fs.Sub(h.embeddedFileSystem, fmt.Sprintf("%sdist", h.embeddedContentFilePathPrefix))
	if err != nil {
		log.Default().Panicln("unable-to-create-file-system-for-static-assets", err)
		return
	}

	// Reset the request URL path to the root ("/") to
	// serve the default index page for non-existent resources
	r = h.spaUpdatePathToIndexFunc(r)

	http.FileServer(http.FS(distDirFS)).ServeHTTP(w, r)
}

const (
	// ErrKeyResourceNotFound is the key used for the resource not found error
	ErrKeyResourceNotFound = "DefaultResourceNotFound"
)

var defaultErrorMap = map[string]reply.ErrorManifestItem{
	// DefaultResourceNotFound is the default resource not found error
	ErrKeyResourceNotFound: {Title: "Resource not found.", StatusCode: 404},
}
