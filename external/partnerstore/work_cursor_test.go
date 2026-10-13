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

func TestDiscoveryCursorReadOutcomes(t *testing.T) {
	cursor := partnermanager.DiscoveryCursor{Revision: 1, AfterID: "source"}
	data, err := json.Marshal(cursor)
	require.NoError(t, err)
	row := recordstore.Record{ID: workDigest("program", partnermanager.WorkSignup), Kind: kindWorkCursor, Partition: workPartition("program", partnermanager.WorkSignup), Revision: 1, Data: data}
	absent := cursorReadResult{err: recordstore.ErrNotFound}
	present := cursorReadResult{row: row}
	failed := errors.New("fixture database failure")
	joined := errors.Join(recordstore.ErrNotFound, failed)
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
		{name: "existing", results: []cursorReadResult{present}, want: cursor, callbacks: []error{nil}},
		{name: "retry_absent_then_present", results: []cursorReadResult{absent, present}, want: cursor, callbacks: []error{nil, nil}},
		{name: "retry_present_then_absent", results: []cursorReadResult{present, absent}, wantError: partnermanager.ErrNotFound, callbacks: []error{nil, nil}},
		{name: "joined_absence_and_failure", results: []cursorReadResult{{err: joined}}, wantError: joined, callbacks: []error{joined}},
		{name: "database_failure", results: []cursorReadResult{{err: failed}}, wantError: failed, callbacks: []error{failed}},
		{name: "read_failure_after_absence", results: []cursorReadResult{absent}, readError: failed, wantError: failed, callbacks: []error{nil}},
		{name: "cancelled_read_after_absence", results: []cursorReadResult{absent}, readError: context.Canceled, wantError: context.Canceled, callbacks: []error{nil}},
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
			if tc.wantError == failed || tc.wantError == context.Canceled || tc.wantError == joined {
				require.False(t, errors.Is(err, partnermanager.ErrNotFound), "an authoritative failure must not become normal absence")
			}
		})
	}
}
