package observability

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type httpIOTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

func (body *httpIOTestBody) Read(buffer []byte) (int, error) {
	return body.read(buffer)
}

func (body *httpIOTestBody) Close() error {
	if body.close != nil {
		return body.close()
	}
	return nil
}

type httpIOTestDuplexBody struct {
	io.ReadCloser
	write func([]byte) (int, error)
}

type httpIOTestClassifiedError struct{ sensitive string }

func (err *httpIOTestClassifiedError) Error() string     { return err.sensitive }
func (err *httpIOTestClassifiedError) ErrorType() string { return err.sensitive }

func (body *httpIOTestDuplexBody) Write(buffer []byte) (int, error) {
	return body.write(buffer)
}

// Reading a response can fail after RoundTrip has succeeded. The application
// must receive the original error and bytes while otelhttp sees only safe text.
func TestRoundTripperResponseBodyIOErrorsAndEOF(t *testing.T) {
	const sensitive = "synthetic-sensitive-body-error-registration-TEST123"
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "read failure", err: errors.New(sensitive)},
		{name: "custom error type stays private", err: &httpIOTestClassifiedError{sensitive: sensitive}},
		{name: "wrapped EOF is still a failure", err: fmt.Errorf("%s: %w", sensitive, io.EOF)},
		{name: "exact EOF ends successful span", err: io.EOF},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, recorder, options := newTestHTTPTraceProvider(t)
			body := &httpIOTestBody{read: func(buffer []byte) (int, error) {
				return copy(buffer, "part"), test.err
			}}
			response := httpIOTestResponse(t, body, http.StatusOK, options...)
			if _, ok := response.Body.(io.Writer); ok {
				t.Fatal("response body gained an unsupported Writer interface")
			}
			buffer := make([]byte, 16)
			n, err := response.Body.Read(buffer)
			if n != 4 || string(buffer[:n]) != "part" || err != test.err {
				t.Fatalf("Read() = (%d, %q, %v), want original partial bytes and error %v", n, buffer[:n], err, test.err)
			}
			if test.err == io.EOF {
				if len(recorder.Ended()) != 1 {
					t.Fatal("exact EOF did not end the span before Close")
				}
			} else if len(recorder.Ended()) != 0 {
				t.Fatal("a non-EOF error ended the span before Close")
			}
			if err := response.Body.Close(); err != nil {
				t.Fatalf("Close() = %v", err)
			}
			span := findEndedSpan(t, recorder, oteltrace.SpanKindClient)
			if test.err != io.EOF && span.Status().Code != codes.Error {
				t.Fatalf("failed read span status = %v, want Error", span.Status())
			}
			if test.err == io.EOF && span.Status().Code == codes.Error {
				t.Fatalf("EOF span status = %v, want successful completion", span.Status())
			}
			assertSpanExcludes(t, span, sensitive)
		})
	}
}

func TestRoundTripperUpgradedBodyIOWriteAndCloseErrors(t *testing.T) {
	_, recorder, options := newTestHTTPTraceProvider(t)
	writeErr := errors.New("synthetic-sensitive-upgraded-write-error-TEST123")
	closeErr := errors.New("synthetic-sensitive-upgraded-close-error-TEST456")
	closeCalls := 0
	body := &httpIOTestDuplexBody{
		ReadCloser: &httpIOTestBody{
			read: func([]byte) (int, error) { return 0, io.EOF },
			close: func() error {
				closeCalls++
				return closeErr
			},
		},
		write: func(buffer []byte) (int, error) {
			if string(buffer) != "outbound" {
				t.Fatalf("underlying Write received %q, want original bytes", buffer)
			}
			return 3, writeErr
		},
	}
	response := httpIOTestResponse(t, body, http.StatusSwitchingProtocols, options...)
	writer, ok := response.Body.(io.Writer)
	if !ok {
		t.Fatal("upgraded response body lost its Writer interface")
	}
	if n, err := writer.Write([]byte("outbound")); n != 3 || err != writeErr {
		t.Fatalf("Write() = (%d, %v), want (3, original write error)", n, err)
	}
	if err := response.Body.Close(); err != closeErr {
		t.Fatalf("Close() = %v, want original close error", err)
	}
	if closeCalls != 1 {
		t.Fatalf("underlying Close calls = %d, want 1", closeCalls)
	}
	span := findEndedSpan(t, recorder, oteltrace.SpanKindClient)
	if span.Status().Code != codes.Error {
		t.Fatalf("failed write span status = %v, want Error", span.Status())
	}
	assertSpanExcludes(t, span, writeErr.Error(), closeErr.Error())
}

func TestRoundTripperBodyIOConcurrentReadWriteErrorsRemainIndependent(t *testing.T) {
	_, recorder, options := newTestHTTPTraceProvider(t)
	readErr := errors.New("synthetic-sensitive-concurrent-read-TEST123")
	writeErr := errors.New("synthetic-sensitive-concurrent-write-TEST456")
	const rounds = 64
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	body := &httpIOTestDuplexBody{
		ReadCloser: &httpIOTestBody{read: func(buffer []byte) (int, error) {
			arrived <- struct{}{}
			<-release
			return copy(buffer, "r"), readErr
		}},
		write: func([]byte) (int, error) {
			arrived <- struct{}{}
			<-release
			return 2, writeErr
		},
	}
	response := httpIOTestResponse(t, body, http.StatusSwitchingProtocols, options...)
	writer, ok := response.Body.(io.Writer)
	if !ok {
		t.Fatal("upgraded response body lost its Writer interface")
	}
	for range rounds {
		var done sync.WaitGroup
		done.Add(2)
		go func() {
			defer done.Done()
			buffer := make([]byte, 4)
			if n, err := response.Body.Read(buffer); n != 1 || err != readErr || buffer[0] != 'r' {
				t.Errorf("concurrent Read() = (%d, %v, %q), want original read result", n, err, buffer)
			}
		}()
		go func() {
			defer done.Done()
			if n, err := writer.Write([]byte("out")); n != 2 || err != writeErr {
				t.Errorf("concurrent Write() = (%d, %v), want original write result", n, err)
			}
		}()
		<-arrived
		<-arrived
		release <- struct{}{}
		release <- struct{}{}
		done.Wait()
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	span := findEndedSpan(t, recorder, oteltrace.SpanKindClient)
	assertSpanExcludes(t, span, readErr.Error(), writeErr.Error())
}

func TestRoundTripperOutgoingRequestBodyIOErrorPreservation(t *testing.T) {
	_, recorder, options := newTestHTTPTraceProvider(t)
	wantErr := errors.New("synthetic-sensitive-request-body-error-TEST123")
	body := &httpIOTestBody{read: func(buffer []byte) (int, error) {
		return copy(buffer, "partial"), wantErr
	}}
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		buffer := make([]byte, 32)
		n, err := request.Body.Read(buffer)
		if n != 7 || string(buffer[:n]) != "partial" || err != wantErr {
			t.Fatalf("transport request Read() = (%d, %q, %v), want original partial bytes and error", n, buffer[:n], err)
		}
		if closeErr := request.Body.Close(); closeErr != nil {
			t.Fatalf("transport request Close() = %v", closeErr)
		}
		return nil, err
	})
	request := httptest.NewRequest(http.MethodPost, "https://upstream.example.test/upload", body)
	response, err := newRoundTripper(base, options...).RoundTrip(request)
	if response != nil || err != wantErr {
		t.Fatalf("RoundTrip() = (%v, %v), want (nil, original body error)", response, err)
	}
	if request.Body != body {
		t.Fatal("instrumentation replaced the caller's request body")
	}
	span := findEndedSpan(t, recorder, oteltrace.SpanKindClient)
	if span.Status().Code != codes.Error {
		t.Fatalf("request-body failure span status = %v, want Error", span.Status())
	}
	assertSpanExcludes(t, span, wantErr.Error())
}

type httpIOTestResponseWriter struct {
	header   http.Header
	writeErr error
	status   int
}

func (writer *httpIOTestResponseWriter) Header() http.Header    { return writer.header }
func (writer *httpIOTestResponseWriter) WriteHeader(status int) { writer.status = status }
func (writer *httpIOTestResponseWriter) Write(buffer []byte) (int, error) {
	return min(3, len(buffer)), writer.writeErr
}

// Server read/write errors are not exported by the pinned otelhttp v0.69.0.
// This verifies preservation and defense in depth across the full middleware,
// including optional message events; it does not claim an existing server leak.
func TestHTTPServerBodyIOErrorsPreserveApplicationResults(t *testing.T) {
	_, recorder, options := newTestHTTPTraceProvider(t)
	options = append(options, otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents))
	readErr := errors.New("synthetic-sensitive-server-read-TEST123")
	closeErr := errors.New("synthetic-sensitive-server-close-TEST456")
	writeErr := errors.New("synthetic-sensitive-server-write-TEST789")
	body := &httpIOTestBody{
		read:  func(buffer []byte) (int, error) { return copy(buffer, "part"), readErr },
		close: func() error { return closeErr },
	}
	writer := &httpIOTestResponseWriter{header: make(http.Header), writeErr: writeErr}
	handler := httpServerMiddleware("io-test", options...)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		buffer := make([]byte, 16)
		if n, err := request.Body.Read(buffer); n != 4 || string(buffer[:n]) != "part" || err != readErr {
			t.Fatalf("handler Read() = (%d, %q, %v), want original partial bytes and error", n, buffer[:n], err)
		}
		if err := request.Body.Close(); err != closeErr {
			t.Fatalf("handler Close() = %v, want original close error", err)
		}
		if n, err := writer.Write([]byte("response")); n != 3 || err != writeErr {
			t.Fatalf("handler Write() = (%d, %v), want original partial write and error", n, err)
		}
	}))
	request := httptest.NewRequest(http.MethodPost, "/upload", body)
	handler.ServeHTTP(writer, request)
	if request.Body != body {
		t.Fatal("middleware replaced the caller's request body")
	}
	span := findEndedSpan(t, recorder, oteltrace.SpanKindServer)
	assertSpanExcludes(t, span, readErr.Error(), closeErr.Error(), writeErr.Error())
}

func TestHTTPServerBodyIOPreservesNilAndNoBodyIdentity(t *testing.T) {
	for _, test := range []struct {
		name string
		body io.ReadCloser
	}{
		{name: "nil"},
		{name: "NoBody", body: http.NoBody},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, recorder, options := newTestHTTPTraceProvider(t)
			handler := httpServerMiddleware("io-test", options...)(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Body != test.body {
					t.Fatalf("handler body = %T, want identity of %T", request.Body, test.body)
				}
				writer.WriteHeader(http.StatusNoContent)
			}))
			request := httptest.NewRequest(http.MethodGet, "/health", nil)
			request.Body = test.body
			handler.ServeHTTP(httptest.NewRecorder(), request)
			if request.Body != test.body {
				t.Fatal("middleware changed the caller's empty body identity")
			}
			findEndedSpan(t, recorder, oteltrace.SpanKindServer)
		})
	}
}

type httpIOTestOptionalWriter struct {
	*httpIOTestResponseWriter
	readFromErr  error
	hijackErr    error
	pushErr      error
	closed       chan bool
	flushCalls   int
	readFromData string
	pushTarget   string
}

func (writer *httpIOTestOptionalWriter) Flush() { writer.flushCalls++ }
func (writer *httpIOTestOptionalWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, writer.hijackErr
}
func (writer *httpIOTestOptionalWriter) Push(target string, _ *http.PushOptions) error {
	writer.pushTarget = target
	return writer.pushErr
}
func (writer *httpIOTestOptionalWriter) CloseNotify() <-chan bool { return writer.closed }
func (writer *httpIOTestOptionalWriter) ReadFrom(reader io.Reader) (int64, error) {
	buffer := make([]byte, 3)
	n, _ := io.ReadFull(reader, buffer)
	writer.readFromData = string(buffer[:n])
	return int64(n), writer.readFromErr
}

func TestHTTPServerIOPreservesResponseWriterOptionalInterfaces(t *testing.T) {
	for _, optional := range []bool{false, true} {
		t.Run(fmt.Sprintf("optional=%t", optional), func(t *testing.T) {
			_, recorder, options := newTestHTTPTraceProvider(t)
			base := &httpIOTestResponseWriter{header: make(http.Header)}
			extended := &httpIOTestOptionalWriter{
				httpIOTestResponseWriter: base,
				readFromErr:              errors.New("synthetic-sensitive-readfrom-TEST123"),
				hijackErr:                errors.New("synthetic-sensitive-hijack-TEST456"),
				pushErr:                  errors.New("synthetic-sensitive-push-TEST789"),
				closed:                   make(chan bool),
			}
			var original http.ResponseWriter = base
			if optional {
				original = extended
			}
			handler := httpServerMiddleware("io-test", options...)(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				flusher, hasFlusher := writer.(http.Flusher)
				hijacker, hasHijacker := writer.(http.Hijacker)
				pusher, hasPusher := writer.(http.Pusher)
				notifier, hasNotifier := writer.(http.CloseNotifier)
				readerFrom, hasReaderFrom := writer.(io.ReaderFrom)
				for name, present := range map[string]bool{
					"Flusher": hasFlusher, "Hijacker": hasHijacker, "Pusher": hasPusher,
					"CloseNotifier": hasNotifier, "ReaderFrom": hasReaderFrom,
				} {
					if present != optional {
						t.Fatalf("%s presence = %t, want %t", name, present, optional)
					}
				}
				writer.Header().Set("X-Application", "preserved")
				if !optional {
					writer.WriteHeader(http.StatusNoContent)
					return
				}
				if n, err := readerFrom.ReadFrom(strings.NewReader("source")); n != 3 || err != extended.readFromErr {
					t.Fatalf("ReadFrom() = (%d, %v), want partial bytes and original error", n, err)
				}
				if connection, buffer, err := hijacker.Hijack(); connection != nil || buffer != nil || err != extended.hijackErr {
					t.Fatalf("Hijack() = (%v, %v, %v), want original results", connection, buffer, err)
				}
				if err := pusher.Push("/asset", nil); err != extended.pushErr {
					t.Fatalf("Push() = %v, want original error", err)
				}
				if notifier.CloseNotify() != extended.closed {
					t.Fatal("CloseNotify() did not return the original channel")
				}
				flusher.Flush()
			}))
			handler.ServeHTTP(original, httptest.NewRequest(http.MethodGet, "/stream", nil))
			if base.Header().Get("X-Application") != "preserved" {
				t.Fatal("handler Header changes did not reach original writer")
			}
			if optional && (extended.flushCalls != 1 || extended.readFromData != "sou" || extended.pushTarget != "/asset") {
				t.Fatalf("optional methods did not delegate correctly: flushes=%d, readFrom=%q, push=%q", extended.flushCalls, extended.readFromData, extended.pushTarget)
			}
			span := findEndedSpan(t, recorder, oteltrace.SpanKindServer)
			assertSpanExcludes(t, span, extended.readFromErr.Error(), extended.hijackErr.Error(), extended.pushErr.Error())
		})
	}
}

func httpIOTestResponse(t *testing.T, body io.ReadCloser, status int, options ...otelhttp.Option) *http.Response {
	t.Helper()
	base := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: make(http.Header), Body: body, Request: request}, nil
	})
	request := httptest.NewRequest(http.MethodGet, "https://upstream.example.test/resource", nil)
	response, err := newRoundTripper(base, options...).RoundTrip(request)
	if err != nil {
		t.Fatalf("RoundTrip() = %v", err)
	}
	return response
}
