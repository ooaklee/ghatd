package spa

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/ooaklee/ghatd/external/router"
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

	// HandleUpdatePathToIndex is the function that handles updating
	// request path that should be sent to the / path
	HandleUpdatePathToIndex func(r *http.Request) *http.Request
}

// AttachRoutes attaches spa handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {

	// Create filesystem only holding dist dir assets
	distDirFS, err := fs.Sub(request.SpaFileSystem, fmt.Sprintf("%sdist", request.EmbeddedContentFilePathPrefix))
	if err != nil {
		log.Default().Panicln("unable-to-create-file-system-for-static-assets", err)
		return
	}

	httpRouter := request.Router.GetRouter()

	fileServer := http.FileServer(http.FS(distDirFS))
	fileMatcher := regexp.MustCompile(`\/([^\/?\s#]*)(?:[\?#].*)?`)

	httpRouter.PathPrefix("/").HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fileMatcher.MatchString(r.URL.Path) {
			w.WriteHeader(http.StatusInternalServerError)
			// TODO: Update to include anchor to mailto email passed in appSettings
			w.Write([]byte("<h1>Internal Server Error</h1><br>"))
			return
		} else {

			// if the r.URL.Path does not have a suffix such as .js,
			// .css, .png, .jpg, .jpeg, .gif, .svg, or .ico then we
			// should update path to go to /
			r = request.HandleUpdatePathToIndex(r)

			fileServer.ServeHTTP(w, r)
		}
	})

}

// HandleUpdatePathToIndex updates the request's URL path to the root ("/") for non-asset routes.
// It ensures that routes without specific file extensions are redirected to the index page,
// preserving routing for static assets like JavaScript, CSS, images, and icons.
func HandleUpdatePathToIndex(r *http.Request) *http.Request {
	if !strings.HasSuffix(r.URL.Path, ".js") &&
		!strings.HasSuffix(r.URL.Path, ".css") &&
		!strings.HasSuffix(r.URL.Path, ".png") &&
		!strings.HasSuffix(r.URL.Path, ".jpg") &&
		!strings.HasSuffix(r.URL.Path, ".jpeg") &&
		!strings.HasSuffix(r.URL.Path, ".gif") &&
		!strings.HasSuffix(r.URL.Path, ".svg") &&
		!strings.HasSuffix(r.URL.Path, ".ico") &&
		!strings.HasSuffix(r.URL.Path, ".txt") &&
		!strings.HasSuffix(r.URL.Path, ".woff") &&
		!strings.HasSuffix(r.URL.Path, ".woff2") &&
		!strings.HasSuffix(r.URL.Path, ".xml") {
		r.URL.Path = "/"
	}
	return r
}
