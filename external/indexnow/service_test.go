package indexnow

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "example-key-1234"

type recordingAttemptStore struct {
	recorded, finished   *Attempt
	recordErr, finishErr error
	mutate               bool
}

func (r *recordingAttemptStore) RecordAttempt(_ context.Context, a *Attempt) error {
	r.recorded = cloneSubmissionAttempt(a)
	if r.mutate {
		a.ID = "changed"
		a.URLs[0] = "https://attacker.test"
	}
	return r.recordErr
}
func (r *recordingAttemptStore) FinishAttempt(_ context.Context, a *Attempt) error {
	r.finished = cloneSubmissionAttempt(a)
	return r.finishErr
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func newTestService(t *testing.T, cfg Config) *Service {
	t.Helper()
	if cfg.SiteURL == "" {
		cfg.SiteURL = "https://example.com"
	}
	s, err := NewService(testAPIKey, cfg)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return s
}

func TestSubmissionValidationBeforeDispatch(t *testing.T) {
	type testCase struct {
		name        string
		urls        []string
		keyLocation string
		want        error
	}
	cases := []testCase{
		{name: "empty", want: ErrNoURLs},
		{name: "oversize", urls: make([]string, MaxURLsPerRequest+1), want: ErrTooManyURLs},
		{name: "valid batch with invalid tail", urls: []string{"https://example.com/catalog/good", "https://other.test/catalog/bad"}, want: ErrURLHostMismatch},
		{name: "subdomain", urls: []string{"https://sub.example.com/post"}, want: ErrURLHostMismatch},
		{name: "different port", urls: []string{"https://example.com:8443/post"}, want: ErrURLHostMismatch},
		{name: "relative", urls: []string{"/post"}, want: ErrInvalidURL},
		{name: "credentials", urls: []string{"https://user:password@example.com/post"}, want: ErrInvalidURL},
		{name: "fragment", urls: []string{"https://example.com/post#part"}, want: ErrInvalidURL},
		{name: "space", urls: []string{"https://example.com/a b"}, want: ErrInvalidURL},
		{name: "backslash", urls: []string{"https://example.com/catalog\\..\\private"}, want: ErrInvalidURL},
		{name: "decoded traversal", urls: []string{"https://example.com/catalog/%2e%2e/private"}, want: ErrInvalidURL},
		{name: "encoded separator", urls: []string{"https://example.com/catalog%2fprivate"}, want: ErrInvalidURL},
		{name: "double encoded traversal", urls: []string{"https://example.com/catalog/%252e%252e/private"}, want: ErrInvalidURL},
		{name: "nested outside scope", urls: []string{"https://example.com/other/post"}, keyLocation: "/catalog/key.txt", want: ErrInvalidKeyLocation},
		{name: "nested sibling prefix", urls: []string{"https://example.com/catalogue/post"}, keyLocation: "/catalog/key.txt", want: ErrInvalidKeyLocation},
		{name: "nested encoded valid text", urls: []string{"https://example.com/catalog/caf%C3%A9"}, keyLocation: "/catalog/key.txt"},
		{name: "nested inside scope", urls: []string{"https://example.com/catalog/post"}, keyLocation: "/catalog/key.txt"},
		{name: "mixed HTTP HTTPS", urls: []string{"http://example.com/a", "https://example.com/b"}},
		{name: "explicit default ports", urls: []string{"http://example.com:80/a", "https://example.com:443/b"}},
		{name: "maximum batch", urls: repeatURL(MaxURLsPerRequest, "https://example.com/a")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			store := &recordingAttemptStore{}
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			service := newTestService(t, Config{HTTPClient: client, Repository: store, KeyLocation: tc.keyLocation})
			_, err := service.SubmitURLs(context.Background(), &SubmitURLsRequest{URLs: tc.urls})
			if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v,want=%v", err, tc.want)
			}
			wantCalls := 0
			if tc.want == nil {
				wantCalls = 1
			}
			if calls != wantCalls {
				t.Fatalf("outbound calls=%d,want=%d", calls, wantCalls)
			}
			if tc.want != nil && store.recorded != nil {
				t.Fatal("invalid request wrote an audit record")
			}
		})
	}
}
func repeatURL(n int, value string) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = value
	}
	return out
}

func TestSubmissionWireForms(t *testing.T) {
	type testCase struct{ name, method, location string }
	cases := []testCase{{"single default", http.MethodGet, ""}, {"batch default", http.MethodPost, ""}, {"single custom", http.MethodGet, "/catalog/ownership.txt"}, {"batch custom", http.MethodPost, "https://example.com/catalog/ownership.txt"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			submitted := []string{"https://example.com/catalog/a?value=one%26two&other=3"}
			var method, key, host, location string
			var urls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				method = r.Method
				if r.Method == http.MethodGet {
					key = r.URL.Query().Get("key")
					urls = []string{r.URL.Query().Get("url")}
					location = r.URL.Query().Get("keyLocation")
				} else {
					if r.URL.RawQuery != "" {
						t.Error("POST placed protocol fields in query")
					}
					var body struct {
						Host, Key, KeyLocation string
						URLList                []string
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					key = body.Key
					host = body.Host
					location = body.KeyLocation
					urls = body.URLList
					if r.Header.Get("Content-Type") != "application/json; charset=utf-8" {
						t.Error("missing JSON content type")
					}
				}
				w.WriteHeader(200)
			}))
			t.Cleanup(srv.Close)
			store := &recordingAttemptStore{}
			s := newTestService(t, Config{Endpoint: srv.URL, KeyLocation: tc.location, Repository: store})
			var response *SubmitURLsResponse
			var err error
			if tc.method == http.MethodGet {
				response, err = s.SubmitURL(context.Background(), submitted[0])
			} else {
				response, err = s.SubmitURLs(context.Background(), &SubmitURLsRequest{URLs: submitted})
			}
			if err != nil {
				t.Fatal(err)
			}
			if method != tc.method || key != testAPIKey || !reflect.DeepEqual(urls, submitted) {
				t.Fatalf("unexpected protocol request method=%s,urls=%v", method, urls)
			}
			if tc.method == http.MethodPost && host != "example.com" {
				t.Fatalf("host=%q", host)
			}
			wantLocation := ""
			if tc.location != "" {
				wantLocation = "https://example.com/catalog/ownership.txt"
			}
			if location != wantLocation {
				t.Fatalf("keyLocation=%q,want=%q", location, wantLocation)
			}
			if store.recorded.Outcome != OutcomeInFlight || !store.recorded.FinishedAt.IsZero() || store.finished.Outcome != OutcomeAcknowledged || store.finished.FinishedAt.Before(store.recorded.StartedAt) || response.AttemptID != store.recorded.ID {
				t.Fatal("audit lifecycle does not match receipt")
			}
			raw, _ := json.Marshal(store.finished)
			if strings.Contains(string(raw), testAPIKey) {
				t.Fatal("API key leaked into audit")
			}
		})
	}
}

func TestSubmissionProtocolOutcomes(t *testing.T) {
	type testCase struct {
		name           string
		status         int
		outcome        Outcome
		success, retry bool
	}
	cases := []testCase{{"received", 200, OutcomeAcknowledged, true, false}, {"key pending", 202, OutcomePending, true, false}, {"bad request", 400, OutcomeBadRequest, false, false}, {"invalid key", 403, OutcomeRejected, false, false}, {"invalid URLs", 422, OutcomeRejected, false, false}, {"throttled", 429, OutcomeRateLimited, false, true}, {"engine unavailable", 503, OutcomeServerError, false, true}, {"unexpected redirect", 302, OutcomeUnexpectedStatus, false, false}, {"unexpected success", 204, OutcomeUnexpectedStatus, false, false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingAttemptStore{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, "raw engine body "+testAPIKey)
			}))
			t.Cleanup(srv.Close)
			s := newTestService(t, Config{Endpoint: srv.URL, Repository: store})
			got, err := s.SubmitURL(context.Background(), "https://example.com/post")
			if (err == nil) != tc.success {
				t.Fatalf("error=%v,success=%v", err, tc.success)
			}
			if got == nil || got.Outcome != tc.outcome || got.StatusCode != tc.status || got.Success != tc.success || got.Retryable != tc.retry || got.RetryAfter != time.Minute {
				t.Fatalf("receipt=%+v", got)
			}
			if store.finished.RetryAfter != time.Minute {
				t.Fatal("retry delay missing from audit")
			}
			audit, _ := json.Marshal(store.finished)
			if strings.Contains(string(audit), testAPIKey) || err != nil && strings.Contains(err.Error(), testAPIKey) {
				t.Fatal("raw response leaked secret")
			}
			if !tc.success {
				var protocol *Error
				if !errors.As(err, &protocol) || protocol.Outcome != tc.outcome {
					t.Fatal("missing classified protocol error")
				}
			}
		})
	}
}

func TestSubmissionAuditFailures(t *testing.T) {
	type testCase struct {
		name                                        string
		recordFail, finishFail, networkFail, mutate bool
	}
	cases := []testCase{{name: "record failure", recordFail: true}, {name: "finish after receipt", finishFail: true}, {name: "finish after transport failure", finishFail: true, networkFail: true}, {name: "transport failure", networkFail: true}, {name: "repository argument ownership", mutate: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sensitive := errors.New("transport diagnostic " + testAPIKey)
			store := &recordingAttemptStore{mutate: tc.mutate}
			if tc.recordFail {
				store.recordErr = sensitive
			}
			if tc.finishFail {
				store.finishErr = sensitive
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if tc.networkFail {
					return nil, sensitive
				}
				if got := r.URL.Query().Get("url"); got != "https://example.com/post" {
					t.Errorf("repository changed dispatch URL=%s", got)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
			})}
			s := newTestService(t, Config{HTTPClient: client, Repository: store})
			response, err := s.SubmitURL(context.Background(), "https://example.com/post")
			if err != nil && strings.Contains(err.Error(), testAPIKey) {
				t.Fatal("dependency secret leaked")
			}
			if tc.recordFail {
				var audit *AuditError
				if response != nil || calls != 0 || !errors.As(err, &audit) || audit.Dispatched {
					t.Fatalf("pre-dispatch failure result=%+v,error=%v,calls=%d", response, err, calls)
				}
				return
			}
			if response == nil || calls != 1 || response.Success == tc.networkFail {
				t.Fatalf("lost authoritative receipt=%+v,error=%v", response, err)
			}
			if tc.finishFail {
				var audit *AuditError
				if !errors.As(err, &audit) || !audit.Dispatched || !errors.Is(err, ErrAuditUnavailable) {
					t.Fatal("completion audit failure missing")
				}
			}
			if tc.networkFail {
				var protocol *Error
				if !errors.As(err, &protocol) || !protocol.Retryable {
					t.Fatal("transport metadata lost")
				}
			}
			if !tc.finishFail && !tc.networkFail && err != nil {
				t.Fatal(err)
			}
			if tc.mutate && (response.AttemptID == "changed" || store.finished.ID != store.recorded.ID) {
				t.Fatal("repository changed authoritative identity")
			}
		})
	}
}

func TestSubmissionContextAndWiring(t *testing.T) {
	type testCase struct {
		name                                                               string
		nilService, zeroService, nilContext, nilRequest, canceled, expired bool
		want                                                               error
	}
	cases := []testCase{{name: "nil service", nilService: true, want: ErrNilService}, {name: "zero service", zeroService: true, want: ErrNilService}, {name: "nil context", nilContext: true, want: ErrNilContext}, {name: "nil request", nilRequest: true, want: ErrNilRequest}, {name: "canceled", canceled: true, want: context.Canceled}, {name: "expired", expired: true, want: context.DeadlineExceeded}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := newTestService(t, Config{HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not dispatch") })}})
			if tc.nilService {
				s = nil
			}
			if tc.zeroService {
				s = &Service{}
			}
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			if tc.expired {
				c, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				ctx = c
			}
			req := &SubmitURLsRequest{URLs: []string{"https://example.com/post"}}
			if tc.nilRequest {
				req = nil
			}
			_, err := s.SubmitURLs(ctx, req)
			if !errors.Is(err, tc.want) || calls != 0 {
				t.Fatalf("error=%v,want=%v,calls=%d", err, tc.want, calls)
			}
		})
	}
}

func TestInjectedClientCannotFollowRedirects(t *testing.T) {
	type testCase struct {
		name   string
		method string
		status int
	}
	cases := []testCase{{"GET moved", http.MethodGet, 302}, {"POST temporary preserve body", http.MethodPost, 307}, {"POST permanent preserve body", http.MethodPost, 308}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			leaks := 0
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks++ }))
			t.Cleanup(target.Close)
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, tc.status) }))
			t.Cleanup(source.Close)
			originalRedirect := func(*http.Request, []*http.Request) error { return nil }
			client := &http.Client{CheckRedirect: originalRedirect}
			s := newTestService(t, Config{Endpoint: source.URL, HTTPClient: client})
			if tc.method == http.MethodGet {
				_, _ = s.SubmitURL(context.Background(), "https://example.com/post")
			} else {
				_, _ = s.SubmitURLs(context.Background(), &SubmitURLsRequest{URLs: []string{"https://example.com/post"}})
			}
			if leaks != 0 {
				t.Fatalf("followed redirect %d times", leaks)
			}
			if client.Timeout != 0 || reflect.ValueOf(client.CheckRedirect).Pointer() != reflect.ValueOf(originalRedirect).Pointer() {
				t.Fatal("caller client mutated")
			}
		})
	}
}

func TestOutboundTimeout(t *testing.T) {
	type testCase struct {
		name                      string
		clientTimeout, configured time.Duration
	}
	cases := []testCase{{"configured bound", 0, 30 * time.Millisecond}, {"shorter client bound", 30 * time.Millisecond, time.Second}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
			t.Cleanup(srv.Close)
			s := newTestService(t, Config{Endpoint: srv.URL, HTTPClient: &http.Client{Timeout: tc.clientTimeout}, Timeout: tc.configured})
			response, err := s.SubmitURL(context.Background(), "https://example.com/post")
			if !errors.Is(err, context.DeadlineExceeded) || response == nil || !response.Retryable {
				t.Fatalf("timeout receipt=%+v,error=%v", response, err)
			}
		})
	}
}
