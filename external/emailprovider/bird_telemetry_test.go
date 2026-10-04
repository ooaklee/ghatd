package emailprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// Global OTel providers are installed/restored per case. These cases must not
// run in parallel; each uses an isolated recorder, reader and log observer.
func TestBirdTelemetryPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		override   bool
	}{
		{"accepted with host client", "accepted", false},
		{"accepted with transport override", "accepted", true},
		{"transport diagnostics", "transport-error", false},
		{"body diagnostics", "body-error", false},
		{"provider rejection", "rejected", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const private = "private-bird-canary"
			traces, recorder := newRecordingTraceSDK(t)
			reader := sdkmetric.NewManualReader()
			metrics := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
			previousTrace, previousMetrics, previousPropagator := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
			otel.SetTracerProvider(traces)
			otel.SetMeterProvider(metrics)
			otel.SetTextMapPropagator(propagation.TraceContext{})
			t.Cleanup(func() {
				otel.SetTracerProvider(previousTrace)
				otel.SetMeterProvider(previousMetrics)
				otel.SetTextMapPropagator(previousPropagator)
				require.NoError(t, metrics.Shutdown(context.Background()))
			})
			var traceparent string
			calls := 0
			base := birdTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				traceparent = r.Header.Get("traceparent")
				require.Equal(t, "Bearer "+birdTestKey, r.Header.Get("Authorization"))
				require.Equal(t, "/v1/email/messages", r.URL.Path)
				switch tc.mode {
				case "transport-error":
					return nil, errors.New(private + birdTestKey)
				case "body-error":
					response := birdTestResponse(202, "")
					response.Body = &birdReadCloser{Reader: birdErrorReader{}}
					return response, nil
				case "rejected":
					return birdTestResponse(422, private+birdTestKey), nil
				default:
					return birdTestResponse(202, birdTestReceipt), nil
				}
			})
			host := observability.NewHTTPClient(base, time.Second)
			request := (&NewBirdClientRequest{APIKey: birdTestKey}).WithHTTPClient(host)
			if tc.override {
				request.HTTPClient = &http.Client{Transport: birdTestTransport(func(*http.Request) (*http.Response, error) {
					t.Error("wrong transport")
					return nil, errors.New("wrong transport")
				})}
				request.Transport = host.Transport
			}
			client, err := NewBirdClient(request)
			require.NoError(t, err)
			core, logs := observer.New(zap.InfoLevel)
			ctx, parent := traces.Tracer("emailprovider/test").Start(context.Background(), "transactional-send")
			ctx = ghatdlogger.TransitWith(ctx, zap.New(core))
			email := birdTestEmail()
			email.To = private + "@example.test"
			email.Subject, email.HTMLBody, email.TextBody = private, private, private
			result, err := NewBirdEmailProvider(client).Send(ctx, email)
			if tc.mode == "accepted" {
				require.NoError(t, err)
				require.True(t, result.Success)
			} else {
				require.ErrorIs(t, err, ErrEmailProviderSendFailed)
			}
			require.Equal(t, 1, calls)
			require.NotEmpty(t, traceparent)
			children := recorder.Ended()
			require.Len(t, children, 1)
			child := children[0]
			require.Equal(t, trace.SpanKindClient, child.SpanKind())
			require.Equal(t, parent.SpanContext().SpanID(), child.Parent().SpanID())
			require.Equal(t, parent.SpanContext().TraceID(), child.SpanContext().TraceID())
			require.Contains(t, traceparent, child.SpanContext().SpanID().String())
			parent.End()
			var data metricdata.ResourceMetrics
			require.NoError(t, reader.Collect(context.Background(), &data))
			require.NotEmpty(t, data.ScopeMetrics)
			serialized, err := json.Marshal(data)
			require.NoError(t, err)
			var emitted strings.Builder
			emitted.Write(serialized)
			for _, span := range recorder.Ended() {
				fmt.Fprintf(&emitted, "%s %v %v %v", span.Name(), span.Attributes(), span.Events(), span.Status())
			}
			for _, entry := range logs.All() {
				fields, err := json.Marshal(entry.ContextMap())
				require.NoError(t, err)
				emitted.WriteString(entry.Message)
				emitted.Write(fields)
			}
			for _, sensitive := range []string{private, birdTestKey, email.To, email.From, email.ReplyTo, "private-stream-secret", "em_testreceipt"} {
				require.NotContains(t, emitted.String(), sensitive)
			}
			require.NotEmpty(t, logs.All())
		})
	}
}
