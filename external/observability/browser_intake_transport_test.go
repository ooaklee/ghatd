package observability

import (
	"bytes"
	"compress/gzip"
	"context"
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

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func browserPOST(t *testing.T, client *http.Client, endpoint string, body []byte, headers ...map[string]string) (int, []byte) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("Content-Type", "application/json")
	for _, values := range headers {
		for key, value := range values {
			request.Header.Set(key, value)
		}
	}
	response, err := client.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	result, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, result
}

func TestBrowserIntakeHTTPUsesRebuiltProtobufTLSCompressionAndServerHeaders(t *testing.T) {
	type received struct {
		path, encoding, authorization, baggage string
		body                                   []byte
		err                                    error
	}
	receivedBatch := make(chan received, 1)
	backend := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reader, err := gzip.NewReader(r.Body)
		var body []byte
		if err == nil {
			body, err = io.ReadAll(reader)
			_ = reader.Close()
		}
		receivedBatch <- received{r.URL.Path, r.Header.Get("Content-Encoding"), r.Header.Get("Authorization"), r.Header.Get("Baggage"), body, err}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: backend.Certificate().Raw})
	file := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(file, certificate, 0600))
	intake := browserFixtureIntake(t, backend.URL+"/base", func(c *BrowserTraceIntakeConfig) {
		t.Setenv("OTEL_EXPORTER_OTLP_CERTIFICATE", file)
		t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "Authorization=Basic%20server-only-credential")
		t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", backend.URL+"/private%2Ftrace")
	})
	server := httptest.NewServer(intake)
	defer server.Close()
	code, body := browserPOST(t, server.Client(), server.URL, browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.request")), map[string]string{"Authorization": browserCanary, "Baggage": browserCanary, "Cookie": browserCanary})
	require.Equal(t, http.StatusAccepted, code)
	require.Empty(t, body)
	got := <-receivedBatch
	require.NoError(t, got.err)
	require.Equal(t, "/private/trace", got.path)
	require.Equal(t, "gzip", got.encoding)
	require.Equal(t, "Basic server-only-credential", got.authorization)
	require.Empty(t, got.baggage)
	require.NotContains(t, string(got.body), browserCanary)
	decoded := &collectortrace.ExportTraceServiceRequest{}
	require.NoError(t, proto.Unmarshal(got.body, decoded))
	require.Equal(t, "example-web", decoded.ResourceSpans[0].Resource.Attributes[0].Value.GetStringValue())
}

func TestBrowserIntakeHTTPRejectsUnacceptedRepliesWithoutEchoOrRedirect(t *testing.T) {
	var redirected atomic.Int32
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer foreign.Close()
	partial, _ := proto.Marshal(&collectortrace.ExportTraceServiceResponse{PartialSuccess: &collectortrace.ExportTracePartialSuccess{RejectedSpans: 1, ErrorMessage: browserCanary}})
	cases := []struct {
		name        string
		status      int
		contentType string
		body        []byte
		expected    int
	}{
		{"accepted", 200, "application/x-protobuf", nil, 202},
		{"partial", 200, "application/x-protobuf", partial, 502},
		{"html", 200, "text/html", []byte(browserCanary), 502},
		{"invalid protobuf", 200, "application/x-protobuf", []byte(browserCanary), 502},
		{"oversize", 200, "application/x-protobuf", bytes.Repeat([]byte("x"), browserIntakeMaximumBytes+1), 502},
		{"auth", 401, "text/plain", []byte(browserCanary), 502},
		{"unavailable", 503, "text/plain", []byte(browserCanary), 503},
		{"redirect", 307, "text/plain", []byte(browserCanary), 502},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int32
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				w.Header().Set("Content-Type", test.contentType)
				w.Header().Set("Location", foreign.URL)
				w.WriteHeader(test.status)
				_, _ = w.Write(test.body)
			}))
			defer backend.Close()
			intake := browserFixtureIntake(t, backend.URL)
			server := httptest.NewServer(intake)
			defer server.Close()
			code, body := browserPOST(t, server.Client(), server.URL, browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation")))
			require.Equal(t, test.expected, code)
			require.Empty(t, body)
			require.Equal(t, int32(1), requests.Load())
		})
	}
	require.Zero(t, redirected.Load())
}

type browserGRPCReceiver struct {
	collectortrace.UnimplementedTraceServiceServer
	code     codes.Code
	partial  int64
	received chan metadata.MD
}

func (receiver *browserGRPCReceiver) Export(ctx context.Context, batch *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	receiver.received <- md
	if receiver.code != codes.OK {
		return nil, status.Error(receiver.code, browserCanary)
	}
	return &collectortrace.ExportTraceServiceResponse{PartialSuccess: &collectortrace.ExportTracePartialSuccess{RejectedSpans: receiver.partial, ErrorMessage: browserCanary}}, nil
}

func TestBrowserIntakeGRPCReusesConfiguredMetadataAndFixedStatuses(t *testing.T) {
	for _, test := range []struct {
		code     codes.Code
		partial  int64
		expected int
	}{{codes.OK, 0, 202}, {codes.OK, 1, 502}, {codes.InvalidArgument, 0, 502}, {codes.Unauthenticated, 0, 502}, {codes.Unavailable, 0, 503}} {
		t.Run(test.code.String()+strings.Repeat("partial", int(test.partial)), func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			require.NoError(t, err)
			receiver := &browserGRPCReceiver{code: test.code, partial: test.partial, received: make(chan metadata.MD, 2)}
			grpcServer := grpc.NewServer()
			collectortrace.RegisterTraceServiceServer(grpcServer, receiver)
			go func() { _ = grpcServer.Serve(listener) }()
			defer grpcServer.Stop()
			intake := browserFixtureIntake(t, "http://"+listener.Addr().String(), func(c *BrowserTraceIntakeConfig) {
				t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "grpc")
				t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-fixture=server-only")
				t.Setenv("OTEL_EXPORTER_OTLP_COMPRESSION", "gzip")
			})
			server := httptest.NewServer(intake)
			defer server.Close()
			for range 2 {
				code, body := browserPOST(t, server.Client(), server.URL, browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation")), map[string]string{"Baggage": browserCanary})
				require.Equal(t, test.expected, code)
				require.Empty(t, body)
				md := <-receiver.received
				require.Equal(t, []string{"server-only"}, md.Get("x-fixture"))
				require.Empty(t, md.Get("baggage"))
			}
		})
	}
}

func TestBrowserIntakeConcurrencyRateShutdownAndMetrics(t *testing.T) {
	entered := make(chan struct{}, 1)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		<-r.Context().Done()
	}))
	defer backend.Close()
	reader := metric.NewManualReader()
	provider := metric.NewMeterProvider(metric.WithReader(reader))
	defer provider.Shutdown(context.Background())
	intake := browserFixtureIntake(t, backend.URL, func(c *BrowserTraceIntakeConfig) {
		c.MaxConcurrent = 1
		c.Timeout = time.Second
		c.MeterProvider = provider
	})
	server := httptest.NewServer(intake)
	defer server.Close()
	body := browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation"))
	result := make(chan int, 1)
	go func() { code, _ := browserPOST(t, server.Client(), server.URL, body); result <- code }()
	<-entered
	code, response := browserPOST(t, server.Client(), server.URL, body)
	require.Equal(t, 429, code)
	require.Empty(t, response)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	require.NoError(t, intake.Shutdown(ctx))
	require.Equal(t, 503, <-result)
	code, response = browserPOST(t, server.Client(), server.URL, body)
	require.Equal(t, 503, code)
	require.Empty(t, response)
	var exported metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &exported))
	outcomes := map[string]int64{}
	for _, scope := range exported.ScopeMetrics {
		for _, instrument := range scope.Metrics {
			if instrument.Name == "ghatd.browser.intake.batch.count" {
				for _, point := range instrument.Data.(metricdata.Sum[int64]).DataPoints {
					value, _ := point.Attributes.Value("outcome")
					outcomes[value.AsString()] += point.Value
				}
			}
		}
	}
	require.Equal(t, map[string]int64{"rate-limited": 1, "unavailable": 2}, outcomes)
	// The rate budget counts admitted batches, independently from completed work.
	other := browserFixtureIntake(t, "http://127.0.0.1:4318", func(c *BrowserTraceIntakeConfig) { c.Burst = 1; c.BatchesPerMinute = 60 })
	now := time.Now()
	admitted, _ := other.admit(now)
	require.True(t, admitted)
	other.release()
	admitted, _ = other.admit(now)
	require.False(t, admitted)
	admitted, _ = other.admit(now.Add(time.Second))
	require.True(t, admitted)
	other.release()
}

func TestBrowserIntakeForwardTimeoutIsBounded(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer backend.Close()
	intake := browserFixtureIntake(t, backend.URL, func(c *BrowserTraceIntakeConfig) { c.Timeout = 100 * time.Millisecond })
	server := httptest.NewServer(intake)
	defer server.Close()
	started := time.Now()
	code, body := browserPOST(t, server.Client(), server.URL, browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation")))
	require.Equal(t, 503, code)
	require.Empty(t, body)
	require.Less(t, time.Since(started), time.Second)
}

type browserBlockedResponseWriter struct {
	*httptest.ResponseRecorder
	entered, unblock chan struct{}
}

func (*browserBlockedResponseWriter) SetReadDeadline(time.Time) error { return nil }
func (writer *browserBlockedResponseWriter) WriteHeader(code int) {
	close(writer.entered)
	<-writer.unblock
	writer.ResponseRecorder.WriteHeader(code)
}

func TestBrowserIntakeShutdownWaitsUntilResponseCompletion(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()
	intake := browserFixtureIntake(t, backend.URL)
	request := httptest.NewRequest(http.MethodPost, "/intake", bytes.NewReader(browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation"))))
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("Content-Type", "application/json")
	writer := &browserBlockedResponseWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), unblock: make(chan struct{})}
	var unblockOnce sync.Once
	defer unblockOnce.Do(func() { close(writer.unblock) })
	completed := make(chan struct{})
	go func() { intake.ServeHTTP(writer, request); close(completed) }()
	<-writer.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	require.Error(t, intake.Shutdown(ctx), "the final response write still owns its admission slot")
	unblockOnce.Do(func() { close(writer.unblock) })
	<-completed
	require.NoError(t, intake.Shutdown(context.Background()))
	require.Equal(t, http.StatusAccepted, writer.Code)
}
