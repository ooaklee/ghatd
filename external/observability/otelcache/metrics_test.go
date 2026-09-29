package otelcache

import (
	"context"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"testing"
)

func newDecisionMetrics(t *testing.T) (*Observer, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
	metrics, err := New(Config{MeterProvider: provider})
	require.NoError(t, err)
	return metrics, reader
}

func collectDecisions(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var data metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &data))
	return data
}

//nolint:staticcheck // Missing context is an intentional optional-instrumentation contract test.
func TestCacheMetricsRejectUnknownDimensions(t *testing.T) {
	metrics, reader := newDecisionMetrics(t)
	metrics.Record(nil, "private-cache-key", Hit)
	metrics.Record(nil, HTTPResponse, "private-event")
	metrics.Record(nil, HTTPResponse, Hit)
	var optional *Observer
	optional.Record(nil, HTTPResponse, Hit)
	var zero Observer
	zero.Record(nil, HTTPResponse, Hit)
	data := collectDecisions(t, reader)
	require.Len(t, data.ScopeMetrics, 1)
	sum := data.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.Len(t, sum.DataPoints, 1)
	require.EqualValues(t, 1, sum.DataPoints[0].Value)
}

func TestCacheConfigurationCopiesVocabularyAndPreservesMetricName(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	names := []string{"token"}
	metrics, err := New(Config{MeterProvider: provider, MetricName: "service.cache.event.count", Names: names})
	require.NoError(t, err)
	names[0] = "private-key"
	metrics.Record(context.Background(), names[0], Hit)
	metrics.Record(context.Background(), "token", Hit)
	data := collectDecisions(t, reader)
	require.Equal(t, "service.cache.event.count", data.ScopeMetrics[0].Metrics[0].Name)
	sum := data.ScopeMetrics[0].Metrics[0].Data.(metricdata.Sum[int64])
	require.Len(t, sum.DataPoints, 1)
	value, _ := sum.DataPoints[0].Attributes.Value("cache")
	require.Equal(t, "token", value.AsString())
	_, err = New(Config{Names: []string{"private secret"}})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private secret")
	_, err = New(Config{Names: make([]string, 65)})
	require.Error(t, err)
}
