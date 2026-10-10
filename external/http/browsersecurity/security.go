package browsersecurity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CSRFHeader carries the principal-bound token returned by Issue.
const CSRFHeader = "X-CSRF-Token"

// Config is trusted host wiring. Keys and allow-lists are copied at construction.
// It neither authenticates sessions nor grants permission to perform an action.
type Config struct {
	AllowedOrigins []string
	Key            []byte
	// CSRFCookieName is a bounded, valid cookie name chosen by the host.
	CSRFCookieName string
	Now            func() time.Time
	// CSRFTTL defaults to one hour and must be between one minute and one day.
	CSRFTTL time.Duration
	// InsecureLocalCookies permits non-Secure cookies only on loopback origins.
	// The cookie name must not use a Secure or Host prefix in this mode.
	InsecureLocalCookies bool
	// TrustedNativeClientIDs are verified token audiences supplied by the host
	// resolver, never client-submitted names or unverified token claims.
	TrustedNativeClientIDs []string
}

// Security protects browser transport using signed, principal-bound CSRF cookies.
// It owns no session store, financial receipt, provider, worker or database.
type Security struct {
	origins       map[string]bool
	nativeClients map[string]bool
	key           []byte
	now           func() time.Time
	ttl           time.Duration
	insecureLocal bool
	csrfName      string
}

// csrfSession is the cookie payload: a 43-byte nonce, its principal binding and
// an expiry.
type csrfSession struct {
	Nonce     string `json:"nonce"`
	Binding   string `json:"binding"`
	ExpiresAt int64  `json:"expires_at"`
}

// New validates immutable wiring without network or storage I/O.
func New(config Config) (*Security, error) {
	if len(config.Key) < 32 || len(config.AllowedOrigins) == 0 || len(config.CSRFCookieName) > 128 || config.CSRFCookieName == "" || (&http.Cookie{Name: config.CSRFCookieName}).Valid() != nil {
		return nil, ErrConfigurationInvalid
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.CSRFTTL == 0 {
		config.CSRFTTL = time.Hour
	}
	if config.CSRFTTL < time.Minute || config.CSRFTTL > 24*time.Hour {
		return nil, ErrConfigurationInvalid
	}
	security := &Security{origins: map[string]bool{}, nativeClients: map[string]bool{}, key: append([]byte(nil), config.Key...), now: config.Now, ttl: config.CSRFTTL, insecureLocal: config.InsecureLocalCookies, csrfName: config.CSRFCookieName}
	if security.insecureLocal && (strings.HasPrefix(config.CSRFCookieName, "__Host-") || strings.HasPrefix(config.CSRFCookieName, "__Secure-")) {
		return nil, ErrConfigurationInvalid
	}
	for _, origin := range config.AllowedOrigins {
		u, err := url.Parse(origin)
		// The CORS host consumes the same values. Reject wildcard syntax here,
		// not only in TrustedOrigin's exact-match map, so it cannot broaden
		// credentialed browser access in a downstream CORS implementation.
		if strings.Contains(origin, "*") || err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.ForceQuery || u.Opaque != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, ErrConfigurationInvalid
		}
		loopback := u.Hostname() == "localhost"
		if ip := net.ParseIP(u.Hostname()); ip != nil {
			loopback = ip.IsLoopback()
		}
		if u.Scheme == "http" && !loopback {
			return nil, ErrConfigurationInvalid
		}
		if security.insecureLocal && !loopback {
			return nil, ErrConfigurationInvalid
		}
		security.origins[u.Scheme+"://"+u.Host] = true
	}
	for _, client := range config.TrustedNativeClientIDs {
		if client == "" || len(client) > 128 {
			return nil, ErrConfigurationInvalid
		}
		security.nativeClients[client] = true
	}
	return security, nil
}

// mac returns the base64url HMAC-SHA256 of value under the configured key.
func (s *Security) mac(value string) string {
	m := hmac.New(sha256.New, s.key)
	_, _ = m.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// configured reports whether the receiver has a clock and a key of at least 32
// bytes plus a cookie name, i.e. is usable for cookie signing.
func (s *Security) configured() bool {
	return s != nil && s.now != nil && len(s.key) >= 32 && s.csrfName != ""
}

// UniqueCookie rejects ambiguous duplicate cookies. It does not authenticate
// a bearer read from a cookie; the owning session authority must verify it.
func UniqueCookie(r *http.Request, name string) (*http.Cookie, error) {
	if r == nil {
		return nil, ErrInvalidRequest
	}
	var found *http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name == name {
			if found != nil {
				return nil, errors.New("duplicate cookie")
			}
			found = cookie
		}
	}
	if found == nil {
		return nil, http.ErrNoCookie
	}
	return found, nil
}

// decodeCookie reads the single CSRF cookie, verifies its HMAC, decodes the
// JSON session and rejects nonces of wrong length or expired timestamps.
func (s *Security) decodeCookie(r *http.Request) (csrfSession, error) {
	cookie, err := UniqueCookie(r, s.csrfName)
	if err != nil {
		return csrfSession{}, err
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || len(cookie.Value) > 2048 || !hmac.Equal([]byte(parts[1]), []byte(s.mac("cookie\n"+parts[0]))) {
		return csrfSession{}, errors.New("invalid csrf cookie")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return csrfSession{}, err
	}
	var session csrfSession
	if err = json.Unmarshal(body, &session); err != nil || len(session.Nonce) != 43 || session.ExpiresAt <= s.now().Unix() {
		return csrfSession{}, errors.New("expired csrf cookie")
	}
	return session, nil
}

// Native admits only a host-proven allow-listed bearer audience with no cookies.
func (s *Security) Native(r *http.Request, identity Identity) bool {
	if !s.configured() || r == nil || identity.binding == "" {
		return false
	}
	values := r.Header.Values("Authorization")
	if len(values) != 1 || !strings.HasPrefix(values[0], "Bearer ") {
		return false
	}
	token := strings.TrimPrefix(values[0], "Bearer ")
	if token == "" || len(token) > 8192 {
		return false
	}
	for _, character := range token {
		if character < 33 || character > 126 || character == ',' {
			return false
		}
	}
	return len(r.Header.Values("Cookie")) == 0 && identity.nativeClientID != "" && s.nativeClients[identity.nativeClientID] && identity.nativeAuthenticated
}

// TrustedOrigin requires exactly one allow-listed Origin or, when absent,
// one Referer whose origin is allow-listed. It never accepts wildcard matches.
func (s *Security) TrustedOrigin(r *http.Request) bool {
	if !s.configured() || r == nil {
		return false
	}
	values := r.Header.Values("Origin")
	if len(values) > 1 {
		return false
	}
	if len(values) == 1 {
		return s.origins[values[0]]
	}
	values = r.Header.Values("Referer")
	if len(values) != 1 {
		return false
	}
	u, err := url.Parse(values[0])
	return err == nil && u.User == nil && s.origins[u.Scheme+"://"+u.Host]
}

// Verify rejects unsafe browser requests without current principal-bound proof.
// Native admission is transport protection, not an authorization decision.
func (s *Security) Verify(r *http.Request, identity Identity) error {
	if !s.configured() || r == nil || identity.binding == "" {
		return ErrForbidden
	}
	if len(r.Header.Values("Authorization")) > 0 {
		if s.Native(r, identity) {
			return nil
		}
		return ErrAuthenticationRequired
	}
	if !s.TrustedOrigin(r) {
		return ErrForbidden
	}
	session, err := s.decodeCookie(r)
	if err != nil || session.Binding != identity.binding {
		return ErrForbidden
	}
	values := r.Header.Values(CSRFHeader)
	if len(values) != 1 || !hmac.Equal([]byte(values[0]), []byte(s.mac("token\n"+session.Nonce+"\n"+session.Binding))) {
		return ErrForbidden
	}
	return nil
}

// Issue is safe for GET responses, including a generic unauthenticated error.
// Reuse avoids cross-site GETs rotating a live browser's token. Cross-site GETs
// do not receive a token/cookie. Public identity derives from the signed nonce.
func (s *Security) Issue(w http.ResponseWriter, r *http.Request, identity Identity) error {
	if !s.configured() || r == nil || w == nil || identity.binding == "" {
		return ErrInvalidRequest
	}
	if r.Method != "GET" || len(r.Header.Values("Authorization")) > 0 || (len(r.Header.Values("Origin")) > 0 && !s.TrustedOrigin(r)) || !issuanceSiteAllowed(r) {
		return nil
	}
	session, err := s.decodeCookie(r)
	if err != nil || session.Binding != identity.binding {
		if err != nil {
			nonce := make([]byte, 32)
			if _, err = rand.Read(nonce); err != nil {
				return err
			}
			session = csrfSession{Nonce: base64.RawURLEncoding.EncodeToString(nonce), ExpiresAt: s.now().Add(s.ttl).Unix()}
		}
		// Keep the nonce and absolute expiry across principal binding changes.
		// Anonymous replay identity stays stable after a host session exchange.
		session.Binding = identity.binding
		body, err := json.Marshal(session)
		if err != nil {
			return err
		}
		encoded := base64.RawURLEncoding.EncodeToString(body)
		s.setCookie(w, s.csrfName, encoded+"."+s.mac("cookie\n"+encoded), time.Unix(session.ExpiresAt, 0))
	}
	w.Header().Set(CSRFHeader, s.mac("token\n"+session.Nonce+"\n"+session.Binding))
	return nil
}

// issuanceSiteAllowed permits token issuance only when Sec-Fetch-Site is
// absent, or exactly one value equal to same-origin, same-site or none.
func issuanceSiteAllowed(r *http.Request) bool {
	values := r.Header.Values("Sec-Fetch-Site")
	if len(values) == 0 {
		return true
	}
	if len(values) != 1 {
		return false
	}
	switch values[0] {
	case "same-origin", "same-site", "none":
		return true
	default:
		return false
	}
}

// PublicIdentity derives a pseudonymous nonce identity, falling back to the
// immediate peer when no valid signed cookie is present. It is not authentication.
func (s *Security) PublicIdentity(r *http.Request) string {
	if !s.configured() || r == nil {
		return ""
	}
	if session, err := s.decodeCookie(r); err == nil {
		return "public_" + s.mac("public\n"+session.Nonce)
	}
	return "public_" + s.mac("client\n"+clientAddress(r))
}

// clientAddress extracts the host from the connection's RemoteAddr, returning
// the raw address when it lacks a port.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

// RateIdentity does not trust forwarded addresses or client-supplied identifiers.
// Actor rate limits persist across session rotation for authenticated members.
func (s *Security) RateIdentity(r *http.Request, identity Identity) string {
	if !s.configured() || r == nil || identity.binding == "" {
		return ""
	}
	if identity.public {
		return "public:" + s.mac("rate\n"+clientAddress(r))
	}
	if identity.actorID != "" {
		return "member:" + s.mac("rate\nmember\n"+identity.actorID)
	}
	return "authenticated:" + s.mac("rate\n"+identity.binding)
}

// setCookie writes a host-wide Strict, HttpOnly cookie that is Secure unless
// local insecurity is configured; a non-future expiry becomes a session cookie.
func (s *Security) setCookie(w http.ResponseWriter, name, value string, expires time.Time) {
	age := int(expires.Sub(s.now()).Seconds())
	if age < 1 {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: !s.insecureLocal, HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires.UTC(), MaxAge: age})
}
