package observability

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// TestNewMongoCommandMonitorAlwaysDisablesCommandText verifies that callers cannot enable sensitive command text.
func TestNewMongoCommandMonitorAlwaysDisablesCommandText(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	// Supplying false verifies that the helper's privacy setting cannot be
	// overridden by a caller-provided option.
	monitor := NewMongoCommandMonitor(
		otelmongo.WithTracerProvider(tracerProvider),
		otelmongo.WithCommandAttributeDisabled(false),
		otelmongo.WithSpanNameFormatter(func(*event.CommandStartedEvent) string {
			return "PRIVATE-VRN-123"
		}),
	)

	const secret = "PRIVATE-VRN-123"
	command, err := bson.Marshal(bson.D{
		{Key: "find", Value: "vehicles"},
		{Key: "filter", Value: bson.D{{Key: "vrn", Value: secret}}},
	})
	require.NoError(t, err)

	ctx := context.Background()
	monitor.Started(ctx, &event.CommandStartedEvent{
		Command:      command,
		DatabaseName: "vehicle-tax",
		CommandName:  "find",
		RequestID:    42,
		ConnectionID: "mongo.internal:27017",
	})
	monitor.Succeeded(ctx, &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration:     5 * time.Millisecond,
			CommandName:  "find",
			DatabaseName: "vehicle-tax",
			RequestID:    42,
			ConnectionID: "mongo.internal:27017",
		},
	})

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	assert.Equal(t, trace.SpanKindClient, span.SpanKind())
	assert.Equal(t, "mongodb.find", span.Name())
	assert.Equal(t, "mongodb", mongoSpanAttribute(t, span.Attributes(), "db.system.name").AsString())
	assert.Equal(t, "find", mongoSpanAttribute(t, span.Attributes(), "db.operation.name").AsString())
	assert.Equal(t, "mongo.internal", mongoSpanAttribute(t, span.Attributes(), "network.peer.address").AsString())

	for _, spanAttribute := range span.Attributes() {
		assert.NotEqual(t, attribute.Key("db.query.text"), spanAttribute.Key)
		assert.NotContains(t, fmt.Sprint(spanAttribute.Value.AsInterface()), secret)
	}
	assert.NotContains(t, span.Name(), secret)
	assert.NotContains(t, span.Status().Description, secret)
}

// TestNewMongoCommandMonitorSanitisesFailureDescription verifies that failure spans omit driver error details.
func TestNewMongoCommandMonitorSanitisesFailureDescription(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	t.Cleanup(func() {
		require.NoError(t, tracerProvider.Shutdown(context.Background()))
	})

	monitor := NewMongoCommandMonitor(otelmongo.WithTracerProvider(tracerProvider))
	ctx := context.Background()
	monitor.Started(ctx, &event.CommandStartedEvent{
		DatabaseName: "vehicle-tax",
		CommandName:  "find",
		RequestID:    43,
		ConnectionID: "mongo.internal:27017",
	})
	monitor.Failed(ctx, &event.CommandFailedEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			CommandName:  "find",
			DatabaseName: "vehicle-tax",
			RequestID:    43,
			ConnectionID: "mongo.internal:27017",
		},
		Failure: fmt.Errorf("query for registration PRIVATE-VRN-123 failed"),
	})

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)
	assert.Equal(t, "MongoDB command failed", spans[0].Status().Description)
	assert.NotContains(t, spans[0].Status().Description, "PRIVATE-VRN-123")
}

// mongoSpanAttribute returns a required MongoDB span attribute.
func mongoSpanAttribute(t *testing.T, attributes []attribute.KeyValue, key attribute.Key) attribute.Value {
	t.Helper()
	for _, spanAttribute := range attributes {
		if spanAttribute.Key == key {
			return spanAttribute.Value
		}
	}

	require.FailNow(t, "span attribute not found", strings.TrimSpace(string(key)))
	return attribute.Value{}
}
