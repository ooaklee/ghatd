package partnerstore

import (
	"context"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type analyticsPageTx struct {
	recordstore.Tx
	rows   []recordstore.Record
	calls  int
	repeat bool
	err    error
}

func (s *analyticsPageTx) Find(_ context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := []recordstore.Record{}
	for _, r := range s.rows {
		if !s.repeat && r.ID <= q.AfterID {
			continue
		}
		out = append(out, r)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

// Audit disposition: named complete/empty/overflow/non-advancing/error cases
// prove the bound detects additional evidence rather than truncating a page.
func TestAnalyticsCompleteReadCapacity(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rows, budget int
		repeat       bool
		want         error
	}{{"empty_scope", 0, 0, false, nil}, {"exact_boundary_requires_empty_tail", 400, 400, false, nil}, {"last_partial_page", 399, 400, false, nil}, {"one_extra_row_is_explicit_capacity", 401, 400, false, referral.ErrCapacity}, {"zero_budget_is_not_truncation", 1, 0, false, referral.ErrCapacity}, {"nonadvancing_driver_page_discards_evidence", 200, 500, true, recordstore.ErrUnavailable}} {
		t.Run(tc.name, func(t *testing.T) {
			tx := &analyticsPageTx{repeat: tc.repeat}
			for i := 0; i < tc.rows; i++ {
				tx.rows = append(tx.rows, recordstore.Record{ID: fmt.Sprintf("row-%08d", i), Kind: kindClick, Partition: "own-link"})
			}
			budget := tc.budget
			out, err := analyticsRecords(context.Background(), tx, recordstore.Query{Kind: kindClick, Partition: "own-link"}, &budget)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, out)
			} else {
				require.Len(t, out, tc.rows)
				require.Equal(t, tc.budget-tc.rows, budget)
			}
			require.LessOrEqual(t, tx.calls, 3)
		})
	}
}
