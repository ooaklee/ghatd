package observability

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const doctorCanary = "synthetic-private-probe-canary"

func TestDoctorHTTPExportsValidCorrelatedSyntheticPayloads(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	traces, meters, logs, propagation := otel.GetTracerProvider(), otel.GetMeterProvider(), logglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	var mutex sync.Mutex
	received := map[string]proto.Message{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		assert.Equal(t, http.MethodPost, request.Method)
		assert.Equal(t, "application/x-protobuf", request.Header.Get("Content-Type"))
		assert.Equal(t, "gzip", request.Header.Get("Content-Encoding"))
		compressed, err := gzip.NewReader(request.Body)
		if !assert.NoError(t, err) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer compressed.Close()
		payload, err := io.ReadAll(io.LimitReader(compressed, 65536))
		if !assert.NoError(t, err) {
			return
		}
		var message proto.Message
		switch request.URL.Path {
		case "/custom-traces":
			message = &collectortrace.ExportTraceServiceRequest{}
		case "/base/v1/metrics":
			message = &collectormetrics.ExportMetricsServiceRequest{}
		case "/base/v1/logs":
			message = &collectorlogs.ExportLogsServiceRequest{}
		default:
			t.Error("unexpected signal path")
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if request.URL.Path == "/base/v1/logs" {
			assert.Empty(t, request.Header.Get("X-Generic"))
			assert.Equal(t, "logs-only", request.Header.Get("X-Logs"))
		} else {
			assert.Equal(t, "a+b/c==", request.Header.Get("X-Generic"))
		}
		assert.NoError(t, proto.Unmarshal(payload, message))
		mutex.Lock()
		received[request.URL.Path] = message
		mutex.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	certificate := filepath.Join(t.TempDir(), "diagnostic-ca.pem")
	require.NoError(t, os.WriteFile(certificate, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL+"/base")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", server.URL+"/custom-traces")
	t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", certificate)
	t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Generic=a+b%2Fc%3D%3D")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_HEADERS", "X-Logs=logs-only")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "custom.private="+doctorCanary)
	report, err := ProbeConfiguration(context.Background(), Config{ServiceName: doctorCanary}, time.Second)
	require.NoError(t, err)
	assertDoctorStatuses(t, report, ProbeAccepted)
	mutex.Lock()
	defer mutex.Unlock()
	require.Len(t, received, 3)
	traceRequest := received["/custom-traces"].(*collectortrace.ExportTraceServiceRequest)
	metricRequest := received["/base/v1/metrics"].(*collectormetrics.ExportMetricsServiceRequest)
	logRequest := received["/base/v1/logs"].(*collectorlogs.ExportLogsServiceRequest)
	span := traceRequest.ResourceSpans[0].ScopeSpans[0].Spans[0]
	log := logRequest.ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	assert.Len(t, span.TraceId, 16)
	assert.Len(t, span.SpanId, 8)
	assert.Equal(t, span.TraceId, log.TraceId)
	assert.Equal(t, span.SpanId, log.SpanId)
	assert.Equal(t, "ghatd-telemetry-doctor", runtimeOTLPAttribute(traceRequest.ResourceSpans[0].Resource.Attributes, "service.name"))
	assert.EqualValues(t, 1, metricRequest.ResourceMetrics[0].ScopeMetrics[0].Metrics[0].GetGauge().DataPoints[0].GetAsInt())
	for _, request := range received {
		payload, err := proto.Marshal(request)
		require.NoError(t, err)
		assert.NotContains(t, string(payload), doctorCanary)
	}
	assert.Same(t, traces, otel.GetTracerProvider())
	assert.Same(t, meters, otel.GetMeterProvider())
	assert.Same(t, logs, logglobal.GetLoggerProvider())
	assert.Equal(t, propagation, otel.GetTextMapPropagator())
}

func TestDoctorHTTPFailureClassificationAndNoRedirect(t *testing.T) {
	for _, test := range []struct {
		name string
		code int
		want ProbeStatus
	}{
		{"authentication", 401, ProbeAuthentication}, {"permission", 403, ProbeAuthentication},
		{"receiver rejected", 503, ProbeRejected}, {"receiver timeout", 504, ProbeTimeout},
		{"redirect", 307, ProbeRejected}, {"html success", 200, ProbeRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			var redirected atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { redirected.Add(1) }))
			defer target.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", target.URL+"/"+doctorCanary)
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(test.code)
				_, _ = io.WriteString(w, doctorCanary)
			}))
			defer server.Close()
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization="+doctorCanary)
			report, err := ProbeConfiguration(context.Background(), Config{}, time.Second)
			require.ErrorIs(t, err, ErrProbeFailed)
			assertDoctorStatuses(t, report, test.want)
			assert.Zero(t, redirected.Load(), "redirects must not receive telemetry or credentials")
			assert.NotContains(t, err.Error(), doctorCanary)
		})
	}
}

func TestDoctorHTTPPartialSuccessAndTimeout(t *testing.T) {
	for _, partial := range []bool{true, false} {
		t.Run(map[bool]string{true: "partial", false: "deadline"}[partial], func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if !partial {
					_, _ = io.Copy(io.Discard, request.Body)
					select {
					case <-request.Context().Done():
					case <-time.After(time.Second):
					}
					return
				}
				var response proto.Message
				switch request.URL.Path {
				case "/v1/traces":
					response = &collectortrace.ExportTraceServiceResponse{PartialSuccess: &collectortrace.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: doctorCanary}}
				case "/v1/metrics":
					response = &collectormetrics.ExportMetricsServiceResponse{PartialSuccess: &collectormetrics.ExportMetricsPartialSuccess{RejectedDataPoints: 1, ErrorMessage: doctorCanary}}
				case "/v1/logs":
					response = &collectorlogs.ExportLogsServiceResponse{PartialSuccess: &collectorlogs.ExportLogsPartialSuccess{RejectedLogRecords: 1, ErrorMessage: doctorCanary}}
				}
				payload, err := proto.Marshal(response)
				assert.NoError(t, err)
				w.Header().Set("Content-Type", "application/x-protobuf")
				_, _ = w.Write(payload)
			}))
			defer server.Close()
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
			started := time.Now()
			report, err := ProbeConfiguration(context.Background(), Config{}, 100*time.Millisecond)
			require.ErrorIs(t, err, ErrProbeFailed)
			want := ProbeTimeout
			if partial {
				want = ProbePartial
			}
			assertDoctorStatuses(t, report, want)
			assert.EqualValues(t, 3, requests.Load(), "one attempt per signal, no retries")
			assert.Less(t, time.Since(started), time.Second, "one shared deadline, not three serial budgets")
		})
	}
}

func TestDoctorGRPCReceiverAcceptanceAndTypedFailures(t *testing.T) {
	for _, test := range []struct {
		name    string
		code    codes.Code
		partial bool
		want    ProbeStatus
	}{
		{"accepted", codes.OK, false, ProbeAccepted},
		{"unauthenticated", codes.Unauthenticated, false, ProbeAuthentication},
		{"permission", codes.PermissionDenied, false, ProbeAuthentication},
		{"unavailable", codes.Unavailable, false, ProbeUnreachable},
		{"timeout", codes.DeadlineExceeded, false, ProbeTimeout},
		{"context deadline", codes.OK, false, ProbeTimeout},
		{"rejected", codes.InvalidArgument, false, ProbeRejected},
		{"partial", codes.OK, true, ProbePartial},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			var count atomic.Int32
			server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, request any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
				count.Add(1)
				headers, _ := metadata.FromIncomingContext(ctx)
				assert.Equal(t, []string{"a+b/c=="}, headers.Get("x-probe"))
				if test.name == "context deadline" {
					select {
					case <-ctx.Done():
					case <-time.After(time.Second):
					}
					return nil, status.Error(codes.DeadlineExceeded, doctorCanary)
				}
				if test.code != codes.OK {
					return nil, status.Error(test.code, doctorCanary)
				}
				switch typed := request.(type) {
				case *collectortrace.ExportTraceServiceRequest:
					assert.Len(t, typed.ResourceSpans[0].ScopeSpans[0].Spans, 1)
					response := &collectortrace.ExportTraceServiceResponse{}
					if test.partial {
						response.PartialSuccess = &collectortrace.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: doctorCanary}
					}
					return response, nil
				case *collectormetrics.ExportMetricsServiceRequest:
					assert.Len(t, typed.ResourceMetrics[0].ScopeMetrics[0].Metrics, 1)
					response := &collectormetrics.ExportMetricsServiceResponse{}
					if test.partial {
						response.PartialSuccess = &collectormetrics.ExportMetricsPartialSuccess{RejectedDataPoints: 1, ErrorMessage: doctorCanary}
					}
					return response, nil
				case *collectorlogs.ExportLogsServiceRequest:
					assert.Len(t, typed.ResourceLogs[0].ScopeLogs[0].LogRecords, 1)
					response := &collectorlogs.ExportLogsServiceResponse{}
					if test.partial {
						response.PartialSuccess = &collectorlogs.ExportLogsPartialSuccess{RejectedLogRecords: 1, ErrorMessage: doctorCanary}
					}
					return response, nil
				default:
					return nil, status.Error(codes.Unimplemented, doctorCanary)
				}
			}))
			collectortrace.RegisterTraceServiceServer(server, &collectortrace.UnimplementedTraceServiceServer{})
			collectormetrics.RegisterMetricsServiceServer(server, &collectormetrics.UnimplementedMetricsServiceServer{})
			collectorlogs.RegisterLogsServiceServer(server, &collectorlogs.UnimplementedLogsServiceServer{})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			go func() { _ = server.Serve(listener) }()
			defer server.Stop()
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+listener.Addr().String())
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Probe=a+b%2Fc%3D%3D")
			t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
			if test.name == "context deadline" {
				// The configured signal timeout also bounds a longer CLI budget.
				t.Setenv("OTEL_EXPORTER_OTLP_TIMEOUT", "100")
			}
			started := time.Now()
			report, err := ProbeConfiguration(context.Background(), Config{}, time.Second)
			if test.want == ProbeAccepted {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrProbeFailed)
			}
			assertDoctorStatuses(t, report, test.want)
			assert.EqualValues(t, 3, count.Load())
			if test.name == "context deadline" {
				assert.Less(t, time.Since(started), 750*time.Millisecond)
			}
		})
	}
}

func TestDoctorRefusedConnectionAndNonOTLPStates(t *testing.T) {
	for _, protocol := range []string{"http/protobuf", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			clearRuntimeOTELTestEnvironment(t)
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			endpoint := "http://" + listener.Addr().String()
			require.NoError(t, listener.Close())
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
			t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", protocol)
			report, err := ProbeConfiguration(context.Background(), Config{}, time.Second)
			require.ErrorIs(t, err, ErrProbeFailed)
			assertDoctorStatuses(t, report, ProbeUnreachable)
		})
	}
	t.Run("disabled", func(t *testing.T) {
		clearRuntimeOTELTestEnvironment(t)
		for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
			t.Setenv("OTEL_"+signal+"_EXPORTER", "none")
		}
		report, err := ProbeConfiguration(context.Background(), Config{}, 0)
		require.NoError(t, err)
		assertDoctorStatuses(t, report, ProbeDisabled)
	})
	t.Run("unsupported", func(t *testing.T) {
		clearRuntimeOTELTestEnvironment(t)
		for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
			t.Setenv("OTEL_"+signal+"_EXPORTER", "console")
		}
		report, err := ProbeConfiguration(context.Background(), Config{}, time.Second)
		require.ErrorIs(t, err, ErrProbeFailed)
		assertDoctorStatuses(t, report, ProbeUnsupported)
	})
	t.Run("invalid", func(t *testing.T) {
		clearRuntimeOTELTestEnvironment(t)
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://"+doctorCanary+"@localhost:4318")
		report, err := ProbeConfiguration(context.Background(), Config{}, time.Second)
		require.ErrorIs(t, err, ErrProbeInvalidConfiguration)
		assertDoctorStatuses(t, report, ProbeInvalidConfiguration)
		assert.NotContains(t, err.Error(), doctorCanary)
	})
}

func assertDoctorStatuses(t *testing.T, report ProbeReport, want ProbeStatus) {
	t.Helper()
	require.Len(t, report.Signals, 3)
	assert.Equal(t, "receiver_acceptance", report.Mode)
	for index, signal := range report.Signals {
		assert.Equal(t, []string{"traces", "metrics", "logs"}[index], signal.Signal)
		assert.Equal(t, want, signal.Status)
	}
	encoded, err := json.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), doctorCanary)
	assert.False(t, strings.Contains(string(encoded), "http://"))
}
