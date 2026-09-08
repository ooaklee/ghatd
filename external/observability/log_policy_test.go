package observability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// TestLogPolicyCanonicalFields exercises both bound and per-entry fields through
// the real OpenTelemetry bridge and SDK, including unchanged local field names.
func TestLogPolicyCanonicalFields(t *testing.T) {
	tests := []struct {
		name  string
		field zap.Field
		key   string
		value otellog.Value
	}{
		{"framework source", zap.String("source", "ghatd"), "source", otellog.StringValue("ghatd")},
		{"source alias", zap.String("Source", "ghatd"), "source", otellog.StringValue("ghatd")},
		{"secret source", zap.String("source", "https://user:source-secret@example.test"), "", otellog.Value{}},
		{"unregistered source", zap.String("source", "unregistered-source"), "", otellog.Value{}},
		{"source wrong type", zap.Int("source", 1), "", otellog.Value{}},
		{"status hyphen", zap.Int("status-code", 401), "status", otellog.Int64Value(401)},
		{"status underscore", zap.Int64("status_code", 502), "status", otellog.Int64Value(502)},
		{"status camel case", zap.Int32("statusCode", 200), "status", otellog.Int64Value(200)},
		{"status HTTP alias", zap.Int16("http.status_code", 599), "status", otellog.Int64Value(599)},
		{"status semantic alias", zap.Uint16("http.response.status_code", 201), "status", otellog.Int64Value(201)},
		{"status lower bound", zap.Int8("status", 100), "status", otellog.Int64Value(100)},
		{"status upper bound", zap.Uint64("status", 999), "status", otellog.Int64Value(999)},
		{"status uint32", zap.Uint32("status", 204), "status", otellog.Int64Value(204)},
		{"status uint8", zap.Uint8("status", 200), "status", otellog.Int64Value(200)},
		{"status uintptr", zap.Uintptr("status-code", 200), "status", otellog.Int64Value(200)},
		{"status too low", zap.Int("status", 99), "", otellog.Value{}},
		{"status too high", zap.Int("status", 1000), "", otellog.Value{}},
		{"status negative", zap.Int("status", -1), "", otellog.Value{}},
		{"status uint overflow", zap.Uint64("status", math.MaxUint64), "", otellog.Value{}},
		{"status float", zap.Float64("status-code", 401), "", otellog.Value{}},
		{"status bool", zap.Bool("status-code", true), "", otellog.Value{}},
		{"status duration", zap.Duration("status-code", 401*time.Nanosecond), "", otellog.Value{}},
		{"status string", zap.String("status-code", "401"), "", otellog.Value{}},
		{"status string canary", zap.String("http.response.status_code", "401 status-secret"), "", otellog.Value{}},
		{"legacy domain status", zap.String("status", "CUSTOM_PROVISIONED_STATE"), "status", otellog.StringValue("CUSTOM_PROVISIONED_STATE")},
		{"method", zap.String("method", "GET"), "method", otellog.StringValue("GET")},
		{"method lowercase alias", zap.String("http.request.method", "post"), "method", otellog.StringValue("POST")},
		{"method canary", zap.String("METHOD", "private-method-secret"), "method", otellog.StringValue("OTHER")},
		{"method wrong type", zap.Int("method", 5), "", otellog.Value{}},
		{"provider alias", zap.String("Provider", "stripe"), "provider", otellog.StringValue("stripe")},
		{"provider canary", zap.String("provider", "https://provider-secret@example.test"), "provider", otellog.StringValue("other")},
		{"provider identifier canary", zap.String("provider", "provider-secret"), "provider", otellog.StringValue("other")},
		{"provider wrong type", zap.Int("provider", 5), "", otellog.Value{}},
		{"panic alias", zap.String("error_type", "panic"), "error.type", otellog.StringValue("panic")},
		{"error type canary", zap.String("error.type", "error-type-secret"), "", otellog.Value{}},
		{"error type shaped canary", zap.String("error.type", "*account.secretType"), "", otellog.Value{}},
		{"error type wrong type", zap.Int("error.type", 4), "", otellog.Value{}},
		{"existing package spelling", zap.String("ghatd-package", "external/example"), "ghatd-package", otellog.StringValue("external/example")},
		{"existing correlation spelling", zap.String("correlation-id", "static-test-correlation"), "correlation-id", otellog.StringValue("static-test-correlation")},
		{"scalar duration", zap.Duration("duration", time.Millisecond), "duration", otellog.Int64Value(int64(time.Millisecond))},
		{"complex nested value", zap.Complex128("count", complex(1, 2)), "", otellog.Value{}},
		{"opaque value", zap.Reflect("operation", map[string]string{"secret": "nested-secret"}), "", otellog.Value{}},
	}
	for _, provider := range []string{"kofi", "lemonsqueezy", "stripe", "SPARKPOST", "LOCAL"} {
		tests = append(tests, struct {
			name  string
			field zap.Field
			key   string
			value otellog.Value
		}{"built-in " + provider, zap.String("provider", provider), "provider", otellog.StringValue(provider)})
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, local, exporter := logPolicyTestLogger(t)
			logger.Info("entry", test.field)
			logger.With(test.field).Info("bound")
			records := exporter.Records()
			require.Len(t, records, 2)
			for _, record := range records {
				attributes := recordAttributes(record)
				if test.key == "" {
					assert.Empty(t, attributes)
				} else {
					assert.Equal(t, map[string]otellog.Value{test.key: test.value}, attributes)
				}
			}
			for _, entry := range local.All() {
				require.Len(t, entry.Context, 1)
				assert.Equal(t, test.field, entry.Context[0], "local field remains unchanged")
			}
		})
	}
}

// TestLogPolicyOptionsSnapshotAndIsolation verifies finite per-logger membership,
// case-sensitive names, replacement semantics and independence from caller data.
func TestLogPolicyOptionsSnapshotAndIsolation(t *testing.T) {
	names := []string{"custom-provider"}
	first, err := WithLogFieldValues("provider", names...)
	require.NoError(t, err)
	names[0] = "mutated-private-value"
	second, err := WithLogFieldValues("provider", "replacement-provider")
	require.NoError(t, err)
	clear, err := WithLogFieldValues("provider")
	require.NoError(t, err)
	source, err := WithLogFieldValues("source", "application")
	require.NoError(t, err)

	tests := []struct {
		name    string
		options []LogOption
		custom  string
		replace string
		source  bool
	}{
		{"default", nil, "other", "other", false},
		{"snapshot", []LogOption{first, source, nil}, "custom-provider", "other", true},
		{"last wins", []LogOption{first, source, second}, "other", "replacement-provider", true},
		{"cleared", []LogOption{first, source, clear}, "other", "other", true},
		{"option reused", []LogOption{first}, "custom-provider", "other", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			logger, _, exporter := logPolicyTestLogger(t, test.options...)
			for _, provider := range []string{"custom-provider", "replacement-provider", "mutated-private-value", "Custom-Provider", "kofi"} {
				logger.With(zap.String("provider", provider)).Info("provider configured")
			}
			logger.Info("source configured", zap.String("source", "application"))
			logger.Info("framework source", zap.String("source", "ghatd"))
			records := exporter.Records()
			require.Len(t, records, 7)
			for index, want := range []string{test.custom, test.replace, "other", "other", "kofi"} {
				assert.Equal(t, want, recordAttributes(records[index])["provider"].AsString())
			}
			if test.source {
				assert.Equal(t, "application", recordAttributes(records[5])["source"].AsString())
			} else {
				assert.Empty(t, recordAttributes(records[5]))
			}
			assert.Equal(t, "ghatd", recordAttributes(records[6])["source"].AsString())
		})
	}
}

// TestLogPolicyUnknownProviderCardinality verifies that syntactically plausible
// runtime names never register themselves or become distinct remote values.
func TestLogPolicyUnknownProviderCardinality(t *testing.T) {
	logger, _, exporter := logPolicyTestLogger(t)
	for index := 0; index < 100; index++ {
		logger.Info("provider unavailable", zap.String("provider", fmt.Sprintf("private-provider-%d", index)))
	}
	records := exporter.Records()
	require.Len(t, records, 100)
	for _, record := range records {
		assert.Equal(t, "other", recordAttributes(record)["provider"].AsString())
	}
}

// TestWithLogFieldValuesValidatesConfiguration verifies that invalid or excessive
// configuration fails before constructing a logger, without repeating its data.
func TestWithLogFieldValuesValidatesConfiguration(t *testing.T) {
	for _, field := range []string{"", "status", "error.type", "source-secret"} {
		option, err := WithLogFieldValues(field, "valid")
		require.Error(t, err)
		assert.Nil(t, option)
		assert.NotContains(t, err.Error(), "source-secret")
	}
	for _, value := range []string{"", "9name", "with space", "with\nnewline", "éname", "user@example.test", "https://secret.example.test", strings.Repeat("a", 65)} {
		option, err := WithLogFieldValues("source", value)
		require.Error(t, err)
		assert.Nil(t, option)
		if value != "" {
			assert.NotContains(t, err.Error(), value)
		}
	}
	names := make([]string, 32)
	for index := range names {
		names[index] = fmt.Sprintf("provider-%d", index)
	}
	_, err := WithLogFieldValues("provider", names...)
	require.NoError(t, err)
	_, err = WithLogFieldValues("provider", append(names, names...)...)
	require.NoError(t, err, "limit is distinct names, not duplicate entries")
	_, err = WithLogFieldValues("provider", append(names, "one-more-provider")...)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "one-more-provider")
	_, err = WithLogFieldValues("source", strings.Repeat("a", 64), "Example.Source_2-v1")
	require.NoError(t, err)
}

// TestLogPolicyCanonicalDuplicateFields verifies that entry fields override bound
// canonical aliases without exporting duplicate keys or changing local spelling.
func TestLogPolicyCanonicalDuplicateFields(t *testing.T) {
	logger, local, exporter := logPolicyTestLogger(t)
	logger.With(zap.Int("status-code", 200)).With(zap.Int("http.status_code", 201)).Info("request completed",
		zap.Int("status_code", 202), zap.Int("status", 503),
	)
	records := exporter.Records()
	require.Len(t, records, 1)
	assert.Equal(t, map[string]otellog.Value{"status": otellog.Int64Value(503)}, recordAttributes(records[0]))
	assert.Equal(t, 1, records[0].AttributesLen())
	assert.Equal(t, map[string]interface{}{"status-code": int64(200), "http.status_code": int64(201), "status_code": int64(202), "status": int64(503)}, local.All()[0].ContextMap())
}

// TestLogPolicyDropsNamespaceControl verifies that namespace fields cannot turn
// accepted scalar fields into nested remote objects, even when bound to a logger.
func TestLogPolicyDropsNamespaceControl(t *testing.T) {
	logger, _, exporter := logPolicyTestLogger(t)
	logger.Info("entry", zap.Namespace("operation"), zap.Int("status-code", 401))
	logger.With(zap.Namespace("operation")).Info("bound", zap.Int("status-code", 401))
	records := exporter.Records()
	require.Len(t, records, 2)
	for _, record := range records {
		assert.Equal(t, map[string]otellog.Value{"status": otellog.Int64Value(401)}, recordAttributes(record))
	}
}

type logPolicyError struct {
	message string
	calls   *int
}

// Error returns the original local error and counts serialization calls.
func (err *logPolicyError) Error() string {
	*err.calls++
	return err.message
}

// ErrorType must never be invoked to obtain an unbounded classification.
func (*logPolicyError) ErrorType() string {
	panic("custom error classification method must not run")
}

type logPolicyGenericError[T any] struct{ error }

// TestLogPolicyErrorTypesKeepLocalErrors verifies that only the local encoder
// evaluates errors, and that reflected anonymous fields and generic type
// arguments cannot leak into exported error classifications.
func TestLogPolicyErrorTypesKeepLocalErrors(t *testing.T) {
	exporter := &recordingLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	var local bytes.Buffer
	base := zap.New(zapcore.NewCore(zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()), zapcore.AddSync(&local), zapcore.InfoLevel))
	logger := TeeLogger(base, provider)
	calls := 0
	named := &logPolicyError{message: "raw-error-secret", calls: &calls}
	anonymous := struct {
		error `json:"anonymous-tag-secret"`
	}{errors.New("anonymous-error-secret")}
	generic := &logPolicyGenericError[struct {
		Value string `json:"generic-tag-secret"`
	}]{error: errors.New("generic-error-secret")}

	logger.Error("named failure", zap.Error(named))
	logger.Error("anonymous failure", zap.Error(anonymous))
	logger.With(zap.NamedError("dependency-error", generic)).Error("generic failure")
	logger.Error("wrapped failure", zap.Error(fmt.Errorf("wrapped-error-secret: %w", named)))
	require.Equal(t, 2, calls, "one local serialization and one explicit fmt.Errorf construction")
	assert.Contains(t, local.String(), "raw-error-secret")
	assert.Contains(t, local.String(), "anonymous-error-secret")
	assert.Contains(t, local.String(), "generic-error-secret")

	records := exporter.Records()
	require.Len(t, records, 4)
	for index, want := range []string{"*observability.logPolicyError", "error", "*observability.logPolicyGenericError", "*fmt.wrapError"} {
		assert.Equal(t, map[string]otellog.Value{"error.type": otellog.StringValue(want)}, recordAttributes(records[index]))
		assert.NotContains(t, fmt.Sprint(recordAttributes(records[index])), "secret")
	}
}

// logPolicyTestLogger creates a real OTLP log pipeline and observable local core.
func logPolicyTestLogger(t *testing.T, options ...LogOption) (*zap.Logger, *observer.ObservedLogs, *recordingLogExporter) {
	t.Helper()
	exporter := &recordingLogExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exporter)))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	core, local := observer.New(zapcore.InfoLevel)
	return TeeLogger(zap.New(core), provider, options...), local, exporter
}
