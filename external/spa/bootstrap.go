package spa

import (
	"fmt"
	"io/fs"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// Bootstrap groups the SPA fallback handler and router created from the same
// embedded filesystem inputs.
type Bootstrap struct {
	Router  *router.Router
	Handler *Handler

	embeddedContentFilePathPrefix string
	handleUpdatePathToIndexFunc   func(r *http.Request) *http.Request
	spaFileSystem                 fs.FS
}

// BootstrapRequest holds the common SPA server wiring inputs.
type BootstrapRequest struct {
	EmbeddedContent               fs.FS
	EmbeddedContentFilePathPrefix string
	HandleUpdatePathToIndexFunc   func(r *http.Request) *http.Request
	DefaultHealthcheckHandler     func(w http.ResponseWriter, r *http.Request)
	Middlewares                   []mux.MiddlewareFunc
}

// NewBootstrap creates a router with the SPA 404 fallback and keeps enough
// state to attach the SPA static asset route later.
func NewBootstrap(request *BootstrapRequest) (*Bootstrap, error) {
	if request == nil {
		return nil, fmt.Errorf("spa/bootstrap-nil-request")
	}
	if request.EmbeddedContent == nil {
		return nil, fmt.Errorf("spa/bootstrap-missing-embedded-content")
	}

	handleUpdatePathToIndexFunc := request.HandleUpdatePathToIndexFunc
	if handleUpdatePathToIndexFunc == nil {
		handleUpdatePathToIndexFunc = NewHandleUpdatePathToIndex()
	}

	handler := NewSpaHandler(&NewSpaHandlerRequest{
		EmbeddedContent:               request.EmbeddedContent,
		EmbeddedContentFilePathPrefix: request.EmbeddedContentFilePathPrefix,
		HandleUpdatePathToIndexFunc:   handleUpdatePathToIndexFunc,
	})

	return &Bootstrap{
		Router: router.NewRouter(
			handler.GetResourceNotFoundError,
			request.DefaultHealthcheckHandler,
			request.Middlewares...,
		),
		Handler:                       handler,
		embeddedContentFilePathPrefix: request.EmbeddedContentFilePathPrefix,
		handleUpdatePathToIndexFunc:   handleUpdatePathToIndexFunc,
		spaFileSystem:                 request.EmbeddedContent,
	}, nil
}

// AttachRoutes attaches the SPA static asset route to the bootstrap router.
func (b *Bootstrap) AttachRoutes() error {
	if b == nil {
		return fmt.Errorf("spa/bootstrap-nil")
	}
	if b.Router == nil {
		return fmt.Errorf("spa/bootstrap-missing-router")
	}
	if b.spaFileSystem == nil {
		return fmt.Errorf("spa/bootstrap-missing-file-system")
	}

	return AttachRoutes(&AttachRoutesRequest{
		Router:                        b.Router,
		SpaFileSystem:                 b.spaFileSystem,
		EmbeddedContentFilePathPrefix: b.embeddedContentFilePathPrefix,
		HandleUpdatePathToIndexFunc:   b.handleUpdatePathToIndexFunc,
	})
}
