package billinglifecycle

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// The same native read validates required original/forward/reverse provenance
// before its optional receipt. Monitor counts distinguish acknowledged reads
// from callback aborts; no receipt or other record is seeded to avoid absence.
func TestCheckoutReceiptReadEncryptedDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, missingKind string
		capture           bool
		want              error
	}{
		{name: "normal_receipt_absence", want: billing.ErrRevenueNotFound},
		{name: "captured_receipt", capture: true},
		{name: "missing_original", missingKind: "billing_checkout_intent", want: billing.ErrRevenueUnavailable},
		{name: "missing_forward_ack", missingKind: "billing_checkout_acknowledgement", want: billing.ErrRevenueUnavailable},
		{name: "missing_reverse_ack", missingKind: "billing_checkout_session", want: billing.ErrRevenueUnavailable},
		{name: "missing_joined_anchor", capture: true, missingKind: "billing_checkout_lifecycle_anchor", want: billing.ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var commits, aborts, failed atomic.Int64
			monitor := &event.CommandMonitor{
				Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
					switch e.CommandName {
					case "commitTransaction":
						commits.Add(1)
					case "abortTransaction":
						aborts.Add(1)
					}
				},
				Failed: func(context.Context, *event.CommandFailedEvent) { failed.Add(1) },
			}
			f := nativeInputFixtureWithMonitor(t, monitor)
			if tc.capture {
				_, err := f.native.CaptureCheckoutLifecycleEvidence(f.ctx, f.intent, f.provider.evidence)
				require.NoError(t, err)
			}
			if tc.missingKind != "" {
				deleted, err := f.db.Collection("ghatd_owned_records").DeleteOne(f.ctx, bson.M{"kind": tc.missingKind})
				require.NoError(t, err)
				require.EqualValues(t, 1, deleted.DeletedCount)
			}
			beforeCommits, beforeAborts, beforeFailed := commits.Load(), aborts.Load(), failed.Load()
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(f.ctx, zap.New(core))
			out, err := f.native.FindCheckoutLifecycleReceipt(ctx, f.intent)
			require.ErrorIs(t, err, tc.want)
			entries := logs.FilterMessage("mongo-operation-failed").All()
			t.Logf("receipt read: errors=%d commits=%d aborts=%d native failures=%d", len(entries), commits.Load()-beforeCommits, aborts.Load()-beforeAborts, failed.Load()-beforeFailed)
			require.Equal(t, beforeFailed, failed.Load())
			if tc.want == nil || tc.want == billing.ErrRevenueNotFound {
				require.Empty(t, entries)
				require.EqualValues(t, 1, commits.Load()-beforeCommits)
				require.Equal(t, beforeAborts, aborts.Load())
				if tc.want == nil {
					require.Equal(t, f.intent.ID, out.IntentID)
				} else {
					require.Zero(t, out)
				}
			} else {
				require.Zero(t, out)
				require.Len(t, entries, 1)
				require.Equal(t, zap.ErrorLevel, entries[0].Level)
				require.EqualValues(t, 1, entries[0].ContextMap()["callback_attempts"])
				require.Equal(t, "internal", entries[0].ContextMap()["last_callback_error_identity"])
				require.Equal(t, beforeCommits, commits.Load())
				require.EqualValues(t, 1, aborts.Load()-beforeAborts)
			}
			require.Zero(t, f.provider.calls, "receipt recovery performs no provider lookup")
			count, err := f.db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}

func TestCheckoutReceiptReadNativeCommitOutcomes(t *testing.T) {
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
		{name: "nonretryable_commit_failure", attempts: 1, failure: true},
		{name: "unknown_commit_retries_commit_only", labels: []string{"UnknownTransactionCommitResult"}, attempts: 1},
		{name: "transient_commit_retries_whole_snapshot", labels: []string{"TransientTransactionError"}, attempts: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reads, commits, failures atomic.Int64
			monitor := &event.CommandMonitor{
				Started: func(_ context.Context, e *event.CommandStartedEvent) {
					if e.CommandName == "find" {
						filter := e.Command.Lookup("filter").Document()
						if kind, ok := filter.Lookup("kind").StringValueOK(); ok && kind == "billing_checkout_lifecycle_receipt" {
							reads.Add(1)
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
			appName := "ghatd-checkout-receipt-" + rand.Text()
			f := nativeInputFixtureAtURI(t, uri, appName, monitor)
			admin := f.db.Client().Database("admin")
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
			beforeReads, beforeCommits, beforeFailures := reads.Load(), commits.Load(), failures.Load()
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(f.ctx, zap.New(core))
			out, err := f.native.FindCheckoutLifecycleReceipt(ctx, f.intent)
			require.Zero(t, out)
			require.Equal(t, tc.attempts, reads.Load()-beforeReads)
			require.EqualValues(t, 1, failures.Load()-beforeFailures)
			entries := logs.FilterMessage("mongo-operation-failed").All()
			if tc.failure {
				require.Error(t, err)
				require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
				var native mongo.ServerError
				require.ErrorAs(t, err, &native)
				require.True(t, native.HasErrorCode(2))
				require.Equal(t, beforeCommits, commits.Load())
				require.Len(t, entries, 1)
				require.Equal(t, "success", entries[0].ContextMap()["last_callback_error_identity"])
			} else {
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
				require.EqualValues(t, 1, commits.Load()-beforeCommits)
				require.Empty(t, entries)
			}
			t.Logf("receipt commit: reads=%d failed commits=%d acknowledged commits=%d errors=%d", reads.Load()-beforeReads, failures.Load()-beforeFailures, commits.Load()-beforeCommits, len(entries))
			disable()
			_, err = f.native.FindCheckoutLifecycleReceipt(ctx, f.intent)
			require.ErrorIs(t, err, billing.ErrRevenueNotFound)
			require.Len(t, logs.FilterMessage("mongo-operation-failed").All(), len(entries), "a subsequent healthy absence adds no error")
		})
	}
}

type receiptCancelStore struct {
	recordstore.Store
	cancel context.CancelFunc
}

func (s receiptCancelStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(receiptCancelTx{tx, s.cancel}) })
}

type receiptCancelTx struct {
	recordstore.Tx
	cancel context.CancelFunc
}

func (tx receiptCancelTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := tx.Tx.Get(ctx, kind, id)
	if kind == "billing_checkout_lifecycle_receipt" {
		tx.cancel()
	}
	return row, err
}

// Cancellation is injected immediately after the native optional receipt find;
// the native snapshot has already validated the original and both ack joins.
func TestCheckoutReceiptReadCancellationBeforeCommit(t *testing.T) {
	for _, captured := range []bool{false, true} {
		name := "absent_receipt"
		if captured {
			name = "existing_receipt"
		}
		t.Run(name, func(t *testing.T) {
			var commits atomic.Int64
			f := nativeInputFixtureWithMonitor(t, &event.CommandMonitor{Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
				if e.CommandName == "commitTransaction" {
					commits.Add(1)
				}
			}})
			if captured {
				_, err := f.native.CaptureCheckoutLifecycleEvidence(f.ctx, f.intent, f.provider.evidence)
				require.NoError(t, err)
			}
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(f.ctx, zap.New(core)))
			t.Cleanup(cancel)
			repo, err := revenuestore.NewRepository(receiptCancelStore{f.store, cancel})
			require.NoError(t, err)
			service, err := billing.NewCheckoutService(repo, f.clock, f.provider)
			require.NoError(t, err)
			before := commits.Load()
			out, err := service.FindCheckoutLifecycleReceipt(ctx, f.intent)
			require.ErrorIs(t, err, context.Canceled)
			require.False(t, errors.Is(err, billing.ErrRevenueNotFound))
			require.Zero(t, out)
			require.Equal(t, before, commits.Load())
			entries := logs.FilterMessage("mongo-operation-failed").All()
			require.Len(t, entries, 1)
			require.Equal(t, "cancelled", entries[0].ContextMap()["outcome"])
		})
	}
}
