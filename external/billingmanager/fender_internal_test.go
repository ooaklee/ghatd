package billingmanager

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhookRequestMappingErrorDoesNotRequireRequestURL(t *testing.T) {
	t.Parallel()

	req := &http.Request{}

	require.NotPanics(t, func() {
		_, _ = mapRequestToProcessBillingProviderWebhooksRequest(req, nil)
	})
}
