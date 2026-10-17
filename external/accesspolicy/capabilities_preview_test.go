package accesspolicy

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestCapabilityPreviewReadOnlyCanonicalAndDetached(t *testing.T) {
	for _, variant := range []string{"changed", "semantic noop", "absence", "invalid duplicate", "revoked"} {
		t.Run(variant, func(t *testing.T) {
			subject := Subject{System: "example", Kind: UserSubject, ID: "selected"}
			original := Grant{Subject: subject, Revision: 3, Enabled: true, Scopes: []string{"a", "b"}, Permissions: []string{"read"}, Tokens: TokenLimits{Permanent: 2}, Limits: map[string]Limit{"api": {Maximum: 4, WindowSeconds: 60}}, ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 123000000, time.UTC)}
			store := &migrationStore{current: copyMigrationBefore(&original)}
			patch := Capabilities{Enabled: false, Scopes: []string{"b", "a"}, Permissions: []string{"write"}, ExpiresAt: original.ExpiresAt}
			calls := 0
			service, err := NewService(store, func(context.Context, string) (string, error) {
				calls++
				if variant == "revoked" && calls > 1 {
					return "", ErrDenied
				}
				return "operator", nil
			})
			require.NoError(t, err)
			switch variant {
			case "semantic noop":
				patch.Enabled = true
				patch.Permissions = []string{"read"}
			case "absence":
				store.current = nil
			case "invalid duplicate":
				patch.Scopes = []string{"a", "a"}
			}
			result, err := service.PreviewCapabilities(context.Background(), subject, patch)
			require.Zero(t, store.writes)
			if variant == "invalid duplicate" || variant == "revoked" {
				require.Error(t, err)
				require.Zero(t, result)
				return
			}
			require.NoError(t, err)
			require.Equal(t, variant != "semantic noop", result.Changed)
			if variant == "absence" {
				require.Nil(t, result.Before)
				require.Zero(t, result.After.Tokens)
				require.Empty(t, result.After.Limits)
			} else {
				require.Equal(t, original.Tokens, result.After.Tokens)
				require.Equal(t, original.Limits, result.After.Limits)
				result.Before.Scopes[0] = "tamper"
				result.After.Limits["api"] = Limit{}
				require.Equal(t, "a", store.current.Scopes[0])
				require.Equal(t, original.Limits, store.current.Limits)
			}
			require.Equal(t, []string{"a", "b"}, result.After.Scopes)
			patch.Scopes[0] = "caller mutation"
			require.Equal(t, "a", result.After.Scopes[0])
		})
	}
}
