package apitoken

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestRepositoryRetainedInventoryAndNonMutatingReads(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		ephemeral, permanent bool
		want                 int
	}{
		{"all stored states", false, false, 138}, {"permanent legacy forms", false, true, 133}, {"ephemeral including expired and invalid", true, false, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			collection, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			for i := 0; i < 130; i++ {
				_, err = collection.InsertOne(ctx, UserAPIToken{ID: fmt.Sprintf("permanent-%03d", i), CreatedByID: "owner", Status: UserTokenStatusKeyActive})
				require.NoError(t, err)
			}
			for i, expiry := range []any{nil, "", nil, time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano), "malformed", "2020-01-01T00:00:00Z", "2020-01-02T00:00:00Z"} {
				doc := bson.M{"_id": fmt.Sprintf("edge-%d", i), "created_by_id": "owner", "status": UserTokenStatusKeyRevoked, "ttl_expires_at": expiry}
				_, err = collection.InsertOne(ctx, doc)
				require.NoError(t, err)
			}
			_, err = collection.InsertOne(ctx, UserAPIToken{ID: "other", CreatedByID: "other-owner", Status: UserTokenStatusKeyActive})
			require.NoError(t, err)
			service := NewService(repo)
			inventory, err := service.CountTokenInventory(ctx, "owner")
			require.NoError(t, err)
			require.Equal(t, Inventory{133, 5}, inventory)
			for pass := 0; pass < 2; pass++ {
				got, err := service.GetAPITokensFor(ctx, &GetAPITokensForRequest{ID: "owner", OnlyEphemeral: tc.ephemeral, OnlyPermanent: tc.permanent, PerPage: 100})
				require.NoError(t, err)
				require.Equal(t, tc.want, got.Total)
				require.Len(t, got.APITokens, min(tc.want, 100))
				all, err := service.GetAPITokens(ctx, &GetAPITokensRequest{CreatedByID: "owner", OnlyEphemeral: tc.ephemeral, OnlyPermanent: tc.permanent, PerPage: 100})
				require.NoError(t, err)
				require.Equal(t, got.Total, all.Total)
				single, err := service.GetAPIToken(ctx, &GetAPITokenRequest{ID: "edge-4"})
				require.NoError(t, err)
				require.Equal(t, "edge-4", single.APIToken.ID)
				count, err := collection.CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.Equal(t, int64(139), count)
			}
		})
	}
}

func TestRepositoryLiteralSearchFilters(t *testing.T) {
	for _, field := range []string{"description", "status"} {
		t.Run(field, func(t *testing.T) {
			for _, needle := range []string{".*", "[", "(a+)+$", "a.b", "MIXED"} {
				t.Run(needle, func(t *testing.T) {
					repo, ctx := isolatedTokenRepository(t)
					collection, err := repo.GetApiTokenCollection(ctx)
					require.NoError(t, err)
					_, err = collection.InsertMany(ctx, []any{
						bson.M{"_id": "matching", "created_by_id": "owner", field: "before " + strings.ToLower(needle) + " after"},
						bson.M{"_id": "unrelated", "created_by_id": "owner", field: "acb unrelated"},
						bson.M{"_id": "foreign", "created_by_id": "other", field: needle},
					})
					require.NoError(t, err)
					request := &GetAPITokensRequest{CreatedByID: "owner", Page: 1, PerPage: 25}
					if field == "description" {
						request.Description = needle
					} else {
						request.Status = needle
					}
					count, err := repo.GetTotalApiTokens(ctx, "owner", "", request.Description, request.Status, "", "", false, false)
					require.NoError(t, err)
					require.Equal(t, int64(1), count)
					found, err := repo.GetAPITokens(ctx, request)
					require.NoError(t, err)
					require.Len(t, found, 1)
					require.Equal(t, "matching", found[0].ID)
				})
			}
		})
	}
}

func TestRepositoryOwnerBoundMutations(t *testing.T) {
	for _, tc := range []struct {
		name, owner, status string
		delete              bool
		want                error
	}{
		{"owner delete", "owner", "", true, nil}, {"wrong owner delete", "other", "", true, ErrResourceNotFound}, {"missing owner delete", "", "", true, ErrResourceNotFound},
		{"owner revoke", "owner", UserTokenStatusKeyRevoked, false, nil}, {"owner activate", "owner", UserTokenStatusKeyActive, false, nil},
		{"wrong owner revoke", "other", UserTokenStatusKeyRevoked, false, ErrResourceNotFound}, {"wrong owner activate", "other", UserTokenStatusKeyActive, false, ErrResourceNotFound}, {"missing owner status", "", UserTokenStatusKeyActive, false, ErrResourceNotFound}, {"invalid status", "owner", "UNKNOWN", false, ErrTokenStatusInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			collection, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			original := UserAPIToken{ID: "target", CreatedByID: "owner", Status: UserTokenStatusKeyActive, Description: "keep", ValueSHA: []byte("stored-digest"), TtlExpiresAt: "2000-01-01T00:00:00Z", LastUsedAt: "keep-time"}
			_, err = collection.InsertOne(ctx, original)
			require.NoError(t, err)
			if tc.delete {
				err = repo.DeleteAPITokenFor(ctx, tc.owner, "target")
			} else {
				err = repo.SetAPITokenStatusFor(ctx, tc.owner, "target", tc.status)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
			if tc.delete && tc.want == nil {
				count, err := collection.CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.Zero(t, count)
				return
			}
			var got UserAPIToken
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": "target"}).Decode(&got))
			if tc.want == nil {
				original.Status = tc.status
				require.NotEmpty(t, got.UpdatedAt)
				original.UpdatedAt = got.UpdatedAt
			}
			require.Equal(t, original, got)
		})
	}
}
