package partnermanagerhelper

import (
	"context"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/ephemeral"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

const (
	partnersSignupCookieName = "host-partners-signup"
	partnersVisitCookieName  = "host-partners-visit"
)

func referralTestRenderer(w io.Writer, page ReferralPageView) error {
	_, err := fmt.Fprintf(w, "Available=%t Window=%s Visit=%s Continue without referral", page.Available, page.Window, page.VisitWindow)
	return err
}

const partnersReferralTestCode = "Abcdefghijklmnopqrs_12"

type partnersVisitFixture struct {
	out     partnermanager.PreparedVisit
	err     error
	cancel  context.CancelFunc
	calls   int
	request partnermanager.PrepareVisitRequest
}

func (p *partnersVisitFixture) PrepareVisit(_ context.Context, request partnermanager.PrepareVisitRequest) (partnermanager.PreparedVisit, error) {
	p.calls++
	p.request = request
	if p.cancel != nil {
		p.cancel()
	}
	return p.out, p.err
}

func partnersReferralFixture() (*PartnersReferralHandler, *partnersVisitFixture, time.Time) {
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	visit := &partnersVisitFixture{out: partnermanager.PreparedVisit{Evidence: "private-signed-evidence", VisitCookie: "private-signed-visit", EvidenceExpiresAt: now.Add(30 * 24 * time.Hour), VisitExpiresAt: now.Add(time.Hour), Measurement: "eligible"}}
	h := &PartnersReferralHandler{signupCookieName: partnersSignupCookieName, visitCookieName: partnersVisitCookieName, signupPath: "/auth/signup", renderPage: referralTestRenderer, visits: visit, origin: "https://app.example.test", window: 30 * 24 * time.Hour, visitWindow: time.Hour, clock: partnerearnings.ClockFunc(func() time.Time { return now }), admission: func(next http.Handler) http.Handler { return next }}
	return h, visit, now
}

func TestPartnersReferralExplicitConsentAndFailureResponses(t *testing.T) {
	type testCase struct {
		name, state, method, body string
		status, calls, cookies    int
	}
	for _, tc := range []testCase{
		{name: "landing_issues_no_cookie", method: "GET", status: 200},
		{name: "head_issues_no_cookie_or_body", method: "HEAD", status: 200},
		{name: "disabled_landing_keeps_decline", state: "disabled", method: "GET", status: 503},
		{name: "positive_consent_issues_exact_cookies", body: "choice=yes", status: 303, calls: 1, cookies: 2},
		{name: "no_measurement_clears_only_visit_cookie", state: "unmeasured", body: "choice=yes", status: 303, calls: 1, cookies: 2},
		{name: "analytics_outage_keeps_valid_signup_evidence", state: "analytics-outage", body: "choice=yes", status: 303, calls: 1, cookies: 2},
		{name: "known_bot_does_not_seed_signup", state: "bot", body: "choice=yes", status: 303, calls: 1, cookies: 2},
		{name: "decline_clears_referral_cookies", body: "choice=no", status: 303, cookies: 2},
		{name: "decline_bypasses_missing_manager", state: "disabled", body: "choice=no", status: 303, cookies: 2},
		{name: "decline_bypasses_failed_rate_admission", state: "rate", body: "choice=no", status: 303, cookies: 2},
		{name: "decline_bypasses_invalid_code_and_cookies", state: "decline-invalid", body: "choice=no", status: 303, cookies: 2},
		{name: "missing_origin_cannot_opt_in", state: "missing-origin", body: "choice=yes", status: 403},
		{name: "cross_origin_cannot_opt_in", state: "cross-origin", body: "choice=yes", status: 403},
		{name: "null_origin_cannot_opt_in", state: "null-origin", body: "choice=yes", status: 403},
		{name: "duplicate_origin_cannot_opt_in", state: "duplicate-origin", body: "choice=yes", status: 403},
		{name: "decline_is_also_same_origin", state: "cross-origin", body: "choice=no", status: 403},
		{name: "json_is_not_a_consent_form", state: "json", body: "choice=yes", status: 400},
		{name: "bounded_body", state: "large-body", status: 400},
		{name: "duplicate_choice_rejected", body: "choice=yes&choice=no", status: 400},
		{name: "query_is_not_consent", state: "query", status: 400},
		{name: "unknown_body_fields_rejected", body: "choice=yes&actor=customer", status: 400},
		{name: "unknown_choice_rejected", body: "choice=maybe", status: 400},
		{name: "unsupported_method_rejected", method: "PUT", body: "choice=yes", status: 405},
		{name: "duplicate_evidence_cookie_rejected", state: "duplicate-cookie", body: "choice=yes", status: 400},
		{name: "bounded_evidence_cookie", state: "large-cookie", body: "choice=yes", status: 400},
		{name: "unavailable_runtime_has_no_capture", state: "disabled", body: "choice=yes", status: 503},
		{name: "invalid_code_has_no_capture", state: "invalid-code", body: "choice=yes", status: 503},
		{name: "rate_limit_keeps_decline_navigation", state: "rate", body: "choice=yes", status: 429},
		{name: "admission_outage_is_unavailable", state: "admission-outage", body: "choice=yes", status: 503},
		{name: "contradictory_admission_rejected", state: "contradictory-admission", body: "choice=yes", status: 503},
		{name: "native_denial_has_no_cookies", state: "native-denied", body: "choice=yes", status: 503, calls: 1},
		{name: "native_outage_has_no_diagnostic_leak", state: "native-outage", body: "choice=yes", status: 503, calls: 1},
		{name: "missing_signup_evidence_is_not_success", state: "missing-evidence", body: "choice=yes", status: 503, calls: 1},
		{name: "unsafe_cookie_value_not_sanitized_to_success", state: "unsafe-token", body: "choice=yes", status: 503, calls: 1},
		{name: "issuer_evidence_is_bounded", state: "large-token", body: "choice=yes", status: 503, calls: 1},
		{name: "expired_evidence_is_not_set", state: "expired", body: "choice=yes", status: 503, calls: 1},
		{name: "overlong_evidence_lifetime_is_not_set", state: "long-expiry", body: "choice=yes", status: 503, calls: 1},
		{name: "missing_visit_expiry_cannot_partially_set_signup", state: "bad-visit", body: "choice=yes", status: 503, calls: 1},
		{name: "cancelled_before_capture_has_no_cookies", state: "pre-cancel", body: "choice=yes", status: 503},
		{name: "cancelled_after_capture_has_no_cookies", state: "post-cancel", body: "choice=yes", status: 503, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, visit, now := partnersReferralFixture()
			method := tc.method
			if method == "" {
				method = "POST"
			}
			body := tc.body
			if tc.state == "large-body" {
				body = "choice=yes&padding=" + strings.Repeat("a", 4096)
			}
			r := httptest.NewRequest(method, "/ref/"+partnersReferralTestCode+"?next=https://evil.example.test&c=attacker-key&choice=yes", strings.NewReader(body))
			r.Header.Set("Origin", h.origin)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.RemoteAddr = "192.0.2.1:45321"
			r = mux.SetURLVars(r, map[string]string{"code": partnersReferralTestCode})
			r.AddCookie(&http.Cookie{Name: partnersSignupCookieName, Value: "prior-evidence"})
			r.AddCookie(&http.Cookie{Name: partnersVisitCookieName, Value: "prior-visit"})
			switch tc.state {
			case "disabled":
				h.visits = nil
			case "unmeasured":
				h.visitWindow = 0
				visit.out.VisitCookie, visit.out.VisitExpiresAt, visit.out.Measurement = "", time.Time{}, "disabled"
			case "analytics-outage":
				visit.out.Measurement = "unavailable"
			case "bot":
				r.Header.Set("User-Agent", "Googlebot/2.1")
				visit.out = partnermanager.PreparedVisit{Measurement: "known_bot"}
			case "missing-origin":
				r.Header.Del("Origin")
			case "cross-origin":
				r.Header.Set("Origin", "https://evil.example.test")
			case "null-origin":
				r.Header.Set("Origin", "null")
			case "duplicate-origin":
				r.Header.Add("Origin", h.origin)
			case "json":
				r.Header.Set("Content-Type", "application/json")
			case "duplicate-cookie":
				r.AddCookie(&http.Cookie{Name: partnersSignupCookieName, Value: "second-evidence"})
			case "large-cookie":
				r.Header.Set("Cookie", partnersSignupCookieName+"="+strings.Repeat("a", 2049))
			case "decline-invalid", "invalid-code":
				r = mux.SetURLVars(r, map[string]string{"code": "bad/code"})
				r.Header.Set("Cookie", partnersSignupCookieName+"="+strings.Repeat("a", 2049))
			case "rate", "admission-outage", "contradictory-admission":
				h.admission = func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Set-Cookie", "private-admission-token")
						status := 500
						if tc.state == "rate" {
							status = 429
						}
						w.WriteHeader(status)
						_, _ = w.Write([]byte("private-admission-error"))
						if tc.state == "contradictory-admission" {
							next.ServeHTTP(w, r)
						}
					})
				}
			case "native-denied":
				visit.err = partnermanager.ErrDenied
			case "native-outage":
				visit.err = errors.New("private-driver-error")
			case "missing-evidence":
				visit.out.Evidence = ""
			case "unsafe-token":
				visit.out.Evidence = "private;invalid"
			case "large-token":
				visit.out.Evidence = strings.Repeat("a", 2049)
			case "expired":
				visit.out.EvidenceExpiresAt = now
			case "long-expiry":
				visit.out.EvidenceExpiresAt = now.Add(31 * 24 * time.Hour)
			case "bad-visit":
				visit.out.VisitExpiresAt = time.Time{}
			case "pre-cancel", "post-cancel":
				ctx, cancel := context.WithCancel(r.Context())
				t.Cleanup(cancel)
				r = r.WithContext(ctx)
				if tc.state == "pre-cancel" {
					cancel()
				} else {
					visit.cancel = cancel
				}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code)
			require.Equal(t, tc.calls, visit.calls)
			cookies := w.Result().Cookies()
			require.Len(t, cookies, tc.cookies)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			require.Equal(t, "same-origin", w.Header().Get("Referrer-Policy"), "browser consent forms must retain their Origin without leaking referral URLs cross-origin")
			require.Equal(t, "noindex, nofollow", w.Header().Get("X-Robots-Tag"))
			require.Contains(t, w.Header().Get("Content-Security-Policy"), "form-action 'self'; frame-ancestors 'none'")
			for _, private := range []string{"private-signed-evidence", "private-signed-visit", "prior-evidence", "private-driver-error", "private-admission-error", "private-admission-token", "attacker-key", "evil.example.test"} {
				require.NotContains(t, w.Body.String(), private)
			}
			if method == "HEAD" {
				require.Empty(t, w.Body.String())
			}
			if tc.status == 303 {
				require.Equal(t, "/auth/signup", w.Header().Get("Location"))
				for _, cookie := range cookies {
					require.True(t, cookie.HttpOnly)
					require.True(t, cookie.Secure)
					require.Empty(t, cookie.Domain)
					require.Equal(t, "/", cookie.Path)
					require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
					if cookie.Value == "" {
						require.Equal(t, -1, cookie.MaxAge)
					} else {
						require.Zero(t, cookie.MaxAge)
					}
				}
			}
			if visit.calls != 0 {
				require.Equal(t, partnersReferralTestCode, visit.request.Code, "case-sensitive path code preserved")
				require.True(t, visit.request.Consented)
				require.Equal(t, tc.state == "bot", visit.request.KnownBot)
				require.Equal(t, "prior-evidence", visit.request.PriorEvidence)
				require.Equal(t, "prior-visit", visit.request.PriorVisit)
			}
		})
	}
}

type partnersAdmissionStoreFixture struct {
	ip, code   string
	blocked    bool
	err        error
	checks     int
	attempts   int
	blockCalls int
}

func (s *partnersAdmissionStoreFixture) IsIPBlocked(_ context.Context, ip string) (bool, error) {
	s.checks++
	s.ip = ip
	return s.blocked, s.err
}
func (s *partnersAdmissionStoreFixture) TrackHardenedAttempt(_ context.Context, ip, code string, _ int, _ time.Duration) error {
	s.attempts++
	s.ip, s.code = ip, code
	return s.err
}
func (s *partnersAdmissionStoreFixture) BlockIP(context.Context, string, time.Duration) error {
	s.blockCalls++
	return nil
}

func TestPartnersReferralUsesNativeAdmissionWithTrustedPeerAndPrivateLogs(t *testing.T) {
	type testCase struct {
		name, peer, forwarded, expectedIP   string
		trusted, duplicate, blocked, outage bool
		status, attempts                    int
	}
	for _, tc := range []testCase{
		{name: "socket_peer_without_source_port", peer: "192.0.2.1:1000", expectedIP: "192.0.2.1", status: 200, attempts: 1},
		{name: "new_socket_port_does_not_change_identity", peer: "192.0.2.1:9999", expectedIP: "192.0.2.1", status: 200, attempts: 1},
		{name: "untrusted_forwarded_identity_ignored", peer: "192.0.2.1:1000", forwarded: "203.0.113.42", expectedIP: "192.0.2.1", status: 200, attempts: 1},
		{name: "explicit_proxy_uses_valid_client_ip", peer: "192.0.2.1:1000", forwarded: "203.0.113.42", trusted: true, expectedIP: "203.0.113.42", status: 200, attempts: 1},
		{name: "ipv6_normalized_without_port", peer: "[2001:db8::1]:1000", expectedIP: "2001:db8::1", status: 200, attempts: 1},
		{name: "mapped_ipv4_normalized", peer: "[::ffff:192.0.2.1]:1000", expectedIP: "192.0.2.1", status: 200, attempts: 1},
		{name: "trusted_proxy_missing_header_closed", peer: "192.0.2.1:1000", trusted: true, status: 503},
		{name: "trusted_proxy_duplicate_header_closed", peer: "192.0.2.1:1000", forwarded: "203.0.113.42", trusted: true, duplicate: true, status: 503},
		{name: "trusted_proxy_malformed_ip_closed", peer: "192.0.2.1:1000", forwarded: "attacker-canary", trusted: true, status: 503},
		{name: "invalid_socket_peer_closed", peer: "attacker-canary", status: 503},
		{name: "actual_native_block_preserves_429", peer: "192.0.2.1:1000", expectedIP: "192.0.2.1", blocked: true, status: 429},
		{name: "actual_native_outage_preserves_unavailable", peer: "192.0.2.1:1000", expectedIP: "192.0.2.1", outage: true, status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := partnersReferralFixture()
			store := &partnersAdmissionStoreFixture{blocked: tc.blocked}
			if tc.outage {
				store.err = errors.New("private-storage-diagnostic")
			}
			h.admission = accessmiddleware.NewHardenedRateLimitProtection(&accessmiddleware.NewHardenedRateLimitProtectionRequest{EphemeralStore: store, ErrorMaps: []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap}}).Middleware()
			if tc.trusted {
				h.trustedProxies = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
			}
			r := httptest.NewRequest("POST", "/ref/"+partnersReferralTestCode+"?c=arbitrary-public-code", nil)
			r.RemoteAddr = tc.peer
			if tc.forwarded != "" {
				r.Header.Set("CF-Connecting-IP", tc.forwarded)
			}
			if tc.duplicate {
				r.Header.Add("CF-Connecting-IP", tc.forwarded)
			}
			core, logs := observer.New(zap.DebugLevel)
			r = r.WithContext(ghatdlogger.TransitWith(r.Context(), zap.New(core)))
			require.Equal(t, tc.status, h.admit(r))
			require.Equal(t, tc.expectedIP, store.ip)
			require.Equal(t, tc.attempts, store.attempts)
			require.Empty(t, store.code, "public referral links must not consume a shared per-code quota")
			require.Zero(t, store.blockCalls, "outage is never an abuse ban")
			require.Empty(t, logs.All(), "native admission raw-IP diagnostics must not enter the request logger")
			ghatdlogger.AcquireFrom(r.Context()).Info("original-context-still-active")
			require.Len(t, logs.All(), 1, "suppression must not mutate the domain/request logger")
			require.Equal(t, tc.peer, r.RemoteAddr)
			require.Equal(t, "arbitrary-public-code", r.URL.Query().Get("c"))
			require.Equal(t, tc.forwarded, r.Header.Get("CF-Connecting-IP"))
		})
	}
}
