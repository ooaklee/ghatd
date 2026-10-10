package partnermanagerhelper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	ghatdrouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

func TestReferralRendererHasOnlyBodyAccess(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			cfg, deps := referralConfigFixture()
			calls := 0
			deps.RenderPage = func(w io.Writer, view ReferralPageView) error {
				calls++
				_, canChangeHeaders := w.(http.ResponseWriter)
				require.False(t, canChangeHeaders)
				return referralTestRenderer(w, view)
			}
			h, err := NewPartnersReferralHandler(cfg, deps)
			require.NoError(t, err)
			r := httptest.NewRequest(method, "/ref/"+partnersReferralTestCode, nil)
			r = mux.SetURLVars(r, map[string]string{"code": partnersReferralTestCode})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, http.StatusOK, w.Code)
			require.Contains(t, w.Result().Header.Get("Content-Security-Policy"), "frame-ancestors 'none'")
			if method == http.MethodHead {
				require.Zero(t, calls)
				require.Empty(t, w.Body.String())
			} else {
				require.Equal(t, 1, calls)
				require.NotEmpty(t, w.Body.String())
			}
		})
	}
}

func referralConfigFixture() (PartnersReferralConfig, ReferralDependencies) {
	h, visits, _ := partnersReferralFixture()
	return PartnersReferralConfig{PublicOrigin: h.origin, SignupCookieName: partnersSignupCookieName, VisitCookieName: partnersVisitCookieName, AttributionWindow: h.window, VisitWindow: h.visitWindow}, ReferralDependencies{Visits: visits, Clock: h.clock, Admission: h.admission, RenderPage: referralTestRenderer}
}

func TestReferralConfigurationRejectsUnsafeHostChoices(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"missing_renderer", "renderer"}, {"missing_cookie", "cookie"},
		{"duplicate_cookie_names", "same-cookie"}, {"invalid_cookie_name", "invalid-cookie"},
		{"off_site_signup", "external-signup"}, {"scheme_relative_signup", "scheme-signup"},
		{"signup_query", "query"}, {"encoded_signup", "encoded"},
		{"parent_path", "parent"}, {"root_referral", "root"},
		{"redirect_into_referral", "loop"}, {"unsecured_public_origin", "http"},
		{"local_http_requires_flag", "local-no-flag"}, {"non_loopback_local_exception", "non-loopback"},
		{"origin_credentials", "credentials"}, {"origin_query", "origin-query"},
		{"origin_fragment", "fragment"}, {"origin_path", "origin-path"},
		{"missing_live_clock", "clock"}, {"typed_nil_live_clock", "nil-clock"},
		{"missing_live_admission", "admission"}, {"missing_live_window", "window"},
		{"negative_attribution_window", "negative"}, {"unbounded_visit_window", "visit"},
		{"subsecond_visit_window", "subsecond"}, {"unbounded_trusted_proxy", "proxy"},
		{"noncanonical_trusted_proxy", "noncanonical"}, {"too_many_trusted_proxies", "many-proxies"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, deps := referralConfigFixture()
			switch tc.state {
			case "renderer":
				deps.RenderPage = nil
			case "cookie":
				cfg.SignupCookieName = ""
			case "same-cookie":
				cfg.VisitCookieName = cfg.SignupCookieName
			case "invalid-cookie":
				cfg.VisitCookieName = "invalid;name"
			case "external-signup":
				cfg.SignupPath = "https://other.example.test/signup"
			case "scheme-signup":
				cfg.SignupPath = "//other.example.test/signup"
			case "query":
				cfg.SignupPath = "/signup?next=other"
			case "encoded":
				cfg.SignupPath = "/%2fother"
			case "parent":
				cfg.ReferralPath = "/ref/../admin"
			case "root":
				cfg.ReferralPath = "/"
			case "loop":
				cfg.SignupPath = "/ref/signup"
			case "http":
				cfg.PublicOrigin = "http://app.example.test"
			case "local-no-flag":
				cfg.PublicOrigin = "http://localhost:5174"
			case "non-loopback":
				cfg.PublicOrigin, cfg.AllowInsecureLoopback = "http://192.0.2.1:5174", true
			case "credentials":
				cfg.PublicOrigin = "https://user@app.example.test"
			case "origin-query":
				cfg.PublicOrigin = "https://app.example.test?"
			case "fragment":
				cfg.PublicOrigin = "https://app.example.test#fragment"
			case "origin-path":
				cfg.PublicOrigin = "https://app.example.test/other"
			case "clock":
				deps.Clock = nil
			case "nil-clock":
				deps.Clock = partnerearnings.ClockFunc(nil)
			case "admission":
				deps.Admission = nil
			case "window":
				cfg.AttributionWindow = 0
			case "negative":
				cfg.AttributionWindow = -time.Second
			case "visit":
				cfg.VisitWindow = 25 * time.Hour
			case "subsecond":
				cfg.VisitWindow = time.Millisecond
			case "proxy":
				cfg.TrustedProxyCIDRs = []string{"0.0.0.0/0"}
			case "noncanonical":
				cfg.TrustedProxyCIDRs = []string{"192.0.2.1/24"}
			case "many-proxies":
				cfg.TrustedProxyCIDRs = make([]string, 33)
			}
			router := ghatdrouter.NewRouter(nil, nil)
			require.Error(t, AttachPartnersReferralRoutes(router, cfg, deps))
			require.Empty(t, router.RouteInventory(), "invalid configuration must register no routes")
		})
	}
}

func TestReferralConfiguredRoutesCookiesAndDisabledCapture(t *testing.T) {
	for _, tc := range []struct {
		name, origin    string
		local, disabled bool
	}{
		{"secure_enabled", "https://app.example.test", false, false},
		{"explicit_ipv4_loopback", "http://127.0.0.1:5174", true, false},
		{"explicit_ipv6_loopback", "http://[::1]:5174", true, false},
		{"disabled_capture_without_clock_or_admission", "https://app.example.test", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, deps := referralConfigFixture()
			cfg.PublicOrigin, cfg.AllowInsecureLoopback = tc.origin, tc.local
			cfg.ReferralPath, cfg.SignupPath = "/invite", "/join"
			cfg.SignupCookieName, cfg.VisitCookieName = "host-signup", "host-visit"
			if tc.disabled {
				deps.Visits, deps.Clock, deps.Admission = (*partnersVisitFixture)(nil), nil, nil
			}
			router := ghatdrouter.NewRouter(nil, nil)
			require.NoError(t, AttachPartnersReferralRoutes(router, cfg, deps))
			for _, choice := range []string{"yes", "no"} {
				r := httptest.NewRequest("POST", "/invite/"+partnersReferralTestCode, strings.NewReader("choice="+choice))
				r.Header.Set("Origin", tc.origin)
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.RemoteAddr = "192.0.2.1:1234"
				w := httptest.NewRecorder()
				router.GetRouter().ServeHTTP(w, r)
				if tc.disabled && choice == "yes" {
					require.Equal(t, 503, w.Code)
					require.Empty(t, w.Result().Cookies())
					continue
				}
				require.Equal(t, 303, w.Code)
				require.Equal(t, "/join", w.Header().Get("Location"))
				cookies := w.Result().Cookies()
				require.Len(t, cookies, 2)
				require.Equal(t, "host-signup", cookies[0].Name)
				require.Equal(t, "host-visit", cookies[1].Name)
				for _, cookie := range cookies {
					require.Equal(t, !tc.local, cookie.Secure)
					require.True(t, cookie.HttpOnly)
					require.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
				}
			}
		})
	}
}

func TestReferralConfigurationCopiesProxyTrust(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"caller_changes_prefix", "198.51.100.0/24"}, {"caller_expands_trust", "0.0.0.0/0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, deps := referralConfigFixture()
			cfg.TrustedProxyCIDRs = []string{"192.0.2.0/24"}
			h, err := NewPartnersReferralHandler(cfg, deps)
			require.NoError(t, err)
			cfg.TrustedProxyCIDRs[0] = tc.mutation
			// Original trusted peer must still require the one validated forwarded IP.
			r := httptest.NewRequest("POST", "/ref/"+partnersReferralTestCode, nil)
			r.RemoteAddr = "192.0.2.1:1234"
			r = mux.SetURLVars(r, map[string]string{"code": partnersReferralTestCode})
			require.Equal(t, 503, h.admit(r))
		})
	}
}
