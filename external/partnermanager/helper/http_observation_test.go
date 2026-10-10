package partnermanagerhelper

import (
	"context"
	"errors"
	"fmt"

	ghobservability "github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"testing"
	"time"
)

type observationPrivateKey struct{}

func TestHTTPObserverFiniteContract(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		level  zapcore.Level
	}{
		{"success", 200, zapcore.InfoLevel}, {"rejection", 400, zapcore.WarnLevel}, {"dependency", 503, zapcore.ErrorLevel},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := sdkmetric.NewManualReader()
			provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			t.Cleanup(func() { require.NoError(t, provider.Shutdown(context.Background())) })
			recorder := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { require.NoError(t, tracer.Shutdown(context.Background())) })
			core, logs := observer.New(zapcore.InfoLevel)
			observe, err := NewHTTPObserver(HTTPObserverConfig{MeterName: "fixture/partners", MetricPrefix: "fixture.partners", Logger: zap.New(core), MeterProvider: provider})
			require.NoError(t, err)
			ctx, span := tracer.Tracer("fixture").Start(context.WithValue(t.Context(), observationPrivateKey{}, "private-query-body-actor"), "http request")
			o := partnerhttp.Observation{Method: "GET", Operation: "partners.overview", RouteTemplate: "/api/v1/partners/overview", Stage: "completed", ErrorCode: "PARTNERS_DEPENDENCY_UNAVAILABLE", Status: tc.status, Duration: 250 * time.Millisecond}
			observe(ctx, o)
			span.End()
			var metrics metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(t.Context(), &metrics))
			require.Len(t, metrics.ScopeMetrics, 1)
			scope := metrics.ScopeMetrics[0]
			require.Equal(t, "fixture/partners", scope.Scope.Name)
			require.Len(t, scope.Metrics, 2)
			want := attribute.NewSet(attribute.String("partners.operation", o.Operation), attribute.String("partners.route", o.RouteTemplate), attribute.String("http.request.method", o.Method), attribute.Int("http.response.status_code", tc.status))
			for _, m := range scope.Metrics {
				switch m.Name {
				case "fixture.partners.request.count":
					sum, ok := m.Data.(metricdata.Sum[int64])
					require.True(t, ok)
					require.Equal(t, "{request}", m.Unit)
					require.Len(t, sum.DataPoints, 1)
					require.Equal(t, int64(1), sum.DataPoints[0].Value)
					require.Equal(t, want, sum.DataPoints[0].Attributes)
				case "fixture.partners.request.duration":
					histogram, ok := m.Data.(metricdata.Histogram[float64])
					require.True(t, ok)
					require.Equal(t, "s", m.Unit)
					require.Len(t, histogram.DataPoints, 1)
					require.Equal(t, uint64(1), histogram.DataPoints[0].Count)
					require.Equal(t, 0.25, histogram.DataPoints[0].Sum)
					require.Equal(t, want, histogram.DataPoints[0].Attributes)
				default:
					t.Fatalf("unexpected metric %s", m.Name)
				}
			}
			require.Len(t, recorder.Ended(), 1)
			attrs := attribute.NewSet(recorder.Ended()[0].Attributes()...)
			require.Equal(t, 6, attrs.Len())
			stage, ok := attrs.Value("partners.stage")
			require.True(t, ok)
			require.Equal(t, "completed", stage.AsString())
			errorCode, ok := attrs.Value("partners.error.code")
			require.True(t, ok)
			require.Equal(t, o.ErrorCode, errorCode.AsString())
			entries := logs.All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.level, entries[0].Level)
			fields := entries[0].ContextMap()
			require.Len(t, fields, 10)
			require.Equal(t, "partners", fields["source"])
			require.Equal(t, o.Operation, fields["operation"])
			require.Equal(t, float64(250), fields["duration_ms"])
			require.Equal(t, span.SpanContext().TraceID().String(), fields[ghobservability.TraceIDKey])
			require.Equal(t, span.SpanContext().SpanID().String(), fields[ghobservability.SpanIDKey])
			// Inspect exported attributes/serialized fields, not Zap's intentional
			// SkipType context carrier retained only for intrinsic correlation.
			require.NotContains(t, fields, "context")
			require.NotContains(t, fmt.Sprint(metrics, fields, attrs), "private-query-body-actor")
		})
	}
}

func TestHTTPObserverConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, meter, prefix string
		typedNil            bool
		want                error
	}{
		{"defaults use borrowed global/no-op ports", "fixture/partners", "fixture.partners", false, nil},
		{"typed nil meter falls back", "fixture/partners", "fixture.partners", true, nil},
		{"meter required", "", "fixture.partners", false, partnermanager.ErrInvalid},
		{"prefix required", "fixture/partners", "", false, partnermanager.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := HTTPObserverConfig{MeterName: tc.meter, MetricPrefix: tc.prefix}
			if tc.typedNil {
				var provider *sdkmetric.MeterProvider
				cfg.MeterProvider = provider
			}
			observe, err := NewHTTPObserver(cfg)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, observe)
			} else {
				require.NotNil(t, observe)
				observe(t.Context(), partnerhttp.Observation{Method: "GET", Operation: "partners.overview", RouteTemplate: "/api/v1/partners/overview", Status: 200})
			}
		})
	}
}

type failureMeter struct {
	metric.Meter
	failCounter bool
	failure     error
}

func (m failureMeter) Int64Counter(string, ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	if m.failCounter {
		return nil, m.failure
	}
	return metricnoop.NewMeterProvider().Meter("fixture").Int64Counter("fixture.count")
}
func (m failureMeter) Float64Histogram(string, ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	return nil, m.failure
}

type failureMeterProvider struct {
	metric.MeterProvider
	meter metric.Meter
}

func (p failureMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter { return p.meter }

func TestHTTPObserverPropagatesInstrumentFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counter bool
	}{{"counter", true}, {"histogram", false}} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("fixture instrument failure")
			provider := failureMeterProvider{MeterProvider: metricnoop.NewMeterProvider(), meter: failureMeter{Meter: metricnoop.NewMeterProvider().Meter("fixture"), failCounter: tc.counter, failure: failure}}
			observe, err := NewHTTPObserver(HTTPObserverConfig{MeterName: "fixture", MetricPrefix: "fixture", MeterProvider: provider})
			require.ErrorIs(t, err, failure)
			require.Nil(t, observe)
		})
	}
}
