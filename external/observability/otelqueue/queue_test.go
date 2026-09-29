package otelqueue

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"sync"
	"testing"
)

func testQueueObserver(t *testing.T) (*Observer, *tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()); _ = mp.Shutdown(context.Background()) })
	q, err := New(Config{TracerProvider: tp, MeterProvider: mp, Operations: []string{"queue.first.consume", "queue.second.consume"}})
	require.NoError(t, err)
	return q, recorder, reader
}

func TestQueueJobPreservesCorrelationAndRecordsSettlementOnce(t *testing.T) {
	q, recorder, reader := testQueueObserver(t)
	core, logs := observer.New(zapcore.InfoLevel)
	root := logger.TransitWith(context.Background(), zap.New(core))
	parent := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	root = trace.ContextWithSpanContext(root, parent)
	job, ctx := q.Begin(root, "queue.first.consume")
	logger.AcquireFrom(ctx).Info("job completed")
	job.Settle(OutcomeAcked, nil)
	job.Settle(OutcomeRejected, errors.New("must be ignored"))
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	span := spans[0]
	require.False(t, span.Parent().IsValid(), "unversioned queue payloads have no parent carrier")
	require.Equal(t, trace.SpanKindConsumer, span.SpanKind())
	require.Equal(t, "queue.first.consume", span.Name())
	require.Equal(t, "acked", attributeValue(span.Attributes(), "queue.outcome"))
	record := logs.All()[0].ContextMap()
	require.Equal(t, span.SpanContext().TraceID().String(), record["trace_id"])
	require.Equal(t, span.SpanContext().SpanID().String(), record["span_id"])
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	var count int64
	for _, scope := range data.ScopeMetrics {
		for _, metric := range scope.Metrics {
			if sum, ok := metric.Data.(metricdata.Sum[int64]); ok {
				for _, point := range sum.DataPoints {
					count += point.Value
				}
			}
		}
	}
	require.EqualValues(t, 1, count)
}

func TestQueueNamesAndFailuresDoNotLeakPayloads(t *testing.T) {
	q, recorder, _ := testQueueObserver(t)
	secret := "payload-and-user-secret"
	job, _ := q.Begin(context.Background(), secret)
	job.Settle(secret, errors.New(secret))
	span := recorder.Ended()[0]
	require.Equal(t, "queue.other.consume", span.Name())
	require.Equal(t, "unknown", attributeValue(span.Attributes(), "queue.outcome"))
	require.Equal(t, codes.Error, span.Status().Code)
	require.Equal(t, "queue job failed", span.Status().Description)
	require.Empty(t, span.Events())
	for _, attr := range span.Attributes() {
		require.NotContains(t, attr.Value.AsString(), secret)
	}
}

func TestEachQueueObserverUsesItsOwnProvider(t *testing.T) {
	first, firstRecorder, _ := testQueueObserver(t)
	second, secondRecorder, _ := testQueueObserver(t)
	job, _ := first.Begin(context.Background(), "queue.first.consume")
	job.Settle(OutcomeAcked, nil)
	job, _ = second.Begin(context.Background(), "queue.second.consume")
	job.Settle(OutcomeRequeued, nil)
	require.Len(t, firstRecorder.Ended(), 1)
	require.Len(t, secondRecorder.Ended(), 1)
	require.Equal(t, codes.Unset, secondRecorder.Ended()[0].Status().Code)
}

func attributeValue(attrs []attribute.KeyValue, key string) string {
	for _, attr := range attrs {
		if string(attr.Key) == key {
			return attr.Value.AsString()
		}
	}
	return ""
}

func TestConcurrentSettlementRecordsOnlyOneOutcome(t *testing.T) {
	q, recorder, reader := testQueueObserver(t)
	job, _ := q.Begin(context.Background(), "queue.first.consume")
	var group sync.WaitGroup
	for range 30 {
		group.Go(func() { job.Settle(OutcomeAcked, nil) })
	}
	group.Wait()
	require.Len(t, recorder.Ended(), 1)
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	for _, item := range data.ScopeMetrics[0].Metrics {
		switch values := item.Data.(type) {
		case metricdata.Sum[int64]:
			require.Len(t, values.DataPoints, 1)
			require.EqualValues(t, 1, values.DataPoints[0].Value)
		case metricdata.Histogram[float64]:
			require.Len(t, values.DataPoints, 1)
			require.EqualValues(t, 1, values.DataPoints[0].Count)
		}
	}
}

func TestConfiguredVocabularyIsCopiedAndNamesPreserved(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()); _ = tp.Shutdown(context.Background()) })
	operations := []string{"queue.first.consume"}
	q, err := New(Config{MeterProvider: mp, TracerProvider: tp, Operations: operations, CountMetric: "service.worker.count", DurationMetric: "service.worker.duration"})
	require.NoError(t, err)
	operations[0] = "private-payload"
	job, _ := q.Begin(context.Background(), operations[0])
	job.Settle(OutcomeAcked, nil)
	require.Equal(t, OtherOperation, recorder.Ended()[0].Name())
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	names := []string{}
	for _, item := range data.ScopeMetrics[0].Metrics {
		names = append(names, item.Name)
	}
	require.ElementsMatch(t, []string{"service.worker.count", "service.worker.duration"}, names)
	_, err = New(Config{Operations: []string{"private payload"}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private payload")
	_, err = New(Config{Operations: make([]string, 65)})
	require.Error(t, err)
}
