package observability

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip invokes the wrapped transport function.
func (function roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type apiKeyRoundTripper struct {
	base   http.RoundTripper
	apiKey string
}

// RoundTrip adds the test API key before delegating to the base transport.
func (transport *apiKeyRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	requestWithAPIKey := request.Clone(request.Context())
	requestWithAPIKey.Header.Set("X-Api-Key", transport.apiKey)
	return transport.base.RoundTrip(requestWithAPIKey)
}

// TestNewRoundTripperPropagatesTraceContextOnceAndKeepsRawURLPrivate verifies propagation and URL redaction.
func TestNewRoundTripperPropagatesTraceContextOnceAndKeepsRawURLPrivate(t *testing.T) {
	tracerProvider, spanRecorder, options := newTestHTTPTraceProvider(t)

	const (
		sensitivePath  = "/vehicles/PV19SZK/history"
		sensitiveQuery = "access_token=secret-token&vrm=PV19SZK"
	)
	requestURL := "https://dvla.example.test" + sensitivePath + "?" + sensitiveQuery

	baseCalls := 0
	var observedRequest *http.Request
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		baseCalls++
		observedRequest = request
		return responseWithBody(http.StatusAccepted, request), nil
	})

	ctx, parent := tracerProvider.Tracer("test-parent").Start(context.Background(), "parent")
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("X-Request-ID", "request-id")

	response, err := newRoundTripper(base, options...).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	parent.End()

	if baseCalls != 1 {
		t.Fatalf("base RoundTrip calls = %d, want 1", baseCalls)
	}
	if observedRequest == nil {
		t.Fatal("base transport did not receive a request")
	}
	if got := observedRequest.URL.String(); got != requestURL {
		t.Fatalf("base request URL = %q, want %q", got, requestURL)
	}
	if got := observedRequest.Header.Get("X-Request-ID"); got != "request-id" {
		t.Fatalf("base X-Request-ID = %q, want request-id", got)
	}
	if got := observedRequest.Header.Get("traceparent"); got == "" {
		t.Fatal("base request did not receive a W3C traceparent header")
	}
	if got := request.Header.Get("traceparent"); got != "" {
		t.Fatalf("caller's traceparent header = %q, want empty", got)
	}
	if got := request.URL.String(); got != requestURL {
		t.Fatalf("caller's request URL changed to %q", got)
	}

	clientSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindClient)
	propagatedContext := propagation.TraceContext{}.Extract(
		context.Background(),
		propagation.HeaderCarrier(observedRequest.Header),
	)
	propagatedSpanContext := oteltrace.SpanContextFromContext(propagatedContext)
	if !propagatedSpanContext.IsValid() || !propagatedSpanContext.IsRemote() {
		t.Fatalf("propagated span context = %v, want valid remote context", propagatedSpanContext)
	}
	if propagatedSpanContext.TraceID() != clientSpan.SpanContext().TraceID() || propagatedSpanContext.SpanID() != clientSpan.SpanContext().SpanID() {
		t.Fatalf("propagated context %v does not identify client span %v", propagatedSpanContext, clientSpan.SpanContext())
	}

	assertAttributeEquals(t, clientSpan, "url.full", "https://dvla.example.test/")
	assertSpanExcludes(t, clientSpan, sensitivePath, sensitiveQuery, "PV19SZK", "secret-token")
}

// TestNewRoundTripperPreservesCustomTransportHeadersAndTraceContext verifies custom transport composition.
func TestNewRoundTripperPreservesCustomTransportHeadersAndTraceContext(t *testing.T) {
	tracerProvider, spanRecorder, options := newTestHTTPTraceProvider(t)
	const requestURL = "https://dvla.example.test/vehicle-enquiry/v1/vehicles/PV19SZK?token=private"

	baseCalls := 0
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		baseCalls++
		if got := request.URL.String(); got != requestURL {
			t.Fatalf("base request URL = %q, want %q", got, requestURL)
		}
		if got := request.Header.Get("X-Api-Key"); got != "dvla-key" {
			t.Fatalf("X-Api-Key = %q, want dvla-key", got)
		}
		if got := request.Header.Get("traceparent"); got == "" {
			t.Fatal("custom transport chain lost traceparent")
		}
		return responseWithBody(http.StatusNoContent, request), nil
	})
	customTransport := &apiKeyRoundTripper{base: base, apiKey: "dvla-key"}

	ctx, parent := tracerProvider.Tracer("test-parent").Start(context.Background(), "parent")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	response, err := newRoundTripper(customTransport, options...).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}
	parent.End()

	if baseCalls != 1 {
		t.Fatalf("base RoundTrip calls = %d, want 1", baseCalls)
	}
	if got := request.Header.Get("X-Api-Key"); got != "" {
		t.Fatalf("caller's X-Api-Key = %q, want empty", got)
	}
	if got := request.Header.Get("traceparent"); got != "" {
		t.Fatalf("caller's traceparent = %q, want empty", got)
	}

	clientSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindClient)
	assertSpanExcludes(t, clientSpan, "PV19SZK", "token=private")
}

// TestNewRoundTripperReturnsOriginalErrorButRecordsPrivacySafeError verifies safe telemetry without changing caller errors.
func TestNewRoundTripperReturnsOriginalErrorButRecordsPrivacySafeError(t *testing.T) {
	_, spanRecorder, options := newTestHTTPTraceProvider(t)
	const requestURL = "https://upstream.example.test/private/PV19SZK?api_key=secret-token"
	wantErr := fmt.Errorf("request to %s failed", requestURL)

	baseCalls := 0
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		baseCalls++
		if got := request.URL.String(); got != requestURL {
			t.Fatalf("base request URL = %q, want %q", got, requestURL)
		}
		return nil, wantErr
	})
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	response, gotErr := newRoundTripper(base, options...).RoundTrip(request)
	if response != nil {
		t.Fatalf("RoundTrip() response = %#v, want nil", response)
	}
	if gotErr != wantErr {
		t.Fatalf("RoundTrip() error = %v, want original error %v", gotErr, wantErr)
	}
	if baseCalls != 1 {
		t.Fatalf("base RoundTrip calls = %d, want 1", baseCalls)
	}

	clientSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindClient)
	if clientSpan.Status().Code != codes.Error {
		t.Fatalf("client span status = %v, want Error", clientSpan.Status())
	}
	if got := clientSpan.Status().Description; got != telemetrySafeTransportError.Error() {
		t.Fatalf("client span status description = %q, want %q", got, telemetrySafeTransportError.Error())
	}
	assertSpanExcludes(t, clientSpan, requestURL, "PV19SZK", "secret-token")
}

// TestNewRoundTripperRecordsHTTPErrorStatusWithoutSensitiveURL verifies safe spans for unsuccessful responses.
func TestNewRoundTripperRecordsHTTPErrorStatusWithoutSensitiveURL(t *testing.T) {
	_, spanRecorder, options := newTestHTTPTraceProvider(t)
	const requestURL = "https://upstream.example.test/private/PV19SZK?token=secret-token"

	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return responseWithBody(http.StatusServiceUnavailable, request), nil
	})
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	response, err := newRoundTripper(base, options...).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response body: %v", err)
	}

	clientSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindClient)
	if clientSpan.Status().Code != codes.Error {
		t.Fatalf("client span status = %v, want Error", clientSpan.Status())
	}
	assertAttributeEquals(t, clientSpan, "http.response.status_code", int64(http.StatusServiceUnavailable))
	assertSpanExcludes(t, clientSpan, "PV19SZK", "secret-token")
}

// TestNewRoundTripperPreservesCancellation verifies cancellation propagation through the instrumented transport.
func TestNewRoundTripperPreservesCancellation(t *testing.T) {
	tracerProvider, spanRecorder, options := newTestHTTPTraceProvider(t)
	const requestURL = "https://upstream.example.test/private/PV19SZK?token=secret-token"

	baseCalls := 0
	ctx, cancel := context.WithCancel(context.Background())
	parentCtx, parent := tracerProvider.Tracer("test-parent").Start(ctx, "parent")
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		baseCalls++
		if got := request.URL.String(); got != requestURL {
			t.Fatalf("base request URL = %q, want %q", got, requestURL)
		}
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	request, err := http.NewRequestWithContext(parentCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	_, gotErr := newRoundTripper(base, options...).RoundTrip(request)
	parent.End()
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("RoundTrip() error = %v, want context.Canceled", gotErr)
	}
	if baseCalls != 1 {
		t.Fatalf("base RoundTrip calls = %d, want 1", baseCalls)
	}

	clientSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindClient)
	if clientSpan.Status().Code != codes.Error {
		t.Fatalf("client span status = %v, want Error", clientSpan.Status())
	}
	assertSpanExcludes(t, clientSpan, "PV19SZK", "secret-token")
}

// TestNewHTTPClientUsesInstrumentedTransportAndTimeout verifies client construction and timeout handling.
func TestNewHTTPClientUsesInstrumentedTransportAndTimeout(t *testing.T) {
	const timeout = 7 * time.Second
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return responseWithBody(http.StatusOK, request), nil
	})

	client := NewHTTPClient(base, timeout)
	if client.Timeout != timeout {
		t.Fatalf("client timeout = %s, want %s", client.Timeout, timeout)
	}
	if _, ok := client.Transport.(*privacySafeRoundTripper); !ok {
		t.Fatalf("client transport = %T, want privacy-safe instrumented transport", client.Transport)
	}
}

// TestHTTPServerMiddlewareUsesRouteTemplateAndRestoresRequestTarget verifies safe server spans without mutating requests.
func TestHTTPServerMiddlewareUsesRouteTemplateAndRestoresRequestTarget(t *testing.T) {
	_, spanRecorder, options := newTestHTTPTraceProvider(t)
	const (
		traceparent  = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
		requestURI   = "/vehicles/PV19SZK?access_token=secret-token"
		remoteAddr   = "203.0.113.42:4242"
		forwardedFor = "198.51.100.7"
		userAgent    = "private-fleet-client/vehicle-PV19SZK"
		requestHost  = "tenant-PV19SZK.example.test"
	)

	handlerCalls := 0
	router := mux.NewRouter()
	router.Use(httpServerMiddleware("vehicle-api", options...))
	router.HandleFunc("/vehicles/{vrn}", func(responseWriter http.ResponseWriter, request *http.Request) {
		handlerCalls++
		if got := request.URL.Path; got != "/vehicles/PV19SZK" {
			t.Fatalf("handler URL path = %q, want original path", got)
		}
		if got := request.URL.RawQuery; got != "access_token=secret-token" {
			t.Fatalf("handler raw query = %q, want original query", got)
		}
		if got := request.RequestURI; got != requestURI {
			t.Fatalf("handler RequestURI = %q, want %q", got, requestURI)
		}
		if got := mux.Vars(request)["vrn"]; got != "PV19SZK" {
			t.Fatalf("handler route variable = %q, want PV19SZK", got)
		}
		if got := request.RemoteAddr; got != remoteAddr {
			t.Fatalf("handler RemoteAddr = %q, want %q", got, remoteAddr)
		}
		if got := request.Host; got != requestHost {
			t.Fatalf("handler Host = %q, want %q", got, requestHost)
		}
		if got := request.Header.Get("X-Forwarded-For"); got != forwardedFor {
			t.Fatalf("handler X-Forwarded-For = %q, want %q", got, forwardedFor)
		}
		if got := request.UserAgent(); got != userAgent {
			t.Fatalf("handler User-Agent = %q, want %q", got, userAgent)
		}
		if got := request.Header.Get("X-Application-Metadata"); got != "preserved" {
			t.Fatalf("handler application header = %q, want preserved", got)
		}
		if spanContext := oteltrace.SpanContextFromContext(request.Context()); !spanContext.IsValid() {
			t.Fatal("handler context does not contain a valid server span")
		}
		responseWriter.WriteHeader(http.StatusAccepted)
	}).Methods(http.MethodGet)

	request := httptest.NewRequest(http.MethodGet, requestURI, nil)
	request.Host = requestHost
	request.RemoteAddr = remoteAddr
	request.Header.Set("traceparent", traceparent)
	request.Header.Set("X-Forwarded-For", forwardedFor)
	request.Header.Set("Forwarded", "for=192.0.2.60;proto=https")
	request.Header.Set("X-Real-IP", "192.0.2.61")
	request.Header.Set("X-Client-IP", "192.0.2.62")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("X-Application-Metadata", "preserved")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}
	if response.Code != http.StatusAccepted {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if got := request.URL.Path; got != "/vehicles/PV19SZK" {
		t.Fatalf("caller's URL path changed to %q", got)
	}
	if got := request.URL.RawQuery; got != "access_token=secret-token" {
		t.Fatalf("caller's raw query changed to %q", got)
	}
	if got := request.RemoteAddr; got != remoteAddr {
		t.Fatalf("caller's RemoteAddr changed to %q", got)
	}
	if got := request.Host; got != requestHost {
		t.Fatalf("caller's Host changed to %q", got)
	}
	if got := request.UserAgent(); got != userAgent {
		t.Fatalf("caller's User-Agent changed to %q", got)
	}

	serverSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindServer)
	if got, want := serverSpan.Name(), "GET /vehicles/{vrn}"; got != want {
		t.Fatalf("server span name = %q, want %q", got, want)
	}
	if got, want := serverSpan.Parent().TraceID().String(), "4bf92f3577b34da6a3ce929d0e0e4736"; got != want {
		t.Fatalf("server parent trace ID = %q, want %q", got, want)
	}
	if got, want := serverSpan.Parent().SpanID().String(), "00f067aa0ba902b7"; got != want {
		t.Fatalf("server parent span ID = %q, want %q", got, want)
	}
	assertAttributeEquals(t, serverSpan, "http.route", "/vehicles/{vrn}")
	assertAttributeEquals(t, serverSpan, "url.path", redactedURLPath)
	assertSpanExcludes(
		t,
		serverSpan,
		requestURI,
		"PV19SZK",
		"secret-token",
		remoteAddr,
		"203.0.113.42",
		forwardedFor,
		userAgent,
		requestHost,
		"192.0.2.60",
		"192.0.2.61",
		"192.0.2.62",
	)
}

// TestHTTPServerMiddlewareBoundsUnknownMethodCardinality verifies unusual methods stay private to handlers.
func TestHTTPServerMiddlewareBoundsUnknownMethodCardinality(t *testing.T) {
	_, spanRecorder, options := newTestHTTPTraceProvider(t)
	const (
		originalMethod = "SEARCH-PRIVATE-VRN"
		originalHost   = "private-tenant.example.test"
	)

	router := mux.NewRouter()
	router.Use(httpServerMiddleware("vehicle-api", options...))
	router.HandleFunc("/vehicles/{vrn}", func(responseWriter http.ResponseWriter, request *http.Request) {
		if request.Method != originalMethod {
			t.Fatalf("handler method = %q, want %q", request.Method, originalMethod)
		}
		if request.Host != originalHost {
			t.Fatalf("handler Host = %q, want %q", request.Host, originalHost)
		}
		responseWriter.WriteHeader(http.StatusNoContent)
	})

	request := httptest.NewRequest(originalMethod, "/vehicles/PV19SZK", nil)
	request.Host = originalHost
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("response status = %d, want %d", response.Code, http.StatusNoContent)
	}

	serverSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindServer)
	if got, want := serverSpan.Name(), "HTTP /vehicles/{vrn}"; got != want {
		t.Fatalf("server span name = %q, want %q", got, want)
	}
	assertSpanExcludes(t, serverSpan, originalMethod, originalHost)
}

// TestHTTPServerMiddlewareRecordsErrorStatusWithoutSensitiveURL verifies safe telemetry for server failures.
func TestHTTPServerMiddlewareRecordsErrorStatusWithoutSensitiveURL(t *testing.T) {
	_, spanRecorder, options := newTestHTTPTraceProvider(t)
	router := mux.NewRouter()
	router.Use(httpServerMiddleware("vehicle-api", options...))
	router.HandleFunc("/vehicles/{vrn}", func(responseWriter http.ResponseWriter, _ *http.Request) {
		http.Error(responseWriter, "upstream failed", http.StatusServiceUnavailable)
	}).Methods(http.MethodGet)

	request := httptest.NewRequest(
		http.MethodGet,
		"https://service.example.test/vehicles/PV19SZK?token=secret-token",
		nil,
	)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	serverSpan := findEndedSpan(t, spanRecorder, oteltrace.SpanKindServer)
	if serverSpan.Status().Code != codes.Error {
		t.Fatalf("server span status = %v, want Error", serverSpan.Status())
	}
	assertAttributeEquals(t, serverSpan, "http.response.status_code", int64(http.StatusServiceUnavailable))
	assertSpanExcludes(t, serverSpan, "PV19SZK", "secret-token", "service.example.test")
}

// newTestHTTPTraceProvider constructs an in-memory tracer provider and matching HTTP options.
func newTestHTTPTraceProvider(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder, []otelhttp.Option) {
	t.Helper()

	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(spanRecorder),
	)
	t.Cleanup(func() {
		if err := tracerProvider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown tracer provider: %v", err)
		}
	})

	return tracerProvider, spanRecorder, []otelhttp.Option{
		otelhttp.WithTracerProvider(tracerProvider),
		otelhttp.WithPropagators(propagation.TraceContext{}),
	}
}

// responseWithBody constructs a test response with a closable body.
func responseWithBody(statusCode int, request *http.Request) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("response")),
		Request:    request,
	}
}

// findEndedSpan returns the sole completed span of the requested kind.
func findEndedSpan(t *testing.T, recorder *tracetest.SpanRecorder, kind oteltrace.SpanKind) sdktrace.ReadOnlySpan {
	t.Helper()

	var found sdktrace.ReadOnlySpan
	for _, span := range recorder.Ended() {
		if span.SpanKind() != kind {
			continue
		}
		if found != nil {
			t.Fatalf("found more than one ended %s span", kind)
		}
		found = span
	}
	if found == nil {
		t.Fatalf("did not find an ended %s span", kind)
	}
	return found
}

// assertAttributeEquals checks a required span attribute against its expected value.
func assertAttributeEquals(t *testing.T, span sdktrace.ReadOnlySpan, key string, want any) {
	t.Helper()

	for _, item := range span.Attributes() {
		if string(item.Key) != key {
			continue
		}
		got := item.Value.AsInterface()
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("span attribute %s = %v, want %v", key, got, want)
		}
		return
	}
	t.Fatalf("span does not contain attribute %s", key)
}

// assertSpanExcludes checks that span telemetry omits each sensitive value.
func assertSpanExcludes(t *testing.T, span sdktrace.ReadOnlySpan, sensitiveValues ...string) {
	t.Helper()

	var telemetry strings.Builder
	telemetry.WriteString(span.Name())
	telemetry.WriteString(" ")
	telemetry.WriteString(span.Status().Description)
	for _, item := range span.Attributes() {
		fmt.Fprintf(&telemetry, " %s=%v", item.Key, item.Value.AsInterface())
	}
	for _, event := range span.Events() {
		telemetry.WriteString(" ")
		telemetry.WriteString(event.Name)
		for _, item := range event.Attributes {
			fmt.Fprintf(&telemetry, " %s=%v", item.Key, item.Value.AsInterface())
		}
	}

	telemetryText := telemetry.String()
	for _, sensitiveValue := range sensitiveValues {
		if sensitiveValue != "" && strings.Contains(telemetryText, sensitiveValue) {
			t.Fatalf("span telemetry contains sensitive value %q: %s", sensitiveValue, telemetryText)
		}
	}
}

// TestSanitisedURLRemovesEveryRequestTargetComponent verifies complete request-target redaction.
func TestSanitisedURLRemovesEveryRequestTargetComponent(t *testing.T) {
	original, err := urlWithEverySensitiveComponent()
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	sanitised := sanitisedURL(original)
	if got, want := sanitised.String(), "https://example.test/"; got != want {
		t.Fatalf("sanitised URL = %q, want %q", got, want)
	}
	if got := original.String(); got != "https://user:password@example.test/private/PV19SZK?token=secret#private-fragment" {
		t.Fatalf("original URL changed to %q", got)
	}
}

// urlWithEverySensitiveComponent returns a URL containing each request-target component under test.
func urlWithEverySensitiveComponent() (*url.URL, error) {
	return url.Parse("https://user:password@example.test/private/PV19SZK?token=secret#private-fragment")
}
