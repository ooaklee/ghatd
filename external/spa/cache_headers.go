package spa

import (
	"net/http"
	"path"
	"strings"
)

const (
	serviceWorkerFileName     = "service-worker.js"
	serviceWorkerCacheControl = "no-cache, no-store, must-revalidate"
	expiredHTTPDate           = "Thu, 01 Jan 1970 00:00:00 GMT"
)

// applyStaticAssetCachePolicy sets cache headers for service-worker scripts that must be revalidated.
func applyStaticAssetCachePolicy(w http.ResponseWriter, r *http.Request) {
	if !isRootServiceWorkerRequest(r) {
		return
	}

	w.Header().Set("Cache-Control", serviceWorkerCacheControl)
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", expiredHTTPDate)
}

// isRootServiceWorkerRequest reports whether the request targets the root service-worker script.
func isRootServiceWorkerRequest(r *http.Request) bool {
	if r == nil || r.URL == nil {
		return false
	}

	requestPath := path.Clean("/" + strings.TrimLeft(r.URL.Path, "/"))
	return requestPath == "/"+serviceWorkerFileName
}
