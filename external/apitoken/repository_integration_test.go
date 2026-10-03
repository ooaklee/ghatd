package apitoken

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// isolatedTokenRepository uses the managed repository/client against an
// explicitly supplied test server. Every case owns a newly named database;
// cleanup never drops a caller-supplied database or application collection.
func isolatedTokenRepository(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated MongoDB test server")
	}
	database := "ghatd_apitoken_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	manager, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, database))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	store := repository.NewMongoDbRepositoryWithDefaults(manager, database)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, manager.Close(cleanup))
	})
	return NewRepository(store), ctx
}

func TestRepositoryExactDigestLookup(t *testing.T) {
	for _, tc := range []struct {
		name, nano, secret string
		want               bool
	}{
		{"later credential beyond legacy page", "owner-prefix", "target-secret", true},
		{"other owner namespace", "other-prefix", "target-secret", false},
		{"wrong secret", "owner-prefix", "wrong-secret", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			collection, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			for i := 0; i < 40; i++ {
				digest := sha256.Sum256([]byte(fmt.Sprintf("test-secret-%d", i)))
				_, err := collection.InsertOne(ctx, UserAPIToken{ID: fmt.Sprintf("older-%d", i), CreatedByID: "owner", CreatedByNanoId: "owner-prefix", Status: UserTokenStatusKeyActive, ValueSHA: digest[:]})
				require.NoError(t, err)
			}
			digest := sha256.Sum256([]byte("target-secret"))
			_, err = collection.InsertOne(ctx, UserAPIToken{ID: "target", CreatedByID: "owner", CreatedByNanoId: "owner-prefix", Status: UserTokenStatusKeyActive, ValueSHA: digest[:]})
			require.NoError(t, err)
			lookup := sha256.Sum256([]byte(tc.secret))
			got, err := repo.GetAPITokenByDigest(ctx, tc.nano, lookup[:])
			if tc.want {
				require.NoError(t, err)
				require.Equal(t, "target", got.ID)
				require.Empty(t, got.Value)
			} else {
				require.ErrorIs(t, err, ErrUnableToValidateUserAPIToken)
				require.Nil(t, got)
			}
		})
	}
}

func TestRepositoryAtomicTouchPreservesAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, status, owner, tokenID string
		badDigest, concurrentRevoke  bool
		wantError                    bool
	}{
		{"active", UserTokenStatusKeyActive, "owner", "target", false, false, false},
		{"revoked", UserTokenStatusKeyRevoked, "owner", "target", false, false, true},
		{"wrong owner", UserTokenStatusKeyActive, "other", "target", false, false, true},
		{"wrong token", UserTokenStatusKeyActive, "owner", "missing", false, false, true},
		{"wrong digest", UserTokenStatusKeyActive, "owner", "target", true, false, true},
		{"concurrent revocation", UserTokenStatusKeyActive, "owner", "target", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			collection, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			digest := sha256.Sum256([]byte("test-secret"))
			before := UserAPIToken{ID: "target", CreatedByID: "owner", CreatedByNanoId: "prefix", Status: tc.status, Description: "unchanged", ValueSHA: digest[:], UpdatedAt: "unchanged", TtlExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)}
			_, err = collection.InsertOne(ctx, before)
			require.NoError(t, err)
			at := time.Date(2026, time.October, 2, 12, 0, 0, 123, time.UTC)
			passed := append([]byte(nil), digest[:]...)
			if tc.badDigest {
				passed[0] ^= 0xff
			}
			if tc.concurrentRevoke {
				var wg sync.WaitGroup
				start := make(chan struct{})
				touchErr := make(chan error, 1)
				revokeErr := make(chan error, 1)
				wg.Add(2)
				go func() {
					defer wg.Done()
					<-start
					touchErr <- repo.TouchAPIToken(ctx, tc.tokenID, tc.owner, passed, at)
				}()
				go func() {
					defer wg.Done()
					<-start
					_, err := collection.UpdateOne(ctx, bson.M{"_id": "target"}, bson.M{"$set": bson.M{"status": UserTokenStatusKeyRevoked}})
					revokeErr <- err
				}()
				close(start)
				wg.Wait()
				require.NoError(t, <-revokeErr)
				err = <-touchErr
				if err != nil {
					require.ErrorIs(t, err, ErrNoMatchingUserAPITokenFound)
				}
				before.Status = UserTokenStatusKeyRevoked
			} else {
				err = repo.TouchAPIToken(ctx, tc.tokenID, tc.owner, passed, at)
				if tc.wantError {
					require.ErrorIs(t, err, ErrNoMatchingUserAPITokenFound)
				} else {
					require.NoError(t, err)
					before.LastUsedAt = at.Format(time.RFC3339Nano)
				}
			}
			var after UserAPIToken
			require.NoError(t, collection.FindOne(ctx, bson.M{"_id": "target"}).Decode(&after))
			if tc.concurrentRevoke {
				require.Contains(t, []string{"", at.Format(time.RFC3339Nano)}, after.LastUsedAt)
				before.LastUsedAt = after.LastUsedAt
			}
			require.Equal(t, before, after, "usage telemetry must not alter authority or other fields")
			count, err := collection.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, int64(1), count, "no upsert for a mismatched credential")
		})
	}
}
