package adminaccess

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

// inertPolicy makes construction tests fail closed if mistakenly used for I/O.
type inertPolicy struct{}

func (inertPolicy) Preview(context.Context, string, accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error) {
	return accesspolicy.TokenLimitPreview{}, ErrUnavailable
}
func (inertPolicy) Apply(context.Context, string, int64, accesspolicy.TokenLimits) (accesspolicy.Grant, error) {
	return accesspolicy.Grant{}, ErrUnavailable
}

func constructorConfig() Config {
	return Config{Origin: "https://app.example.test", System: "example", Environment: "test", CookieName: "access", Window: time.Minute,
		Store: &RedisStore{}, Manager: inertPolicy{}, Authorize: func(context.Context, string) (string, error) { return "", accesspolicy.ErrDenied },
		BearerSession: func(next http.Handler) http.Handler { return next }, Email: &capturedMail{}}
}

func TestNewValidatesTrustedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, origin, missing string
		window                time.Duration
		valid                 bool
	}{
		{"HTTPS", "https://app.example.test", "", time.Minute, true},
		{"loopback IPv4", "http://127.0.0.1:4000", "", 5 * time.Minute, true},
		{"loopback IPv6", "http://[::1]:4000", "", time.Minute, true},
		{"remote HTTP", "http://app.example.test", "", time.Minute, false},
		{"userinfo", "https://user@app.example.test", "", time.Minute, false},
		{"path", "https://app.example.test/", "", time.Minute, false},
		{"query", "https://app.example.test?x=1", "", time.Minute, false},
		{"empty query", "https://app.example.test?", "", time.Minute, false},
		{"fragment", "https://app.example.test#x", "", time.Minute, false},
		{"short window", "https://app.example.test", "", time.Second, false},
		{"long window", "https://app.example.test", "", 6 * time.Minute, false},
		{"no system", "https://app.example.test", "system", time.Minute, false},
		{"no environment", "https://app.example.test", "environment", time.Minute, false},
		{"no cookie", "https://app.example.test", "cookie", time.Minute, false},
		{"no store", "https://app.example.test", "store", time.Minute, false},
		{"no manager", "https://app.example.test", "manager", time.Minute, false},
		{"no authorizer", "https://app.example.test", "authorizer", time.Minute, false},
		{"no middleware", "https://app.example.test", "middleware", time.Minute, false},
		{"no email", "https://app.example.test", "email", time.Minute, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := constructorConfig()
			c.Origin, c.Window = tc.origin, tc.window
			switch tc.missing {
			case "system":
				c.System = ""
			case "environment":
				c.Environment = ""
			case "cookie":
				c.CookieName = ""
			case "store":
				c.Store = nil
			case "manager":
				c.Manager = nil
			case "authorizer":
				c.Authorize = nil
			case "middleware":
				c.BearerSession = nil
			case "email":
				c.Email = nil
			}
			b, err := New(c)
			if tc.valid {
				require.NoError(t, err)
				require.NotNil(t, b)
			} else {
				require.ErrorIs(t, err, ErrUnavailable)
				require.Nil(t, b)
			}
		})
	}
}

func TestBrowserRouteContract(t *testing.T) {
	b, err := New(constructorConfig())
	require.NoError(t, err)
	r := router.NewRouter(nil, nil)
	require.NoError(t, r.SetRouteAuthorizer(func(context.Context, *http.Request, router.RouteDefinition) error { return nil }))
	require.NoError(t, b.Attach(r))
	inventory := r.RouteInventory()
	require.Len(t, inventory, 6)
	for _, tc := range []struct {
		suffix, operation, method string
		revision                  bool
	}{
		{"/session", "Session", http.MethodPost, false},
		{"/users/{userID}/token-limits/preview", "Preview", http.MethodPost, false},
		{"/challenge", "Challenge", http.MethodPost, false},
		{"/confirm", "Confirm", http.MethodPost, false},
		{"/cancel", "Cancel", http.MethodPost, false},
		{"/users/{userID}/token-limits", "Apply", http.MethodPut, true},
	} {
		t.Run(tc.operation, func(t *testing.T) {
			var matches []router.RouteDefinition
			for _, def := range inventory {
				if def.Path == BasePath+tc.suffix {
					matches = append(matches, def)
				}
			}
			require.Len(t, matches, 1)
			require.Equal(t, "adminaccess."+tc.operation, matches[0].Operation)
			require.Equal(t, []string{tc.method}, matches[0].Methods)
			require.Equal(t, router.AdminSession, matches[0].Access)
			require.Equal(t, tc.revision, matches[0].Policy.RevisionRequired)
		})
	}
}
