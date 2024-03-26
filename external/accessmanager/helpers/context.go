package accessmanagerhelpers

import "context"

// contextKey represents the key to reference the requestor in context
type contextKey string

// RequestorKey is the key used to hold userID is context
const RequestorKey contextKey = "ContextRequestor"

// TransitWith packages both passed context and requestorID to  move
// across processes.
func TransitWith(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, RequestorKey, userID)
}

// AcquireFrom pulls requestor ID (userID) from context if exists or returns empty string
func AcquireFrom(ctx context.Context) string {

	userID, ok := ctx.Value(RequestorKey).(string)
	if ok && userID != "" {
		return userID
	}

	return ""

}
