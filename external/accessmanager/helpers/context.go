// Package accessmanagerhelpers provides request-context helpers shared by
// authentication middleware and downstream request handlers.
//
// On optional-auth routes, a non-empty requestor ID does not prove that the
// request was authenticated. Rate-limited anonymous requests may carry a
// placeholder ID. Use AcquireAuthenticatedFrom when branching on authentication
// state, or AcquireAuthenticatedUserIDFrom when an authenticated user ID is
// required for a lookup or actor-specific operation.
package accessmanagerhelpers

import (
	"context"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// contextKey defines a distinct type used as keys when storing values in a
// request context. It reduces the likelihood of key collisions with other
// context values that may use plain strings.
type contextKey string

// RequestorKey stores the authenticated user's ID in the request context.
const RequestorKey contextKey = "ContextRequestor"

// RequestorUserKey stores the full authenticated user object in the request context.
const RequestorUserKey contextKey = "ContextRequestorUser"

// RequestorAuthenticatedKey stores whether the requestor was authenticated.
// This is intentionally separate from RequestorKey because optional-auth
// middleware assigns a placeholder user ID to public requests.
const RequestorAuthenticatedKey contextKey = "ContextRequestorAuthenticated"

// TransitWith returns a new context derived from ctx that carries the
// authenticated user's ID.
func TransitWith(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, RequestorKey, userID)
}

// AcquireFrom extracts the authenticated user's ID from ctx or returns an
// empty string if no ID is present.
func AcquireFrom(ctx context.Context) string {
	userID, ok := ctx.Value(RequestorKey).(string)
	if ok && userID != "" {
		return userID
	}
	return ""
}

// TransitUserWith returns a new context derived from ctx that carries the
// full authenticated user object.
func TransitUserWith(ctx context.Context, user *userv2.UniversalUser) context.Context {
	return context.WithValue(ctx, RequestorUserKey, user)
}

// AcquireUserFrom retrieves the authenticated user object from ctx or nil if
// it is not present.
func AcquireUserFrom(ctx context.Context) *userv2.UniversalUser {
	user, ok := ctx.Value(RequestorUserKey).(*userv2.UniversalUser)
	if ok && user != nil {
		return user
	}
	return nil
}

// TransitAuthenticatedWith returns a new context carrying the requestor's
// authentication state.
func TransitAuthenticatedWith(ctx context.Context, authenticated bool) context.Context {
	return context.WithValue(ctx, RequestorAuthenticatedKey, authenticated)
}

// AcquireAuthenticatedFrom reports whether the requestor was authenticated.
// A missing value is treated as unauthenticated so privacy-sensitive response
// projections and database lookups fail closed. Callers must not infer
// authentication from AcquireFrom returning a non-empty ID because
// optional-auth middleware may attach a placeholder ID to anonymous requests.
func AcquireAuthenticatedFrom(ctx context.Context) bool {
	authenticated, ok := ctx.Value(RequestorAuthenticatedKey).(bool)
	return ok && authenticated
}

// AcquireAuthenticatedUserIDFrom returns the requestor ID only when ctx
// explicitly records an authenticated request. It returns an empty string for
// anonymous requests, missing authentication state, and missing user IDs.
//
// Use this helper when a context-derived ID will drive a user lookup,
// authorization decision, or actor attribution on an optional-auth route. Use
// AcquireFrom directly on routes whose middleware strictly requires a user, or
// when the placeholder ID is intentionally part of rate-limit bookkeeping.
func AcquireAuthenticatedUserIDFrom(ctx context.Context) string {
	if !AcquireAuthenticatedFrom(ctx) {
		return ""
	}
	return AcquireFrom(ctx)
}
