package observability

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestTeeLoggerSanitisesOnlyOpenTelemetryAndCorrelatesTrace verifies safe export whilst preserving local fields.
func TestTeeLoggerSanitisesOnlyOpenTelemetryAndCorrelatesTrace(t *testing.T) {
	exporter := &recordingLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	stdoutCore, stdout := observer.New(zapcore.InfoLevel)
	logger := TeeLogger(zap.New(stdoutCore), provider)

	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	spanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanContext)
	ctx = context.WithValue(ctx, sensitiveTestContextKey{}, "context-secret")

	secretErr := errors.New("database failed for AB12CDE with token=raw-secret")
	WithTraceContext(ctx, logger).Error("vehicle lookup failed",
		zap.String("operation", "vehicle-lookup"),
		zap.String("outcome", "failure"),
		zap.Int("status", 502),
		zap.String("api_token", "raw-secret"),
		zap.String("password", "raw-password"),
		zap.String("Authorization", "Bearer raw-secret"),
		zap.String("session-cookie", "raw-cookie"),
		zap.String("VRN", "AB12CDE"),
		zap.String("registration-number", "AB12CDE"),
		zap.String("drivingLicence", "raw-licence"),
		zap.String("result_id", "result-123"),
		zap.String("nano-id", "nano-123"),
		zap.String("customerId", "customer-123"),
		zap.String("subscription.id", "subscription-123"),
		zap.String("orderItemID", "item-123"),
		zap.String("price_id", "price-123"),
		zap.String("provider-user-id", "provider-user-123"),
		zap.String("groupId", "group-123"),
		zap.String("organisation_id", "org-123"),
		zap.String("organizationId", "org-456"),
		zap.String("user-id", "user-123"),
		zap.String("emailAddress", "driver@example.com"),
		zap.String("request_uri", "/vehicle/AB12CDE?token=raw-secret"),
		zap.String("query", "token=raw-secret"),
		zap.String("body", "raw-body"),
		zap.String("request", "raw-request"),
		zap.String("response", "raw-response"),
		zap.String("payload", "raw-payload"),
		zap.String("signature", "raw-signature"),
		zap.String("client-ip", "192.0.2.1"),
		zap.String("X-Forwarded-For", "192.0.2.2"),
		zap.String("phone", "+44 7700 900123"),
		zap.String("address", "1 Sensitive Street"),
		zap.String("account_id", "account-123"),
		zap.String("session_id", "session-123"),
		zap.String("vehicle_id", "vehicle-123"),
		zap.String("value", "raw-secret"),
		zap.String("source", "https://user:secret@example.test/repository?token=raw-secret"),
		zap.String("context", "sensitive-context"),
		zap.Any("details", map[string]string{"password": "nested-secret"}),
		zap.Error(secretErr),
	)

	stdoutEntries := stdout.All()
	require.Len(t, stdoutEntries, 1)
	stdoutFields := stdoutEntries[0].ContextMap()
	assert.Equal(t, "raw-secret", stdoutFields["api_token"])
	assert.Equal(t, "AB12CDE", stdoutFields["VRN"])
	assert.Equal(t, "https://user:secret@example.test/repository?token=raw-secret", stdoutFields["source"])
	assert.Equal(t, secretErr.Error(), stdoutFields["error"])
	assert.Equal(t, "sensitive-context", stdoutFields[logContextKey])

	records := exporter.Records()
	require.Len(t, records, 1)
	record := records[0]
	assert.Equal(t, traceID, record.TraceID())
	assert.Equal(t, spanID, record.SpanID())

	attributes := recordAttributes(record)
	assert.Equal(t, "vehicle-lookup", attributes["operation"].AsString())
	assert.Equal(t, "failure", attributes["outcome"].AsString())
	assert.EqualValues(t, 502, attributes["status"].AsInt64())
	assert.Equal(t, traceID.String(), attributes[TraceIDKey].AsString())
	assert.Equal(t, spanID.String(), attributes[SpanIDKey].AsString())
	assert.Equal(t, "*errors.errorString", attributes["error.type"].AsString())

	for _, key := range []string{
		"api_token", "password", "Authorization", "session-cookie", "VRN",
		"registration-number", "drivingLicence", "result_id", "nano-id", "customerId",
		"subscription.id", "orderItemID", "price_id", "provider-user-id", "groupId",
		"organisation_id", "organizationId", "user-id", "emailAddress",
		"request_uri", "query", "body", "request", "response", "payload",
		"signature", "client-ip", "X-Forwarded-For", "details", "error",
		"phone", "address", "account_id", "session_id", "vehicle_id", "value", "source", "context",
	} {
		assert.NotContains(t, attributes, key)
	}
	assert.NotContains(t, record.Body().AsString(), "raw-secret")
	assert.NotContains(t, record.Body().AsString(), "context-secret")
}

// TestTeeLoggerPreservesBaseSamplingAndHooks verifies remote logs mirror local acceptance decisions.
func TestTeeLoggerPreservesBaseSamplingAndHooks(t *testing.T) {
	exporter := &recordingLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	stdoutCore, stdout := observer.New(zapcore.InfoLevel)
	hookCalls := 0
	baseCore := zapcore.RegisterHooks(
		zapcore.NewSamplerWithOptions(stdoutCore, time.Hour, 1, 0),
		func(zapcore.Entry) error {
			hookCalls++
			return nil
		},
	)
	logger := TeeLogger(zap.New(baseCore), provider)

	logger.Info("sampled message", zap.String("operation", "first"))
	logger.Info("sampled message", zap.String("operation", "second"))

	require.Len(t, stdout.All(), 1)
	assert.Equal(t, 1, hookCalls)
	records := exporter.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "first", recordAttributes(records[0])["operation"].AsString())
}

// TestTeeLoggerRespectsBaseLevel verifies that the telemetry core honours the configured log level.
func TestTeeLoggerRespectsBaseLevel(t *testing.T) {
	exporter := &recordingLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })

	stdoutCore, stdout := observer.New(zapcore.WarnLevel)
	logger := TeeLogger(zap.New(stdoutCore), provider)
	logger.Info("disabled")
	logger.Warn("enabled", zap.String("outcome", "retry"))

	stdoutEntries := stdout.All()
	require.Len(t, stdoutEntries, 1)
	assert.Equal(t, "enabled", stdoutEntries[0].Message)
	records := exporter.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "enabled", records[0].Body().AsString())
	assert.Equal(t, "retry", recordAttributes(records[0])["outcome"].AsString())
}

type sensitiveTestContextKey struct{}

type recordingLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

// Export records cloned log entries for later assertions.
func (exporter *recordingLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()

	for index := range records {
		exporter.records = append(exporter.records, records[index].Clone())
	}
	return nil
}

// Shutdown completes without external resources to release.
func (*recordingLogExporter) Shutdown(context.Context) error {
	return nil
}

// ForceFlush completes immediately because exports are synchronous.
func (*recordingLogExporter) ForceFlush(context.Context) error {
	return nil
}

// Records returns a cloned snapshot of exported log records.
func (exporter *recordingLogExporter) Records() []sdklog.Record {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()

	records := make([]sdklog.Record, len(exporter.records))
	for index := range exporter.records {
		records[index] = exporter.records[index].Clone()
	}
	return records
}

// recordAttributes indexes a log record's attributes by key.
func recordAttributes(record sdklog.Record) map[string]otellog.Value {
	attributes := make(map[string]otellog.Value, record.AttributesLen())
	record.WalkAttributes(func(attribute otellog.KeyValue) bool {
		attributes[attribute.Key] = attribute.Value
		return true
	})
	return attributes
}
