package observability

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip" // Register the configured wire compression.
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ProbeStatus describes receiver acceptance without carrying server messages.
type ProbeStatus string

const (
	ProbeAccepted             ProbeStatus = "accepted"
	ProbeDisabled             ProbeStatus = "disabled"
	ProbeInvalidConfiguration ProbeStatus = "invalid_configuration"
	ProbeUnsupported          ProbeStatus = "unsupported"
	ProbeAuthentication       ProbeStatus = "auth"
	ProbeUnreachable          ProbeStatus = "unreachable"
	ProbeTimeout              ProbeStatus = "timeout"
	ProbeRejected             ProbeStatus = "rejected"
	ProbePartial              ProbeStatus = "partial"

	DefaultProbeTimeout = 10 * time.Second
	MaximumProbeTimeout = time.Minute
)

var (
	ErrProbeFailed               = errors.New("observability: telemetry receiver probe did not pass")
	ErrProbeInvalidConfiguration = errors.New("observability: telemetry probe configuration is invalid")
)

// SignalProbeResult contains only a fixed signal name and bounded status.
type SignalProbeResult struct {
	Signal string      `json:"signal"`
	Status ProbeStatus `json:"status"`
}

// ProbeReport describes one synthetic receiver-acceptance attempt per enabled
// OTLP signal. Acceptance does not prove SDK delivery or backend indexing.
type ProbeReport struct {
	Mode    string              `json:"mode"`
	Service string              `json:"synthetic_service"`
	Signals []SignalProbeResult `json:"signals"`
}

// ProbeConfiguration sends valid synthetic OTLP protobuf requests using the
// inspected endpoint, headers, TLS, compression and timeout configuration. It
// does not construct an SDK, sample application data, install global providers,
// retry exports, or send configured resource values. Traces and logs share a
// fresh random trace/span identity under the fixed ghatd-telemetry-doctor service.
//
// All signals share one bounded deadline; each also respects its export timeout.
// Zero timeout selects DefaultProbeTimeout; values above MaximumProbeTimeout or
// below zero fail without network activity. Non-OTLP exporters are unsupported,
// not disabled. Call InspectConfiguration alone for a read-only diagnosis.
func ProbeConfiguration(ctx context.Context, config Config, timeout time.Duration) (ProbeReport, error) {
	report := ProbeReport{Mode: "receiver_acceptance", Service: "ghatd-telemetry-doctor"}
	for _, name := range []string{"traces", "metrics", "logs"} {
		report.Signals = append(report.Signals, SignalProbeResult{Signal: name, Status: ProbeInvalidConfiguration})
	}
	if timeout < 0 || timeout > MaximumProbeTimeout {
		return report, ErrProbeInvalidConfiguration
	}
	if timeout == 0 {
		timeout = DefaultProbeTimeout
	}
	_, resolved, err := resolveConfiguration(config, os.Getenv)
	if err != nil {
		return report, ErrProbeInvalidConfiguration
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	messages, err := doctorMessages()
	if err != nil {
		return report, ErrProbeFailed
	}
	var waiting sync.WaitGroup
	for index := range report.Signals {
		for _, signal := range resolved.signals {
			if signal.signal != report.Signals[index].Signal {
				continue
			}
			if signal.exporter == "none" {
				report.Signals[index].Status = ProbeDisabled
				continue
			}
			if signal.exporter != "otlp" {
				report.Signals[index].Status = ProbeUnsupported
				continue
			}
			waiting.Add(1)
			go func(index int, signal resolvedSignalConfiguration) {
				defer waiting.Done()
				probeCtx, stop := context.WithTimeout(ctx, signal.timeout)
				defer stop()
				message := messages[signal.signal]
				if signal.protocol == "grpc" {
					report.Signals[index].Status = probeGRPC(probeCtx, signal, message)
				} else {
					report.Signals[index].Status = probeHTTP(probeCtx, signal, message)
				}
			}(index, signal)
		}
	}
	waiting.Wait()
	for _, signal := range report.Signals {
		if signal.Status != ProbeAccepted && signal.Status != ProbeDisabled {
			return report, ErrProbeFailed
		}
	}
	return report, nil
}

type doctorMessage struct {
	request  proto.Message
	response proto.Message
	rejected func() int64
}

func doctorMessages() (map[string]doctorMessage, error) {
	traceID, spanID := make([]byte, 16), make([]byte, 8)
	if _, err := rand.Read(traceID); err != nil {
		return nil, ErrProbeFailed
	}
	if _, err := rand.Read(spanID); err != nil {
		return nil, ErrProbeFailed
	}
	text := func(value string) *commonpb.AnyValue {
		return &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}
	}
	resource := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		{Key: "service.name", Value: text("ghatd-telemetry-doctor")},
		{Key: "service.namespace", Value: text("ghatd")},
		{Key: "deployment.environment.name", Value: text("diagnostic")},
	}}
	scope := &commonpb.InstrumentationScope{Name: "github.com/ooaklee/ghatd/external/observability/doctor"}
	now := uint64(time.Now().UnixNano())
	traces := &collectortrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{
		Resource: resource, ScopeSpans: []*tracepb.ScopeSpans{{Scope: scope, Spans: []*tracepb.Span{{
			TraceId: traceID, SpanId: spanID, Name: "telemetry-doctor", Kind: tracepb.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: now, EndTimeUnixNano: now + 1,
		}}}},
	}}}
	metrics := &collectormetrics.ExportMetricsServiceRequest{ResourceMetrics: []*metricspb.ResourceMetrics{{
		Resource: resource, ScopeMetrics: []*metricspb.ScopeMetrics{{Scope: scope, Metrics: []*metricspb.Metric{{
			Name: "ghatd.telemetry.doctor.probe", Unit: "{probe}", Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
				DataPoints: []*metricspb.NumberDataPoint{{TimeUnixNano: now, Value: &metricspb.NumberDataPoint_AsInt{AsInt: 1}}},
			}},
		}}}},
	}}}
	logs := &collectorlogs.ExportLogsServiceRequest{ResourceLogs: []*logspb.ResourceLogs{{
		Resource: resource, ScopeLogs: []*logspb.ScopeLogs{{Scope: scope, LogRecords: []*logspb.LogRecord{{
			TimeUnixNano: now, ObservedTimeUnixNano: now, TraceId: traceID, SpanId: spanID,
			SeverityNumber: logspb.SeverityNumber_SEVERITY_NUMBER_INFO, SeverityText: "INFO", Body: text("telemetry doctor probe"),
		}}}},
	}}}
	traceResponse := &collectortrace.ExportTraceServiceResponse{}
	metricResponse := &collectormetrics.ExportMetricsServiceResponse{}
	logResponse := &collectorlogs.ExportLogsServiceResponse{}
	return map[string]doctorMessage{
		"traces":  {traces, traceResponse, func() int64 { return traceResponse.GetPartialSuccess().GetRejectedSpans() }},
		"metrics": {metrics, metricResponse, func() int64 { return metricResponse.GetPartialSuccess().GetRejectedDataPoints() }},
		"logs":    {logs, logResponse, func() int64 { return logResponse.GetPartialSuccess().GetRejectedLogRecords() }},
	}, nil
}

func probeHTTP(ctx context.Context, signal resolvedSignalConfiguration, message doctorMessage) ProbeStatus {
	payload, err := proto.Marshal(message.request)
	if err != nil {
		return ProbeInvalidConfiguration
	}
	if signal.compression == "gzip" {
		var compressed bytes.Buffer
		writer := gzip.NewWriter(&compressed)
		_, _ = writer.Write(payload)
		_ = writer.Close()
		payload = compressed.Bytes()
	}
	endpoint := *signal.endpoint
	if signal.insecure {
		endpoint.Scheme = "http"
	} else {
		endpoint.Scheme = "https"
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(payload))
	if err != nil {
		return ProbeInvalidConfiguration
	}
	for key, value := range signal.headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Accept", "application/x-protobuf")
	if signal.compression == "gzip" {
		request.Header.Set("Content-Encoding", "gzip")
	}
	transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: signal.tlsConfig,
		DialContext: (&net.Dialer{Timeout: signal.timeout}).DialContext, TLSHandshakeTimeout: signal.timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(request)
	if err != nil {
		return probeTransportStatus(ctx, err)
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return ProbeAuthentication
	case http.StatusRequestTimeout, http.StatusGatewayTimeout:
		return ProbeTimeout
	case http.StatusOK:
	default:
		return ProbeRejected
	}
	// A generic successful HTML page must not count as OTLP acceptance. The
	// protocol requires a protobuf ExportResponse, including for partial success.
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/x-protobuf" {
		return ProbeRejected
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return probeTransportStatus(ctx, err)
	}
	if len(body) > 65536 || proto.Unmarshal(body, message.response) != nil {
		return ProbeRejected
	}
	if message.rejected() < 0 {
		return ProbeRejected
	}
	if message.rejected() > 0 {
		return ProbePartial
	}
	return ProbeAccepted
}

func probeGRPC(ctx context.Context, signal resolvedSignalConfiguration, message doctorMessage) ProbeStatus {
	var security credentials.TransportCredentials
	if signal.insecure {
		security = insecure.NewCredentials()
	} else {
		config := signal.tlsConfig
		if config == nil {
			config = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		security = credentials.NewTLS(config)
	}
	options := []grpc.DialOption{grpc.WithTransportCredentials(security), grpc.WithDisableRetry(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(65536))}
	connection, err := grpc.NewClient(signal.endpoint.Host, options...)
	if err != nil {
		return ProbeInvalidConfiguration
	}
	defer connection.Close()
	ctx = metadata.NewOutgoingContext(ctx, metadata.New(signal.headers))
	var callOptions []grpc.CallOption
	if signal.compression == "gzip" {
		callOptions = append(callOptions, grpc.UseCompressor("gzip"))
	}
	switch request := message.request.(type) {
	case *collectortrace.ExportTraceServiceRequest:
		response, callErr := collectortrace.NewTraceServiceClient(connection).Export(ctx, request, callOptions...)
		err = callErr
		if response != nil {
			proto.Merge(message.response, response)
		}
	case *collectormetrics.ExportMetricsServiceRequest:
		response, callErr := collectormetrics.NewMetricsServiceClient(connection).Export(ctx, request, callOptions...)
		err = callErr
		if response != nil {
			proto.Merge(message.response, response)
		}
	case *collectorlogs.ExportLogsServiceRequest:
		response, callErr := collectorlogs.NewLogsServiceClient(connection).Export(ctx, request, callOptions...)
		err = callErr
		if response != nil {
			proto.Merge(message.response, response)
		}
	}
	if err != nil {
		switch status.Code(err) {
		case codes.Unauthenticated, codes.PermissionDenied:
			return ProbeAuthentication
		case codes.DeadlineExceeded, codes.Canceled:
			return ProbeTimeout
		case codes.Unavailable:
			return probeTransportStatus(ctx, err)
		default:
			return ProbeRejected
		}
	}
	if message.rejected() < 0 {
		return ProbeRejected
	}
	if message.rejected() > 0 {
		return ProbePartial
	}
	return ProbeAccepted
}

func probeTransportStatus(ctx context.Context, err error) ProbeStatus {
	var networkError net.Error
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) ||
		(errors.As(err, &networkError) && networkError.Timeout()) {
		return ProbeTimeout
	}
	return ProbeUnreachable
}
