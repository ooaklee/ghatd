package partnermanagerhelper

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/gorilla/mux"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partnermanager"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"go.uber.org/zap"
)

// PartnersReferralConfig supplies trusted host choices for anonymous consent.
// Cookie security and same-origin consent are mandatory; no settings or manager
// runtime types from the host application enter this package.
type PartnersReferralConfig struct {
	// PublicOrigin is an HTTPS origin, without credentials, query or path.
	PublicOrigin string
	// ReferralPath and SignupPath are canonical root-relative paths. Empty values
	// select /ref and /auth/signup respectively.
	ReferralPath, SignupPath string
	// Both cookie names are explicit, distinct host-owned names.
	SignupCookieName, VisitCookieName string
	AttributionWindow, VisitWindow    time.Duration
	// AllowInsecureLoopback permits HTTP cookies only for a loopback origin.
	// The host must enable it solely for its explicit local environment.
	AllowInsecureLoopback bool
	// TrustedProxyCIDRs limits CF-Connecting-IP trust to canonical peer networks.
	TrustedProxyCIDRs []string
}

// VisitPreparer is the owning manager's consented referral issuance port.
type VisitPreparer interface {
	PrepareVisit(context.Context, partnermanager.PrepareVisitRequest) (partnermanager.PreparedVisit, error)
}

// ReferralPageView contains only public consent presentation data, never tokens,
// provider evidence, selected identities or raw dependency diagnostics.
type ReferralPageView struct {
	Available, Measured bool
	Window, VisitWindow string
}

// ReferralPageRenderer renders the host's branded consent page. It receives an
// io.Writer so it cannot override the handler's response security headers.
type ReferralPageRenderer func(io.Writer, ReferralPageView) error

// ReferralDependencies borrows the owning manager and rate-admission middleware.
// Visits=nil explicitly disables capture; decline/navigation remain available.
// A live Visits port requires a clock, admission middleware and positive window.
// RenderPage is required in both enabled and disabled modes.
type ReferralDependencies struct {
	Visits     VisitPreparer
	Clock      partnermanager.Clock
	Admission  mux.MiddlewareFunc
	RenderPage ReferralPageRenderer
}

// PartnersReferralHandler owns immutable validated transport configuration.
// Authentication is not required for anonymous consent; this grants no account
// or worker authority. Financial and eligibility decisions stay with Visits.
type PartnersReferralHandler struct {
	visits                                                              VisitPreparer
	admission                                                           mux.MiddlewareFunc
	clock                                                               partnermanager.Clock
	origin, referralPath, signupPath, signupCookieName, visitCookieName string
	window, visitWindow                                                 time.Duration
	insecureLocal                                                       bool
	trustedProxies                                                      []netip.Prefix
	renderPage                                                          ReferralPageRenderer
}

// NewPartnersReferralHandler validates and copies host configuration without
// invoking storage, providers or admission, registering routes or starting work.
func NewPartnersReferralHandler(cfg PartnersReferralConfig, deps ReferralDependencies) (*PartnersReferralHandler, error) {
	invalid := fmt.Errorf("partners/referral-configuration-invalid")
	if deps.RenderPage == nil || !validReferralCookieName(cfg.SignupCookieName) || !validReferralCookieName(cfg.VisitCookieName) || cfg.SignupCookieName == cfg.VisitCookieName {
		return nil, invalid
	}
	if cfg.ReferralPath == "" {
		cfg.ReferralPath = "/ref"
	}
	if cfg.SignupPath == "" {
		cfg.SignupPath = "/auth/signup"
	}
	if !validReferralPath(cfg.ReferralPath) || !validReferralPath(cfg.SignupPath) || cfg.ReferralPath == cfg.SignupPath || strings.HasPrefix(cfg.SignupPath, cfg.ReferralPath+"/") {
		return nil, invalid
	}
	u, err := url.Parse(cfg.PublicOrigin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return nil, invalid
	}
	insecure := cfg.AllowInsecureLoopback && u.Scheme == "http" && referralLoopbackHost(u.Hostname())
	if u.Scheme != "https" && !insecure {
		return nil, invalid
	}
	if cfg.AttributionWindow < 0 || cfg.AttributionWindow > 180*24*time.Hour || cfg.VisitWindow < 0 || cfg.VisitWindow > 24*time.Hour || cfg.VisitWindow > 0 && cfg.VisitWindow < time.Second {
		return nil, invalid
	}
	if !nilHelperPort(deps.Visits) && (nilHelperPort(deps.Clock) || deps.Admission == nil || cfg.AttributionWindow <= 0) {
		return nil, invalid
	}
	if len(cfg.TrustedProxyCIDRs) > 32 {
		return nil, invalid
	}
	h := &PartnersReferralHandler{visits: deps.Visits, admission: deps.Admission, clock: deps.Clock, origin: u.Scheme + "://" + u.Host, referralPath: cfg.ReferralPath, signupPath: cfg.SignupPath, signupCookieName: cfg.SignupCookieName, visitCookieName: cfg.VisitCookieName, window: cfg.AttributionWindow, visitWindow: cfg.VisitWindow, insecureLocal: insecure, renderPage: deps.RenderPage}
	for _, value := range cfg.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return nil, invalid
		}
		h.trustedProxies = append(h.trustedProxies, prefix)
	}
	return h, nil
}

// AttachPartnersReferralRoutes registers consent routes before the SPA fallback,
// including safe refusal/navigation while the owning runtime is disabled.
func AttachPartnersReferralRoutes(router *ghatdrouter.Router, cfg PartnersReferralConfig, deps ReferralDependencies) error {
	if router == nil {
		return fmt.Errorf("partners/referral-router-unavailable")
	}
	h, err := NewPartnersReferralHandler(cfg, deps)
	if err != nil {
		return err
	}
	group := router.NewRouteGroup(h.referralPath, ghatdrouter.Public, nil)
	for _, route := range []string{"", "/{code:.*}"} {
		suffix := "entry"
		if route == "" {
			suffix = "missing-code"
		}
		group.Handle(ghatdrouter.RouteDefinition{Methods: []string{http.MethodGet, http.MethodHead}, Path: route, Operation: "partners.referral." + suffix}, h.ServeHTTP)
		group.Handle(ghatdrouter.RouteDefinition{Methods: []string{http.MethodPost}, Path: route, Operation: "partners.referral." + suffix + ".consent", Access: ghatdrouter.HandlerVerified, Policy: ghatdrouter.RoutePolicy{Proof: "partners.referral.same-origin-consent.rate-native-eligibility"}}, h.ServeHTTP)
		group.Handle(ghatdrouter.RouteDefinition{Methods: []string{http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions}, Path: route, Operation: "partners.referral." + suffix + ".unsupported-method"}, h.ServeHTTP)
	}
	router.GetRouter().Path(h.referralPath).Handler(h)
	router.GetRouter().PathPrefix(h.referralPath + "/").Handler(h)
	return router.ValidateRoutePolicies()
}

func referralLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip, err := netip.ParseAddr(host)
	return err == nil && ip.IsLoopback()
}

func validReferralCookieName(name string) bool {
	return name != "" && len(name) <= 128 && (&http.Cookie{Name: name, Value: "valid"}).Valid() == nil
}

func validReferralPath(value string) bool {
	if len(value) > 256 || value == "/" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || path.Clean(value) != value {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/._-", c)) {
			return false
		}
	}
	return true
}

func nilHelperPort(port any) bool {
	if port == nil {
		return true
	}
	value := reflect.ValueOf(port)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (h *PartnersReferralHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.headers(w)
	code := mux.Vars(r)["code"]
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		available := !nilHelperPort(h.visits) && validPartnersReferralCode(code)
		status := http.StatusOK
		if !available {
			status = http.StatusServiceUnavailable
		}
		h.page(w, r, status, available)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, HEAD, POST")
		h.page(w, r, http.StatusMethodNotAllowed, false)
		return
	}
	// An anonymous choice grants no account authority. Same-origin browser
	// proof prevents another site silently opting a visitor into referral cookies.
	if origins := r.Header.Values("Origin"); len(origins) != 1 || origins[0] != h.origin {
		h.page(w, r, http.StatusForbidden, false)
		return
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" || (len(params) != 0 && (len(params) != 1 || !strings.EqualFold(params["charset"], "utf-8"))) {
		h.page(w, r, http.StatusBadRequest, false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil || len(r.PostForm) != 1 || len(r.PostForm["choice"]) != 1 {
		h.page(w, r, http.StatusBadRequest, false)
		return
	}
	choice := r.PostForm.Get("choice")
	if choice != "yes" && choice != "no" {
		h.page(w, r, http.StatusBadRequest, false)
		return
	}
	if r.Context().Err() != nil {
		h.page(w, r, http.StatusServiceUnavailable, false)
		return
	}
	if choice == "no" {
		h.clearCookies(w)
		h.signup(w, r)
		return
	}
	if nilHelperPort(h.visits) || nilHelperPort(h.clock) || h.admission == nil || !validPartnersReferralCode(code) {
		h.page(w, r, http.StatusServiceUnavailable, false)
		return
	}
	evidence, validEvidence := partnersReferralCookie(r, h.signupCookieName)
	visit, validVisit := partnersReferralCookie(r, h.visitCookieName)
	if !validEvidence || !validVisit {
		h.page(w, r, http.StatusBadRequest, false)
		return
	}
	if status := h.admit(r); status != http.StatusOK {
		h.page(w, r, status, false)
		return
	}
	knownBot := partnersKnownBot(r.UserAgent())
	out, err := h.visits.PrepareVisit(r.Context(), partnermanager.PrepareVisitRequest{Code: code, Consented: true, KnownBot: knownBot, PriorEvidence: evidence, PriorVisit: visit})
	if err != nil || !h.validPreparedVisit(out, knownBot) || r.Context().Err() != nil {
		h.page(w, r, http.StatusServiceUnavailable, false)
		return
	}
	// Validate both outputs before setting either cookie. Native absolute expiry
	// is preserved; a reload cannot introduce a sliding Max-Age lifetime.
	h.cookie(w, h.signupCookieName, out.Evidence, out.EvidenceExpiresAt)
	h.cookie(w, h.visitCookieName, out.VisitCookie, out.VisitExpiresAt)
	h.signup(w, r)
}

// Reuse the owning rate limiter in a separate referral namespace. Its legacy
// code key is deliberately empty: a public referral code is shared by visitors,
// not a secret verification code. Browser query parameters cannot select keys.
func (h *PartnersReferralHandler) admit(r *http.Request) int {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return http.StatusServiceUnavailable
	}
	peer, err := netip.ParseAddr(host)
	if err != nil || peer.Zone() != "" {
		return http.StatusServiceUnavailable
	}
	peer = peer.Unmap()
	client := peer
	for _, prefix := range h.trustedProxies {
		if !prefix.Contains(peer) {
			continue
		}
		values := r.Header.Values("CF-Connecting-IP")
		if len(values) != 1 {
			return http.StatusServiceUnavailable
		}
		client, err = netip.ParseAddr(values[0])
		if err != nil || client.Zone() != "" {
			return http.StatusServiceUnavailable
		}
		client = client.Unmap()
		break
	}
	// The shared middleware/store log raw client IPs. Suppress only admission's
	// contextual logger; the domain call keeps the original request context.
	request := r.Clone(ghatdlogger.TransitWith(r.Context(), zap.NewNop()))
	request.RemoteAddr = client.String()
	request.URL.RawQuery = ""
	for key := range request.Header {
		if strings.EqualFold(key, "CF-Connecting-IP") {
			delete(request.Header, key)
		}
	}
	response := &partnersAdmissionResponse{header: make(http.Header)}
	admitted := 0
	h.admission(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { admitted++ })).ServeHTTP(response, request)
	if admitted == 1 && response.status == 0 && r.Context().Err() == nil {
		return http.StatusOK
	}
	if admitted == 0 && response.status == http.StatusTooManyRequests {
		return http.StatusTooManyRequests
	}
	return http.StatusServiceUnavailable
}

type partnersAdmissionResponse struct {
	header http.Header
	status int
}

func (w *partnersAdmissionResponse) Header() http.Header { return w.header }
func (w *partnersAdmissionResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *partnersAdmissionResponse) Write(body []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return len(body), nil
}

func partnersReferralCookie(r *http.Request, name string) (string, bool) {
	var found string
	count := 0
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			count++
			found = cookie.Value
		}
	}
	return found, count <= 1 && len(found) <= 2048
}

func validPartnersReferralCode(code string) bool {
	if len(code) != 22 {
		return false
	}
	for _, char := range code {
		allowed := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_'
		if !allowed {
			return false
		}
	}
	return true
}

func partnersKnownBot(userAgent string) bool {
	if len(userAgent) > 512 {
		return true // unknown oversized agent cannot seed attribution
	}
	value := strings.ToLower(userAgent)
	for _, name := range []string{"googlebot", "bingbot", "duckduckbot", "baiduspider", "yandexbot", "facebookexternalhit", "twitterbot", "slackbot"} {
		if strings.Contains(value, name) {
			return true
		}
	}
	return false // an exclusion heuristic, never proof of a human visitor
}

func (h *PartnersReferralHandler) validPreparedVisit(out partnermanager.PreparedVisit, knownBot bool) bool {
	now := h.clock.Now().UTC()
	if now.IsZero() || h.window <= 0 || h.window > 180*24*time.Hour || h.visitWindow < 0 || h.visitWindow > 24*time.Hour {
		return false
	}
	if knownBot {
		return out.Evidence == "" && out.VisitCookie == "" && out.EvidenceExpiresAt.IsZero() && out.VisitExpiresAt.IsZero()
	}
	valid := func(name, value string, expiry time.Time, window time.Duration) bool {
		if value == "" {
			return expiry.IsZero()
		}
		return window > 0 && len(value) <= 2048 && expiry.Truncate(time.Second).After(now) && expiry.Sub(now) <= window && (&http.Cookie{Name: name, Value: value, Expires: expiry}).Valid() == nil
	}
	return out.Evidence != "" && valid(h.signupCookieName, out.Evidence, out.EvidenceExpiresAt, h.window) && valid(h.visitCookieName, out.VisitCookie, out.VisitExpiresAt, h.visitWindow)
}

func (h *PartnersReferralHandler) cookie(w http.ResponseWriter, name, value string, expiry time.Time) {
	cookie := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !h.insecureLocal, SameSite: http.SameSiteLaxMode, Expires: expiry}
	if value == "" {
		cookie.Expires, cookie.MaxAge = time.Unix(1, 0).UTC(), -1
	}
	http.SetCookie(w, cookie)
}

func (h *PartnersReferralHandler) clearCookies(w http.ResponseWriter) {
	h.cookie(w, h.signupCookieName, "", time.Time{})
	h.cookie(w, h.visitCookieName, "", time.Time{})
}

func (h *PartnersReferralHandler) signup(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, h.signupPath, http.StatusSeeOther)
}

func (*PartnersReferralHandler) headers(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// no-referrer makes browser form POSTs send Origin: null, which cannot
	// establish consent. Keep the origin for same-origin forms while withholding
	// referral URLs from cross-origin navigation and resources.
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
}

func (h *PartnersReferralHandler) page(w http.ResponseWriter, r *http.Request, status int, available bool) {
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	_ = h.renderPage(referralPageWriter{w}, ReferralPageView{Available: available, Measured: h.visitWindow > 0, Window: partnersReferralWindow(h.window), VisitWindow: partnersReferralWindow(h.visitWindow)})
}

// Hide ResponseWriter capabilities even from a renderer's type assertion.
type referralPageWriter struct{ writer io.Writer }

func (w referralPageWriter) Write(body []byte) (int, error) { return w.writer.Write(body) }

func partnersReferralWindow(window time.Duration) string {
	if window >= 24*time.Hour && window%(24*time.Hour) == 0 {
		return fmt.Sprintf("%d days", window/(24*time.Hour))
	}
	return window.String()
}
