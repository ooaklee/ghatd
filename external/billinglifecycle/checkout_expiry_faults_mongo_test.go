package billinglifecycle

import (
	"context"
	"crypto/rand"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
)

func TestCheckoutExpiryNativeCommitOutcomes(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_FAILPOINT_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_FAILPOINT_URI to a disposable replica set with enableTestCommands")
	}
	for _, tc := range []struct {
		name     string
		labels   []string
		attempts int64
		failure  bool
	}{
		{"nonretryable_commit_failure", nil, 1, true},
		{"unknown_commit_retries_commit_only", []string{"UnknownTransactionCommitResult"}, 1, false},
		{"transient_commit_retries_transaction", []string{"TransientTransactionError"}, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var jobs, commits, failures atomic.Int64
			monitor := &event.CommandMonitor{
				Started: func(_ context.Context, e *event.CommandStartedEvent) {
					if e.CommandName == "find" {
						if kind, ok := e.Command.Lookup("filter").Document().Lookup("kind").StringValueOK(); ok && kind == scheduleJobKind {
							jobs.Add(1)
						}
					}
				},
				Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
					if e.CommandName == "commitTransaction" {
						commits.Add(1)
					}
				},
				Failed: func(_ context.Context, e *event.CommandFailedEvent) {
					if e.CommandName == "commitTransaction" {
						failures.Add(1)
					}
				},
			}
			appName := "ghatd-checkout-expiry-" + rand.Text()
			base := nativeInputFixtureAtURI(t, uri, appName, monitor)
			f, completion, o := nativeExpiryObservation(t, base)
			admin := base.db.Client().Database("admin")
			disable := func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				require.NoError(t, admin.RunCommand(ctx, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: "off"}}).Err())
			}
			t.Cleanup(disable)
			data := bson.M{"failCommands": []string{"commitTransaction"}, "errorCode": 2, "appName": appName}
			if len(tc.labels) > 0 {
				data["errorLabels"] = tc.labels
			}
			require.NoError(t, admin.RunCommand(f.ctx, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: bson.M{"times": 1}}, {Key: "data", Value: data}}).Err())
			beforeJobs, beforeCommits, beforeFailures := jobs.Load(), commits.Load(), failures.Load()
			out, err := f.repo.ExpireCheckout(f.ctx, o)
			require.Equal(t, tc.attempts, jobs.Load()-beforeJobs)
			require.EqualValues(t, 1, failures.Load()-beforeFailures)
			if tc.failure {
				require.Error(t, err)
				require.Zero(t, out)
				require.Equal(t, beforeCommits, commits.Load())
			} else {
				require.NoError(t, err)
				require.NotNil(t, out.Expiry)
				require.EqualValues(t, 1, commits.Load()-beforeCommits)
			}
			t.Logf("expiry commit: callbacks=%d failed commits=%d acknowledged commits=%d", jobs.Load()-beforeJobs, failures.Load()-beforeFailures, commits.Load()-beforeCommits)
			disable()
			// Advance the same native session after a simulated commit error before
			// inspecting durable state or dropping the fixture database.
			require.NoError(t, base.store.Read(f.ctx, func(tx recordstore.Tx) error {
				_, err := tx.Get(f.ctx, scheduleJobKind, scheduledIdentity(o.Job.Source))
				return err
			}))
			for _, kind := range []string{completionKind, "billing_checkout_lifecycle_receipt"} {
				n, err := base.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				if tc.failure {
					require.Zero(t, n)
				} else {
					require.EqualValues(t, 1, n)
				}
			}
			if tc.failure {
				_, err = f.repo.ExpireCheckout(f.ctx, o)
				require.NoError(t, err)
			}
			calls := f.calls.Load()
			got, err := completion.InspectLast(f.ctx, o.Job.Source)
			require.NoError(t, err)
			require.NotNil(t, got.Expiry)
			require.Equal(t, o.Expiry.Fingerprint, got.Expiry.Fingerprint)
			require.Equal(t, calls, f.calls.Load(), "replay performs no provider request")
			_, err = f.repo.ExpireCheckout(f.ctx, o)
			require.ErrorIs(t, err, recordstore.ErrConflict)
		})
	}
}
