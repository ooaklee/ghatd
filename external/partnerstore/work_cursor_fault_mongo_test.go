package partnerstore

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func TestMongoDiscoveryCursorCommitOutcomes(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_FAILPOINT_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_FAILPOINT_URI to a disposable replica set with enableTestCommands")
	}
	cases := []struct {
		name        string
		labels      []string
		wantError   bool
		wantFinds   int
		wantCommits int
	}{
		{name: "nonretryable_commit_failure", wantError: true, wantFinds: 1},
		{name: "unknown_commit_result_retries_commit", labels: []string{"UnknownTransactionCommitResult"}, wantFinds: 1, wantCommits: 1},
		{name: "transient_commit_failure_retries_callback", labels: []string{"TransientTransactionError"}, wantFinds: 2, wantCommits: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A separate opt-in server and unique application name prevent this
			// failpoint from affecting another fixture's transactions.
			appName := "ghatd-cursor-commit-" + rand.Text()
			repo, _, db, ctx, log, commands := mongoCursorFixtureAtURI(t, uri, appName, false)
			admin := db.Client().Database("admin")
			disable := func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				require.NoError(t, admin.RunCommand(cleanup, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: "off"}}).Err())
			}
			t.Cleanup(disable)
			beforeFinds := commands.succeeded["find"]
			beforeCommits := commands.succeeded["commitTransaction"]
			beforeFailures := commands.failed
			data := bson.M{"failCommands": []string{"commitTransaction"}, "errorCode": 2, "appName": appName}
			if len(tc.labels) != 0 {
				data["errorLabels"] = tc.labels
			}
			require.NoError(t, admin.RunCommand(ctx, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: bson.M{"times": 1}}, {Key: "data", Value: data}}).Err())
			cursor, err := repo.GetDiscoveryCursor(ctx, "fixture-program", partnermanager.WorkRevenueSource)
			require.Equal(t, partnermanager.DiscoveryCursor{}, cursor)
			require.Equal(t, 1, commands.failed-beforeFailures, "one native commit must fail")
			require.Equal(t, tc.wantFinds, commands.succeeded["find"]-beforeFinds)
			require.Equal(t, tc.wantCommits, commands.succeeded["commitTransaction"]-beforeCommits)
			if tc.wantError {
				require.Error(t, err)
				require.False(t, errors.Is(err, partnermanager.ErrNotFound))
				var native mongo.ServerError
				require.ErrorAs(t, err, &native)
				require.True(t, native.HasErrorCode(2))
				require.Len(t, log.errors, 1)
				require.Contains(t, log.errors[0].fields, repository.Field{Key: "operation", Value: "transaction"})
				require.Contains(t, log.errors[0].fields, repository.Field{Key: "outcome", Value: "database"})
				requireCursorDiagnosticFields(t, log, partnermanager.WorkRevenueSource, "internal", "success", 1)
			} else {
				require.ErrorIs(t, err, partnermanager.ErrNotFound)
				require.Empty(t, log.errors)
			}
			t.Logf("native commit outcome: failed commands=%d, successful reads=%d, successful commits=%d, repository errors=%d", commands.failed-beforeFailures, commands.succeeded["find"]-beforeFinds, commands.succeeded["commitTransaction"]-beforeCommits, len(log.errors))
			disable()
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindWorkCursor})
			require.NoError(t, err)
			require.Zero(t, count)
			priorLogs := len(log.errors)
			_, err = repo.GetDiscoveryCursor(ctx, "fixture-program", partnermanager.WorkRevenueSource)
			require.ErrorIs(t, err, partnermanager.ErrNotFound)
			require.Len(t, log.errors, priorLogs, "subsequent healthy absence must add no failure log")
		})
	}
}

type cursorCancelReadStore struct {
	recordstore.Store
	cancel context.CancelFunc
}

func (s cursorCancelReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(cursorCancelReadTx{tx, s.cancel}) })
}

type cursorCancelReadTx struct {
	recordstore.Tx
	cancel context.CancelFunc
}

func (tx cursorCancelReadTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := tx.Tx.Get(ctx, kind, id)
	tx.cancel()
	return row, err
}

func TestMongoDiscoveryCursorCancellationBeforeCommit(t *testing.T) {
	cases := []struct{ name, kind string }{
		{"signup", partnermanager.WorkSignup},
		{"revenue_source", partnermanager.WorkRevenueSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx, log, commands := mongoCursorFixture(t, false)
			op, cancel := context.WithCancel(ctx)
			t.Cleanup(cancel)
			repo, err := NewWorkRepository(cursorCancelReadStore{repo.store, cancel})
			require.NoError(t, err)
			beforeFinds := commands.succeeded["find"]
			beforeCommits := commands.succeeded["commitTransaction"]
			_, err = repo.GetDiscoveryCursor(op, "fixture-program", tc.kind)
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, errors.Is(err, partnermanager.ErrNotFound))
			require.Equal(t, 1, commands.succeeded["find"]-beforeFinds, "cancellation occurs after the native find succeeds")
			require.Equal(t, beforeCommits, commands.succeeded["commitTransaction"], "cancelled read must not commit")
			require.Len(t, log.errors, 1)
			require.Contains(t, log.errors[0].fields, repository.Field{Key: "outcome", Value: "cancelled"})
			requireCursorDiagnosticFields(t, log, tc.kind, "cancelled", "cancelled", 1)
			t.Logf("post-find cancellation: repository errors=%d, successful reads=%d, no new committed transaction", len(log.errors), commands.succeeded["find"]-beforeFinds)
		})
	}
}
