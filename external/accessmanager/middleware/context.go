package middleware

import (
	"context"

	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
)

// ContextWithAuthentication publishes a trusted access-manager result using
// the standard Acquire helpers. It is shared by the preloaded middleware and
// hosts that need explicit credential selection without automatic refresh.
// Inconsistent results fail closed and clear inherited identity/claims.
// Anonymous placeholder IDs remain distinguishable from authenticated users.
// Canceled contexts never receive identity. A nil input yields a cleared
// background context and an unavailable-verification error rather than panicking.
func ContextWithAuthentication(ctx context.Context, result *accessmanager.MiddlewareAuthedUserResponse) (context.Context, error) {
	if ctx == nil {
		return clearAuthentication(context.Background()), accessmanager.ErrSessionVerificationUnavailable
	}
	ctx = clearAuthentication(ctx)
	if err := ctx.Err(); err != nil {
		return ctx, err
	}
	if result == nil || result.User == nil || result.UserID == "" || result.User.GetUserId() != result.UserID {
		return ctx, auth.ErrUnauthorized
	}
	if result.Authenticated && result.Token != nil {
		if result.Token.UserID != result.UserID || !result.Token.IsSessionCredential() || !auth.MatchesUserType(result.Token.UserType, result.User) {
			return ctx, auth.ErrUnauthorized
		}
	}
	if result.Authenticated && result.APIToken != nil {
		if result.Token != nil || result.APIToken.TokenID == "" || result.APIToken.UserID != result.UserID {
			return ctx, auth.ErrUnauthorized
		}
	}
	ctx = accessmanagerhelpers.TransitWith(ctx, result.UserID)
	ctx = accessmanagerhelpers.TransitUserWith(ctx, result.User)
	ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, result.Authenticated)
	if result.Authenticated {
		ctx = accessmanagerhelpers.TransitSessionWith(ctx, result.Token)
		ctx = accessmanagerhelpers.TransitAPITokenWith(ctx, result.APIToken)
	}
	return ctx, nil
}

// clearAuthentication removes inherited actor, credential and transport authority while
// retaining cancellation, deadlines and unrelated request metadata.
func clearAuthentication(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, explicitBearerKey{}, explicitBearerOrigin{})
	ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, false)
	ctx = accessmanagerhelpers.TransitWith(ctx, "")
	ctx = accessmanagerhelpers.TransitUserWith(ctx, nil)
	ctx = accessmanagerhelpers.TransitSessionWith(ctx, nil)
	return accessmanagerhelpers.TransitAPITokenWith(ctx, nil)
}
