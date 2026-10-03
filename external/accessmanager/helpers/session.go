package accessmanagerhelpers

import (
	"context"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
)

// requestorSessionKey carries validated claims independently of any bearer.
const requestorSessionKey contextKey = "ContextRequestorSession"

// TransitSessionWith attaches a defensive copy of already verified claims.
// This is a trusted middleware operation, not a token-verification function.
// Passing nil clears inherited session metadata. No raw credential is stored.
func TransitSessionWith(ctx context.Context, details *auth.TokenAccessDetails) context.Context {
	var snapshot *auth.TokenAccessDetails
	if details != nil {
		value := *details
		value.Audience = append([]string(nil), details.Audience...)
		snapshot = &value
	}
	return context.WithValue(ctx, requestorSessionKey, snapshot)
}

// AcquireSessionFrom returns an independent claim snapshot only when the
// context is explicitly authenticated and its user ID matches the token.
// Legacy, anonymous, API-token and inconsistent contexts return nil. A result
// does not establish current resource authority or a still-live session.
func AcquireSessionFrom(ctx context.Context) *auth.TokenAccessDetails {
	details := sessionSnapshot(ctx)
	if !details.IsSessionCredential() || details.UserID == "" || AcquireAuthenticatedUserIDFrom(ctx) != details.UserID {
		return nil
	}
	if apiToken, _ := ctx.Value(requestorAPIKey).(*apitoken.CredentialDetails); apiToken != nil {
		return nil
	}
	value := *details
	value.Audience = append([]string(nil), details.Audience...)
	return &value
}

// sessionSnapshot reads the internal snapshot without treating it as authority.
func sessionSnapshot(ctx context.Context) *auth.TokenAccessDetails {
	details, _ := ctx.Value(requestorSessionKey).(*auth.TokenAccessDetails)
	return details
}
