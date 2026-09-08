package reader

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type readerContextKey struct{}

type readerRoundTripperFunc func(*http.Request) (*http.Response, error)

// RoundTrip invokes the transport function used by the test client.
func (function readerRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// TestReadRemoteFileContextUsesInjectedClientAndCallerContext verifies remote reads retain their caller context.
func TestReadRemoteFileContextUsesInjectedClientAndCallerContext(t *testing.T) {
	const requestURL = "https://config.example.test/manifests/private-id?revision=secret"

	requests := make(chan *http.Request, 1)
	client := &http.Client{Transport: readerRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests <- request
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("name: vehicle-api\n")),
			Request:    request,
		}, nil
	})}
	ctx := context.WithValue(context.Background(), readerContextKey{}, "caller-context")

	data, err := ReadRemoteFileContext(ctx, client, requestURL)
	require.NoError(t, err)
	assert.Equal(t, "name: vehicle-api\n", string(data))

	request := <-requests
	assert.Equal(t, http.MethodGet, request.Method)
	assert.Equal(t, requestURL, request.URL.String())
	assert.Equal(t, "caller-context", request.Context().Value(readerContextKey{}))
}

// TestReadRemoteFileContextPreservesCancellation verifies remote reads return the caller cancellation unchanged.
func TestReadRemoteFileContextPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &http.Client{Transport: readerRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
		cancel()
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}

	data, err := ReadRemoteFileContext(ctx, client, "https://config.example.test/manifest")
	assert.Nil(t, data)
	assert.ErrorIs(t, err, context.Canceled)
	assert.True(t, errors.Is(err, context.Canceled))
}
