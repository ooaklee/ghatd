package errormanifest

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// ambiguousError models an adapter claiming two different public causes.
type ambiguousError struct{}

func (ambiguousError) Error() string { return "ambiguous" }
func (ambiguousError) Is(error) bool { return true }

// uncomparableError ensures an unknown dynamic error cannot panic reply's map lookup.
type uncomparableError []string

func (uncomparableError) Error() string { return "private diagnostic" }

// cyclicError models a broken adapter without allowing resolution to loop.
type cyclicError struct{}

func (e *cyclicError) Error() string { return "private diagnostic" }
func (e *cyclicError) Unwrap() error { return e }

func TestCanonicalErrorResponseBoundaries(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	registeredWrapper := fmt.Errorf("private diagnostic: %w", first)
	var typedNil *classifiedResponseError
	deep := error(first)
	for i := 0; i < 65; i++ {
		deep = fmt.Errorf("diagnostic: %w", deep)
	}
	for _, tc := range []struct {
		name          string
		err           error
		wantCanonical error
		status        int
	}{
		{"nil", nil, nil, 0},
		{"direct", first, first, 409}, {"wrapped", fmt.Errorf("private diagnostic: %w", first), first, 409},
		{"registered wrapper and cause are ambiguous", registeredWrapper, nil, 500},
		{"nested", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", second)), second, 403},
		{"unknown", errors.New("private diagnostic"), nil, 500}, {"same message is not identity", errors.New("first"), nil, 500},
		{"joined known and unknown", errors.Join(first, errors.New("storage unavailable")), nil, 500},
		{"joined knowns", errors.Join(first, second), nil, 500}, {"ambiguous custom Is", ambiguousError{}, nil, 500}, {"too deep", deep, nil, 500},
		{"wrapped join", fmt.Errorf("private diagnostic: %w", errors.Join(first, second)), nil, 500},
		{"uncomparable unknown", uncomparableError{"private diagnostic"}, nil, 500},
		{"homogeneous join", errors.Join(first, first), nil, 500},
		{"cyclic adapter", &cyclicError{}, nil, 500},
		{"typed nil adapter", typedNil, nil, 500},
		{"wrapped typed nil adapter", &classifiedResponseError{class: first, cause: typedNil}, nil, 500},
		{"nil slice error", uncomparableError(nil), nil, 500},
		{"dynamic uncomparable value", interfaceValueError{value: []string{"private diagnostic"}}, nil, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Guard against regressions to raw fallback logging. These cases
			// remain serial because they capture the process-wide standard logger.
			var logs bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&logs)
			t.Cleanup(func() { log.SetOutput(previous) })
			maps := NewComposer().Add(reply.ErrorManifest{first: {Title: "Conflict", StatusCode: 400, Code: "FIRST"}, second: {Title: "Denied", StatusCode: 403, Code: "SECOND"}, registeredWrapper: {Title: "Registered wrapper", StatusCode: 422, Code: "WRAPPER"}}).AddOverrides(reply.ErrorManifest{first: {Title: "Conflict", StatusCode: 409, Code: "OVERRIDE"}}).Build()
			got := CanonicalError(tc.err, maps)
			if tc.err == nil {
				require.Nil(t, got)
				return // No failure response should be written for nil.
			}
			if tc.wantCanonical != nil {
				require.Equal(t, tc.wantCanonical, got)
			} else {
				require.Same(t, unmappedError, got)
			}
			rec := httptest.NewRecorder()
			require.NoError(t, reply.NewReplier(maps).NewHTTPErrorResponse(rec, got))
			require.Equal(t, tc.status, rec.Code)
			require.NotContains(t, rec.Body.String(), "private diagnostic")
			require.NotContains(t, logs.String(), "private diagnostic")
			if tc.wantCanonical == first {
				require.Contains(t, rec.Body.String(), "OVERRIDE")
			}
		})
	}
}

func TestCanonicalErrorManifestBoundaries(t *testing.T) {
	known := errors.New("known")
	var typedNil *classifiedResponseError
	for _, tc := range []struct {
		name string
		maps []reply.ErrorManifest
		want error
	}{
		{"no manifests", nil, unmappedError},
		{"empty manifest", []reply.ErrorManifest{{}}, unmappedError},
		{"nil key ignored", []reply.ErrorManifest{{nil: {StatusCode: 400}}}, unmappedError},
		{"known key beside nil", []reply.ErrorManifest{{nil: {StatusCode: 400}, known: {StatusCode: 403}}}, known},
		{"typed nil key ignored", []reply.ErrorManifest{{typedNil: {StatusCode: 400}}}, unmappedError},
		{"known key beside typed nil", []reply.ErrorManifest{{typedNil: {StatusCode: 400}, known: {StatusCode: 403}}}, known},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, CanonicalError(fmt.Errorf("private diagnostic: %w", known), tc.maps))
		})
	}
}
