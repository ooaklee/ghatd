package user_test

import (
	"encoding/json"
	"testing"

	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

func TestApplePrivateRelayDomains(t *testing.T) {
	for _, email := range []string{"alias@privaterelay.appleid.com", " alias@Private.ICLOUD.com ", "ALIAS@PRIVATERELAY.APPLEID.COM"} {
		require.True(t, user.IsApplePrivateRelayEmail(email), email)
	}
	for _, email := range []string{"alias@icloud.com", "alias@relay.example", "alias@notprivaterelay.appleid.com", "alias@sub.privaterelay.appleid.com", "alias@private.icloud.com.example", "invalid"} {
		require.False(t, user.IsApplePrivateRelayEmail(email), email)
	}
}

func TestRelayAllowlistCannotBeStoredInProof(t *testing.T) {
	raw, err := json.Marshal(user.DisconnectOAuthProviderRequest{AllowedRelayFallbackProviders: []string{"google"}})
	require.NoError(t, err)
	require.NotContains(t, string(raw), "google")
	var request user.DisconnectOAuthProviderRequest
	require.NoError(t, json.Unmarshal([]byte(`{"AllowedRelayFallbackProviders":["google"]}`), &request))
	require.Empty(t, request.AllowedRelayFallbackProviders)
}

func TestMissingRelayRepositoryCapabilityFailsClosed(t *testing.T) {
	require.False(t, (&user.Service{}).SupportsOAuthRelayFallback())
}
