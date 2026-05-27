package spa

import (
	"fmt"
	"io/fs"
	"net/http"
	"regexp"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	"go.uber.org/zap"
)

// SpaHandler expected methods for valid spa handler
type SpaHandler interface {
}

// AttachRoutesRequest holds everything needed to attach spa
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by server
	Router *router.Router

	// SpaFileSystem the file system that holds files utilised
	// by the spa
	SpaFileSystem fs.FS

	// EmbeddedContentFilePathPrefix the prefix used to access the embedded files
	EmbeddedContentFilePathPrefix string

	// HandleUpdatePathToIndexFunc is the function that handles updating
	// request path that should be sent to the / path
	HandleUpdatePathToIndexFunc func(r *http.Request) *http.Request
}

// AttachRoutes attaches spa handler to corresponding routes on router.
func AttachRoutes(request *AttachRoutesRequest) error {
	if request == nil {
		return fmt.Errorf("spa/attach-routes-nil-request")
	}
	if request.Router == nil {
		return fmt.Errorf("spa/attach-routes-missing-router")
	}
	if request.Router.GetRouter() == nil {
		return fmt.Errorf("spa/attach-routes-missing-http-router")
	}
	if request.SpaFileSystem == nil {
		return fmt.Errorf("spa/attach-routes-missing-file-system")
	}

	// Create filesystem only holding dist dir assets
	distDirFS, err := fs.Sub(request.SpaFileSystem, fmt.Sprintf("%sdist", request.EmbeddedContentFilePathPrefix))
	if err != nil {
		return fmt.Errorf("spa/attach-routes-file-system: %w", err)
	}
	if _, err := fs.Stat(distDirFS, "."); err != nil {
		return fmt.Errorf("spa/attach-routes-file-system: %w", err)
	}

	handleUpdatePathToIndexFunc := request.HandleUpdatePathToIndexFunc
	if handleUpdatePathToIndexFunc == nil {
		handleUpdatePathToIndexFunc = NewHandleUpdatePathToIndex()
	}

	httpRouter := request.Router.GetRouter()

	fileServer := http.FileServer(http.FS(distDirFS))
	fileMatcher := regexp.MustCompile(`\/([^\/?\s#]*)(?:[\?#].*)?`)

	httpRouter.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger := logger.AcquireOperationFrom(r.Context(), "external/spa", "serve-static-asset")
		if !fileMatcher.MatchString(r.URL.Path) {
			logger.Error("spa-static-route-invalid-path", zap.String("path", r.URL.Path))
			w.WriteHeader(http.StatusInternalServerError)
			// TODO: Update to include anchor to mailto email passed in appSettings
			w.Write([]byte("<h1>Internal Server Error</h1><br>"))
			return
		} else {

			// if the r.URL.Path does not have a suffix such as .js,
			// .css, .png, .jpg, .jpeg, .gif, .svg, or .ico then we
			// should update path to go to /
			applyStaticAssetCachePolicy(w, r)
			r = handleUpdatePathToIndexFunc(r)

			logger.Debug("spa-static-route-serving", zap.String("path", r.URL.Path))
			fileServer.ServeHTTP(w, r)
		}
	})

	return nil
}
