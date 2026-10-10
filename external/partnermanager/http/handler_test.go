package partnerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/http/browsersecurity"
	"github.com/stretchr/testify/require"
)

type transportServiceFunc func(context.Context, Principal, Request) (Response, error)

func (f transportServiceFunc) Handle(c context.Context, p Principal, r Request) (Response, error) {
	return f(c, p, r)
}

type transportLimiter struct {
	retry time.Duration
	err   error
}

func (l *transportLimiter) Allow(context.Context, string) (time.Duration, error) {
	return l.retry, l.err
}

func transportFixture(t *testing.T, service HTTPService, config Config) (*Handler, Principal) {
	t.Helper()
	guard, err := browsersecurity.New(browsersecurity.Config{Key: []byte("fixture-only-independent-csrf-key-0000"), AllowedOrigins: []string{"https://host.example.test"}, CSRFCookieName: "fixture-csrf"})
	require.NoError(t, err)
	identity, err := browsersecurity.NewIdentity(browsersecurity.IdentityConfig{BindingParts: []string{"member", "actor", "credential"}, ActorID: "actor"})
	require.NoError(t, err)
	principal := Principal{ActorID: "actor", Credential: "credential", Verified: true, Transport: identity}
	config.Security = guard
	h, err := New(service, PrincipalResolverFunc(func(context.Context, *http.Request) (Principal, error) { return principal, nil }), config)
	require.NoError(t, err)
	return h, principal
}
func transportRequest(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, "https://host.example.test"+path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}
func protectTransport(t *testing.T, h *Handler, r *http.Request) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, transportRequest("GET", BootstrapPath, ""))
	require.Equal(t, 204, w.Code, w.Body.String())
	require.Len(t, w.Result().Cookies(), 1)
	r.AddCookie(w.Result().Cookies()[0])
	r.Header.Set("Origin", "https://host.example.test")
	r.Header.Set(browsersecurity.CSRFHeader, w.Header().Get(browsersecurity.CSRFHeader))
	r.Header.Set("Idempotency-Key", "original-intent-key")
}

func TestRegistryDispatchesEveryOperationAndSelectedOpaqueIDs(t *testing.T) {
	cases := Routes()
	require.Len(t, cases, 33)
	cases = append(cases, Route{"GET", BasePath + "/admin/claims/status", "admin.partners.status.read"}, Route{"GET", BasePath + "/admin/claims/claim-preparation", "admin.partners.claim-preparation.read"}, Route{"GET", BasePath + "/admin/claims/inspect", "admin.partners.inspect"})
	for _, tc := range cases {
		t.Run(tc.Method+tc.Path, func(t *testing.T) {
			calls := 0
			var received Request
			h, p := transportFixture(t, transportServiceFunc(func(_ context.Context, actor Principal, r Request) (Response, error) {
				calls++
				received = r
				require.Equal(t, "actor", actor.ActorID)
				return Response{Status: 200, Body: []byte(`{"data":{}}`)}, nil
			}), Config{})
			path := strings.ReplaceAll(tc.Path, "{id}", "selected-record")
			body := ""
			if tc.Method != "GET" {
				body = `{}`
			}
			r := transportRequest(tc.Method, path, body)
			if tc.Method != "GET" {
				protectTransport(t, h, r)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Equal(t, 1, calls)
			require.Equal(t, tc.Operation, received.Operation)
			if strings.Contains(tc.Path, "{id}") {
				require.Equal(t, "selected-record", received.ID)
			}
			if strings.HasSuffix(tc.Path, "/claims/status") || strings.HasSuffix(tc.Path, "/claims/inspect") || strings.HasSuffix(tc.Path, "/claims/claim-preparation") {
				require.Equal(t, "claims", received.ID)
			}
			if tc.Method != "GET" {
				require.Equal(t, "original-intent-key", received.Key)
			}
			require.Contains(t, w.Header().Get("Cache-Control"), "no-store")
			require.NotContains(t, w.Body.String(), p.Credential)
		})
	}
	// A caller cannot mutate the registry used by an existing/future handler.
	cases[0].Operation = "untrusted"
	require.Equal(t, "partners.program.read", Routes()[0].Operation)
}

func TestTransportRejectsMalformedRequestsBeforeService(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body, mode string
		status                         int
	}{
		{"unknown_route", "GET", "/unknown", "", "", 404}, {"wrong_method", "DELETE", "/overview", "", "", 405},
		{"duplicate_audience", "GET", "/csrf?audience=session&audience=session", "", "", 400}, {"public_bootstrap", "GET", "/csrf?audience=public", "", "", 400},
		{"malformed_query", "GET", "/overview?limit=%ZZ", "", "", 400}, {"get_body", "GET", "/overview", `{}`, "", 400},
		{"whitespace_media", "POST", "/claims", " \t\n", "media", 400}, {"whitespace_json", "POST", "/claims", " \t\n", "", 400}, {"whitespace_duplicate_media", "POST", "/claims", " ", "duplicate-media", 400},
		{"array", "POST", "/claims", `[]`, "", 400}, {"null", "POST", "/claims", `null`, "", 400}, {"scalar", "POST", "/claims", `1`, "", 400},
		{"trailing_json", "POST", "/claims", `{} {}`, "", 400}, {"wrong_media", "POST", "/claims", `{}`, "media", 400}, {"duplicate_media", "POST", "/claims", `{}`, "duplicate-media", 400},
		{"oversized_body", "POST", "/claims", strings.Repeat(" ", 65537), "", 413},
		{"missing_key", "POST", "/claims", `{}`, "no-key", 400}, {"duplicate_key", "POST", "/claims", `{}`, "duplicate-key", 400}, {"padded_key", "POST", "/claims", `{}`, "padded-key", 400},
		{"missing_csrf", "POST", "/claims", `{}`, "no-csrf", 403}, {"duplicate_csrf", "POST", "/claims", `{}`, "duplicate-csrf", 403},
		{"cross_origin", "POST", "/claims", `{}`, "origin", 403}, {"weak_match", "POST", "/claims", `{}`, "weak-etag", 400}, {"duplicate_match", "POST", "/claims", `{}`, "duplicate-etag", 400},
		{"unverified_bearer", "GET", "/overview", "", "bearer", 401},
		{"invalid_target", "GET", "/claims/bad%20id", "", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			h, _ := transportFixture(t, transportServiceFunc(func(context.Context, Principal, Request) (Response, error) { calls++; return Response{}, nil }), Config{})
			r := transportRequest(tc.method, BasePath+tc.path, tc.body)
			if tc.method == "POST" {
				protectTransport(t, h, r)
			}
			switch tc.mode {
			case "media":
				r.Header.Set("Content-Type", "text/plain")
			case "duplicate-media":
				r.Header.Add("Content-Type", "application/json")
			case "no-key":
				r.Header.Del("Idempotency-Key")
			case "duplicate-key":
				r.Header.Add("Idempotency-Key", "different")
			case "padded-key":
				r.Header.Set("Idempotency-Key", " key ")
			case "no-csrf":
				r.Header.Del(browsersecurity.CSRFHeader)
			case "duplicate-csrf":
				r.Header.Add(browsersecurity.CSRFHeader, "different")
			case "origin":
				r.Header.Set("Origin", "https://other.example.test")
			case "weak-etag":
				r.Header.Set("If-Match", `W/"1"`)
			case "duplicate-etag":
				r.Header.Add("If-Match", `"1"`)
				r.Header.Add("If-Match", `"2"`)
			case "bearer":
				r.Header.Set("Authorization", "Bearer unverified")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Zero(t, calls)
			if tc.status == 405 {
				require.Equal(t, "GET", w.Header().Get("Allow"))
			}
			require.NotContains(t, w.Body.String(), tc.body+"private-diagnostic")
		})
	}
}

func TestAdmissionRatesAndSafeResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   string
		status int
		code   string
	}{
		{"live_session_denied", "principal", 403, "PARTNERS_FORBIDDEN"}, {"missing_actor", "actor", 401, "PARTNERS_AUTH_REQUIRED"}, {"unverified_member", "verification", 403, "PARTNERS_VERIFICATION_REQUIRED"},
		{"rate_delay", "rate", 429, "PARTNERS_RATE_LIMITED"}, {"rate_outage", "rate-outage", 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"},
		{"private_service_error", "private", 500, "PARTNERS_INTERNAL_ERROR"}, {"unsupported_error_pair", "error-pair", 500, "PARTNERS_INTERNAL_ERROR"},
		{"joined_unknown", "joined-unknown", 500, "PARTNERS_INTERNAL_ERROR"}, {"joined_outage", "joined-outage", 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"}, {"safe_error", "safe", 412, "PARTNERS_STALE_WRITE"}, {"cancelled", "cancelled", 503, "PARTNERS_DEPENDENCY_UNAVAILABLE"},
		{"invalid_json_response", "json", 500, "PARTNERS_INTERNAL_ERROR"}, {"invalid_response_status", "status", 500, "PARTNERS_INTERNAL_ERROR"}, {"weak_response_etag", "etag", 500, "PARTNERS_INTERNAL_ERROR"},
		{"response", "success", 201, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			limiter := &transportLimiter{}
			h, p := transportFixture(t, transportServiceFunc(func(context.Context, Principal, Request) (Response, error) {
				calls++
				switch tc.mode {
				case "joined-unknown":
					return Response{}, errors.Join(fail("PARTNERS_NOT_FOUND", 404), errors.New("private-diagnostic"))
				case "joined-outage":
					return Response{}, errors.Join(fail("PARTNERS_FORBIDDEN", 403), fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503))
				case "private":
					return Response{}, errors.New("private-diagnostic")
				case "error-pair":
					return Response{}, fail("PARTNERS_NOT_FOUND", 403)
				case "safe":
					return Response{}, fail("PARTNERS_STALE_WRITE", 412)
				case "cancelled":
					return Response{}, context.Canceled
				}
				response := Response{Status: 201, Body: []byte(`{"data":{}}`), ETag: `"1"`}
				switch tc.mode {
				case "json":
					response.Body = []byte("private-diagnostic")
				case "status":
					response.Status = 302
				case "etag":
					response.ETag = `W/"1"`
				}
				return response, nil
			}), Config{Limiter: limiter})
			switch tc.mode {
			case "principal":
				h.resolver = PrincipalResolverFunc(func(context.Context, *http.Request) (Principal, error) {
					return Principal{}, fail("PARTNERS_FORBIDDEN", 403)
				})
			case "actor":
				p.ActorID = ""
			case "verification":
				p.Verified = false
			case "rate":
				limiter.retry = time.Nanosecond
			case "rate-outage":
				limiter.err = errors.New("private-diagnostic")
			}
			if tc.mode == "actor" || tc.mode == "verification" {
				h.resolver = PrincipalResolverFunc(func(context.Context, *http.Request) (Principal, error) { return p, nil })
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, transportRequest("GET", BasePath+"/overview", ""))
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "private-diagnostic")
			if tc.code != "" {
				var envelope struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
				require.Equal(t, tc.code, envelope.Error.Code)
			}
			if tc.mode == "rate" {
				require.Equal(t, "1", w.Header().Get("Retry-After"))
			}
			if tc.mode == "success" {
				require.Equal(t, `"1"`, w.Header().Get("ETag"))
			}
			if tc.mode == "principal" || tc.mode == "actor" || tc.mode == "verification" || strings.HasPrefix(tc.mode, "rate") {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestObservationContainsOnlyFiniteRoutingMetadata(t *testing.T) {
	for _, tc := range []struct {
		method, path     string
		status           int
		operation, route string
	}{
		{"GET", BasePath + "/claims/secret-target?after=secret-query", 200, "partners.claim.read", BasePath + "/claims/{id}"},
		{"PRIVATE-SECRET-METHOD", BasePath + "/secret-path?actor=secret-actor", 404, "", ""},
	} {
		t.Run(tc.method, func(t *testing.T) {
			var observed []Observation
			h, _ := transportFixture(t, transportServiceFunc(func(context.Context, Principal, Request) (Response, error) {
				return Response{Status: 200, Body: []byte(`{"data":{}}`)}, nil
			}), Config{Observe: func(_ context.Context, o Observation) { observed = append(observed, o) }})
			w := httptest.NewRecorder()
			h.ServeHTTP(w, transportRequest(tc.method, tc.path, ""))
			require.Equal(t, tc.status, w.Code)
			require.Len(t, observed, 1)
			require.Equal(t, tc.operation, observed[0].Operation)
			require.Equal(t, tc.route, observed[0].RouteTemplate)
			raw, err := json.Marshal(observed[0])
			require.NoError(t, err)
			require.NotContains(t, string(raw), "secret")
			require.NotContains(t, string(raw), "PRIVATE")
		})
	}
}
