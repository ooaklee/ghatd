package partnermanagerhelper

import (
	"context"
	"errors"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type principalPorts struct {
	member                                               *accessmanager.MiddlewareAuthedUserResponse
	details                                              *auth.TokenAccessDetails
	token                                                *jwt.Token
	memberErr, admissionErr, metadataErr, tokenErr       error
	authCalls, admissionCalls, metadataCalls, tokenCalls int
	onAuth, onAdmission, onMetadata, onToken             func()
	mutateRequest                                        bool
	credential                                           string
}

func (f *principalPorts) AuthenticateSession(_ context.Context, credential string) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	f.authCalls++
	f.credential = credential
	if f.onAuth != nil {
		f.onAuth()
	}
	return f.member, f.memberErr
}
func (f *principalPorts) CheckPartnerSession(_ context.Context, actor, credential string) error {
	f.admissionCalls++
	if f.onAdmission != nil {
		f.onAdmission()
	}
	return f.admissionErr
}
func (f *principalPorts) ExtractTokenMetadata(_ context.Context, r *http.Request) (*auth.TokenAccessDetails, error) {
	f.metadataCalls++
	if f.onMetadata != nil {
		f.onMetadata()
	}
	if f.mutateRequest {
		r.Header.Set("Private-Modified", "clone-only")
	}
	return f.details, f.metadataErr
}
func (f *principalPorts) VerifyToken(context.Context, *http.Request) (*jwt.Token, error) {
	f.tokenCalls++
	if f.onToken != nil {
		f.onToken()
	}
	return f.token, f.tokenErr
}
func principalFixture(t *testing.T) (HTTPPrincipalConfig, *principalPorts) {
	t.Helper()
	f := &principalPorts{member: &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: "member", User: &userv2.UniversalUser{ID: "member", Status: userv2.AccountStatusKeyActive, Verification: &userv2.VerificationStatus{EmailVerified: true}}}, details: &auth.TokenAccessDetails{UserID: "member"}}
	f.token = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "member", Audience: []string{"trusted-native"}})
	f.token.Valid = true
	cfg := HTTPPrincipalConfig{CookieName: "host-auth", TrustedNativeClientIDs: []string{"trusted-native"}, Members: f, Sessions: f, Tokens: f, TransportIdentity: func(_ *http.Request, actor, credential, client string) (browsersecurity.Identity, error) {
		require.Equal(t, "member", actor)
		require.Equal(t, "fixture-credential", credential)
		return browsersecurity.NewIdentity(browsersecurity.IdentityConfig{BindingParts: []string{"member", actor, credential}, ActorID: actor, NativeClientID: client, NativeAuthenticated: client != ""})
	}}
	return cfg, f
}

func TestHTTPPrincipalAdmission(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		status     int
		code       string
	}{
		{"browser member", "browser", 0, ""}, {"native audience", "native", 0, ""}, {"clone isolates metadata mutation", "native-clone", 0, ""},
		{"anonymous", "anonymous", 401, "PARTNERS_AUTH_REQUIRED"}, {"revoked", "revoked", 401, "PARTNERS_AUTH_REQUIRED"},
		{"unverified", "unverified", 403, "PARTNERS_VERIFICATION_REQUIRED"}, {"inactive", "inactive", 401, "PARTNERS_AUTH_REQUIRED"},
		{"mismatched owning actor", "actor", 401, "PARTNERS_AUTH_REQUIRED"}, {"missing owning user", "user", 401, "PARTNERS_AUTH_REQUIRED"},
		{"missing owning result", "result", 401, "PARTNERS_AUTH_REQUIRED"}, {"auth dependency failure", "auth-error", 401, "PARTNERS_AUTH_REQUIRED"},
		{"duplicate cookie", "duplicate", 401, "PARTNERS_AUTH_REQUIRED"}, {"malformed cookie", "malformed-cookie", 401, "PARTNERS_AUTH_REQUIRED"},
		{"native with cookie", "native-cookie", 401, "PARTNERS_AUTH_REQUIRED"}, {"native empty cookie header", "native-empty-cookie", 401, "PARTNERS_AUTH_REQUIRED"},
		{"native malformed cookie header", "native-malformed-cookie", 401, "PARTNERS_AUTH_REQUIRED"}, {"duplicate authorization", "native-duplicate", 401, "PARTNERS_AUTH_REQUIRED"},
		{"bad bearer scheme", "native-scheme", 401, "PARTNERS_AUTH_REQUIRED"}, {"empty bearer", "native-empty", 401, "PARTNERS_AUTH_REQUIRED"},
		{"wrong audience", "native-audience", 401, "PARTNERS_AUTH_REQUIRED"}, {"multiple audiences", "native-audiences", 401, "PARTNERS_AUTH_REQUIRED"},
		{"no audience", "native-no-audience", 401, "PARTNERS_AUTH_REQUIRED"}, {"native disabled", "native-disabled", 401, "PARTNERS_AUTH_REQUIRED"},
		{"unsigned token", "native-unsigned", 401, "PARTNERS_AUTH_REQUIRED"}, {"wrong algorithm", "native-algorithm", 401, "PARTNERS_AUTH_REQUIRED"},
		{"forged metadata actor", "native-actor", 401, "PARTNERS_AUTH_REQUIRED"}, {"metadata unavailable", "native-metadata", 401, "PARTNERS_AUTH_REQUIRED"},
		{"token unavailable", "native-token", 401, "PARTNERS_AUTH_REQUIRED"}, {"claims unavailable", "native-claims", 401, "PARTNERS_AUTH_REQUIRED"},
		{"api token", "api-token", 401, "PARTNERS_AUTH_REQUIRED"}, {"empty api header", "api-empty", 401, "PARTNERS_AUTH_REQUIRED"},
		{"blocked account", "blocked", 403, "PARTNERS_FORBIDDEN"}, {"admission unavailable", "admission-error", 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"},
		{"typed admission error", "admission-typed", 409, "PARTNERS_CONFLICT"}, {"binding refusal", "binding", 401, "PARTNERS_AUTH_REQUIRED"},
		{"nil request", "nil-request", 401, "PARTNERS_AUTH_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, f := principalFixture(t)
			r := httptest.NewRequest("GET", "https://app.example.test/api/v1/partners/program?account=forged", strings.NewReader(`{"actor_id":"forged"}`))
			r.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "fixture-credential"})
			if strings.HasPrefix(tc.mode, "native") {
				r.Header.Del("Cookie")
				r.Header.Set("Authorization", "Bearer fixture-credential")
			}
			switch tc.mode {
			case "anonymous":
				r.Header.Del("Cookie")
			case "revoked":
				f.member.Authenticated = false
			case "unverified":
				f.member.User.Verification.EmailVerified = false
			case "inactive":
				f.member.User.Status = "blocked"
			case "actor":
				f.member.User.ID = "different"
			case "user":
				f.member.User = nil
			case "result":
				f.member = nil
			case "auth-error":
				f.memberErr = errors.New("private auth diagnostic")
			case "duplicate":
				r.AddCookie(&http.Cookie{Name: cfg.CookieName, Value: "another"})
			case "malformed-cookie":
				r.Header.Set("Cookie", "host-auth=bad,credential")
			case "native-cookie":
				r.AddCookie(&http.Cookie{Name: "unrelated", Value: "value"})
			case "native-empty-cookie":
				r.Header["Cookie"] = []string{""}
			case "native-malformed-cookie":
				r.Header.Set("Cookie", "malformed")
			case "native-duplicate":
				r.Header.Add("Authorization", "Bearer fixture-credential")
			case "native-scheme":
				r.Header.Set("Authorization", "Basic fixture-credential")
			case "native-empty":
				r.Header.Set("Authorization", "Bearer ")
			case "native-audience":
				f.token.Claims = jwt.RegisteredClaims{Audience: []string{"untrusted"}}
			case "native-audiences":
				f.token.Claims = jwt.RegisteredClaims{Audience: []string{"trusted-native", "other"}}
			case "native-no-audience":
				f.token.Claims = jwt.RegisteredClaims{}
			case "native-disabled":
				cfg.TrustedNativeClientIDs = nil
			case "native-unsigned":
				f.token.Valid = false
			case "native-algorithm":
				f.token.Method = jwt.SigningMethodNone
			case "native-actor":
				f.details.UserID = "forged"
			case "native-metadata":
				f.details = nil
			case "native-token":
				f.token = nil
			case "native-claims":
				f.token.Claims = nil
			case "native-clone":
				f.mutateRequest = true
			case "api-token":
				r.Header.Set("X-Api-Token", "fixture-only")
			case "api-empty":
				r.Header["X-Api-Token"] = []string{""}
			case "blocked":
				f.admissionErr = partnermanager.ErrDenied
			case "admission-error":
				f.admissionErr = errors.New("private storage diagnostic")
			case "admission-typed":
				f.admissionErr = &partnerhttp.Error{Status: 409, Code: "PARTNERS_CONFLICT"}
			case "binding":
				cfg.TransportIdentity = func(*http.Request, string, string, string) (browsersecurity.Identity, error) {
					return browsersecurity.Identity{}, errors.New("private binding error")
				}
			case "nil-request":
				r = nil
			}
			resolver, err := NewHTTPPrincipalResolver(cfg)
			require.NoError(t, err)
			require.Zero(t, f.authCalls, "construction is passive")
			cfg.TrustedNativeClientIDs = nil // Constructor owns its own immutable selection.
			p, err := resolver.Resolve(t.Context(), r)
			if tc.status == 0 {
				require.NoError(t, err)
				require.True(t, p.Verified)
				require.Equal(t, "member", p.ActorID)
				require.Equal(t, "fixture-credential", p.Credential)
				require.Equal(t, "fixture-credential", f.credential)
				require.Equal(t, 1, f.authCalls)
				require.Equal(t, 1, f.admissionCalls)
				require.Empty(t, r.Header.Get("Private-Modified"))
				if strings.HasPrefix(tc.mode, "native") {
					require.Equal(t, 1, f.metadataCalls)
					require.Equal(t, 1, f.tokenCalls)
				} else {
					require.Zero(t, f.metadataCalls)
					require.Zero(t, f.tokenCalls)
				}
			} else {
				var failure *partnerhttp.Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, tc.status, failure.Status)
				require.Equal(t, tc.code, failure.Code)
				require.Empty(t, p.ActorID)
				require.False(t, p.Verified)
				require.NotContains(t, err.Error(), "private")
			}
		})
	}
}

func TestHTTPPrincipalConstructorRefusesMissingConfiguration(t *testing.T) {
	for _, mode := range []string{"members", "sessions", "tokens", "typed-member", "typed-session", "typed-token", "binding", "cookie", "invalid-cookie", "empty-audience", "long-audience"} {
		t.Run(mode, func(t *testing.T) {
			cfg, f := principalFixture(t)
			want := partnermanager.ErrUnavailable
			var missing *principalPorts
			switch mode {
			case "members":
				cfg.Members = nil
			case "sessions":
				cfg.Sessions = nil
			case "tokens":
				cfg.Tokens = nil
			case "typed-member":
				cfg.Members = missing
			case "typed-session":
				cfg.Sessions = missing
			case "typed-token":
				cfg.Tokens = missing
			case "binding":
				cfg.TransportIdentity = nil
			case "cookie":
				cfg.CookieName = ""
				want = partnermanager.ErrInvalid
			case "invalid-cookie":
				cfg.CookieName = "bad cookie"
				want = partnermanager.ErrInvalid
			case "empty-audience":
				cfg.TrustedNativeClientIDs = []string{""}
				want = partnermanager.ErrInvalid
			case "long-audience":
				cfg.TrustedNativeClientIDs = []string{strings.Repeat("x", 129)}
				want = partnermanager.ErrInvalid
			}
			p, err := NewHTTPPrincipalResolver(cfg)
			require.ErrorIs(t, err, want)
			require.Nil(t, p)
			require.Zero(t, f.authCalls)
			require.Zero(t, f.admissionCalls)
			require.Zero(t, f.metadataCalls)
			require.Zero(t, f.tokenCalls)
		})
	}
}

func TestHTTPPrincipalCancellationAndFreshAdmission(t *testing.T) {
	for _, mode := range []string{"before", "auth", "admission", "metadata", "token", "binding", "fresh-revocation"} {
		t.Run(mode, func(t *testing.T) {
			cfg, f := principalFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := httptest.NewRequest("GET", "https://app.example.test/", nil)
			r.Header.Set("Authorization", "Bearer fixture-credential")
			switch mode {
			case "before":
				cancel()
			case "auth":
				f.onAuth = cancel
			case "admission":
				f.onAdmission = cancel
				f.admissionErr = partnermanager.ErrDenied
			case "metadata":
				f.onMetadata = cancel
				f.metadataErr = errors.New("private")
			case "token":
				f.onToken = cancel
				f.tokenErr = errors.New("private")
			case "binding":
				base := cfg.TransportIdentity
				cfg.TransportIdentity = func(r *http.Request, a, c, n string) (browsersecurity.Identity, error) {
					cancel()
					return base(r, a, c, n)
				}
			}
			resolver, err := NewHTTPPrincipalResolver(cfg)
			require.NoError(t, err)
			if mode == "fresh-revocation" {
				p, err := resolver.Resolve(ctx, r)
				require.NoError(t, err)
				require.True(t, p.Verified)
				f.member.Authenticated = false
				p, err = resolver.Resolve(ctx, r)
				require.Error(t, err)
				require.Empty(t, p.ActorID)
				require.Equal(t, 2, f.authCalls)
			} else {
				p, err := resolver.Resolve(ctx, r)
				require.ErrorIs(t, err, context.Canceled)
				require.Empty(t, p.ActorID)
				require.False(t, p.Verified)
			}
		})
	}
}

func TestHTTPPrincipalCredentialBounds(t *testing.T) {
	for _, tc := range []struct {
		name, credential string
		allowed          bool
	}{
		{"upper bound", strings.Repeat("x", 8192), true}, {"too long", strings.Repeat("x", 8193), false},
		{"empty", "", false}, {"space", "bad value", false}, {"tab", "bad\tvalue", false},
		{"control", "bad\x00value", false}, {"non ASCII", "café", false}, {"comma", "bad,value", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, f := principalFixture(t)
			cfg.TransportIdentity = func(_ *http.Request, a, c, n string) (browsersecurity.Identity, error) {
				return browsersecurity.NewIdentity(browsersecurity.IdentityConfig{BindingParts: []string{a, c}, ActorID: a, NativeClientID: n, NativeAuthenticated: true})
			}
			resolver, err := NewHTTPPrincipalResolver(cfg)
			require.NoError(t, err)
			r := httptest.NewRequest("GET", "https://app.example.test/", nil)
			r.Header.Set("Authorization", "Bearer "+tc.credential)
			p, err := resolver.Resolve(t.Context(), r)
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, tc.credential, p.Credential)
				require.Equal(t, tc.credential, f.credential)
			} else {
				var failure *partnerhttp.Error
				require.ErrorAs(t, err, &failure)
				require.Equal(t, 401, failure.Status)
				require.Empty(t, p.ActorID)
				require.Zero(t, f.authCalls)
				require.Zero(t, f.admissionCalls)
			}
		})
	}
}

func TestHTTPPrincipalZeroStates(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		nilResolver, nilContext bool
		status                  int
	}{
		{"nil resolver", true, false, 503}, {"zero resolver", false, false, 503}, {"nil context", false, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var resolver *httpPrincipalResolver
			if !tc.nilResolver {
				resolver = &httpPrincipalResolver{}
			}
			ctx := t.Context()
			if tc.nilContext {
				ctx = nil
			}
			p, err := resolver.Resolve(ctx, httptest.NewRequest("GET", "https://app.example.test/", nil))
			var failure *partnerhttp.Error
			require.ErrorAs(t, err, &failure)
			require.Equal(t, tc.status, failure.Status)
			require.False(t, p.Verified)
			require.Empty(t, p.ActorID)
		})
	}
}
