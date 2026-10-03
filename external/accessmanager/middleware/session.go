package middleware

import (
	"context"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
)

// SessionAuthenticator verifies one explicitly selected credential against its
// signature, expiry, live session and current account identity. Implementations
// must preserve operational errors; raw claims or cached contexts are not proof.
// Access Manager's Service implements this port without HTTP or cookie effects.
// Return a request-owned user observation: the standard user-context API retains
// its pointer, so adapters and consumers must not mutate it after publication.
type SessionAuthenticator interface {
	// AuthenticateSession resolves current authority without refresh or fallback.
	AuthenticateSession(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error)
}

// AuthenticateSessionContext authenticates once and publishes a complete,
// unmixed session through the standard Acquire helpers. Hosts still choose the
// credential and enforce account status, verification, audience and resource
// permissions. Reinvoke at sensitive command/replay boundaries, not just entry.
//
// Every failure returns a context with inherited authentication cleared and
// preserves the underlying error. Nil context/dependency or a nil successful
// result indicates unavailable verification, not an invalid credential. A nil
// input context yields a cleared background context; other context values and
// cancellation survive. No bearer is stored in context, no cookies are changed,
// and no refresh, API-token fallback or anonymous admission is attempted.
func AuthenticateSessionContext(ctx context.Context, verifier SessionAuthenticator, credential string) (context.Context, error) {
	if ctx == nil {
		return clearAuthentication(context.Background()), accessmanager.ErrSessionVerificationUnavailable
	}
	ctx = clearAuthentication(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if verifier == nil {
		return ctx, accessmanager.ErrSessionVerificationUnavailable
	}
	if credential == "" {
		return ctx, auth.ErrNoBearerHeaderFound
	}
	result, err := verifier.AuthenticateSession(ctx, credential)
	if err != nil {
		return ctx, err
	}
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if result == nil {
		return ctx, accessmanager.ErrSessionVerificationUnavailable
	}
	if !result.Authenticated || result.Token == nil || result.Token.AccessUUID == "" || result.APIToken != nil {
		return ctx, auth.ErrUnauthorized
	}
	return ContextWithAuthentication(ctx, result)
}
