package accessmanagerhelpers

import (
	"context"

	"github.com/ooaklee/ghatd/external/apitoken"
)

// requestorAPIKey separates delegated credential identity from session claims.
const requestorAPIKey contextKey = "ContextRequestorAPIToken"

// TransitAPITokenWith attaches a copy of already verified API identity. It does
// not verify secrets; only trusted authentication adapters should call it.
// Passing nil clears inherited API-token context.
func TransitAPITokenWith(ctx context.Context, credential *apitoken.CredentialDetails) context.Context {
	var copy *apitoken.CredentialDetails
	if credential != nil {
		value := *credential
		copy = &value
	}
	return context.WithValue(ctx, requestorAPIKey, copy)
}

// AcquireAPITokenFrom returns an independent verified identity only when the
// authenticated owner agrees and no session claims compete with the API token.
// A nil result cannot be replaced with account-wide authority on scoped routes.
func AcquireAPITokenFrom(ctx context.Context) *apitoken.CredentialDetails {
	credential, ok := ctx.Value(requestorAPIKey).(*apitoken.CredentialDetails)
	if !ok || credential == nil || credential.TokenID == "" || credential.UserID == "" || AcquireAuthenticatedUserIDFrom(ctx) != credential.UserID {
		return nil
	}
	if session := sessionSnapshot(ctx); session != nil {
		return nil
	}
	copy := *credential
	return &copy
}
