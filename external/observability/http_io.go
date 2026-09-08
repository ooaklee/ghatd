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

func (*httpIOError) Error() string { return "HTTP stream failed" }

func sanitiseHTTPIOError(err error) error {
	if err == nil || err == io.EOF {
		return err
	}
	return &httpIOError{original: err}
}

func restoreHTTPIOError(err error) error {
	if safe, ok := err.(*httpIOError); ok {
		return safe.original
	}
	return err
}

type errorTransformBody struct {
	io.ReadCloser
	transform func(error) error
}

func (body *errorTransformBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	return n, body.transform(err)
}

func (body *errorTransformBody) Close() error {
	return body.transform(body.ReadCloser.Close())
}

type errorTransformWriter struct {
	io.Writer
	transform func(error) error
}

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
