package serverhttp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	inherited "github.com/ooaklee/ghatd/external/middleware/cors"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	"github.com/ooaklee/ghatd/external/router"
)

// CORSConfig chooses exact credentialed origins for the shared route families.
// Ordinary routes keep the inherited CORS policy; Handles additionally exposes
// revision headers on the optional user-handle API. Product-specific families
// can be selected by an outer host middleware without weakening this policy.
type CORSConfig struct {
	AllowedOrigins  []string
	PartnersOrigins []string
	// PartnersMethods optionally narrows the shared Partners HTTP vocabulary.
	// Nil uses GET, POST, PATCH, PUT, DELETE and OPTIONS. A non-nil empty list
	// refuses preflight for every method; unknown methods fail construction.
	PartnersMethods []string
	Handles         bool
}

// CORS selects a family by exact path or directory boundary. Partners and handle
// origins cannot contain wildcards, credentials, paths, queries or fragments.
// Empty special-route origin sets deny cross-origin access. Inputs are copied.
func CORS(cfg CORSConfig) (mux.MiddlewareFunc, error) {
	origins := make([]string, len(cfg.AllowedOrigins))
	for i, origin := range cfg.AllowedOrigins {
		origins[i] = strings.TrimSpace(origin)
	}
	// An empty inherited list means wildcard to gorilla/handlers. Preserve a
	// deny-all default instead of turning absent host configuration into opt-in.
	if len(origins) == 0 {
		origins = []string{""}
	}
	partnerOrigins, err := exactOrigins(cfg.PartnersOrigins)
	if err != nil {
		return nil, err
	}
	partnerMethods := []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodPut, http.MethodDelete, http.MethodOptions}
	if cfg.PartnersMethods != nil {
		allowed := make(map[string]bool, len(partnerMethods))
		for _, method := range partnerMethods {
			allowed[method] = true
		}
		partnerMethods = make([]string, 0, len(cfg.PartnersMethods))
		for _, method := range cfg.PartnersMethods {
			if !allowed[method] {
				return nil, errors.New("serverhelpers/cors-partner-method-invalid")
			}
			partnerMethods = append(partnerMethods, method)
		}
	}
	var handleOrigins map[string]bool
	if cfg.Handles {
		handleOrigins, err = exactOrigins(cfg.AllowedOrigins)
		if err != nil {
			return nil, err
		}
	}
	base := inherited.NewCorsMiddleware(origins)
	handle := handlers.CORS(handlers.AllowCredentials(), handlers.AllowedOriginValidator(func(origin string) bool { return handleOrigins[origin] }), handlers.AllowedMethods([]string{"GET", "POST", "PATCH", "OPTIONS"}), handlers.AllowedHeaders([]string{"Content-Type", "Authorization", "If-Match", "X-Platform", "X-Timezone"}), handlers.ExposedHeaders([]string{"ETag"}))
	partners := handlers.CORS(handlers.AllowCredentials(), handlers.AllowedOriginValidator(func(origin string) bool { return partnerOrigins[origin] }), handlers.AllowedMethods(partnerMethods), handlers.AllowedHeaders([]string{"Content-Type", "Authorization", "If-Match", "Idempotency-Key", "X-Platform", "X-Timezone", browsersecurity.CSRFHeader}), handlers.ExposedHeaders([]string{"ETag", "Idempotency-Replayed", "Retry-After", browsersecurity.CSRFHeader}))
	return func(next http.Handler) http.Handler {
		standard, handleHandler, partnerHandler := base(next), handle(next), partners(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == partnerhttp.BasePath || strings.HasPrefix(r.URL.Path, partnerhttp.BasePath+"/") {
				w.Header().Set("Cache-Control", "no-store")
				partnerHandler.ServeHTTP(w, r)
				return
			}
			if cfg.Handles && (r.URL.Path == "/api/v1/ums/me/handle" || r.URL.Path == "/api/v1/ums/me/handle/validate") {
				handleHandler.ServeHTTP(w, r)
				return
			}
			standard.ServeHTTP(w, r)
		})
	}, nil
}

func exactOrigins(values []string) (map[string]bool, error) {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		origin := strings.TrimSpace(value)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(origin, "*") || u.String() != origin {
			return nil, errors.New("serverhelpers/cors-exact-origin-required")
		}
		out[origin] = true
	}
	return out, nil
}

// AttachHandlePreflight declares public OPTIONS only; authenticated handle
// methods remain owner-attached. OperationPrefix is a stable host-selected
// inventory identifier, retained when extracting an existing composition.
// Attach before serving and abort startup if route validation fails.
func AttachHandlePreflight(routes *router.Router, operationPrefix string) error {
	if routes == nil || strings.TrimSpace(operationPrefix) == "" {
		return router.ErrRouteConfiguration
	}
	group := routes.NewRouteGroup("/api/v1/ums", router.Public, nil)
	for _, path := range []string{"/me/handle", "/me/handle/validate"} {
		group.Handle(router.RouteDefinition{Path: path, Operation: operationPrefix + path, Methods: []string{http.MethodOptions}}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	}
	return routes.ValidateRoutePolicies()
}
