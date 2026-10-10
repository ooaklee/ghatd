package observability

import (
	"io"
	"net/http"

	"github.com/felixge/httpsnoop"
)

// httpIOError carries one original I/O error across the instrumentation layer.
// It deliberately has no Unwrap method: instrumentation must see only the safe
// message. A per-call carrier also keeps concurrent reads and writes independent.
type httpIOError struct{ original error }

// Error returns a constant message so instrumentation never sees the original
// stream error text.
func (*httpIOError) Error() string { return "HTTP stream failed" }

// sanitiseHTTPIOError leaves nil and io.EOF unchanged and wraps every other
// error in a safe carrier for telemetry.
func sanitiseHTTPIOError(err error) error {
	if err == nil || err == io.EOF {
		return err
	}
	return &httpIOError{original: err}
}

// restoreHTTPIOError returns the original error carried by a sanitised I/O
// error, passing other errors through unchanged.
func restoreHTTPIOError(err error) error {
	if safe, ok := err.(*httpIOError); ok {
		return safe.original
	}
	return err
}

// errorTransformBody wraps a ReadCloser and applies an error transform to every
// Read and Close result.
type errorTransformBody struct {
	io.ReadCloser
	transform func(error) error
}

// Read delegates to the wrapped body and transforms the returned error.
func (body *errorTransformBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	return n, body.transform(err)
}

// Close closes the wrapped body and transforms any resulting error.
func (body *errorTransformBody) Close() error {
	return body.transform(body.ReadCloser.Close())
}

// errorTransformWriter wraps a Writer and transforms the error returned by each
// Write.
type errorTransformWriter struct {
	io.Writer
	transform func(error) error
}

// Write delegates to the wrapped writer and transforms the returned error.
func (writer errorTransformWriter) Write(buffer []byte) (int, error) {
	n, err := writer.Writer.Write(buffer)
	return n, writer.transform(err)
}

// transformHTTPBody preserves nil/NoBody identity and the optional Writer
// interface used by upgraded HTTP connections. EOF remains exactly io.EOF so
// otelhttp can end successful spans when the body is fully consumed.
func transformHTTPBody(body io.ReadCloser, transform func(error) error) io.ReadCloser {
	if body == nil || body == http.NoBody {
		return body
	}
	reader := &errorTransformBody{ReadCloser: body, transform: transform}
	if writer, ok := body.(io.Writer); ok {
		return struct {
			io.ReadCloser
			io.Writer
		}{reader, errorTransformWriter{Writer: writer, transform: transform}}
	}
	return reader
}

// transformHTTPWriter preserves ResponseWriter's optional interfaces while
// translating only the I/O errors visible across the instrumentation boundary.
func transformHTTPWriter(writer http.ResponseWriter, transform func(error) error) http.ResponseWriter {
	return httpsnoop.Wrap(writer, httpsnoop.Hooks{
		Write: func(next httpsnoop.WriteFunc) httpsnoop.WriteFunc {
			return func(buffer []byte) (int, error) {
				n, err := next(buffer)
				return n, transform(err)
			}
		},
		ReadFrom: func(next httpsnoop.ReadFromFunc) httpsnoop.ReadFromFunc {
			return func(reader io.Reader) (int64, error) {
				n, err := next(reader)
				return n, transform(err)
			}
		},
	})
}
