package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoopbackAddress(t *testing.T) {
	type testCase struct {
		name      string
		address   string
		wantError bool
	}
	tests := []testCase{
		{name: "explicit IPv4 loopback", address: "127.0.0.1:8097"},
		{name: "explicit IPv6 loopback", address: "[::1]:8097"},
		{name: "ephemeral loopback port", address: "127.0.0.1:0"},
		{name: "IPv4 wildcard denied", address: "0.0.0.0:8097", wantError: true},
		{name: "IPv6 wildcard denied", address: "[::]:8097", wantError: true},
		{name: "empty host denied", address: ":8097", wantError: true},
		{name: "hostname denied", address: "localhost:8097", wantError: true},
		{name: "public IP denied", address: "192.0.2.1:8097", wantError: true},
		{name: "missing port denied", address: "127.0.0.1", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := loopbackAddress(tc.address); (err != nil) != tc.wantError {
				t.Fatalf("loopbackAddress(%q) error = %v, want error %v", tc.address, err, tc.wantError)
			}
		})
	}
}

func TestDemoSubmission(t *testing.T) {
	type testCase struct {
		name               string
		mode               string
		urls               []string
		simulation         string
		wantError          bool
		wantEngineRequests int
		wantMethod         string
	}
	tests := []testCase{
		{name: "single acknowledged", mode: "single", urls: []string{demoSite + "/first"}, simulation: "200", wantEngineRequests: 1, wantMethod: http.MethodGet},
		{name: "batch pending", mode: "batch", urls: []string{demoSite + "/first", demoSite + "/second"}, simulation: "202", wantEngineRequests: 1, wantMethod: http.MethodPost},
		{name: "malformed response", mode: "single", urls: []string{demoSite + "/first"}, simulation: "400", wantError: true, wantEngineRequests: 1, wantMethod: http.MethodGet},
		{name: "invalid key response", mode: "single", urls: []string{demoSite + "/first"}, simulation: "403", wantError: true, wantEngineRequests: 1, wantMethod: http.MethodGet},
		{name: "invalid URL response", mode: "batch", urls: []string{demoSite + "/first"}, simulation: "422", wantError: true, wantEngineRequests: 1, wantMethod: http.MethodPost},
		{name: "rate limited response", mode: "batch", urls: []string{demoSite + "/first"}, simulation: "429", wantError: true, wantEngineRequests: 1, wantMethod: http.MethodPost},
		{name: "engine failure", mode: "single", urls: []string{demoSite + "/first"}, simulation: "500", wantError: true, wantEngineRequests: 1, wantMethod: http.MethodGet},
		{name: "transport failure", mode: "single", urls: []string{demoSite + "/first"}, simulation: "network", wantError: true},
		{name: "different host rejected locally", mode: "single", urls: []string{"https://other.example/first"}, simulation: "200", wantError: true},
		{name: "empty batch rejected locally", mode: "batch", simulation: "200", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, cleanup, err := newDemo()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			body, err := json.Marshal(map[string]any{"mode": tc.mode, "urls": tc.urls, "simulation": tc.simulation})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/submit", bytes.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			d.handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			var result submission
			if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if (result.Error != "") != tc.wantError {
				t.Fatalf("service error = %q, want error %v; receipt = %#v", result.Error, tc.wantError, result.Receipt)
			}
			if len(d.requests) != tc.wantEngineRequests {
				t.Fatalf("engine requests = %d, want %d", len(d.requests), tc.wantEngineRequests)
			}
			if len(d.submissions) != 1 {
				t.Fatalf("submission count = %d, want 1", len(d.submissions))
			}
			wantAuditAttempts := 0
			if tc.wantEngineRequests != 0 || tc.simulation == "network" {
				wantAuditAttempts = 1
			}
			if len(d.attempts) != wantAuditAttempts {
				t.Fatalf("audit attempts = %d, want %d", len(d.attempts), wantAuditAttempts)
			}
			for _, attempt := range d.attempts {
				if attempt.FinishedAt.IsZero() || len(attempt.URLs) != len(tc.urls) {
					t.Fatalf("audit attempt was not completed with its submitted URLs: %#v", attempt)
				}
			}
			if len(d.requests) != 0 {
				wire := d.requests[0]
				if wire.Method != tc.wantMethod || len(wire.URLs) != len(tc.urls) {
					t.Fatalf("wire = %#v, want method %s and %d URLs", wire, tc.wantMethod, len(tc.urls))
				}
				for i, value := range wire.URLs {
					if value != tc.urls[i] {
						t.Errorf("wire URL %d = %q, want %q", i, value, tc.urls[i])
					}
				}
			}
		})
	}
}

func TestDemoPublicFixtures(t *testing.T) {
	type testCase struct {
		name          string
		method        string
		path          string
		wantStatus    int
		wantBody      string
		wantEmptyBody bool
	}
	tests := []testCase{
		{name: "review form", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "Local simulation only"},
		{name: "key file", method: http.MethodGet, path: "/" + demoKey + ".txt", wantStatus: http.StatusOK, wantBody: demoKey},
		{name: "key metadata", method: http.MethodHead, path: "/" + demoKey + ".txt", wantStatus: http.StatusOK, wantEmptyBody: true},
		{name: "unknown route is absent", method: http.MethodGet, path: "/missing-page", wantStatus: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d, cleanup, err := newDemo()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(cleanup)
			request := httptest.NewRequest(tc.method, tc.path, nil)
			recorder := httptest.NewRecorder()
			d.handler().ServeHTTP(recorder, request)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(recorder.Body.String(), tc.wantBody) {
				t.Errorf("body = %q, want to contain %q", recorder.Body.String(), tc.wantBody)
			}
			if tc.wantEmptyBody && recorder.Body.Len() != 0 {
				t.Errorf("HEAD body = %q, want empty", recorder.Body.String())
			}
		})
	}
}

func TestDemoRequestValidation(t *testing.T) {
	type testCase struct {
		name        string
		body        string
		origin      string
		contentType string
		wantStatus  int
	}
	validBody := `{"mode":"single","urls":["https://example.com/first"],"simulation":"200"}`
	tests := []testCase{
		{name: "foreign origin denied", body: validBody, origin: "https://other.example", contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "plain form denied", body: validBody, contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
		{name: "malformed JSON denied", body: `{`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "extra JSON denied", body: validBody + ` {}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "unknown fields denied", body: `{"mode":"single","urls":["https://example.com/first"],"simulation":"200","endpoint":"https://other.example"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "unknown mode denied", body: `{"mode":"automatic","simulation":"200"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "single URL count enforced", body: `{"mode":"single","urls":[],"simulation":"200"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
		{name: "unsupported simulation denied", body: `{"mode":"single","urls":["https://example.com/first"],"simulation":"301"}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Invalid review controls must be rejected before any service is used.
			d := &demo{}
			request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8097/api/submit", strings.NewReader(tc.body))
			request.Header.Set("Content-Type", tc.contentType)
			request.Header.Set("Origin", tc.origin)
			recorder := httptest.NewRecorder()
			d.submit(recorder, request)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if len(d.submissions) != 0 || len(d.requests) != 0 {
				t.Fatal("denied request created review history")
			}
		})
	}
}

func TestLocalTransport(t *testing.T) {
	type testCase struct {
		name       string
		requestURL string
		simulation string
		wantError  bool
	}
	tests := []testCase{
		{name: "local engine reachable", requestURL: "/indexnow", simulation: "200"},
		{name: "external host blocked", requestURL: "http://example.com/indexnow", simulation: "200", wantError: true},
		{name: "HTTPS target blocked", requestURL: "https://example.com/indexnow", simulation: "200", wantError: true},
		{name: "network error simulated locally", requestURL: "/indexnow", simulation: "network", wantError: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
			t.Cleanup(engine.Close)
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.Proxy = nil
			t.Cleanup(transport.CloseIdleConnections)
			local := &localTransport{engineHost: strings.TrimPrefix(engine.URL, "http://"), transport: transport}
			requestURL := tc.requestURL
			if strings.HasPrefix(requestURL, "/") {
				requestURL = engine.URL + requestURL
			}
			ctx := context.WithValue(context.Background(), simulationKey{}, tc.simulation)
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := local.RoundTrip(request)
			if response != nil {
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
			}
			if (err != nil) != tc.wantError {
				t.Fatalf("RoundTrip error = %v, want error %v", err, tc.wantError)
			}
		})
	}
}
