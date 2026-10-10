package accesspolicymanager

import (
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"testing"
)

// Native owner interoperability, not real browser or credential verification.
func TestMongoCapabilityAndTokenManagementPreserveEachOther(t *testing.T) {
	for _, first := range []string{"capabilities", "tokens"} {
		t.Run(first, func(t *testing.T) {
			ctx, core, manager, _ := managerMongoFixture(t)
			patch := accesspolicy.Capabilities{Enabled: true, Scopes: []string{"reviewed-scope"}, Permissions: []string{"exact-action"}}
			limits := accesspolicy.TokenLimits{Permanent: 2}
			db, err := core.GetDatabase(ctx, "")
			require.NoError(t, err)
			if first == "capabilities" {
				grant, e := manager.ApplyCapabilities(ctx, "selected", 0, patch)
				require.NoError(t, e)
				require.Zero(t, grant.Tokens)
				count, e := db.Collection(apitoken.InventoryFenceCollection).CountDocuments(ctx, bson.M{})
				require.NoError(t, e)
				require.Zero(t, count)
				_, err = manager.Apply(ctx, "selected", grant.Revision, limits)
			} else {
				grant, e := manager.Apply(ctx, "selected", 0, limits)
				require.NoError(t, e)
				require.Empty(t, grant.Scopes)
				_, err = manager.ApplyCapabilities(ctx, "selected", grant.Revision, patch)
			}
			require.NoError(t, err)
			review, err := manager.ReviewCapabilities(ctx, "selected")
			require.NoError(t, err)
			require.NotNil(t, review)
			require.Equal(t, int64(2), review.Revision)
			require.Equal(t, patch.Scopes, review.Scopes)
			require.Equal(t, patch.Permissions, review.Permissions)
			require.Equal(t, limits, review.Tokens)
			_, err = manager.ApplyCapabilities(ctx, "selected", 1, patch)
			require.ErrorIs(t, err, accesspolicy.ErrConflict)
			audits, err := db.Collection("access_policy_audit").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, int64(2), audits)
		})
	}
}
