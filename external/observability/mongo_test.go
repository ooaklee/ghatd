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
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// Real driver connection IDs contain a pool sequence suffix. That sequence
// must remain available for command correlation, but never become a metric label.
func TestMongoConnectionChurnKeepsMetricsBoundedAndSpansDistinct(t *testing.T) {
	for _, address := range []struct{ connection, host string }{
		{"mongo.example:27018", "mongo.example"},
		{"127.0.0.1:27018", "127.0.0.1"},
		{"[2001:db8::1]:27018", "2001:db8::1"},
	} {
		for _, failure := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failure=%t", address.connection, failure), func(t *testing.T) {
				ctx := context.Background()
				reader := sdkmetric.NewManualReader()
				meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
				recorder := tracetest.NewSpanRecorder()
				tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
				t.Cleanup(func() {
					require.NoError(t, meterProvider.Shutdown(ctx))
					require.NoError(t, tracerProvider.Shutdown(ctx))
				})
				monitor := NewMongoCommandMonitor(otelmongo.WithMeterProvider(meterProvider), otelmongo.WithTracerProvider(tracerProvider))
				const connections = 64
				parents := make([]trace.SpanContext, connections)
				for i := range connections {
					parents[i] = trace.NewSpanContext(trace.SpanContextConfig{
						TraceID: trace.TraceID{1, byte(i + 1)}, SpanID: trace.SpanID{1, byte(i + 1)}, TraceFlags: trace.FlagsSampled,
					})
					monitor.Started(trace.ContextWithSpanContext(ctx, parents[i]), &event.CommandStartedEvent{
						DatabaseName: "example", CommandName: "find", RequestID: 42,
						ConnectionID: fmt.Sprintf("%s[-%d]", address.connection, i+1),
					})
				}
				// Overlapping request IDs on different connections must finish their
				// own spans, even when responses arrive in the opposite order.
				for i := connections - 1; i >= 0; i-- {
					finished := event.CommandFinishedEvent{
						DatabaseName: "example", CommandName: "find", RequestID: 42, Duration: time.Millisecond,
						ConnectionID: fmt.Sprintf("%s[-%d]", address.connection, i+1),
					}
					if failure {
						monitor.Failed(ctx, &event.CommandFailedEvent{CommandFinishedEvent: finished, Failure: fmt.Errorf("private query details")})
					} else {
						monitor.Succeeded(ctx, &event.CommandSucceededEvent{CommandFinishedEvent: finished})
					}
				}
				spans := recorder.Ended()
				require.Len(t, spans, connections)
				for i, span := range spans {
					assert.Equal(t, parents[connections-1-i], span.Parent())
					assert.Equal(t, address.host, mongoSpanAttribute(t, span.Attributes(), "network.peer.address").AsString())
					assert.Equal(t, int64(27018), mongoSpanAttribute(t, span.Attributes(), "network.peer.port").AsInt64())
				}
				var exported metricdata.ResourceMetrics
				require.NoError(t, reader.Collect(ctx, &exported))
				var points []metricdata.HistogramDataPoint[float64]
				for _, scope := range exported.ScopeMetrics {
					for _, instrument := range scope.Metrics {
						if instrument.Name == "db.client.operation.duration" {
							points = append(points, instrument.Data.(metricdata.Histogram[float64]).DataPoints...)
						}
					}
				}
				require.Len(t, points, 1, "connection churn must not create new metric streams")
				assert.Equal(t, uint64(connections), points[0].Count)
				assert.Equal(t, address.host, mongoSpanAttribute(t, points[0].Attributes.ToSlice(), "network.peer.address").AsString())
				assert.Equal(t, int64(27018), mongoSpanAttribute(t, points[0].Attributes.ToSlice(), "network.peer.port").AsInt64())
			})
		}
	}
}

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
