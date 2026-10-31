package partners

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnermanagerhelper "github.com/ooaklee/ghatd/external/partnermanager/helper"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
)

// HTTPConfig supplies the existing security guard and live principal dependencies.
type HTTPConfig struct {
	Principal    partnermanagerhelper.HTTPPrincipalConfig
	Security     *browsersecurity.Security
	Limiter      browsersecurity.Limiter
	Observe      partnerhttp.Observer
	MaxBodyBytes int64
	Now          func() time.Time
}

// BuildHTTP validates and builds transport without mounting routes.
func BuildHTTP(runtime *Runtime, cfg HTTPConfig) (http.Handler, error) {
	if runtime == nil || runtime.API == nil || absent(runtime.Sessions) {
		return nil, partnermanager.ErrUnavailable
	}
	cfg.Principal.Sessions = runtime.Sessions
	resolver, err := partnermanagerhelper.NewHTTPPrincipalResolver(cfg.Principal)
	if err != nil {
		return nil, err
	}
	return partnerhttp.New(runtime.API, resolver, partnerhttp.Config{Security: cfg.Security, Limiter: cfg.Limiter, Observe: cfg.Observe, MaxBodyBytes: cfg.MaxBodyBytes, Now: cfg.Now})
}

// MountRoutes prevents disabled endpoints falling through to the SPA.
func MountRoutes(router *mux.Router, handler http.Handler) error {
	if router == nil {
		return partnermanager.ErrUnavailable
	}
	if absent(handler) {
		handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": "PARTNERS_DEPENDENCY_UNAVAILABLE"}})
		})
	}
	router.Path(partnerhttp.BasePath).Handler(handler)
	router.PathPrefix(partnerhttp.BasePath + "/").Handler(handler)
	return nil
}

// NeverCache bypasses the host cache for private Partners and referral paths.
func NeverCache(cache mux.MiddlewareFunc) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		cached := cache(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := r.URL.Path
			if p == partnerhttp.BasePath || strings.HasPrefix(p, partnerhttp.BasePath+"/") || p == "/ref" || strings.HasPrefix(p, "/ref/") {
				next.ServeHTTP(w, r)
				return
			}
			cached.ServeHTTP(w, r)
		})
	}
}
