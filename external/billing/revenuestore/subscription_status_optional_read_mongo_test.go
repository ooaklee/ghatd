package revenuestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type statusHeadReadStore struct {
	recordstore.Store
	lastHeadError, resultError error
	repeat, noCallback         bool
	cancelAfter                context.CancelFunc
}
type statusHeadReadTx struct {
	recordstore.Tx
	headError error
}

func (tx statusHeadReadTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == kindSubscriptionStatusHead && tx.headError != nil {
		return recordstore.Record{}, tx.headError
	}
	return tx.Tx.Get(ctx, kind, id)
}
func (s statusHeadReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	if s.noCallback {
		return s.resultError
	}
	err := s.Store.Read(ctx, func(tx recordstore.Tx) error {
		if s.repeat {
			if err := fn(tx); err != nil {
				return err
			}
		}
		return fn(statusHeadReadTx{tx, s.lastHeadError})
	})
	if err != nil {
		return err
	}
	if s.cancelAfter != nil {
		s.cancelAfter()
	}
	return s.resultError
}

// Audit disposition: each named case owns encrypted native status records and
// fault ports. First-head absence differs from required receipt loss/outages.
func TestMongoSubscriptionStatusOptionalHeadRead(t *testing.T) {
	for _, tc := range []struct {
		name, fault string
		seed        bool
		want        error
		logs        int
	}{
		{name: "first_head_absence_commits_without_error_log", want: billing.ErrRevenueNotFound},
		{name: "repeated_absence_callback_resets_state", fault: "repeat", want: billing.ErrRevenueNotFound},
		{name: "last_absent_callback_withholds_prior_head", fault: "repeat-found-absent", seed: true, want: billing.ErrRevenueNotFound},
		{name: "wrapped_head_absence", fault: "wrapped", want: billing.ErrRevenueNotFound},
		{name: "existing_head", seed: true},
		{name: "missing_required_capture", seed: true, fault: "missing-capture", want: billing.ErrRevenueUnavailable, logs: 1},
		{name: "corrupt_required_capture", seed: true, fault: "corrupt-capture", logs: 1},
		{name: "head_driver_failure", fault: "driver", logs: 1},
		{name: "cancel_after_absent_head_get", fault: "cancel", want: context.Canceled, logs: 1},
		{name: "late_read_failure_is_not_absence", fault: "late", want: recordstore.ErrUnavailable, logs: 1},
		{name: "mixed_absence_outage_is_not_absence", fault: "mixed", want: recordstore.ErrUnavailable, logs: 1},
		{name: "head_get_mixed_absence_outage_is_not_absence", fault: "mixed-get", want: recordstore.ErrUnavailable, logs: 1},
		{name: "unknown_commit_reply_is_not_absence", fault: "commit", want: recordstore.ErrUncertain},
		{name: "mixed_unknown_commit_reply_is_not_absence", fault: "mixed-commit", want: recordstore.ErrUncertain},
		{name: "store_without_callback_cannot_infer_absence", fault: "no-callback", want: billing.ErrRevenueUnavailable},
		{name: "store_not_found_without_callback_is_snapshot_failure", fault: "read-not-found", want: billing.ErrRevenueUnavailable},
		{name: "cancel_after_successful_native_read", fault: "cancel-after-read", want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newOptionalNativeFixture(t, os.Getenv("GHATD_TEST_MONGO_URI"))
			service, _, factID := nativeStatus(t, f.repo, f.ctx)
			fact, err := service.GetRevenueFact(f.ctx, factID)
			require.NoError(t, err)
			var p billing.SubscriptionStatusPreparation
			if tc.seed {
				p, err = service.PrepareSubscriptionStatus(f.ctx, "fixture-worker", factID)
				require.NoError(t, err)
				_, err = service.CaptureVerifiedSubscriptionStatus(f.ctx, p, nativeStatusEvidence(p, "active"))
				require.NoError(t, err)
			}
			f.port.kind, f.port.id = kindSubscriptionStatusHead, billing.SubscriptionStatusIdentity(fact.Scope, fact.SubscriptionID)
			ctx := f.ctx
			if tc.fault == "missing-capture" {
				_, err = f.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindSubscriptionStatusCapture})
				require.NoError(t, err)
			} else if tc.fault == "corrupt-capture" {
				_, err = f.db.Collection("ghatd_owned_records").UpdateOne(ctx, bson.M{"kind": kindSubscriptionStatusCapture}, bson.M{"$set": bson.M{"payload": []byte("unreadable-test-payload")}})
				require.NoError(t, err)
			} else if tc.fault == "driver" {
				f.port.failFind = true
			} else if tc.fault == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				t.Cleanup(cancel)
				f.port.cancel = cancel
			}
			adapter := f.repo
			if tc.fault == "repeat" || tc.fault == "late" || tc.fault == "mixed" {
				wrapped := &discoveryReadStore{Store: f.repo.store, repeat: tc.fault == "repeat"}
				if tc.fault == "late" {
					wrapped.lateError = recordstore.ErrUnavailable
				}
				if tc.fault == "mixed" {
					wrapped.lateError = errors.Join(recordstore.ErrNotFound, recordstore.ErrUnavailable)
				}
				adapter, err = NewRepository(wrapped)
				require.NoError(t, err)
			}
			if tc.fault == "repeat-found-absent" || tc.fault == "wrapped" || tc.fault == "mixed-get" || tc.fault == "commit" || tc.fault == "mixed-commit" || tc.fault == "no-callback" || tc.fault == "read-not-found" || tc.fault == "cancel-after-read" {
				wrapped := statusHeadReadStore{Store: f.repo.store}
				switch tc.fault {
				case "repeat-found-absent":
					wrapped.repeat, wrapped.lastHeadError = true, recordstore.ErrNotFound
				case "wrapped":
					wrapped.lastHeadError = fmt.Errorf("wrapped owning absence: %w", recordstore.ErrNotFound)
				case "mixed-get":
					wrapped.lastHeadError = errors.Join(recordstore.ErrNotFound, recordstore.ErrUnavailable)
				case "commit":
					wrapped.resultError = fmt.Errorf("lost native read commit reply: %w", recordstore.ErrUncertain)
				case "mixed-commit":
					wrapped.resultError = errors.Join(recordstore.ErrNotFound, recordstore.ErrUncertain)
				case "no-callback":
					wrapped.noCallback = true
				case "read-not-found":
					wrapped.noCallback, wrapped.resultError = true, recordstore.ErrNotFound
				case "cancel-after-read":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					t.Cleanup(cancel)
					wrapped.cancelAfter = cancel
				}
				adapter, err = NewRepository(wrapped)
				require.NoError(t, err)
			}
			f.logs.TakeAll()
			before, _ := f.commands.snapshot()
			out, err := adapter.ReadSubscriptionStatus(ctx, fact.Scope, fact.SubscriptionID)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else if tc.logs != 0 {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.seed && tc.fault == "" {
				require.Equal(t, p.CaptureID, out.Current.Preparation.CaptureID)
			} else {
				require.Zero(t, out)
			}
			if tc.fault == "late" || tc.fault == "mixed" || tc.fault == "mixed-get" || tc.fault == "cancel" || tc.fault == "commit" || tc.fault == "mixed-commit" || tc.fault == "read-not-found" || tc.fault == "cancel-after-read" {
				require.NotErrorIs(t, err, billing.ErrRevenueNotFound)
			}
			require.Equal(t, tc.logs, f.logs.FilterMessage("mongo-operation-failed").Len())
			after, _ := f.commands.snapshot()
			if tc.logs == 0 && tc.fault != "no-callback" && tc.fault != "read-not-found" && tc.fault != "wrapped" {
				require.Equal(t, 1, after["commitTransaction"]-before["commitTransaction"])
			}
		})
	}
}
