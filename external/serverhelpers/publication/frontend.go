package publication

import (
	"io/fs"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/response"
	"github.com/ooaklee/ghatd/external/spa"
)

// newFrontendBootstrap creates an SPA bootstrap using content rooted at prefix
// and the supplied router middlewares. It configures the default health check
// handler and adds WebP files to the assets that bypass the SPA HTML fallback.
func newFrontendBootstrap(content fs.FS, prefix string, middlewares []mux.MiddlewareFunc) (*spa.Bootstrap, error) {
	paths, err := frontendDescriptionPaths(content, prefix)
	if err != nil {
		return nil, err
	}
	return spa.NewBootstrap(&spa.BootstrapRequest{
		EmbeddedContent:               content,
		EmbeddedContentFilePathPrefix: prefix,
		DefaultHealthcheckHandler:     response.GetDefault200Response,
		Middlewares:                   middlewares,
		// Both Vite assets and public images must bypass the SPA HTML fallback.
		HandleUpdatePathToIndexFunc: paths,
	})
}
