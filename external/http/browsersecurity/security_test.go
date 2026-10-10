package browsersecurity

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func fixtureConfig() Config {
	return Config{AllowedOrigins: []string{"https://fixture.example"}, Key: bytes.Repeat([]byte{0x61}, 32),
		CSRFCookieName: "__Host-fixture-csrf", Now: func() time.Time { return time.Unix(1791547200, 0) },
		TrustedNativeClientIDs: []string{"fixture-native"}}
}

func fixtureIdentity(t *testing.T, credential string) Identity {
	t.Helper()
	identity, err := NewIdentity(IdentityConfig{BindingParts: []string{"member", "fixture-actor", credential}, ActorID: "fixture-actor"})
	require.NoError(t, err)
	return identity
}

func issueFixture(t *testing.T, s *Security, identity Identity) (*http.Cookie, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "https://fixture.example/csrf", nil)
	w := httptest.NewRecorder()
	require.NoError(t, s.Issue(w, r, identity))
	require.Len(t, w.Result().Cookies(), 1)
	return w.Result().Cookies()[0], w.Header().Get(CSRFHeader)
}

func TestConfigurationBounds(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(*Config)
	}{
		{"missing-key", func(c *Config) { c.Key = nil }},
		{"short-key", func(c *Config) { c.Key = make([]byte, 31) }},
		{"missing-origins", func(c *Config) { c.AllowedOrigins = nil }},
		{"wildcard", func(c *Config) { c.AllowedOrigins = []string{"https://*.example"} }},
		{"userinfo", func(c *Config) { c.AllowedOrigins = []string{"https://user@fixture.example"} }},
		{"query", func(c *Config) { c.AllowedOrigins = []string{"https://fixture.example?x=1"} }},
		{"empty-query", func(c *Config) { c.AllowedOrigins = []string{"https://fixture.example?"} }},
		{"fragment", func(c *Config) { c.AllowedOrigins = []string{"https://fixture.example#x"} }},
		{"opaque", func(c *Config) { c.AllowedOrigins = []string{"https:fixture.example"} }},
		{"empty-hostname", func(c *Config) { c.AllowedOrigins = []string{"https://:443"} }},
		{"path", func(c *Config) { c.AllowedOrigins = []string{"https://fixture.example/path"} }},
		{"remote-http", func(c *Config) { c.AllowedOrigins = []string{"http://fixture.example"} }},
		{"insecure-remote", func(c *Config) { c.InsecureLocalCookies = true; c.CSRFCookieName = "fixture-csrf" }},
		{"local-host-prefix", func(c *Config) { c.InsecureLocalCookies = true; c.AllowedOrigins = []string{"http://localhost:4000"} }},
		{"local-secure-prefix", func(c *Config) {
			c.InsecureLocalCookies = true
			c.AllowedOrigins = []string{"http://localhost:4000"}
			c.CSRFCookieName = "__Secure-fixture"
		}},
		{"missing-cookie-name", func(c *Config) { c.CSRFCookieName = "" }},
		{"cookie-name-injection", func(c *Config) { c.CSRFCookieName = "csrf; other=value" }},
		{"long-cookie-name", func(c *Config) { c.CSRFCookieName = strings.Repeat("x", 129) }},
		{"short-ttl", func(c *Config) { c.CSRFTTL = time.Minute - time.Nanosecond }},
		{"long-ttl", func(c *Config) { c.CSRFTTL = 24*time.Hour + time.Nanosecond }},
		{"empty-native", func(c *Config) { c.TrustedNativeClientIDs = []string{""} }},
		{"long-native", func(c *Config) { c.TrustedNativeClientIDs = []string{strings.Repeat("x", 129)} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := fixtureConfig()
			test.alter(&config)
			s, err := New(config)
			require.ErrorIs(t, err, ErrConfigurationInvalid)
			require.Nil(t, s)
		})
	}
	for _, origin := range []string{"https://fixture.example", "https://fixture.example/", "https://fixture.example:8443", "http://localhost:4000", "http://127.0.0.1:4000", "http://[::1]:4000"} {
		t.Run(origin, func(t *testing.T) {
			config := fixtureConfig()
			config.AllowedOrigins = []string{origin}
			if strings.HasPrefix(origin, "http:") {
				config.InsecureLocalCookies = true
				config.CSRFCookieName = "fixture-local-csrf"
			}
			s, err := New(config)
			require.NoError(t, err)
			cookie, _ := issueFixture(t, s, fixtureIdentity(t, "session"))
			require.Equal(t, !config.InsecureLocalCookies, cookie.Secure)
			require.True(t, cookie.HttpOnly)
			require.Equal(t, http.SameSiteStrictMode, cookie.SameSite)
			require.Equal(t, "/", cookie.Path)
			require.Empty(t, cookie.Domain)
		})
	}
}

func TestBrowserProofAndBinding(t *testing.T) {
	for _, test := range []struct {
		name  string
		alter func(*http.Request, *Identity)
		want  error
	}{
		{"valid", func(*http.Request, *Identity) {}, nil},
		{"referer", func(r *http.Request, _ *Identity) {
			r.Header.Del("Origin")
			r.Header.Set("Referer", "https://fixture.example/path?query=1")
		}, nil},
		{"missing-origin", func(r *http.Request, _ *Identity) { r.Header.Del("Origin") }, ErrForbidden},
		{"foreign-origin", func(r *http.Request, _ *Identity) { r.Header.Set("Origin", "https://other.example") }, ErrForbidden},
		{"duplicate-origin", func(r *http.Request, _ *Identity) { r.Header.Add("Origin", "https://fixture.example") }, ErrForbidden},
		{"duplicate-referer", func(r *http.Request, _ *Identity) {
			r.Header.Del("Origin")
			r.Header.Add("Referer", "https://fixture.example/path")
			r.Header.Add("Referer", "https://fixture.example/path")
		}, ErrForbidden},
		{"missing-cookie", func(r *http.Request, _ *Identity) { r.Header.Del("Cookie") }, ErrForbidden},
		{"duplicate-cookie", func(r *http.Request, _ *Identity) { r.AddCookie(r.Cookies()[0]) }, ErrForbidden},
		{"tampered-cookie", func(r *http.Request, _ *Identity) { r.Header.Set("Cookie", "__Host-fixture-csrf=invalid.signature") }, ErrForbidden},
		{"missing-token", func(r *http.Request, _ *Identity) { r.Header.Del(CSRFHeader) }, ErrForbidden},
		{"duplicate-token", func(r *http.Request, _ *Identity) { r.Header.Add(CSRFHeader, r.Header.Get(CSRFHeader)) }, ErrForbidden},
		{"tampered-token", func(r *http.Request, _ *Identity) { r.Header.Set(CSRFHeader, "changed") }, ErrForbidden},
		{"rotated-session", func(_ *http.Request, p *Identity) { *p = fixtureIdentity(t, "rotated") }, ErrForbidden},
		{"zero-identity", func(_ *http.Request, p *Identity) { *p = Identity{} }, ErrForbidden},
		{"browser-bearer", func(r *http.Request, _ *Identity) { r.Header.Set("Authorization", "Bearer fixture-session") }, ErrAuthenticationRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := New(fixtureConfig())
			require.NoError(t, err)
			identity := fixtureIdentity(t, "session")
			cookie, token := issueFixture(t, s, identity)
			r := httptest.NewRequest(http.MethodPost, "https://fixture.example/command", nil)
			r.Header.Set("Origin", "https://fixture.example")
			r.Header.Set(CSRFHeader, token)
			r.AddCookie(cookie)
			test.alter(r, &identity)
			require.ErrorIs(t, s.Verify(r, identity), test.want)
		})
	}
}

func TestIssuanceRefusesUntrustedRequests(t *testing.T) {
	for _, test := range []struct {
		name, method string
		headers      http.Header
	}{
		{"post", "POST", nil},
		{"bearer", "GET", http.Header{"Authorization": {"Bearer fixture-session"}}},
		{"foreign-origin", "GET", http.Header{"Origin": {"https://other.example"}}},
		{"duplicate-origin", "GET", http.Header{"Origin": {"https://fixture.example", "https://fixture.example"}}},
		{"cross-site", "GET", http.Header{"Sec-Fetch-Site": {"cross-site"}}},
		{"duplicate-fetch-site", "GET", http.Header{"Sec-Fetch-Site": {"same-origin", "cross-site"}}},
		{"reverse-duplicate-fetch-site", "GET", http.Header{"Sec-Fetch-Site": {"cross-site", "same-origin"}}},
		{"combined-fetch-site", "GET", http.Header{"Sec-Fetch-Site": {"same-origin, cross-site"}}},
		{"empty-fetch-site", "GET", http.Header{"Sec-Fetch-Site": {""}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := New(fixtureConfig())
			require.NoError(t, err)
			r := httptest.NewRequest(test.method, "https://fixture.example/csrf", nil)
			if test.headers != nil {
				r.Header = test.headers.Clone()
			}
			w := httptest.NewRecorder()
			require.NoError(t, s.Issue(w, r, fixtureIdentity(t, "session")))
			require.Empty(t, w.Header().Get(CSRFHeader))
			require.Empty(t, w.Result().Cookies())
		})
	}
}

func TestNativeRequiresVerifiedAudienceAndNoCookieHeader(t *testing.T) {
	for _, test := range []struct {
		name, client, bearer string
		authenticated        bool
		cookies              []string
		duplicate            bool
		allowed              bool
	}{
		{"valid", "fixture-native", "Bearer fixture-token", true, nil, false, true},
		{"unverified", "fixture-native", "Bearer fixture-token", false, nil, false, false},
		{"unlisted", "other-native", "Bearer fixture-token", true, nil, false, false},
		{"cookie", "fixture-native", "Bearer fixture-token", true, []string{"session=value"}, false, false},
		{"malformed-cookie", "fixture-native", "Bearer fixture-token", true, []string{"broken"}, false, false},
		{"empty-cookie-header", "fixture-native", "Bearer fixture-token", true, []string{""}, false, false},
		{"duplicate-bearer", "fixture-native", "Bearer fixture-token", true, nil, true, false},
		{"empty-bearer", "fixture-native", "Bearer ", true, nil, false, false},
		{"wrong-scheme", "fixture-native", "bearer fixture-token", true, nil, false, false},
		{"comma-bearer", "fixture-native", "Bearer one,two", true, nil, false, false},
		{"space-bearer", "fixture-native", "Bearer one two", true, nil, false, false},
		{"long-bearer", "fixture-native", "Bearer " + strings.Repeat("x", 8193), true, nil, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, err := New(fixtureConfig())
			require.NoError(t, err)
			identity, err := NewIdentity(IdentityConfig{BindingParts: []string{"member", "session"}, NativeClientID: test.client, NativeAuthenticated: test.authenticated})
			require.NoError(t, err)
			r := httptest.NewRequest("POST", "https://fixture.example/command", nil)
			r.Header.Set("Authorization", test.bearer)
			if test.duplicate {
				r.Header.Add("Authorization", test.bearer)
			}
			for _, cookie := range test.cookies {
				r.Header.Add("Cookie", cookie)
			}
			require.Equal(t, test.allowed, s.Native(r, identity))
			if test.allowed {
				require.NoError(t, s.Verify(r, identity))
			} else {
				require.ErrorIs(t, s.Verify(r, identity), ErrAuthenticationRequired)
			}
		})
	}
}

func TestRebindingPreservesNonceAndAbsoluteExpiry(t *testing.T) {
	for _, target := range []string{"anonymous", "rotated-session"} {
		t.Run(target, func(t *testing.T) {
			config := fixtureConfig()
			now := config.Now()
			config.Now = func() time.Time { return now }
			s, err := New(config)
			require.NoError(t, err)
			original := fixtureIdentity(t, "original-secret")
			cookie, token := issueFixture(t, s, original)
			r := httptest.NewRequest("GET", "https://fixture.example/csrf", nil)
			r.AddCookie(cookie)
			publicID := s.PublicIdentity(r)
			identity := fixtureIdentity(t, "rotated-secret")
			if target == "anonymous" {
				identity, err = NewIdentity(IdentityConfig{Public: true})
				require.NoError(t, err)
			}
			now = now.Add(30 * time.Minute)
			w := httptest.NewRecorder()
			require.NoError(t, s.Issue(w, r, identity))
			require.Len(t, w.Result().Cookies(), 1)
			rebound := w.Result().Cookies()[0]
			require.Equal(t, cookie.Expires, rebound.Expires)
			require.Equal(t, 1800, rebound.MaxAge)
			require.NotEqual(t, token, w.Header().Get(CSRFHeader))
			post := httptest.NewRequest("POST", "https://fixture.example/command", nil)
			post.AddCookie(rebound)
			post.Header.Set("Origin", "https://fixture.example")
			post.Header.Set(CSRFHeader, w.Header().Get(CSRFHeader))
			require.Equal(t, publicID, s.PublicIdentity(post))
			require.NoError(t, s.Verify(post, identity))
			require.ErrorIs(t, s.Verify(post, original), ErrForbidden)
			now = now.Add(30 * time.Minute)
			require.ErrorIs(t, s.Verify(post, identity), ErrForbidden)
		})
	}
}

func TestLegacyWireCompatibility(t *testing.T) {
	// A fixed fixture generated by the preceding implementation pins the wire
	// contract independently of the refactored guard's issuance implementation.
	var fixture struct {
		Key            string   `json:"key"`
		Origin         string   `json:"origin"`
		CookieName     string   `json:"cookie_name"`
		Cookie         string   `json:"cookie"`
		Token          string   `json:"token"`
		PublicIdentity string   `json:"public_identity"`
		RateIdentity   string   `json:"rate_identity"`
		Now            int64    `json:"now"`
		BindingParts   []string `json:"binding_parts"`
	}
	body, err := os.ReadFile("testdata/legacy-v1.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &fixture))
	for _, test := range []struct {
		name   string
		rotate bool
		want   error
	}{{"same-key-and-binding", false, nil}, {"rotated-key", true, ErrForbidden}} {
		t.Run(test.name, func(t *testing.T) {
			config := Config{AllowedOrigins: []string{fixture.Origin}, CSRFCookieName: fixture.CookieName, Key: []byte(fixture.Key), Now: func() time.Time { return time.Unix(fixture.Now, 0) }}
			if test.rotate {
				config.Key[0] ^= 1
			}
			s, err := New(config)
			require.NoError(t, err)
			identity, err := NewIdentity(IdentityConfig{BindingParts: fixture.BindingParts, ActorID: fixture.BindingParts[1]})
			require.NoError(t, err)
			r := httptest.NewRequest("POST", fixture.Origin+"/command", nil)
			r.AddCookie(&http.Cookie{Name: fixture.CookieName, Value: fixture.Cookie})
			r.Header.Set("Origin", fixture.Origin)
			r.Header.Set(CSRFHeader, fixture.Token)
			require.ErrorIs(t, s.Verify(r, identity), test.want)
			if !test.rotate {
				require.Equal(t, fixture.PublicIdentity, s.PublicIdentity(r))
				require.Equal(t, fixture.RateIdentity, s.RateIdentity(r, identity))
			}
		})
	}
}

func TestConstructionCopiesInputsAndDoesNotExposeCredentials(t *testing.T) {
	// This sequence verifies construction ownership, then uses the resulting
	// identity and cookie. It is one independent lifetime contract.
	config := fixtureConfig()
	s, err := New(config)
	require.NoError(t, err)
	parts := []string{"member", "fixture-actor", "secret-fixture-session"}
	identity, err := NewIdentity(IdentityConfig{BindingParts: parts, ActorID: "fixture-actor"})
	require.NoError(t, err)
	cookie, token := issueFixture(t, s, identity)
	config.Key[0] ^= 1
	config.AllowedOrigins[0] = "https://other.example"
	config.TrustedNativeClientIDs[0] = "unlisted"
	parts[2] = "mutated"
	decoded, err := base64.RawURLEncoding.DecodeString(strings.Split(cookie.Value, ".")[0])
	require.NoError(t, err)
	require.NotContains(t, string(decoded), "secret-fixture-session")
	require.NotContains(t, string(decoded), "fixture-actor")
	r := httptest.NewRequest("POST", "https://fixture.example/command", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://fixture.example")
	r.Header.Set(CSRFHeader, token)
	require.NoError(t, s.Verify(r, fixtureIdentity(t, "secret-fixture-session")))
	independent, err := New(fixtureConfig())
	require.NoError(t, err)
	require.NoError(t, independent.Verify(r, identity), "existing cookie still uses the original copied key")
	laterCookie, laterToken := issueFixture(t, s, identity)
	later := r.Clone(r.Context())
	later.Header.Del("Cookie")
	later.AddCookie(laterCookie)
	later.Header.Set(CSRFHeader, laterToken)
	require.NoError(t, independent.Verify(later, identity), "new cookies still use the original copied key")
	native, err := NewIdentity(IdentityConfig{BindingParts: []string{"member", "session"}, NativeClientID: "fixture-native", NativeAuthenticated: true})
	require.NoError(t, err)
	nativeRequest := httptest.NewRequest("POST", "https://fixture.example/command", nil)
	nativeRequest.Header.Set("Authorization", "Bearer fixture-token")
	require.True(t, s.Native(nativeRequest, native), "native allow-list was copied")
	rotated := fixtureIdentity(t, "rotated-session")
	require.Equal(t, s.RateIdentity(r, identity), s.RateIdentity(r, rotated))
	r.Header.Set("X-Forwarded-For", "203.0.113.1")
	public, err := NewIdentity(IdentityConfig{Public: true})
	require.NoError(t, err)
	rate := s.RateIdentity(r, public)
	r.Header.Set("X-Forwarded-For", "203.0.113.2")
	require.Equal(t, rate, s.RateIdentity(r, public))
}

func TestZeroAndNilGuardsFailClosed(t *testing.T) {
	for _, test := range []struct {
		name  string
		guard *Security
	}{{"nil", nil}, {"zero", &Security{}}} {
		t.Run(test.name, func(t *testing.T) {
			identity := fixtureIdentity(t, "session")
			r := httptest.NewRequest("POST", "https://fixture.example/command", nil)
			require.False(t, test.guard.Native(r, identity))
			require.False(t, test.guard.TrustedOrigin(r))
			require.ErrorIs(t, test.guard.Verify(r, identity), ErrForbidden)
			require.ErrorIs(t, test.guard.Issue(httptest.NewRecorder(), r, identity), ErrInvalidRequest)
			require.Empty(t, test.guard.RateIdentity(r, identity))
			require.Empty(t, test.guard.PublicIdentity(r))
		})
	}
}

func TestIdentityBounds(t *testing.T) {
	for _, test := range []struct {
		name   string
		config IdentityConfig
		valid  bool
	}{
		{"valid", IdentityConfig{BindingParts: []string{"member", "session"}}, true},
		{"public", IdentityConfig{Public: true}, true},
		{"missing-binding", IdentityConfig{}, false},
		{"too-many-parts", IdentityConfig{BindingParts: make([]string, 17)}, false},
		{"long-part", IdentityConfig{BindingParts: []string{strings.Repeat("x", 8193)}}, false},
		{"total-bound", IdentityConfig{BindingParts: []string{strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192)}}, true},
		{"total-exceeded", IdentityConfig{BindingParts: []string{strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), "x"}}, false},
		{"actor-too-long", IdentityConfig{BindingParts: []string{"session"}, ActorID: strings.Repeat("x", 257)}, false},
		{"client-too-long", IdentityConfig{BindingParts: []string{"session"}, NativeClientID: strings.Repeat("x", 129)}, false},
		{"native-without-client", IdentityConfig{BindingParts: []string{"session"}, NativeAuthenticated: true}, false},
		{"public-with-actor", IdentityConfig{Public: true, ActorID: "actor"}, false},
		{"public-with-binding", IdentityConfig{Public: true, BindingParts: []string{"secret"}}, false},
		{"public-with-native", IdentityConfig{Public: true, NativeClientID: "fixture-native", NativeAuthenticated: true}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewIdentity(test.config)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrInvalidRequest)
			}
		})
	}
}
