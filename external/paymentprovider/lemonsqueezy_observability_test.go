package paymentprovider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lemonContextKey struct{}

type lemonContextRecordingTransport struct {
	base   http.RoundTripper
	values chan any
}

// RoundTrip records the request context value before delegating to the underlying transport.
func (transport *lemonContextRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.values <- request.Context().Value(lemonContextKey{})
	return transport.base.RoundTrip(request)
}

type observedLemonRequest struct {
	method        string
	path          string
	authorization string
}

// TestLemonSqueezyUsesInjectedClientAndCallerContext verifies requests retain the caller context and configured client.
func TestLemonSqueezyUsesInjectedClientAndCallerContext(t *testing.T) {
	observedRequests := make(chan observedLemonRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		observedRequests <- observedLemonRequest{
			method:        request.Method,
			path:          request.URL.Path,
			authorization: request.Header.Get("Authorization"),
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"data": {
				"id": "subscription-1",
				"attributes": {
					"customer_id": 42,
					"variant_id": 7,
					"product_name": "Patron",
					"status": "active",
					"renews_at": "2026-09-01"
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	contextValues := make(chan any, 1)
	transport := &lemonContextRecordingTransport{
		base:   server.Client().Transport,
		values: contextValues,
	}
	provider, err := NewLemonSqueezyProvider(&Config{
		WebhookSecret: "webhook-secret",
		APIKey:        "api-key",
		APIBaseURL:    server.URL,
		HTTPClient:    &http.Client{Transport: transport},
	})
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), lemonContextKey{}, "trace-context")
	response, err := provider.GetSubscriptionInfo(ctx, "subscription-1")
	require.NoError(t, err)
	require.Equal(t, "subscription-1", response.SubscriptionID)
	require.Equal(t, "trace-context", <-contextValues)

	observedRequest := <-observedRequests
	assert.Equal(t, http.MethodGet, observedRequest.method)
	assert.Equal(t, "/v1/subscriptions/subscription-1", observedRequest.path)
	assert.Equal(t, "Bearer api-key", observedRequest.authorization)
}

// TestLemonSqueezyDefaultsToInstrumentedHTTPClient verifies the default client includes an instrumented transport.
func TestLemonSqueezyDefaultsToInstrumentedHTTPClient(t *testing.T) {
	provider, err := NewLemonSqueezyProvider(&Config{WebhookSecret: "webhook-secret"})
	require.NoError(t, err)
	require.NotNil(t, provider.client)
	require.NotNil(t, provider.client.Transport)
}
