package adminaccess

import (
	"context"
	"encoding/json"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type inertCapabilities struct{ inertPolicy }

func (inertCapabilities) ReviewCapabilities(context.Context, string) (*accesspolicy.Grant, error) {
	return nil, ErrUnavailable
}
func (inertCapabilities) PreviewCapabilities(context.Context, string, accesspolicy.Capabilities) (accesspolicy.CapabilityPreview, error) {
	return accesspolicy.CapabilityPreview{}, ErrUnavailable
}
func (inertCapabilities) ApplyCapabilities(context.Context, string, int64, accesspolicy.Capabilities) (accesspolicy.Grant, error) {
	return accesspolicy.Grant{}, ErrUnavailable
}

func TestCapabilityConfigurationAndReviewPurpose(t *testing.T) {
	for _, tc := range []struct {
		name           string
		kind           ReviewKind
		capPort, valid bool
	}{
		{"legacy default", "", false, true}, {"explicit token", TokenLimits, false, true}, {"unknown kind", "arbitrary", true, false}, {"missing optional owner", Capabilities, false, false}, {"capability optin", Capabilities, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := constructorConfig()
			c.ReviewKind = tc.kind
			if tc.capPort {
				c.Manager = inertCapabilities{}
			}
			b, err := New(c)
			if !tc.valid {
				require.ErrorIs(t, err, ErrUnavailable)
				return
			}
			require.NoError(t, err)
			if tc.kind != Capabilities {
				require.Equal(t, BasePath, b.basePath())
				require.Equal(t, codeHash("id", "code"), b.codeHash("id", "code"))
			} else {
				require.Equal(t, CapabilityBasePath, b.basePath())
				require.NotEqual(t, codeHash("id", "code"), b.codeHash("id", "code"))
			}
		})
	}
	for _, tc := range []struct {
		name  string
		kind  ReviewKind
		r     review
		valid bool
	}{
		{"old zero limits token", TokenLimits, review{}, true},
		{"old zero limits cannot become capability", Capabilities, review{}, false},
		{"cap purpose requires payload", Capabilities, review{Purpose: string(Capabilities)}, false},
		{"explicit empty replacement", Capabilities, review{Purpose: string(Capabilities), Capabilities: &accesspolicy.Capabilities{}}, true},
		{"cap payload cannot become token", TokenLimits, review{Purpose: string(Capabilities), Capabilities: &accesspolicy.Capabilities{}}, false},
		{"token fields cannot enter cap", Capabilities, review{Purpose: string(Capabilities), Capabilities: &accesspolicy.Capabilities{}, Limits: accesspolicy.TokenLimits{Permanent: 1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := &Bridge{config: Config{ReviewKind: tc.kind}}
			require.Equal(t, tc.valid, b.validReview(tc.r))
		})
	}
}

func TestCanonicalCapabilityComparison(t *testing.T) {
	original := accesspolicy.Capabilities{Enabled: true, ExpiresAt: time.Date(2030, 1, 1, 0, 0, 0, 123000000, time.UTC), Scopes: []string{"a", "b"}, Permissions: []string{"read", "write"}}
	for _, variant := range []string{"identical", "reordered", "same instant", "enabled", "expiry", "scopes", "permissions", "duplicate"} {
		t.Run(variant, func(t *testing.T) {
			raw, _ := json.Marshal(original)
			var patch accesspolicy.Capabilities
			require.NoError(t, json.Unmarshal(raw, &patch))
			same := false
			switch variant {
			case "identical":
				same = true
			case "reordered":
				patch.Scopes = []string{"b", "a"}
				patch.Permissions = []string{"write", "read"}
				same = true
			case "same instant":
				patch.ExpiresAt = patch.ExpiresAt.In(time.FixedZone("offset", 3600))
				same = true
			case "enabled":
				patch.Enabled = false
			case "expiry":
				patch.ExpiresAt = patch.ExpiresAt.Add(time.Millisecond)
			case "scopes":
				patch.Scopes = []string{"a"}
			case "permissions":
				patch.Permissions = []string{"read"}
			case "duplicate":
				patch.Scopes = []string{"a", "a"}
			}
			require.Equal(t, same, sameCapabilities(original, patch))
		})
	}
}
