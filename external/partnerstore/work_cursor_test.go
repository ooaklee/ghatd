package partnerstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type cursorReadResult struct {
	row recordstore.Record
	err error
}

type cursorReadStore struct {
	recordstore.Store
	results   []cursorReadResult
	readError error
	callbacks []error
}

func (s *cursorReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	for _, result := range s.results {
		err := fn(cursorReadTx{result: result})
		s.callbacks = append(s.callbacks, err)
		if err != nil {
			return err
		}
	}
	return s.readError
}

type cursorReadTx struct {
	recordstore.Tx
	result cursorReadResult
}

func (t cursorReadTx) Get(context.Context, string, string) (recordstore.Record, error) {
	return t.result.row, t.result.err
}

type cursorIsOnlyNotFound struct{}

func (cursorIsOnlyNotFound) Error() string { return "fixture claims absence without a sentinel cause" }
func (cursorIsOnlyNotFound) Is(target error) bool {
	return target == recordstore.ErrNotFound || target == partnermanager.ErrNotFound
}

func TestDiscoveryCursorReadOutcomes(t *testing.T) {
	cursor := partnermanager.DiscoveryCursor{Revision: 1, AfterID: "source"}
	data, err := json.Marshal(cursor)
	require.NoError(t, err)
	row := recordstore.Record{ID: workDigest("program", partnermanager.WorkSignup), Kind: kindWorkCursor, Partition: workPartition("program", partnermanager.WorkSignup), Revision: 1, Data: data}
	absent := cursorReadResult{err: recordstore.ErrNotFound}
	present := cursorReadResult{row: row}
	failed := errors.New("fixture database failure")
	joined := errors.Join(recordstore.ErrNotFound, failed)
	joinedOnly := errors.Join(recordstore.ErrNotFound)
	nestedJoined := fmt.Errorf("lookup: %w", joined)
	isOnly := cursorIsOnlyNotFound{}
	wrappedBoundary := error(recordstore.ErrNotFound)
	for range 31 {
		wrappedBoundary = fmt.Errorf("lookup: %w", wrappedBoundary)
	}
	tooDeep := fmt.Errorf("lookup: %w", wrappedBoundary)
	commitFailed := &mongo.CommandError{Code: 91, Message: "fixture commit failure", Labels: []string{"UnknownTransactionCommitResult"}}
	malformed := row
	malformed.Partition = "different-program"
	cases := []struct {
		name      string
		results   []cursorReadResult
		readError error
		want      partnermanager.DiscoveryCursor
		wantError error
		callbacks []error
	}{
		{name: "absent", results: []cursorReadResult{absent}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil}},
		{name: "wrapped_absence", results: []cursorReadResult{{err: fmt.Errorf("lookup: %w", recordstore.ErrNotFound)}}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil}},
		{name: "single_chain_depth_boundary", results: []cursorReadResult{{err: wrappedBoundary}}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil}},
		{name: "existing", results: []cursorReadResult{present}, want: cursor, callbacks: []error{nil}},
		{name: "retry_absent_then_present", results: []cursorReadResult{absent, present}, want: cursor, callbacks: []error{nil, nil}},
		{name: "retry_present_then_absent", results: []cursorReadResult{present, absent}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil, nil}},
		{name: "retry_absent_present_absent", results: []cursorReadResult{absent, present, absent}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil, nil, nil}},
		{name: "retry_present_absent_present", results: []cursorReadResult{present, absent, present}, want: cursor, callbacks: []error{nil, nil, nil}},
		{name: "joined_absence_and_failure", results: []cursorReadResult{{err: joined}}, wantError: joined, callbacks: []error{joined}},
		{name: "joined_absence_only", results: []cursorReadResult{{err: joinedOnly}}, wantError: joinedOnly, callbacks: []error{joinedOnly}},
		{name: "nested_joined_absence_and_failure", results: []cursorReadResult{{err: nestedJoined}}, wantError: nestedJoined, callbacks: []error{nestedJoined}},
		{name: "custom_is_cannot_establish_absence", results: []cursorReadResult{{err: isOnly}}, wantError: isOnly, callbacks: []error{isOnly}},
		{name: "excessive_wrapper_depth_fails_closed", results: []cursorReadResult{{err: tooDeep}}, wantError: tooDeep, callbacks: []error{tooDeep}},
		{name: "database_failure", results: []cursorReadResult{{err: failed}}, wantError: failed, callbacks: []error{failed}},
		{name: "read_failure_after_absence", results: []cursorReadResult{absent}, readError: failed, wantError: failed, callbacks: []error{nil}},
		{name: "cancelled_read_after_absence", results: []cursorReadResult{absent}, readError: context.Canceled, wantError: context.Canceled, callbacks: []error{nil}},
		{name: "deadline_after_absence", results: []cursorReadResult{absent}, readError: context.DeadlineExceeded, wantError: context.DeadlineExceeded, callbacks: []error{nil}},
		{name: "commit_failure_after_absence", results: []cursorReadResult{absent}, readError: commitFailed, wantError: commitFailed, callbacks: []error{nil}},
		{name: "commit_failure_after_present", results: []cursorReadResult{present}, readError: commitFailed, want: cursor, wantError: commitFailed, callbacks: []error{nil}},
		{name: "uncertain_read_after_absence", results: []cursorReadResult{absent}, readError: recordstore.ErrUncertain, wantError: partnermanager.ErrWorkUncertain, callbacks: []error{nil}},
		{name: "unavailable_read_after_absence", results: []cursorReadResult{absent}, readError: recordstore.ErrUnavailable, wantError: partnermanager.ErrUnavailable, callbacks: []error{nil}},
		{name: "malformed_stored_cursor", results: []cursorReadResult{{row: malformed}}, want: cursor, wantError: partnermanager.ErrUnavailable, callbacks: []error{partnermanager.ErrUnavailable}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &cursorReadStore{results: tc.results, readError: tc.readError}
			repo, err := NewWorkRepository(store)
			require.NoError(t, err)
			got, err := repo.GetDiscoveryCursor(context.Background(), "program", partnermanager.WorkSignup)
			require.Equal(t, tc.want, got)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantError)
			}
			require.Equal(t, tc.callbacks, store.callbacks)
			if tc.readError != nil {
				require.False(t, errors.Is(err, partnermanager.ErrNotFound), "an authoritative failure must not become normal absence")
			}
			if tc.wantError != nil && tc.wantError != partnermanager.ErrNotFound && tc.wantError != partnermanager.ErrWorkUncertain && tc.wantError != partnermanager.ErrUnavailable {
				require.Equal(t, tc.wantError, err, "unexpected failures must retain their identity")
			}
		})
	}
}
