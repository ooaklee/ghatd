package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// explicitBearerKey is private so other authentication adapters cannot
// accidentally label a cookie/refreshed session as an explicitly verified bearer.
type explicitBearerKey struct{}

// explicitBearerOrigin binds the transport evidence to the published identity,
// without retaining a bearer secret in request context.
type explicitBearerOrigin struct {
	// userID identifies the account selected by the explicit bearer.
	userID string
	// sessionID prevents reuse after a downstream adapter substitutes a session.
	sessionID string
}

// IsExplicitBearerSession reports whether BearerSessionRequired published the
// current unmixed session. It is a transport-origin check, not authorization:
// callers must still enforce current account, resource and management policy.
// Cookie/API adapters cannot satisfy it by merely publishing a session context.
func IsExplicitBearerSession(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	origin, ok := ctx.Value(explicitBearerKey{}).(explicitBearerOrigin)
	session := helpers.AcquireSessionFrom(ctx)
	return ok && session != nil && origin.userID != "" && origin.sessionID != "" && origin.userID == session.UserID && origin.sessionID == session.AccessUUID
}

// BearerSessionRequired verifies exactly one explicit session bearer, publishes
// the standard authenticated context, and never reads/refreshes cookies. An API
// token header is rejected even alongside a bearer. Current account status and
// resource/admin authority must still be checked by the route/management guard.
// All errors use canonical manifest identities; raw diagnostics are never sent.
func (m *Middleware) BearerSessionRequired(next http.Handler) http.Handler {
	composer := errormanifest.NewComposer().Add(auth.AuthErrorMap, accessmanager.AccessmanagerErrorMap)
	if m != nil {
		composer.AddOverrides(m.errorMaps...)
	}
	manifests := composer.Build()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fail := func(err error) {
			_ = reply.NewReplier(manifests).NewHTTPErrorResponse(w, errormanifest.CanonicalError(err, manifests))
		}
		if m == nil || m.service == nil || next == nil {
			fail(accessmanager.ErrSessionVerificationUnavailable)
			return
		}
		if err := r.Context().Err(); err != nil {
			fail(err)
			return
		}
		values := r.Header.Values("Authorization")
		if len(values) != 1 || len(r.Header.Values(common.SystemWideXApiToken)) != 0 || len(values[0]) > 16384 {
			fail(auth.ErrUnauthorized)
			return
		}
		parts := strings.Split(values[0], " ")
		if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" || strings.ContainsAny(parts[1], "\t\r\n,") {
			fail(auth.ErrUnauthorized)
			return
		}
		result, err := m.service.MiddlewareJWTRequired(r)
		if contextErr := r.Context().Err(); contextErr != nil {
			fail(contextErr)
			return
		}
		if err != nil {
			fail(err)
			return
		}
		if result == nil || !result.Authenticated || result.Token == nil || result.Token.AccessUUID == "" || result.APIToken != nil {
			fail(auth.ErrUnauthorized)
			return
		}
		ctx, err := ContextWithAuthentication(r.Context(), result)
		if err != nil {
			fail(err)
			return
		}
		ctx = context.WithValue(ctx, explicitBearerKey{}, explicitBearerOrigin{userID: result.Token.UserID, sessionID: result.Token.AccessUUID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
