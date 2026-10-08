package accesspolicy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestExplicitCapabilitiesPreservationAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		want          error
		writes        int
	}{
		{"create grants no tokens or budgets", "create", nil, 1},
		{"replace preserves unrelated allowances", "update", nil, 1},
		{"expired disabled grant can be reviewed and activated", "expired", nil, 1},
		{"explicit revocation retains accounting limits", "disable", nil, 1},
		{"stale review cannot overwrite later policy", "stale", ErrConflict, 0},
		{"joined absence is not permission to create", "joined", context.DeadlineExceeded, 0},
		{"revocation during read hides snapshot", "revoke-read", ErrDenied, 0},
		{"revocation before final write prevents mutation", "revoke-write", ErrDenied, 0},
		{"invalid wildcard does not authorize", "wildcard", ErrConfiguration, 0},
		{"api token target cannot borrow user management", "token", ErrConfiguration, 0},
		{"uncertain committed write returns no success", "uncertain", context.DeadlineExceeded, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			subject := Subject{System: "platform", Kind: UserSubject, ID: "selected-user"}
			original := Grant{Subject: subject, Revision: 4, Enabled: true, Scopes: []string{"old-scope"}, Permissions: []string{"old-action"}, Tokens: TokenLimits{Permanent: 2}, Limits: map[string]Limit{"api": {Maximum: 10, WindowSeconds: 60}}}
			store := &migrationStore{current: copyMigrationBefore(&original)}
			expected := int64(4)
			patch := Capabilities{Enabled: true, Scopes: []string{"selected-scope"}, Permissions: []string{"selected-action"}, ExpiresAt: time.Now().Add(time.Hour).UTC()}
			calls := 0
			service, err := NewService(store, func(context.Context, string) (string, error) {
				calls++
				if tc.variant == "revoke-read" && calls >= 2 || tc.variant == "revoke-write" && calls >= 3 {
					return "", ErrDenied
				}
				return "verified-operator", nil
			})
			require.NoError(t, err)
			switch tc.variant {
			case "create":
				store.current = nil
				expected = 0
			case "expired":
				store.current.Enabled = false
				store.current.ExpiresAt = time.Now().Add(-time.Hour)
			case "disable":
				patch.Enabled = false
			case "stale":
				expected = 3
			case "joined":
				store.current = nil
				expected = 0
				store.readErr = errors.Join(ErrDenied, context.DeadlineExceeded)
			case "wildcard":
				patch.Permissions = []string{"*"}
			case "token":
				subject.Kind = APITokenSubject
			case "uncertain":
				store.commitThenErr = context.DeadlineExceeded
			}
			result, err := service.ApplyCapabilities(ctx, subject, expected, patch)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.writes, store.writes)
			if tc.want != nil {
				require.Zero(t, result)
				if tc.variant == "uncertain" {
					require.Equal(t, int64(5), store.current.Revision)
				}
				return
			}
			require.Equal(t, expected+1, result.Revision)
			require.Equal(t, patch.Enabled, result.Enabled)
			require.Equal(t, patch.ExpiresAt, result.ExpiresAt)
			require.Equal(t, patch.Scopes, result.Scopes)
			require.Equal(t, patch.Permissions, result.Permissions)
			require.Equal(t, []string{"verified-operator"}, store.actors)
			if tc.variant == "create" {
				require.Zero(t, result.Tokens)
				require.Empty(t, result.Limits)
			} else {
				require.Equal(t, original.Tokens, result.Tokens)
				require.Equal(t, original.Limits, result.Limits)
			}
			patch.Scopes[0] = "caller-mutation"
			require.Equal(t, "selected-scope", store.current.Scopes[0])
		})
	}
}

func TestCapabilitiesReviewIsReadOnlyAndDetached(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		name := "active"
		if !enabled {
			name = "disabled-expired"
		}
		t.Run(name, func(t *testing.T) {
			subject := Subject{System: "platform", Kind: UserSubject, ID: "selected-user"}
			store := &migrationStore{current: &Grant{Subject: subject, Revision: 1, Enabled: enabled, Scopes: []string{"original"}, ExpiresAt: time.Now().Add(-time.Hour)}}
			service, err := NewService(store, func(context.Context, string) (string, error) { return "verified-operator", nil })
			require.NoError(t, err)
			review, err := service.ReviewGrant(context.Background(), subject)
			require.NoError(t, err)
			review.Scopes[0] = "edited-review"
			require.Equal(t, "original", store.current.Scopes[0])
			require.Zero(t, store.writes)
		})
	}
}
