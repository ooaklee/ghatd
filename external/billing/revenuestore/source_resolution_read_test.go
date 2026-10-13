package revenuestore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type resolutionReadSnapshot struct {
	original, resolution       recordstore.Record
	originalErr, resolutionErr error
}

type resolutionReadStore struct {
	recordstore.Store
	snapshots []resolutionReadSnapshot
	readError error
	callbacks []error
	reads     []string
	cancel    context.CancelFunc
}

func (s *resolutionReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	for _, snapshot := range s.snapshots {
		err := fn(resolutionReadTx{snapshot: snapshot, reads: &s.reads})
		s.callbacks = append(s.callbacks, err)
		if err != nil {
			return err
		}
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.readError
}

type resolutionReadTx struct {
	recordstore.Tx
	snapshot resolutionReadSnapshot
	reads    *[]string
}

func (tx resolutionReadTx) Get(_ context.Context, kind, id string) (recordstore.Record, error) {
	*tx.reads = append(*tx.reads, id)
	if kind != kindObservation {
		return recordstore.Record{}, errors.New("unexpected fixture record kind")
	}
	if id == "fixture-original" {
		return tx.snapshot.original, tx.snapshot.originalErr
	}
	return tx.snapshot.resolution, tx.snapshot.resolutionErr
}

type resolutionIsOnlyMissing struct{}

func (resolutionIsOnlyMissing) Error() string {
	return "fixture claims absence without a sentinel cause"
}
func (resolutionIsOnlyMissing) Is(target error) bool {
	return target == recordstore.ErrNotFound || target == billing.ErrRevenueNotFound
}

func TestSourceResolutionOwningReadOutcomes(t *testing.T) {
	original := billing.RevenueObservation{ID: "fixture-original", Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, EnvelopeID: "original-event", Fingerprint: "original-fingerprint", AcceptedAt: time.Unix(1700000000, 0).UTC(), QuarantineReason: "historical_payer_plan_pending"}
	resolution := billing.RevenueObservation{ID: original.ResolutionID(), Scope: original.Scope, EnvelopeID: original.EnvelopeID, Fingerprint: "resolution-fingerprint", AcceptedAt: original.AcceptedAt.Add(time.Minute), ResolutionOf: original.ID}
	originalRow, err := recordstore.NewRecord(kindObservation, original.ID, partition, 1, persistedObservation{Observation: original})
	require.NoError(t, err)
	resolutionRow, err := recordstore.NewRecord(kindObservation, resolution.ID, partition, 1, persistedObservation{Observation: resolution})
	require.NoError(t, err)
	absent := resolutionReadSnapshot{original: originalRow, resolutionErr: recordstore.ErrNotFound}
	present := resolutionReadSnapshot{original: originalRow, resolution: resolutionRow}
	failed := errors.New("fixture database failure")
	joined := errors.Join(recordstore.ErrNotFound, failed)
	joinedOnly := errors.Join(recordstore.ErrNotFound)
	nested := fmt.Errorf("lookup: %w", joined)
	isOnly := resolutionIsOnlyMissing{}
	boundary := error(recordstore.ErrNotFound)
	for range 31 {
		boundary = fmt.Errorf("lookup: %w", boundary)
	}
	tooDeep := fmt.Errorf("lookup: %w", boundary)
	commitFailure := &mongo.CommandError{Code: 91, Message: "fixture commit failure", Labels: []string{"UnknownTransactionCommitResult"}}
	malformedOriginal := originalRow
	malformedOriginal.Partition = "different-partition"
	malformedResolution := resolutionRow
	malformedResolution.ID = "different-resolution"
	cases := []struct {
		name            string
		snapshots       []resolutionReadSnapshot
		readError       error
		want            billing.RevenueObservation
		wantError       error
		callbacks       []error
		cancelAfterRead bool
	}{
		{name: "required_original_then_optional_absence", snapshots: []resolutionReadSnapshot{absent}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil}},
		{name: "required_original_absence_remains_callback_failure", snapshots: []resolutionReadSnapshot{{originalErr: recordstore.ErrNotFound}}, wantError: billing.ErrRevenueNotFound, callbacks: []error{billing.ErrRevenueNotFound}},
		{name: "required_original_failure", snapshots: []resolutionReadSnapshot{{originalErr: failed}}, wantError: failed, callbacks: []error{failed}},
		{name: "required_original_then_resolution_failure", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: failed}}, wantError: failed, callbacks: []error{failed}},
		{name: "wrapped_optional_absence", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: fmt.Errorf("lookup: %w", recordstore.ErrNotFound)}}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil}},
		{name: "single_chain_depth_boundary", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: boundary}}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil}},
		{name: "existing_resolution", snapshots: []resolutionReadSnapshot{present}, want: resolution, callbacks: []error{nil}},
		{name: "retry_absent_then_present", snapshots: []resolutionReadSnapshot{absent, present}, want: resolution, callbacks: []error{nil, nil}},
		{name: "retry_present_then_absent", snapshots: []resolutionReadSnapshot{present, absent}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil, nil}},
		{name: "retry_absent_present_absent", snapshots: []resolutionReadSnapshot{absent, present, absent}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil, nil, nil}},
		{name: "retry_present_absent_present", snapshots: []resolutionReadSnapshot{present, absent, present}, want: resolution, callbacks: []error{nil, nil, nil}},
		{name: "retry_absence_then_required_original_absence", snapshots: []resolutionReadSnapshot{absent, {originalErr: recordstore.ErrNotFound}}, wantError: billing.ErrRevenueNotFound, callbacks: []error{nil, billing.ErrRevenueNotFound}},
		{name: "joined_absence_and_failure", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: joined}}, wantError: joined, callbacks: []error{joined}},
		{name: "joined_absence_only", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: joinedOnly}}, wantError: joinedOnly, callbacks: []error{joinedOnly}},
		{name: "nested_joined_absence", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: nested}}, wantError: nested, callbacks: []error{nested}},
		{name: "custom_is_does_not_establish_absence", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: isOnly}}, wantError: isOnly, callbacks: []error{isOnly}},
		{name: "excessive_wrapper_depth_fails_closed", snapshots: []resolutionReadSnapshot{{original: originalRow, resolutionErr: tooDeep}}, wantError: tooDeep, callbacks: []error{tooDeep}},
		{name: "read_failure_after_absence", snapshots: []resolutionReadSnapshot{absent}, readError: failed, wantError: failed, callbacks: []error{nil}},
		{name: "commit_failure_after_absence", snapshots: []resolutionReadSnapshot{absent}, readError: commitFailure, wantError: commitFailure, callbacks: []error{nil}},
		{name: "commit_failure_after_present_discards_result", snapshots: []resolutionReadSnapshot{present}, readError: commitFailure, wantError: commitFailure, callbacks: []error{nil}},
		{name: "cancelled_read_after_absence", snapshots: []resolutionReadSnapshot{absent}, readError: context.Canceled, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "deadline_after_absence", snapshots: []resolutionReadSnapshot{absent}, readError: context.DeadlineExceeded, wantError: context.DeadlineExceeded, callbacks: []error{nil}},
		{name: "successful_custom_read_cancels_after_absence", snapshots: []resolutionReadSnapshot{absent}, cancelAfterRead: true, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "successful_custom_read_cancels_after_present", snapshots: []resolutionReadSnapshot{present}, cancelAfterRead: true, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "real_outer_error_precedes_late_cancellation", snapshots: []resolutionReadSnapshot{absent}, readError: failed, cancelAfterRead: true, wantError: failed, callbacks: []error{nil}},
		{name: "malformed_required_original", snapshots: []resolutionReadSnapshot{{original: malformedOriginal}}, wantError: billing.ErrRevenueUnavailable, callbacks: []error{billing.ErrRevenueUnavailable}},
		{name: "malformed_resolution", snapshots: []resolutionReadSnapshot{{original: originalRow, resolution: malformedResolution}}, wantError: billing.ErrRevenueUnavailable, callbacks: []error{billing.ErrRevenueUnavailable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &resolutionReadStore{snapshots: tc.snapshots, readError: tc.readError}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			if tc.cancelAfterRead {
				store.cancel = cancel
			}
			repo, err := NewRepository(store)
			require.NoError(t, err)
			got, err := repo.GetRevenueSourceResolution(ctx, original.ID)
			require.Equal(t, tc.want, got)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantError)
			}
			require.Equal(t, tc.callbacks, store.callbacks)
			for _, id := range store.reads {
				require.Contains(t, []string{original.ID, original.ResolutionID()}, id, "lookup must select only this original and its exact resolution")
			}
			if tc.readError != nil {
				require.False(t, errors.Is(err, billing.ErrRevenueNotFound), "outer failure must take precedence over captured absence")
			}
		})
	}
}
