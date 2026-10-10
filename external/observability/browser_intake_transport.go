package observability

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"time"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// browserIntakeSender owns the intake's outbound transport: either a gRPC
// client connection or an HTTP client with its own transport, built from the
// resolved signal settings.
type browserIntakeSender struct {
	settings  resolvedSignalConfiguration
	http      *http.Client
	transport *http.Transport
	grpc      *grpc.ClientConn
}

// newBrowserIntakeSender builds the sender for the resolved settings, creating
// a non-retrying gRPC client (with TLS unless insecure) or an HTTP/2-capable
// transport with the configured timeouts and redirect handling. It returns a
// fixed error when the gRPC client cannot be created.
func newBrowserIntakeSender(settings resolvedSignalConfiguration) (*browserIntakeSender, error) {
	sender := &browserIntakeSender{settings: settings}
	if settings.protocol == "grpc" {
		var security credentials.TransportCredentials
		if settings.insecure {
			security = insecure.NewCredentials()
		} else {
			config := settings.tlsConfig
			if config == nil {
				config = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			security = credentials.NewTLS(config)
		}
		connection, err := grpc.NewClient(settings.endpoint.Host, grpc.WithTransportCredentials(security), grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(browserIntakeMaximumBytes)))
		if err != nil {
			return nil, errors.New("create browser intake transport failed")
		}
		sender.grpc = connection
	} else {
		transport := &http.Transport{Proxy: http.ProxyFromEnvironment, TLSClientConfig: settings.tlsConfig, DialContext: (&net.Dialer{Timeout: settings.timeout}).DialContext, TLSHandshakeTimeout: settings.timeout, ResponseHeaderTimeout: settings.timeout, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 32, MaxIdleConnsPerHost: 32, MaxResponseHeaderBytes: browserIntakeMaximumBytes, ForceAttemptHTTP2: true}
		sender.transport = transport
		sender.http = &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return sender, nil
}

// close releases the sender's transports, closing idle HTTP connections and the
// gRPC connection when present.
func (sender *browserIntakeSender) close() {
	if sender.transport != nil {
		sender.transport.CloseIdleConnections()
	}
	if sender.grpc != nil {
		_ = sender.grpc.Close()
	}
}

// send performs one synchronous OTLP export under the configured timeout, via
// gRPC with metadata and optional gzip, or HTTP protobuf with a bounded
// response body. It maps transport-level failures to 503, rejected or malformed
// results to 502, and full acceptance to 202; HTTP request bodies are not
// replayable.
func (sender *browserIntakeSender) send(ctx context.Context, batch *collectortrace.ExportTraceServiceRequest) int {
	ctx, cancel := context.WithTimeout(ctx, sender.settings.timeout)
	defer cancel()
	if sender.grpc != nil {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(sender.settings.headers))
		var options []grpc.CallOption
		if sender.settings.compression == "gzip" {
			options = append(options, grpc.UseCompressor("gzip"))
		}
		response, err := collectortrace.NewTraceServiceClient(sender.grpc).Export(ctx, batch, options...)
		if err != nil {
			switch status.Code(err) {
			case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
				return http.StatusServiceUnavailable
			default:
				return http.StatusBadGateway
			}
		}
		if response == nil || response.GetPartialSuccess().GetRejectedSpans() != 0 {
			return http.StatusBadGateway
		}
		return http.StatusAccepted
	}
	encoded, err := proto.Marshal(batch)
	if err != nil {
		return http.StatusBadGateway
	}
	if sender.settings.compression == "gzip" {
		var buffer bytes.Buffer
		compressor := gzip.NewWriter(&buffer)
		_, _ = compressor.Write(encoded)
		_ = compressor.Close()
		encoded = buffer.Bytes()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sender.settings.endpoint.String(), bytes.NewReader(encoded))
	if err != nil {
		return http.StatusBadGateway
	}
	// This bounded intake does not replay a batch after a transport failure.
	request.GetBody = nil
	for key, value := range sender.settings.headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", "application/x-protobuf")
	request.Header.Set("Accept", "application/x-protobuf")
	if sender.settings.compression == "gzip" {
		request.Header.Set("Content-Encoding", "gzip")
	}
	response, err := sender.http.Do(request)
	if err != nil {
		return http.StatusServiceUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		switch response.StatusCode {
		case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return http.StatusServiceUnavailable
		default:
			return http.StatusBadGateway
		}
	}
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || contentType != "application/x-protobuf" {
		return http.StatusBadGateway
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, browserIntakeMaximumBytes+1))
	if err != nil {
		return http.StatusServiceUnavailable
	}
	result := &collectortrace.ExportTraceServiceResponse{}
	if len(body) > browserIntakeMaximumBytes || proto.Unmarshal(body, result) != nil || result.GetPartialSuccess().GetRejectedSpans() != 0 {
		return http.StatusBadGateway
	}
	return http.StatusAccepted
}
