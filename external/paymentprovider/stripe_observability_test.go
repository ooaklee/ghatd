package paymentprovider

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStripeDefaultsToInstrumentedHTTPClient verifies default provider calls use traced transport.
func TestStripeDefaultsToInstrumentedHTTPClient(t *testing.T) {
	provider, err := NewStripeProvider(&Config{WebhookSecret: "whsec_test"})
	require.NoError(t, err)
	require.NotNil(t, provider.httpClient)
	require.NotNil(t, provider.httpClient.Transport)

	assert.NotSame(t, http.DefaultTransport, provider.httpClient.Transport)
	assert.Equal(t, 10*time.Second, provider.httpClient.Timeout)
}
