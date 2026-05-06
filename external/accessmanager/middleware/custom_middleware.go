package middleware

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/reply"
)

// BuildCustomMeEndpointErrorMap prepends a specialised error entry to baseErrorMaps
// for the /me endpoint. The specialised entry returns HTTP 202 (Accepted) instead of
// 401 (Unauthorized) for unauthenticated requests, allowing search engines to index
// the endpoint without treating missing credentials as a hard error.
//
// Use this to generate the errorMap argument for CustomMeEndpointValidApiTokenOrJWTMiddleware.
//
// Example:
//
//	errorMap := BuildCustomMeEndpointErrorMap(middlewareErrorMaps)
//	middleware := amiddleware.NewMiddleware(...).CustomMeEndpointValidApiTokenOrJWTMiddleware(errorMap)
func BuildCustomMeEndpointErrorMap(baseErrorMaps []reply.ErrorManifest) []reply.ErrorManifest {
	errorMap := append(baseErrorMaps, []reply.ErrorManifest{reply.ErrorManifest{
		accessmanager.ErrKeyUnauthorizedUnableToAttainRequestorID: {Title: "Unauthorized", StatusCode: http.StatusAccepted, Code: "AM00-013"},
	}}...)

	return errorMap
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
func (m *Middleware) CustomMeEndpointValidApiTokenOrJWTMiddleware(customErrorMap []reply.ErrorManifest) func(handler http.Handler) http.Handler {
	holder := *m
	holder.errorMaps = customErrorMap
	return holder.ActiveValidApiTokenOrAuthenticated
}
