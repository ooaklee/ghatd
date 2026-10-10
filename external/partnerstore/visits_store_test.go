package partnerstore

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named direct adapter context/dependency cases supplement
// transactional Mongo coverage; invalid ports never panic or start callbacks.
func TestVisitAdapterContextAndDependencies(t *testing.T) {
	for _, operation := range []string{"get_click", "get_receipt", "insert_receipt", "transaction", "record_click", "get_day", "put_day", "read_analytics"} {
		for _, tc := range []struct {
			name, mode string
			want       error
		}{{"nil_context", "context", referral.ErrInvalid}, {"cancelled_context", "cancelled", context.Canceled}, {"nil_receiver", "receiver", referral.ErrUnavailable}, {"nil_store", "store", referral.ErrUnavailable}} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				repo := &ReferralRepository{}
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				switch tc.mode {
				case "context":
					ctx = nil
				case "cancelled":
					cancel()
				case "receiver":
					repo = nil
				}
				var err error
				switch operation {
				case "get_click":
					var out referral.Click
					out, err = repo.GetClick(ctx, "link", "click")
					require.Empty(t, out)
				case "get_receipt":
					var out referral.VisitReceipt
					out, err = repo.GetVisitReceipt(ctx, "link", "digest")
					require.Empty(t, out)
				case "insert_receipt":
					err = repo.InsertVisitReceipt(ctx, referral.VisitReceipt{LinkID: "link", Digest: "digest", MeasuredClickID: "click", ExpiresAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)})
				case "record_click":
					err = repo.RecordClick(ctx, referral.Click{}, time.Time{})
				case "get_day":
					var out referral.VisitDay
					out, err = repo.GetVisitDay(ctx, "link", time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
					require.Empty(t, out)
				case "put_day":
					err = repo.PutVisitDay(ctx, referral.VisitDay{}, 0)
				case "read_analytics":
					var out referral.AnalyticsSnapshot
					out, err = repo.ReadAnalyticsSnapshot(ctx, referral.ProgramID, "partner", referral.AnalyticsQuery{Limit: 1})
					require.Empty(t, out)
				case "transaction":
					err = repo.WithVisitTransaction(ctx, "link", func(referral.Repository) error { t.Fatal("invalid context/dependency entered callback"); return nil })
				}
				require.ErrorIs(t, err, tc.want)
			})
		}
	}
}
