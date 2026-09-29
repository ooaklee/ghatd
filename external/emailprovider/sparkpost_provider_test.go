package emailprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sp "github.com/SparkPost/gosparkpost"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// legacyClient implements only the legacy Send method, mirroring existing
// mocks and custom implementations.
type legacyClient struct {
	mu            sync.Mutex
	transmissions []*sp.Transmission
	messageID     string
	err           error
}

func (c *legacyClient) Send(transmission *sp.Transmission) (string, *sp.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transmissions = append(c.transmissions, transmission)
	return c.messageID, nil, c.err
}

// contextRecordingClient implements SendContext and records the contexts it
// receives. Its Send method returns an error so tests can detect wrong paths.
type contextRecordingClient struct {
	mu        sync.Mutex
	contexts  []context.Context
	messageID string
	err       error
}

func (c *contextRecordingClient) Send(t *sp.Transmission) (string, *sp.Response, error) {
	return "", nil, errors.New("emailprovider/test-legacy-send-unexpected")
}

func (c *contextRecordingClient) SendContext(ctx context.Context, t *sp.Transmission) (string, *sp.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.contexts = append(c.contexts, ctx)
	return c.messageID, nil, c.err
}

type providerTestContextKey string

// TestSparkPostProviderPrefersSendContext verifies that clients implementing
// the optional context-aware interface receive the caller's context.
func TestSparkPostProviderPrefersSendContext(t *testing.T) {
	client := &contextRecordingClient{messageID: "message-id"}
	provider := NewSparkPostEmailProvider(client)
	ctx := context.WithValue(context.Background(), providerTestContextKey("transaction"), "transaction-value")

	result, err := provider.Send(ctx, validTestEmail())
	if err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}
	if result.MessageID != "message-id" || result.Provider != "SPARKPOST" || !result.Success || result.Error != nil {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(client.contexts) != 1 {
		t.Fatalf("expected one SendContext call, got %d", len(client.contexts))
	}
	if client.contexts[0] != ctx {
		t.Fatal("SendContext did not receive the caller's context")
	}
	if got := client.contexts[0].Value(providerTestContextKey("transaction")); got != "transaction-value" {
		t.Fatalf("caller context value not preserved: %v", got)
	}
}

// TestSparkPostProviderFallsBackToLegacySend verifies that clients without
// SendContext still work through the legacy Send method.
func TestSparkPostProviderFallsBackToLegacySend(t *testing.T) {
	client := &legacyClient{messageID: "legacy-message-id"}
	provider := NewSparkPostEmailProvider(client)

	result, err := provider.Send(context.Background(), validTestEmail())
	if err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}
	if result.MessageID != "legacy-message-id" || !result.Success {
		t.Fatalf("unexpected result: %#v", result)
	}
	if len(client.transmissions) != 1 {
		t.Fatalf("expected one legacy Send call, got %d", len(client.transmissions))
	}
}

// TestSparkPostProviderPreservesValidationAndErrorContract verifies the public
// error contract for validation failures and send failures, and that no
// transmission is dispatched for invalid emails.
func TestSparkPostProviderPreservesValidationAndErrorContract(t *testing.T) {
	tests := []struct {
		name    string
		email   *Email
		wantErr error
	}{
		{
			name:    "missing recipient",
			email:   &Email{From: "sender@example.com", Subject: "Subject", HTMLBody: "Body"},
			wantErr: ErrEmailProviderMissingRecipient,
		},
		{
			name:    "missing sender",
			email:   &Email{To: "recipient@example.com", Subject: "Subject", HTMLBody: "Body"},
			wantErr: ErrEmailProviderMissingFrom,
		},
		{
			name:    "missing subject",
			email:   &Email{To: "recipient@example.com", From: "sender@example.com", HTMLBody: "Body"},
			wantErr: ErrEmailProviderMissingSubject,
		},
		{
			name:    "missing body",
			email:   &Email{To: "recipient@example.com", From: "sender@example.com", Subject: "Subject"},
			wantErr: ErrEmailProviderMissingBody,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := &contextRecordingClient{}
			provider := NewSparkPostEmailProvider(client)

			result, err := provider.Send(context.Background(), test.email)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
			if result.Provider != "SPARKPOST" || result.Success || !errors.Is(result.Error, test.wantErr) {
				t.Fatalf("unexpected result: %#v", result)
			}
			if len(client.contexts) != 0 {
				t.Fatalf("expected no client calls after validation failure, got %d", len(client.contexts))
			}
		})
	}

	t.Run("send failure maps to provider error", func(t *testing.T) {
		client := &contextRecordingClient{err: errors.New("provider unavailable")}
		provider := NewSparkPostEmailProvider(client)

		result, err := provider.Send(context.Background(), validTestEmail())
		if !errors.Is(err, ErrEmailProviderSendFailed) {
			t.Fatalf("expected error %v, got %v", ErrEmailProviderSendFailed, err)
		}
		if result.Provider != "SPARKPOST" || result.Success || !errors.Is(result.Error, ErrEmailProviderSendFailed) {
			t.Fatalf("unexpected result: %#v", result)
		}
	})
}

// newRecordingTraceSDK returns a tracer provider whose spans record the
// outbound HTTP request contexts, plus a recorder for inspection.
func newRecordingTraceSDK(t *testing.T) (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() { require.NoError(t, tracerProvider.Shutdown(context.Background())) })
	return tracerProvider, recorder
}

// recordedTransport is a real, dialing HTTP transport that records each
// outbound request after it completes.
type recordedTransport struct {
	mu       sync.Mutex
	requests []*http.Request
	inner    http.RoundTripper
}

func (t *recordedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// The inner (instrumented) transport injects trace context headers, so
	// the request is recorded after the inner trip to observe them.
	response, err := t.inner.RoundTrip(request)
	t.mu.Lock()
	t.requests = append(t.requests, request)
	t.mu.Unlock()
	return response, err
}

func (t *recordedTransport) recorded() []*http.Request {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*http.Request{}, t.requests...)
}

// newTestSparkPostClient builds a real gosparkpost client against the test
// server URL using a real dialing transport wrapped with OpenTelemetry
// instrumentation, so cancellation, deadlines and traceparent propagation
// behave as in production.
func newTestSparkPostClient(t *testing.T, server *httptest.Server) (*sp.Client, *recordedTransport) {
	t.Helper()
	transport := &recordedTransport{inner: otelhttp.NewTransport(server.Client().Transport)}

	client, err := NewSparkPostClient(&NewSparkPostClientRequest{
		BaseURL:   server.URL,
		APIKey:    "api-key",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("NewSparkPostClient returned an error: %v", err)
	}
	return client, transport
}

// validTestEmail returns a minimal valid email.
func validTestEmail() *Email {
	return &Email{
		To:       "recipient@example.com",
		From:     "sender@example.com",
		ReplyTo:  "reply@example.com",
		Subject:  "Subject",
		HTMLBody: "<p>HTML body</p>",
		TextBody: "Text body",
	}
}

// TestSparkPostProviderHTTPContextPropagation verifies real HTTP-level
// propagation: the caller's context values reach the outbound request.
func TestSparkPostProviderHTTPContextPropagation(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"id":"message-id"}}`))
	}))
	defer server.Close()

	client, transport := newTestSparkPostClient(t, server)
	provider := NewSparkPostEmailProvider(client)
	ctx := context.WithValue(context.Background(), providerTestContextKey("transaction"), "transaction-value")

	result, err := provider.Send(ctx, validTestEmail())
	if err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}
	if result.MessageID != "message-id" {
		t.Fatalf("unexpected message ID: %q", result.MessageID)
	}
	if len(transport.recorded()) != 1 {
		t.Fatalf("expected one outbound request, got %d", len(transport.recorded()))
	}
	requestContext := transport.recorded()[0].Context()
	if got := requestContext.Value(providerTestContextKey("transaction")); got != "transaction-value" {
		t.Fatalf("outbound request lost the caller context value: %v", got)
	}
}

// TestSparkPostProviderHTTPContextCancellation verifies that cancelling the
// caller's context aborts the real outbound HTTP request.
func TestSparkPostProviderHTTPContextCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"id":"message-id"}}`))
	}))
	defer server.Close()
	defer close(release)

	client, _ := newTestSparkPostClient(t, server)
	provider := NewSparkPostEmailProvider(client)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := provider.Send(ctx, validTestEmail()); err == nil {
		t.Fatal("expected a context cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("cancellation did not abort the request promptly: %v", elapsed)
	}
}

// TestSparkPostProviderHTTPContextDeadline verifies that an expired deadline
// aborts the real outbound HTTP request with the public send-failed error.
func TestSparkPostProviderHTTPContextDeadline(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"id":"message-id"}}`))
	}))
	defer server.Close()
	defer close(release)

	client, _ := newTestSparkPostClient(t, server)
	provider := NewSparkPostEmailProvider(client)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := provider.Send(ctx, validTestEmail()); err == nil {
		t.Fatal("expected a deadline-exceeded send failure")
	}
}

// TestSparkPostProviderHTTPTraceParentPropagation verifies that the trace
// parent of the caller's active span is propagated onto the outbound HTTP
// request as traceparent headers.
func TestSparkPostProviderHTTPTraceParentPropagation(t *testing.T) {
	var receivedTraceparent atomic.Value
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// otelhttp injects headers on a cloned request, so the traceparent is
		// observed server-side rather than on the caller's request value.
		receivedTraceparent.Store(r.Header.Get("traceparent"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":{"id":"message-id"}}`))
	}))
	defer server.Close()

	tracerProvider, recorder := newRecordingTraceSDK(t)
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tracerProvider)
	defer otel.SetTracerProvider(previous)
	previousPropagator := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTextMapPropagator(previousPropagator)

	client, transport := newTestSparkPostClient(t, server)
	provider := NewSparkPostEmailProvider(client)

	tracer := tracerProvider.Tracer("emailprovider/test")
	ctx, span := tracer.Start(context.Background(), "email-send")
	defer span.End()

	if _, err := provider.Send(ctx, validTestEmail()); err != nil {
		t.Fatalf("Send returned an error: %v", err)
	}
	if len(transport.recorded()) != 1 {
		t.Fatalf("expected one outbound request, got %d", len(transport.recorded()))
	}
	traceparent, _ := receivedTraceparent.Load().(string)
	if traceparent == "" {
		t.Fatal("outbound request did not carry a traceparent header")
	}
	children := recorder.Ended()
	require.Len(t, children, 1)
	require.Equal(t, trace.SpanKindClient, children[0].SpanKind())
	require.Equal(t, span.SpanContext().SpanID(), children[0].Parent().SpanID())
	childSpanContext := children[0].SpanContext()
	require.Equal(t, span.SpanContext().TraceID(), childSpanContext.TraceID())
	if !strings.Contains(traceparent, childSpanContext.TraceID().String()) {
		t.Fatalf("traceparent %q does not carry the propagated trace ID %s", traceparent, childSpanContext.TraceID())
	}
}

func TestSparkPostFailureDoesNotLogSDKBodyOrEmailContent(t *testing.T) {
	const private = "private-email-error-canary"
	client := &contextRecordingClient{err: errors.New(private)}
	core, logs := observer.New(zap.InfoLevel)
	ctx := ghatdlogger.TransitWith(context.Background(), zap.New(core))
	email := validTestEmail()
	email.To = private + "@example.com"
	email.Subject, email.HTMLBody, email.TextBody = private, private, private
	result, err := NewSparkPostEmailProvider(client).Send(ctx, email)
	require.ErrorIs(t, err, ErrEmailProviderSendFailed)
	require.Same(t, err, result.Error)
	require.NotContains(t, err.Error(), private)
	require.Len(t, logs.FilterMessage("sparkpost-email-send-failed").All(), 1)
	for _, entry := range logs.All() {
		encoded, encodeErr := json.Marshal(entry.ContextMap())
		require.NoError(t, encodeErr)
		require.NotContains(t, string(encoded), private)
	}
}

// TestSparkPostProviderConcurrentSendsKeepContextsIsolated verifies that
// concurrent sends cannot exchange their request contexts.
func TestSparkPostProviderConcurrentSendsKeepContextsIsolated(t *testing.T) {
	client := &contextRecordingClient{messageID: "message-id"}
	provider := NewSparkPostEmailProvider(client)
	email := validTestEmail()

	const sends = 20
	var waitGroup sync.WaitGroup
	waitGroup.Add(sends)
	for i := range sends {
		go func() {
			defer waitGroup.Done()
			ctx := context.WithValue(context.Background(), providerTestContextKey("send"), i)
			if _, err := provider.Send(ctx, email); err != nil {
				t.Errorf("Send returned an error: %v", err)
			}
		}()
	}
	waitGroup.Wait()

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.contexts) != sends {
		t.Fatalf("expected %d contexts, got %d", sends, len(client.contexts))
	}
	seen := make(map[int]bool, sends)
	for _, ctx := range client.contexts {
		value, ok := ctx.Value(providerTestContextKey("send")).(int)
		if !ok {
			t.Fatalf("context did not contain its send identifier: %v", ctx.Value(providerTestContextKey("send")))
		}
		seen[value] = true
	}
	if len(seen) != sends {
		t.Fatalf("expected %d unique contexts, got %d", sends, len(seen))
	}
}
