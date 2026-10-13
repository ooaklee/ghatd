package accesspolicy

import (
	"context"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"os"
	"sync"
	"testing"
	"time"
)

// A borrowed reader can enforce a prepared grant without startup's write probe.
// Every store mutation/fenced callback is refused before touching the database.
func TestMongoBorrowedPolicyReader(t *testing.T) {
	for _, name := range []string{"prepared", "missing_collection", "revocation_observed"} {
		t.Run(name, func(t *testing.T) {
			original, ctx := isolatedPolicyStore(t)
			grant := seedPolicy(t, original, ctx)
			if name == "missing_collection" {
				require.NoError(t, original.collection(auditCollection).Drop(ctx))
			}
			var mu sync.Mutex
			mutations := []string{}
			monitor := &event.CommandMonitor{Started: func(_ context.Context, e *event.CommandStartedEvent) {
				switch e.CommandName {
				case "insert", "update", "delete", "findAndModify", "create", "createIndexes", "drop", "dropDatabase", "collMod":
					mu.Lock()
					mutations = append(mutations, e.CommandName)
					mu.Unlock()
				}
			}}
			client, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("GHATD_TEST_MONGO_URI")).SetMonitor(monitor))
			require.NoError(t, err)
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, client.Disconnect(ctx))
			})
			repo, err := repository.NewMongoDbRepositoryFromDatabase(client.Database(original.database.Name()), nil)
			require.NoError(t, err)
			reader, err := NewMongoReadStore(ctx, repo)
			if name == "missing_collection" {
				require.ErrorIs(t, err, ErrConfiguration)
				require.Nil(t, reader)
			} else {
				require.NoError(t, err)
				svc, err := NewService(reader, nil)
				require.NoError(t, err)
				require.NoError(t, svc.Authorize(ctx, grant.Subject, grant.Scopes, grant.Permissions))
				called := false
				_, err = reader.Replace(ctx, grant, grant.Revision, "actor", time.Now())
				require.ErrorIs(t, err, ErrConfiguration)
				err = reader.WithGrant(ctx, grant.Subject, time.Now(), func(context.Context, Grant) error { called = true; return nil })
				require.ErrorIs(t, err, ErrConfiguration)
				require.False(t, called)
				_, err = reader.Consume(ctx, consumptionFixture(), time.Now())
				require.ErrorIs(t, err, ErrConfiguration)
				_, err = reader.WithConsumption(ctx, consumptionFixture(), time.Now(), ConsumptionAction{Check: func(context.Context) error { called = true; return nil }, Apply: func(context.Context, Usage) error { called = true; return nil }})
				require.ErrorIs(t, err, ErrConfiguration)
				require.False(t, called)
				if name == "revocation_observed" {
					grant.Enabled = false
					expected := grant.Revision
					grant.Revision++
					_, err = original.Replace(ctx, grant, expected, "admin", time.Now())
					require.NoError(t, err)
					require.ErrorIs(t, svc.Authorize(ctx, grant.Subject, grant.Scopes, grant.Permissions), ErrDenied)
				}
				count, err := original.collection(grantCollection).CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
			}
			mu.Lock()
			seen := append([]string{}, mutations...)
			mu.Unlock()
			require.Empty(t, seen)
		})
	}
}
