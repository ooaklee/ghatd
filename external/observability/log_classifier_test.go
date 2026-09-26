package observability

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// TestLogClassifierMatchesOperation checks that wrapped domain errors have the
// same stable code in the operation span and its natively correlated OTLP log.
func TestLogClassifierMatchesOperation(t *testing.T) {
	sentinel := errors.New("private-sentinel-message")
	classifier, err := NewErrorClassifier(ErrorRule{Err: sentinel, Code: "APP-001", Outcome: OutcomeRejected})
	require.NoError(t, err)
	spans := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	meters := metric.NewMeterProvider()
	t.Cleanup(func() {
		require.NoError(t, provider.Shutdown(context.Background()))
		require.NoError(t, meters.Shutdown(context.Background()))
	})
	operations, err := NewOperations(OperationConfig{TracerProvider: provider, MeterProvider: meters, Errors: classifier})
	require.NoError(t, err)
	logger, local, exporter := logPolicyTestLogger(t, WithLogErrorClassifier(classifier))
	ctx, parent := provider.Tracer("test").Start(context.Background(), "parent")
	ctx = ghatdlogger.TransitWith(ctx, WithTraceContext(ctx, logger))
	ctx, operation := operations.Start(ctx, "validate-request")
	result := fmt.Errorf("private-wrapper-message: %w", sentinel)
	ghatdlogger.AcquireFrom(ctx).Error("operation-rejected", zap.Error(result),
		zap.String("error.type", "private-direct-code"),
		zap.String("error.category", "private-direct-category"))
	operation.End(result)
	parent.End()
	require.Len(t, spans.Ended(), 2)
	child := spans.Ended()[0]
	assert.Equal(t, codes.Unset, child.Status().Code)
	attributes := make(map[string]string)
	for _, field := range child.Attributes() {
		attributes[string(field.Key)] = field.Value.AsString()
	}
	assert.Equal(t, "APP-001", attributes["error.type"])
	assert.Equal(t, "rejected", attributes["error.category"])
	records := exporter.Records()
	require.Len(t, records, 1)
	assert.Equal(t, child.SpanContext().TraceID(), records[0].TraceID())
	assert.Equal(t, child.SpanContext().SpanID(), records[0].SpanID())
	assert.Equal(t, trace.SpanContextFromContext(ctx), child.SpanContext())
	remote := recordAttributes(records[0])
	assert.Equal(t, attributes["error.type"], remote["error.type"].AsString())
	assert.Equal(t, attributes["error.category"], remote["error.category"].AsString())
	assert.NotContains(t, fmt.Sprint(remote), "private-")
	assert.Equal(t, result, local.All()[0].Context[len(local.All()[0].Context)-3].Interface,
		"the local Zap error field remains the original error")
}

func TestLogClassifierPrecedenceAndIsolation(t *testing.T) {
	sentinel := errors.New("private-domain-value")
	first, err := NewErrorClassifier(ErrorRule{Err: sentinel, Code: "APP-001", Outcome: OutcomeRejected})
	require.NoError(t, err)
	second, err := NewErrorClassifier(ErrorRule{Err: sentinel, Code: "APP-002", Outcome: OutcomeError})
	require.NoError(t, err)
	cases := []struct {
		name           string
		err            error
		options        []LogOption
		code, category string
	}{
		{"wrapped", fmt.Errorf("private-wrap: %w", sentinel), []LogOption{WithLogErrorClassifier(first)}, "APP-001", "rejected"},
		{"joined", errors.Join(errors.New("private-extra"), sentinel), []LogOption{WithLogErrorClassifier(first)}, "APP-001", "rejected"},
		{"cancel wins", errors.Join(sentinel, context.Canceled), []LogOption{WithLogErrorClassifier(first)}, "cancelled", "cancelled"},
		{"deadline wins", errors.Join(sentinel, context.Canceled, context.DeadlineExceeded), []LogOption{WithLogErrorClassifier(first)}, "timeout", "timeout"},
		{"unknown", errors.New("private-unknown"), []LogOption{WithLogErrorClassifier(first)}, "internal", "error"},
		{"last wins", sentinel, []LogOption{WithLogErrorClassifier(first), WithLogErrorClassifier(second)}, "APP-002", "error"},
		{"nil defaults", sentinel, []LogOption{WithLogErrorClassifier(first), WithLogErrorClassifier(nil)}, "internal", "error"},
		{"independent", sentinel, []LogOption{WithLogErrorClassifier(first)}, "APP-001", "rejected"},
		{"legacy", sentinel, nil, "*errors.errorString", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logger, _, exporter := logPolicyTestLogger(t, tc.options...)
			logger.Error("operation-failed", zap.Error(tc.err))
			logger.With(zap.NamedError("cause", tc.err)).Error("bound-operation-failed")
			records := exporter.Records()
			require.Len(t, records, 2)
			for _, record := range records {
				attributes := recordAttributes(record)
				assert.Equal(t, tc.code, attributes["error.type"].AsString())
				if tc.category == "" {
					assert.NotContains(t, attributes, "error.category")
				} else {
					assert.Equal(t, tc.category, attributes["error.category"].AsString())
				}
				assert.NotContains(t, fmt.Sprint(attributes), "private-")
			}
		})
	}
}

type classifierUnprintableError struct{}

func (classifierUnprintableError) Error() string { panic("Error must not be called") }

func TestLogClassifierNeverFormatsError(t *testing.T) {
	policy := newLogFieldPolicy(WithLogErrorClassifier(nil))
	require.NotPanics(t, func() {
		fields := policy.sanitise([]zap.Field{zap.Error(classifierUnprintableError{})})
		require.Len(t, fields, 2)
		assert.Equal(t, "internal", fields[0].String)
		assert.Equal(t, "error", fields[1].String)
	})
}
