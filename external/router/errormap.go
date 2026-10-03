package router

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// PolicyErrorManifest returns independent, client-safe policy error definitions.
// Mutating the copy alone does not change router responses. Supply overrides to
// ConfigureRoutePolicy at startup. Dependency messages and identifiers are omitted.
func PolicyErrorManifest() reply.ErrorManifest {
	return reply.ErrorManifest{
		ErrRouteConfiguration:   {Title: "Endpoint unavailable.", StatusCode: http.StatusServiceUnavailable, Code: "ROUTE_CONFIGURATION"},
		ErrRouteDenied:          {Title: "Access denied.", StatusCode: http.StatusForbidden, Code: "ROUTE_DENIED"},
		ErrRouteUnauthenticated: {Title: "Authentication required.", StatusCode: http.StatusUnauthorized, Code: "ROUTE_AUTHENTICATION"},
		ErrRouteUnavailable:     {Title: "Authorization unavailable.", StatusCode: http.StatusServiceUnavailable, Code: "ROUTE_UNAVAILABLE"},
		ErrRoutePrecondition:    {Title: "A resource revision is required.", StatusCode: http.StatusPreconditionRequired, Code: "ROUTE_REVISION_REQUIRED"},
		ErrRouteInvalidRevision: {Title: "Provide one strong resource revision.", StatusCode: http.StatusBadRequest, Code: "ROUTE_REVISION_INVALID"},
		ErrRouteLimitReached:    {Title: "Usage limit reached.", StatusCode: http.StatusTooManyRequests, Code: "ROUTE_LIMIT_REACHED"},
	}
}

// writeRouteError preserves one unambiguous mapped authorization failure.
// Unknown, joined or malformed failures mean authorization is unavailable,
// never a proven client denial. The shared strict resolver retains no private
// diagnostics; the shared writer carries request context without serializing it.
func writeRouteError(ctx context.Context, w http.ResponseWriter, err error, overrides ...reply.ErrorManifest) {
	manifests := errormanifest.NewComposer().Add(PolicyErrorManifest()).AddOverrides(overrides...).Build()
	public := errormanifest.CanonicalError(err, manifests)
	known := false
	for _, manifest := range manifests {
		if _, found := manifest[public]; found {
			known = true
			break
		}
	}
	if !known {
		public = ErrRouteUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = errormanifest.WriteHTTPError(w, public, manifests, reply.WithContext(ctx))
}
