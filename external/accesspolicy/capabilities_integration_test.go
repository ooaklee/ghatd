package accesspolicy

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoCapabilitiesConcurrentRevisionAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name           string
		seed, disabled bool
	}{
		{"competing first provision", false, false},
		{"competing capability update preserves allowances", true, false},
		{"explicit activation of disabled expired policy", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			service := migrationService(t, store)
			original := policyFixture()
			expected := int64(0)
			if tc.seed {
				if tc.disabled {
					original.Enabled = false
					original.ExpiresAt = time.Unix(100, 0)
				}
				var err error
				original, err = store.Replace(ctx, original, 0, "original-operator", time.Now())
				require.NoError(t, err)
				expected = original.Revision
			}
			initialAudits := collectionCount(t, store, ctx, auditCollection)
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for _, permission := range []string{"action-one", "action-two"} {
				wg.Go(func() {
					<-start
					_, err := service.ApplyCapabilities(ctx, original.Subject, expected, Capabilities{Enabled: true, Scopes: []string{"reviewed-scope"}, Permissions: []string{permission}})
					results <- err
				})
			}
			close(start)
			wg.Wait()
			close(results)
			success, conflict := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.True(t, errors.Is(err, ErrConflict), "unexpected competing write result: %v", err)
					conflict++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, conflict)
			stored, err := store.Read(ctx, original.Subject)
			require.NoError(t, err)
			require.Equal(t, expected+1, stored.Revision)
			require.True(t, stored.Enabled)
			require.True(t, stored.ExpiresAt.IsZero())
			if tc.seed {
				require.Equal(t, original.Tokens, stored.Tokens)
				require.Equal(t, original.Limits, stored.Limits)
			} else {
				require.Zero(t, stored.Tokens)
				require.Empty(t, stored.Limits)
			}
			require.Equal(t, initialAudits+1, collectionCount(t, store, ctx, auditCollection))
			var audit mongoAudit
			require.NoError(t, store.collection(auditCollection).FindOne(ctx, bson.M{"grant.revision": stored.Revision}).Decode(&audit))
			require.Equal(t, "migration-operator", audit.Actor)
			require.Equal(t, expected, audit.Expected)
			// Lost receipt recovery is a review, never an unconditional repeat write.
			review, err := service.ReviewGrant(ctx, original.Subject)
			require.NoError(t, err)
			require.True(t, sameMigrationGrant(*review, stored))
			_, err = service.ApplyCapabilities(ctx, original.Subject, expected, Capabilities{Enabled: true, Scopes: stored.Scopes, Permissions: stored.Permissions})
			require.ErrorIs(t, err, ErrConflict)
			require.Equal(t, initialAudits+1, collectionCount(t, store, ctx, auditCollection))
		})
	}
}
