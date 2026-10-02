package errormanifest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// classifiedResponseError owns a public classification and a private retry cause.
type classifiedResponseError struct {
	// class is the canonical domain sentinel recognised by Is.
	class error
	// cause remains available to internal callers through Unwrap.
	cause error
}

// Error deliberately contains synthetic diagnostics to detect accidental use.
func (e *classifiedResponseError) Error() string { return "private diagnostic" }

// Is compares this node's classification, not the underlying cause.
func (e *classifiedResponseError) Is(target error) bool { return target == e.class }

// Unwrap exposes the internal cause without changing the public classification.
func (e *classifiedResponseError) Unwrap() error { return e.cause }

// malformedJoin exercises custom join implementations, including nil branches.
type malformedJoin []error

// Error makes accidental diagnostic serialization visible in assertions.
func (e malformedJoin) Error() string { return "private diagnostic" }

// Unwrap returns the deliberately malformed fixture branches unchanged.
func (e malformedJoin) Unwrap() []error { return e }

// interfaceValueError has a comparable type but may hold an uncomparable value.
type interfaceValueError struct {
	// value demonstrates why dynamic comparability, not type comparability, matters.
	value any
}

// Error permits the value to exercise error-interface lookup without panicking.
func (e interfaceValueError) Error() string { return "private diagnostic" }

// wrapResponseError constructs an exact node-count fixture for traversal limits.
func wrapResponseError(err error, nodes int) error {
	for i := 1; i < nodes; i++ {
		err = fmt.Errorf("private diagnostic: %w", err)
	}
	return err
}

// TestResponseErrors checks classification, structural completeness and bounds.
func TestResponseErrors(t *testing.T) {
	first, second, internal := errors.New("first"), errors.New("second"), errors.New("internal")
	private := errors.New("private diagnostic")
	classified := &classifiedResponseError{class: first, cause: private}
	var typedNil *classifiedResponseError
	registered := &classifiedResponseError{class: first, cause: private}
	maps := NewComposer().Add(reply.ErrorManifest{
		first: {StatusCode: 400, Code: "FIRST"}, second: {StatusCode: 409, Code: "SECOND"},
		internal: {StatusCode: 503, Code: "INTERNAL"}, registered: {StatusCode: 422, Code: "EXACT"},
	}).Build()
	for _, tc := range []struct {
		name string
		err  error
		want []error
	}{
		{"direct", first, []error{first}},
		{"wrapped", fmt.Errorf("private diagnostic: %w", first), []error{first}},
		{"newlines do not classify", errors.New("first\nsecond"), []error{unmappedError}},
		{"wrapped multiline", fmt.Errorf("private diagnostic\n%w", first), []error{first}},
		{"singleton join", errors.Join(first), []error{first}},
		{"all mapped join", errors.Join(first, second), []error{first, second}},
		{"nested ordered dedup", errors.Join(second, fmt.Errorf("context: %w", errors.Join(first, second))), []error{second, first}},
		{"mapped server failure", errors.Join(first, internal), []error{first, internal}},
		{"mixed unknown last", errors.Join(first, private), []error{unmappedError}},
		{"mixed unknown first", errors.Join(private, first), []error{unmappedError}},
		{"wrapped mixed", fmt.Errorf("context: %w", errors.Join(first, private)), []error{unmappedError}},
		{"explicit domain classification", classified, []error{first}},
		{"classification does not hide sibling", errors.Join(classified, private), []error{unmappedError}},
		{"exact identity precedes Is", registered, []error{registered}},
		{"unknown", private, []error{unmappedError}},
		{"lookalike", errors.New("first"), []error{unmappedError}},
		{"nil", nil, []error{unmappedError}},
		{"typed nil", typedNil, []error{unmappedError}},
		{"empty join", malformedJoin{}, []error{unmappedError}},
		{"nil child", malformedJoin{first, nil}, []error{unmappedError}},
		{"ambiguous Is", ambiguousError{}, []error{unmappedError}},
		{"uncomparable slice", uncomparableError{"diagnostic"}, []error{unmappedError}},
		{"uncomparable interface value", interfaceValueError{value: []string{"diagnostic"}}, []error{unmappedError}},
		{"cyclic", &cyclicError{}, []error{unmappedError}},
		{"63 nodes", wrapResponseError(first, 63), []error{first}},
		{"64 nodes", wrapResponseError(first, 64), []error{first}},
		{"65 nodes", wrapResponseError(first, 65), []error{unmappedError}},
		{"wide tree within budget", malformedJoin(makeRepeatedErrors(first, 63)), []error{first}},
		{"wide tree over budget", malformedJoin(makeRepeatedErrors(first, 64)), []error{unmappedError}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ResponseErrors(tc.err, maps))
		})
	}
}

// makeRepeatedErrors builds breadth-boundary fixtures without shared mutation.
func makeRepeatedErrors(err error, count int) []error {
	result := make([]error, count)
	for i := range result {
		result[i] = err
	}
	return result
}

// TestWriteHTTPError pins reply status selection and diagnostic privacy.
func TestWriteHTTPError(t *testing.T) {
	first, second, internal := errors.New("first"), errors.New("second"), errors.New("internal")
	maps := NewComposer().Add(reply.ErrorManifest{
		first:    {Title: "First", StatusCode: 400, Code: "FIRST"},
		second:   {Title: "Second", StatusCode: 409, Code: "SECOND"},
		internal: {Title: "Unavailable", StatusCode: 503, Code: "INTERNAL"},
	}).AddOverrides(reply.ErrorManifest{first: {Title: "Overridden", StatusCode: 422, Code: "OVERRIDE"}}).Build()
	for _, tc := range []struct {
		name   string
		err    error
		status int
		codes  []string
	}{
		{"direct and override", first, 422, []string{"OVERRIDE"}},
		{"wrapped and override", fmt.Errorf("private diagnostic: %w", first), 422, []string{"OVERRIDE"}},
		{"joined first status", errors.Join(first, second), 422, []string{"OVERRIDE", "SECOND"}},
		{"joined reversed status", errors.Join(second, first), 409, []string{"SECOND", "OVERRIDE"}},
		{"server error dominates", errors.Join(first, internal), 503, []string{"INTERNAL"}},
		{"server error first", errors.Join(internal, first), 503, []string{"INTERNAL"}},
		{"unknown join dominates", errors.Join(first, errors.New("private diagnostic")), 500, []string{""}},
		{"unknown", errors.New("private diagnostic"), 500, []string{""}},
		{"nil is not success", nil, 500, []string{""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Serial cases intentionally capture reply's global fallback logger.
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previous) })
			rec := httptest.NewRecorder()
			require.NoError(t, WriteHTTPError(rec, tc.err, maps))
			require.Equal(t, tc.status, rec.Code)
			var body struct {
				Errors []struct {
					Code string `json:"code"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			var codes []string
			for _, item := range body.Errors {
				codes = append(codes, item.Code)
			}
			require.Equal(t, tc.codes, codes)
			require.NotContains(t, rec.Body.String(), "private diagnostic")
			require.NotContains(t, logs.String(), "private diagnostic")
		})
	}
}

// TestWriteHTTPErrorCompatibilityAndIsolation preserves the existing direct-error
// envelope and verifies attributes and concurrent calls cannot replace its data.
func TestWriteHTTPErrorCompatibilityAndIsolation(t *testing.T) {
	known := errors.New("known")
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"default status", 0}, {"validation", 400}, {"accepted override", 202}, {"server", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			maps := []reply.ErrorManifest{{known: {Code: "KNOWN", Title: "Known", Detail: "Public detail", StatusCode: tc.status}}}
			want, got, other := httptest.NewRecorder(), httptest.NewRecorder(), httptest.NewRecorder()
			attrs := []reply.ResponseAttributes{reply.WithMeta(map[string]interface{}{"request_id": tc.name}), reply.WithHeaders(map[string]string{"X-Request-ID": tc.name})}
			require.NoError(t, reply.NewReplier(maps).NewHTTPErrorResponse(want, known, attrs...))
			attrs = append(attrs, nil, func(r *reply.NewResponseRequest) {
				r.Writer = other
				r.Error = errors.New("private diagnostic")
				r.Errors = []error{errors.New("private diagnostic")}
				r.Data = "private diagnostic"
				r.TokenOne, r.TokenTwo, r.Message = "private diagnostic", "private diagnostic", "private diagnostic"
				r.StatusCode = 201
			})
			require.NoError(t, WriteHTTPError(got, known, maps, attrs...))
			require.Equal(t, want.Code, got.Code)
			require.Equal(t, want.Header(), got.Header())
			require.Equal(t, want.Body.String(), got.Body.String())
			require.Empty(t, other.Body.String())
			// Shared immutable maps are safe; each concurrent write owns its DTO.
			for i := range 8 {
				t.Run(fmt.Sprint(i), func(t *testing.T) {
					t.Parallel()
					rec := httptest.NewRecorder()
					require.NoError(t, WriteHTTPError(rec, known, maps))
					require.NotContains(t, rec.Body.String(), tc.name)
				})
			}
		})
	}
}

// failedResponseWriter simulates a disconnected client after headers are sent.
type failedResponseWriter struct {
	// ResponseWriter records headers even when the following body write fails.
	http.ResponseWriter
}

// Write simulates a transport failure without delivering any bytes.
func (w failedResponseWriter) Write([]byte) (int, error) { return 0, errors.New("disconnected") }

// TestWriteHTTPErrorFailures verifies delivery failures remain visible to callers.
func TestWriteHTTPErrorFailures(t *testing.T) {
	known := errors.New("known")
	for _, tc := range []struct {
		name    string
		writer  http.ResponseWriter
		attrs   []reply.ResponseAttributes
		message string
	}{
		{"missing writer", nil, nil, "no writer"},
		{"writer error", failedResponseWriter{httptest.NewRecorder()}, nil, "disconnected"},
		{"encoding error", httptest.NewRecorder(), []reply.ResponseAttributes{reply.WithMeta(map[string]interface{}{"bad": make(chan int)})}, "unsupported type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := WriteHTTPError(tc.writer, known, []reply.ErrorManifest{{known: {StatusCode: 400}}}, tc.attrs...)
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tc.message), "%v", err)
		})
	}
}
