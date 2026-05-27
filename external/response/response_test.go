package response_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/response"
	"github.com/stretchr/testify/require"
)

func TestDefaultResponsesDoNotRequireRequestURL(t *testing.T) {
	t.Parallel()

	req := &http.Request{Header: http.Header{}}

	require.NotPanics(t, func() {
		response.GetDefault200Response(httptest.NewRecorder(), req)
	})
	require.NotPanics(t, func() {
		response.GetResourceNotFoundError(httptest.NewRecorder(), req)
	})
}
