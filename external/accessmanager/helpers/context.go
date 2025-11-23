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
