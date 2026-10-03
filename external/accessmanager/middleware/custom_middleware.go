package middleware

import (
	"errors"
	"net/http"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// MeEndpointResponseMode selects the response for the missing-session sentinel
// on a host's /me probe. It never changes authentication or admission decisions.
type MeEndpointResponseMode string

const (
	// MeEndpointLegacyAccepted preserves the existing 202 JSON error envelope.
	// The zero value deliberately keeps current clients compatible.
	MeEndpointLegacyAccepted MeEndpointResponseMode = ""
	// MeEndpointAcceptedEmpty returns 202 with no body for a missing session.
	// Clients must not attempt JSON decoding or interpret 202 as authenticated.
	MeEndpointAcceptedEmpty MeEndpointResponseMode = "accepted-empty"
	// MeEndpointUnauthorized uses the structured 401 error envelope for API clients.
	MeEndpointUnauthorized MeEndpointResponseMode = "unauthorized"
)

// ErrInvalidMeEndpointResponseMode rejects unsupported startup configuration.
var ErrInvalidMeEndpointResponseMode = errors.New("accessmanager middleware: invalid me endpoint response mode")

// BuildCustomMeEndpointErrorMap builds a final []reply.ErrorManifest from
// baseErrorMaps with a specialised override appended as the final layer for
// the /me endpoint. It preserves the legacy HTTP 202 response for the missing-
// session sentinel, not every authentication failure. This is client
// compatibility, not an SEO mechanism: private session probes are not content
// to index, and empty or error-bearing 2xx responses can still be soft 404s.
//
// The override is applied via errormanifest.Composer.AddOverrides, placing it
// after all base maps so that last-wins semantics guarantee the endpoint-
// specific status code takes precedence over any prior entry for the same
// error key.
//
// Use this to generate the errorMap argument for CustomMeEndpointValidApiTokenOrJWTMiddleware.
//
// Example:
//
//	errorMap := BuildCustomMeEndpointErrorMap(middlewareErrorMaps)
//	middleware := amiddleware.NewMiddleware(...).CustomMeEndpointValidApiTokenOrJWTMiddleware(errorMap)
func BuildCustomMeEndpointErrorMap(baseErrorMaps []reply.ErrorManifest) []reply.ErrorManifest {
	return meEndpointErrorMaps(baseErrorMaps, http.StatusAccepted)
}

// meEndpointErrorMaps appends a route-local override without mutating base maps.
// The chosen policy wins for AM00-013 only; all other mappings remain unchanged.
func meEndpointErrorMaps(baseErrorMaps []reply.ErrorManifest, status int) []reply.ErrorManifest {
	return errormanifest.NewComposer().
		Add(baseErrorMaps...).
		AddOverrides(reply.ErrorManifest{
			accessmanager.ErrUnauthorizedUnableToAttainRequestorID: {Title: "Unauthorized", StatusCode: status, Code: "AM00-013"},
		}).
		Build()
}

// MeEndpointMiddleware builds a route-scoped session probe using an explicit
// response mode and this middleware's error maps. Invalid modes fail at startup.
// Only AM00-013 is overridden. Invalid credentials, denials and dependency errors
// retain their configured responses; no failure dispatches the protected handler.
// Prefer constructing a separate instance to changing shared maps at runtime.
func (m *Middleware) MeEndpointMiddleware(mode MeEndpointResponseMode) (func(http.Handler) http.Handler, error) {
	if m == nil {
		return nil, ErrNilRequest
	}
	status := http.StatusAccepted
	switch mode {
	case MeEndpointLegacyAccepted, MeEndpointAcceptedEmpty:
	case MeEndpointUnauthorized:
		status = http.StatusUnauthorized
	default:
		return nil, ErrInvalidMeEndpointResponseMode
	}
	holder := *m
	holder.emptyMeSessionResponse = mode == MeEndpointAcceptedEmpty
	return holder.CustomMeEndpointValidApiTokenOrJWTMiddleware(meEndpointErrorMaps(m.errorMaps, status)), nil
}

// CustomMeEndpointValidApiTokenOrJWTMiddleware returns a middleware function
// equivalent to ActiveValidApiTokenOrAuthenticated, but scoped to a custom
// error map for this endpoint-specific edge case.
//
// Use this helper when GET /api/v1/ums/me needs different auth error response
// semantics than the default authenticated middleware path.
//
// This middleware is optional and should be wired through
// usermanager.AttachRoutesRequest.CustomMeEndpointValidApiTokenOrJWTMiddleware.
// If that field is nil, usermanager.AttachRoutes falls back to
// usermanager.AttachRoutesRequest.ValidApiTokenOrJWTMiddleware.
// Every response passing through this wrapper receives no-store and noindex,
// including successful private projections. Place it outside other middleware
// if those middleware's early responses must receive these headers too. These
// headers do not change an enclosing HTML page's indexing or caching policy.
// no-store intentionally replaces inherited cache directives for this private
// projection. Downstream handlers must not override this privacy policy.
func (m *Middleware) CustomMeEndpointValidApiTokenOrJWTMiddleware(customErrorMap []reply.ErrorManifest) func(handler http.Handler) http.Handler {
	holder := *m
	holder.errorMaps = customErrorMap
	return func(handler http.Handler) http.Handler {
		protected := holder.ActiveValidApiTokenOrAuthenticated(handler)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Add("X-Robots-Tag", "noindex")
			protected.ServeHTTP(w, r)
		})
	}
}
