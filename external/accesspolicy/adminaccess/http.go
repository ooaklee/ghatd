// Package adminaccess adapts browser sessions to GHATD's token-policy manager.
// It never creates broader roles, exposes a JWT, or relaxes the shared API guard.
package adminaccess

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

const (
	// BasePath confines the cookie-to-bearer adapter to this feature only.
	BasePath = "/api/v1/admin-access"
	// ContextHeader carries a page-scoped CSRF context, never a JWT.
	ContextHeader = "X-Admin-Context"
	// ReviewHeader selects the immutable proposal and purpose-bound proof.
	ReviewHeader = "X-Admin-Review"
)

// PolicyManager retains all domain validation, live authorization, inventory,
// audited Mongo CAS and uncertain-outcome semantics in GHATD.
type PolicyManager interface {
	Preview(context.Context, string, accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error)
	Apply(context.Context, string, int64, accesspolicy.TokenLimits) (accesspolicy.Grant, error)
}

// EmailSender uses the existing provider/template pipeline; no mailbox is opened.
type EmailSender interface {
	SendCustomEmail(context.Context, *emailmanager.SendCustomEmailRequest) error
}

// Config is trusted host composition, never populated from HTTP inputs.
type Config struct {
	// Origin is the single exact browser origin, HTTPS except loopback development.
	Origin string
	// System and Environment identify the verified deployment in the UI and binding.
	System      string
	Environment string
	// CookieName selects exactly one existing HttpOnly access cookie, not refresh.
	CookieName string
	// Window is the absolute review/proof lifetime, from one to five minutes.
	Window time.Duration
	// Store shares the host Redis connection, isolated from all login/OAuth proofs.
	Store *RedisStore
	// Manager and Authorize must use the same system and live access-policy stack.
	Manager   PolicyManager
	Authorize accesspolicy.ManagementAuthorizer
	// BearerSession must be GHATD's real explicit bearer middleware (marker checked).
	BearerSession mux.MiddlewareFunc
	// Email sends only to the currently verified operator email.
	Email EmailSender
}

// Bridge owns immutable wiring. Each request receives its own command adapter;
// no credentials, actor context, or pending writes are stored on this instance.
type Bridge struct {
	config    Config
	manifests []reply.ErrorManifest
}

// ErrorMap provides safe stable outcomes; raw dependency diagnostics are excluded.
var ErrorMap = reply.ErrorManifest{
	ErrReview:      {Title: "Review required", Detail: "Preview again; this review is expired, changed, cancelled or already used", StatusCode: 428, Code: "ADA0-001"},
	ErrProof:       {Title: "Code not accepted", Detail: "Check the code; after five attempts a new review is required", StatusCode: 422, Code: "ADA0-002"},
	ErrCooldown:    {Title: "Please wait", Detail: "Email or page-context requests are temporarily limited", StatusCode: 429, Code: "ADA0-003"},
	ErrUnavailable: {Title: "Access service unavailable", Detail: "The operation could not be confirmed", StatusCode: 503, Code: "ADA0-004"},
}

// New validates the origin and mandatory security ports without I/O. Missing
// configuration never enables a permissive transport or a default grant.
func New(c Config) (*Bridge, error) {
	u, err := url.Parse(c.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.String() != c.Origin {
		return nil, ErrUnavailable
	}
	loopback := u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, ErrUnavailable
	}
	if c.System == "" || c.Environment == "" || c.CookieName == "" || c.Window < time.Minute || c.Window > 5*time.Minute || c.Store == nil || c.Manager == nil || c.Authorize == nil || c.BearerSession == nil || c.Email == nil {
		return nil, ErrUnavailable
	}
	return &Bridge{config: c, manifests: errormanifest.NewComposer().Add(auth.AuthErrorMap, accessmanager.AccessmanagerErrorMap, accesspolicy.AccessPolicyErrorMap, accesspolicymanager.ErrorMap, ErrorMap).Build()}, nil
}

// Attach declares only feature-owned browser commands. Original bearer-only
// endpoints and their middleware are untouched. No CORS preflight is admitted.
func (b *Bridge) Attach(r *router.Router) error {
	if r == nil {
		return ErrUnavailable
	}
	g := r.NewRouteGroup(BasePath, router.AdminSession, b.browserSession)
	for _, route := range []struct {
		path, operation string
		handler         http.HandlerFunc
	}{
		{"/session", "Session", b.session},
		{"/users/{userID}/token-limits/preview", "Preview", b.preview},
		{"/challenge", "Challenge", b.challenge},
		{"/confirm", "Confirm", b.confirm},
		{"/cancel", "Cancel", b.cancel},
	} {
		g.Handle(router.RouteDefinition{Path: route.path, Methods: []string{http.MethodPost}, Operation: "adminaccess." + route.operation}, route.handler)
	}
	g.Handle(router.RouteDefinition{Path: "/users/{userID}/token-limits", Methods: []string{http.MethodPut}, Operation: "adminaccess.Apply", Policy: router.RoutePolicy{RevisionRequired: true}}, b.apply)
	return r.ValidateRoutePolicies()
}

// one rejects duplicate and comma-combined security headers rather than using
// first/last-wins interpretations across a reverse proxy and the application.
func one(r *http.Request, name string) string {
	values := r.Header.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

// browserSession validates CSRF provenance before privately adapting exactly one
// cookie through the real verifier. It never refreshes or returns credentials.
func (b *Bridge) browserSession(next http.Handler) http.Handler {
	verified := b.config.BearerSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !amiddleware.IsExplicitBearerSession(r.Context()) {
			b.fail(w, auth.ErrUnauthorized)
			return
		}
		if _, err := b.identity(r.Context()); err != nil {
			b.fail(w, err)
			return
		}
		// Downstream feature code needs verified claims, not the adapted credential.
		clean := r.Clone(r.Context())
		clean.Header.Del("Authorization")
		next.ServeHTTP(w, clean)
	}))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Del("Access-Control-Allow-Origin")
		w.Header().Del("Access-Control-Allow-Credentials")
		if !strings.HasPrefix(r.URL.Path, BasePath+"/") || (r.Method != http.MethodPost && r.Method != http.MethodPut) || r.URL.RawQuery != "" ||
			one(r, "Origin") != b.config.Origin || one(r, "Sec-Fetch-Site") != "same-origin" || one(r, "Sec-Fetch-Dest") != "empty" || one(r, "X-Admin-Access") != "1" ||
			len(r.Header.Values("Authorization")) != 0 || len(r.Header.Values("X-Api-Token")) != 0 || len(r.Header.Values("Cookie")) != 1 {
			b.fail(w, accesspolicy.ErrDenied)
			return
		}
		var credential string
		count := 0
		for _, cookie := range r.Cookies() {
			if cookie.Name == b.config.CookieName {
				credential = cookie.Value
				count++
			}
		}
		if count != 1 || credential == "" {
			b.fail(w, auth.ErrUnauthorized)
			return
		}
		clone := r.Clone(r.Context())
		clone.Header.Del("Cookie")
		clone.Header.Set("Authorization", "Bearer "+credential)
		verified.ServeHTTP(w, clone)
	})
}

// identity rechecks live authority and requires a verified email before any
// context issuance or proof operation. Email/session changes invalidate binding.
func (b *Bridge) identity(ctx context.Context) (string, error) {
	actor, err := b.config.Authorize(ctx, b.config.System)
	if err != nil {
		return "", err
	}
	session, account := helpers.AcquireSessionFrom(ctx), helpers.AcquireUserFrom(ctx)
	if session == nil || account == nil || actor != account.ID || session.UserID != actor || account.Status != user.AccountStatusKeyActive || !account.IsAdmin() || account.Email == "" || account.Verification == nil || !account.Verification.EmailVerified {
		return "", accesspolicy.ErrDenied
	}
	snapshot, _ := json.Marshal([]any{"token-allowance", b.config.System, b.config.Environment, b.config.Origin, actor, session.AccessUUID, account.Email, account.EmailRevision})
	return digest(string(snapshot)), nil
}

// session opens a bounded CSRF context, not an elevation or replacement login.
func (b *Bridge) session(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(r) {
		b.fail(w, accesspolicymanager.ErrInvalidRequest)
		return
	}
	binding, err := b.identity(r.Context())
	if err != nil {
		b.fail(w, err)
		return
	}
	id, err := b.config.Store.Create(r.Context(), binding, helpers.AcquireUserFrom(r.Context()).ID)
	if err != nil {
		b.fail(w, err)
		return
	}
	b.respond(w, map[string]any{"context": id, "system": b.config.System, "environment": b.config.Environment, "window_seconds": int(b.config.Window.Seconds())})
}

// command is request-owned glue. GHATD retains its strict JSON/ETag codecs and
// reply envelope; this layer adds only immutable-review/step-up requirements.
type command struct {
	bridge  *Bridge
	request *http.Request
	writer  http.ResponseWriter
}

// Preview persists the exact manager-produced proposal and returns its opaque
// handle in a header. A no-op never becomes authorization to change a policy.
func (c *command) Preview(ctx context.Context, target string, limits accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error) {
	b := c.bridge
	binding, err := b.identity(ctx)
	if err != nil {
		return accesspolicy.TokenLimitPreview{}, err
	}
	if _, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "check", randomID(), "", "", 0); err != nil {
		return accesspolicy.TokenLimitPreview{}, err
	}
	preview, err := b.config.Manager.Preview(ctx, target, limits)
	if err != nil {
		return preview, err
	}
	r := review{ID: randomID(), Target: target, Limits: limits}
	if preview.Before != nil {
		r.Revision = preview.Before.Revision
	}
	payload, _ := json.Marshal(r)
	r, err = b.config.Store.run(ctx, one(c.request, ContextHeader), binding, "preview", r.ID, string(payload), "", b.config.Window)
	if err == nil {
		c.writer.Header().Set(ReviewHeader, r.ID)
		c.writer.Header().Set("X-Admin-Expires", fmt.Sprint(r.Expires))
		c.writer.Header().Set("X-Admin-Remaining-Ms", fmt.Sprint(r.Expires-r.ObservedAt))
	}
	return preview, err
}

// Apply consumes authorization before dispatch. Failure never restores it:
// the manager may have committed even when the browser cannot confirm the result.
func (c *command) Apply(ctx context.Context, target string, revision int64, limits accesspolicy.TokenLimits) (accesspolicy.Grant, error) {
	b := c.bridge
	binding, err := b.identity(ctx)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	id, rid := one(c.request, ContextHeader), one(c.request, ReviewHeader)
	r, err := b.config.Store.run(ctx, id, binding, "read", rid, "", "", 0)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	if r.Target != target || r.Revision != revision || r.Limits != limits {
		return accesspolicy.Grant{}, ErrReview
	}
	if _, err = b.config.Store.run(ctx, id, binding, "consume", rid, "", "", 0); err != nil {
		return accesspolicy.Grant{}, err
	}
	b.record(ctx, "apply_dispatched", r)
	grant, err := b.config.Manager.Apply(ctx, target, revision, limits)
	if err != nil {
		b.record(ctx, "apply_unconfirmed", r)
	} else {
		b.record(ctx, "apply_confirmed", r)
	}
	return grant, err
}

// policy delegates HTTP parsing and response/error mapping to the shared manager.
func (b *Bridge) policy(w http.ResponseWriter, r *http.Request, apply bool) {
	h, err := accesspolicymanager.NewHandler(&command{b, r, w}, ErrorMap)
	if err != nil {
		b.fail(w, err)
		return
	}
	if apply {
		h.ApplyTokenLimits(w, r)
	} else {
		h.PreviewTokenLimits(w, r)
	}
}
func (b *Bridge) preview(w http.ResponseWriter, r *http.Request) { b.policy(w, r, false) }
func (b *Bridge) apply(w http.ResponseWriter, r *http.Request)   { b.policy(w, r, true) }

// bound validates the current page context and exact review before proof I/O.
func (b *Bridge) bound(r *http.Request) (string, review, error) {
	binding, err := b.identity(r.Context())
	if err != nil {
		return "", review{}, err
	}
	rv, err := b.config.Store.run(r.Context(), one(r, ContextHeader), binding, "read", one(r, ReviewHeader), "", "", 0)
	return binding, rv, err
}

// challenge sends a purpose-specific 48-bit code after atomic account-wide
// throttling. Delivery failure cancels the approval and does not refund limits.
func (b *Bridge) challenge(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(r) {
		b.fail(w, accesspolicymanager.ErrInvalidRequest)
		return
	}
	binding, rv, err := b.bound(r)
	if err != nil {
		b.fail(w, err)
		return
	}
	var raw [6]byte
	// Like randomID, Go 1.26 crypto/rand fails closed rather than using zero bytes.
	_, _ = rand.Read(raw[:])
	code := strings.ToUpper(hex.EncodeToString(raw[:]))
	account := helpers.AcquireUserFrom(r.Context())
	_, err = b.config.Store.run(r.Context(), one(r, ContextHeader), binding, "challenge", rv.ID, codeHash(rv.ID, code), account.ID, 0)
	if err != nil {
		b.fail(w, err)
		return
	}
	b.record(r.Context(), "code_requested", rv)
	body := fmt.Sprintf(`<p>Confirm one token-allowance change in %s (%s) for user %s.</p><p>Permanent: %d; ephemeral: %d; TTL: %d–%d seconds; increment: %d seconds; reviewed revision: %d.</p><p>Code: <strong>%s</strong></p><p>Enter this code only in the Admin Access page where you started. It expires with your review, within five minutes. It does not sign you in, change your email or grant an administrator role. If you did not request this, ignore the email.</p>`, html.EscapeString(b.config.System), html.EscapeString(b.config.Environment), html.EscapeString(rv.Target), rv.Limits.Permanent, rv.Limits.Ephemeral, rv.Limits.MinimumTTL, rv.Limits.MaximumTTL, rv.Limits.TTLIncrement, rv.Revision, code)
	err = b.config.Email.SendCustomEmail(r.Context(), &emailmanager.SendCustomEmailRequest{EmailSubject: "Confirm token allowance change", EmailPreview: "One reviewed administrator action", EmailBody: body, EmailTo: account.Email, WithFooter: true, UserId: account.ID, RecipientType: "USER"})
	if err != nil {
		_, _ = b.config.Store.run(r.Context(), one(r, ContextHeader), binding, "cancel", rv.ID, "", "", 0)
		b.fail(w, ErrUnavailable)
		return
	}
	b.respond(w, map[string]any{"expires_at": rv.Expires})
}

// confirm consumes only this review's code. Verification grants no role, does
// not refresh a session, and cannot extend the original operation deadline.
func (b *Bridge) confirm(w http.ResponseWriter, r *http.Request) {
	code, err := readCode(r)
	if err != nil {
		b.fail(w, err)
		return
	}
	binding, rv, err := b.bound(r)
	if err != nil {
		b.fail(w, err)
		return
	}
	_, err = b.config.Store.run(r.Context(), one(r, ContextHeader), binding, "confirm", rv.ID, codeHash(rv.ID, code), "", 0)
	if err != nil {
		b.record(r.Context(), "code_rejected", rv)
		b.fail(w, err)
		return
	}
	b.record(r.Context(), "code_confirmed", rv)
	b.respond(w, map[string]any{"expires_at": rv.Expires})
}

// record provides safe operational evidence alongside the shared manager's
// durable policy audit. It never includes raw context/review handles, proofs,
// email addresses, bodies, credentials or dependency diagnostics.
func (b *Bridge) record(ctx context.Context, event string, r review) {
	logger.AcquireOperationFrom(ctx, "external/accesspolicy/adminaccess", event).Info(event,
		zap.String("actor_id", helpers.AcquireFrom(ctx)), zap.String("target_id", r.Target),
		zap.String("system", b.config.System), zap.String("review_digest", digest(r.ID)), zap.Int64("expected_revision", r.Revision))
}

// cancel ends a review without signing out. It cannot retract an operation
// already consumed/dispatched; clients must not treat cancellation as rollback.
func (b *Bridge) cancel(w http.ResponseWriter, r *http.Request) {
	if !emptyBody(r) {
		b.fail(w, accesspolicymanager.ErrInvalidRequest)
		return
	}
	binding, err := b.identity(r.Context())
	if err != nil {
		b.fail(w, err)
		return
	}
	_, err = b.config.Store.run(r.Context(), one(r, ContextHeader), binding, "cancel", one(r, ReviewHeader), "", "", 0)
	if err != nil {
		b.fail(w, err)
		return
	}
	b.respond(w, map[string]bool{"cancelled": true})
}

// emptyBody permits no representation or content encoding on bodyless commands.
func emptyBody(r *http.Request) bool {
	if len(r.Header.Values("Content-Encoding")) != 0 {
		return false
	}
	if r.Body == nil {
		return true
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1))
	return err == nil && len(raw) == 0
}

// readCode accepts one bounded JSON string field, rejecting duplicate keys,
// unknown members, trailing values and every noncanonical code representation.
func readCode(r *http.Request) (string, error) {
	bad := accesspolicymanager.ErrInvalidRequest
	if one(r, "Content-Type") != "application/json" || len(r.Header.Values("Content-Encoding")) != 0 || r.Body == nil {
		return "", bad
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 129))
	if err != nil || len(raw) > 128 {
		return "", bad
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return "", bad
	}
	key, err := d.Token()
	if err != nil || key != "code" {
		return "", bad
	}
	var code string
	if d.Decode(&code) != nil || len(code) != 12 {
		return "", bad
	}
	for _, ch := range code {
		if !(ch >= '0' && ch <= '9' || ch >= 'A' && ch <= 'F') {
			return "", bad
		}
	}
	last, err := d.Token()
	if err != nil || last != json.Delim('}') {
		return "", bad
	}
	if _, err = d.Token(); !errors.Is(err, io.EOF) {
		return "", bad
	}
	return code, nil
}

// fail canonicalizes errors so unmapped infrastructure details cannot be logged
// or returned by reply. Successful payloads use its standard data envelope too.
func (b *Bridge) fail(w http.ResponseWriter, err error) {
	_ = reply.NewReplier(b.manifests).NewHTTPErrorResponse(w, errormanifest.CanonicalError(err, b.manifests))
}
func (b *Bridge) respond(w http.ResponseWriter, data any) {
	_ = reply.NewReplier(b.manifests).NewHTTPDataResponse(w, http.StatusOK, data)
}
