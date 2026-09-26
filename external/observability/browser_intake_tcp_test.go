package observability_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	ghatdhttp "github.com/ooaklee/ghatd/external/observability/otelhttp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"go.uber.org/zap"
)

func browserTCPServer(t *testing.T, readStarted ...chan struct{}) (*httptest.Server, *observability.BrowserTraceIntake) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	t.Cleanup(backend.Close)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", backend.URL)
	intake, err := observability.NewBrowserTraceIntake(observability.BrowserTraceIntakeConfig{ServiceName: "example-web", AllowedOrigins: []string{"https://example.test"}, Timeout: 100 * time.Millisecond, MeterProvider: noop.NewMeterProvider()})
	require.NoError(t, err)
	var handler http.Handler = intake
	if len(readStarted) != 0 {
		handler = http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			intake.ServeHTTP(&browserTCPDeadlineObserver{ResponseWriter: writer, started: readStarted[0]}, request)
		})
	}
	server := httptest.NewServer(ghatdhttp.Wrap("example-api", zap.NewNop(), handler))
	t.Cleanup(func() { server.Close(); require.NoError(t, intake.Shutdown(context.Background())) })
	return server, intake
}

type browserTCPDeadlineObserver struct {
	http.ResponseWriter
	started chan struct{}
	once    sync.Once
}

func (writer *browserTCPDeadlineObserver) Unwrap() http.ResponseWriter { return writer.ResponseWriter }
func (writer *browserTCPDeadlineObserver) SetReadDeadline(deadline time.Time) error {
	err := http.NewResponseController(writer.ResponseWriter).SetReadDeadline(deadline)
	if err == nil {
		writer.once.Do(func() { close(writer.started) })
	}
	return err
}

func browserTCPBody(t *testing.T) []byte {
	t.Helper()
	now := time.Now().UnixNano()
	body, err := json.Marshal(map[string]any{"resourceSpans": []any{map[string]any{"scopeSpans": []any{map[string]any{"spans": []any{map[string]any{"name": "browser.navigation", "traceId": "0102030405060708090a0b0c0d0e0f10", "spanId": "0102030405060708", "startTimeUnixNano": fmt.Sprint(now), "endTimeUnixNano": fmt.Sprint(now), "kind": 1, "attributes": []any{map[string]any{"key": "browser.outcome", "value": map[string]any{"stringValue": "complete"}}}}}}}}}})
	require.NoError(t, err)
	return body
}

func browserTCPConnect(t *testing.T, server *httptest.Server) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	require.NoError(t, connection.SetDeadline(time.Now().Add(2*time.Second)))
	return connection
}

func browserTCPResponse(t *testing.T, reader *bufio.Reader, expected int) *http.Response {
	t.Helper()
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodPost})
	require.NoError(t, err)
	require.Equal(t, expected, response.StatusCode)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Empty(t, body)
	require.NoError(t, response.Body.Close())
	return response
}

func TestBrowserIntakeRealTCPBodyDeadlineThroughWholeHTTPComposition(t *testing.T) {
	server, _ := browserTCPServer(t)
	connection := browserTCPConnect(t, server)
	started := time.Now()
	_, err := fmt.Fprint(connection, "POST /intake HTTP/1.1\r\nHost: example.test\r\nOrigin: https://example.test\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{")
	require.NoError(t, err)
	reader := bufio.NewReader(connection)
	response := browserTCPResponse(t, reader, http.StatusRequestTimeout)
	require.True(t, response.Close)
	_, err = reader.ReadByte()
	require.ErrorIs(t, err, io.EOF)
	require.Less(t, time.Since(started), time.Second, "context cancellation alone must not leave Body.Read or response drain blocked")
}

func TestBrowserIntakeRealTCPOversizedUnreadBodiesCloseWithoutDraining(t *testing.T) {
	for _, chunked := range []bool{false, true} {
		t.Run(fmt.Sprint(chunked), func(t *testing.T) {
			server, _ := browserTCPServer(t)
			connection := browserTCPConnect(t, server)
			started := time.Now()
			headers := "POST /intake HTTP/1.1\r\nHost: example.test\r\nOrigin: https://example.test\r\nContent-Type: application/json\r\n"
			if chunked {
				headers += "Transfer-Encoding: chunked\r\n\r\n10064\r\n" + strings.Repeat(" ", 65537)
			} else {
				headers += "Content-Length: 65537\r\n\r\n"
			}
			_, err := io.WriteString(connection, headers)
			require.NoError(t, err)
			reader := bufio.NewReader(connection)
			response := browserTCPResponse(t, reader, http.StatusRequestEntityTooLarge)
			require.True(t, response.Close)
			_, err = reader.ReadByte()
			require.ErrorIs(t, err, io.EOF)
			require.Less(t, time.Since(started), time.Second)
		})
	}
}

func TestBrowserIntakeRealTCPSuccessRestoresKeepAliveReadDeadline(t *testing.T) {
	server, _ := browserTCPServer(t)
	connection := browserTCPConnect(t, server)
	reader := bufio.NewReader(connection)
	for index := 0; index < 2; index++ {
		body := browserTCPBody(t)
		_, err := fmt.Fprintf(connection, "POST /intake HTTP/1.1\r\nHost: example.test\r\nOrigin: https://example.test\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s", len(body), body)
		require.NoError(t, err)
		response := browserTCPResponse(t, reader, http.StatusAccepted)
		require.False(t, response.Close)
		if index == 0 {
			time.Sleep(150 * time.Millisecond)
		}
	}
}

func TestBrowserIntakeShutdownInterruptsActualTCPRead(t *testing.T) {
	started := make(chan struct{})
	server, intake := browserTCPServer(t, started)
	connection := browserTCPConnect(t, server)
	_, err := fmt.Fprint(connection, "POST /intake HTTP/1.1\r\nHost: example.test\r\nOrigin: https://example.test\r\nContent-Type: application/json\r\nContent-Length: 1024\r\n\r\n{")
	require.NoError(t, err)
	// Wait for the actual connection deadline, then shutdown must cancel the
	// admitted socket read without completion of the client body.
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("body read was not admitted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, intake.Shutdown(ctx))
	reader := bufio.NewReader(connection)
	response := browserTCPResponse(t, reader, http.StatusRequestTimeout)
	require.True(t, response.Close)
}
