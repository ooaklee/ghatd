package revenuestore

import (
	"fmt"
	"sort"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new real replica-set paging cases prove that failed first
// pages remain pending without hiding later work. Sweep positions are not acks.
func TestMongoRevenuePendingSweeps(t *testing.T) {
	type testCase struct {
		name    string
		sources bool
	}
	cases := []testCase{{name: "fact_poison_page_cannot_hide_later_acceptance"}, {name: "quarantine_poison_page_cannot_hide_later_sources", sources: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, _, ctx := revenueFixture(t)
			svc := revenueService(t, repo)
			total := 205
			var ids []string
			for i := 0; i < total; i++ {
				fact := revenueFact()
				fact.PaymentID = fmt.Sprintf("payment-%03d", i)
				req := billing.VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: fmt.Sprintf("envelope-%03d", i), Facts: []billing.RevenueFact{fact}}
				if tc.sources {
					req.Facts = nil
					req.QuarantineReason = "historical_payer_plan_pending"
				}
				receipt, err := svc.AcceptVerified(ctx, req)
				require.NoError(t, err)
				if tc.sources {
					ids = append(ids, receipt.ID)
				}
			}
			if tc.sources {
				sort.Strings(ids)
				var found []string
				after := ""
				for {
					page, err := svc.UnresolvedRevenueObservationsAfter(ctx, after, 100)
					require.NoError(t, err)
					if len(page) == 0 {
						break
					}
					for _, row := range page {
						require.Greater(t, row.ID, after)
						found = append(found, row.ID)
						after = row.ID
					}
				}
				require.Equal(t, ids, found)
				// Reading beyond the first poison page did not resolve it or lose it.
				restarted, err := svc.UnresolvedRevenueObservationsAfter(ctx, "", 200)
				require.NoError(t, err)
				require.Len(t, restarted, 200)
				require.Equal(t, ids[0], restarted[0].ID)
				return
			}
			var found []billing.RevenueFact
			var after int64
			for {
				page, err := svc.PendingRevenueFactsAfter(ctx, "consumer", after, 100)
				require.NoError(t, err)
				if len(page) == 0 {
					break
				}
				for _, row := range page {
					require.Greater(t, row.Sequence, after)
					found = append(found, row)
					after = row.Sequence
				}
			}
			require.Len(t, found, total)
			// All first-page work is left unresolved. A later durable decision affects
			// only its own fact; the next sweep still sees every earlier failure.
			last := found[len(found)-1]
			require.NoError(t, svc.AcknowledgeRevenueFact(ctx, billing.RevenueAcknowledgement{ConsumerID: "consumer", FactID: last.ID, AcceptanceID: "durable-owner-receipt", Outcome: "accepted", ActorID: "worker"}))
			restarted, err := svc.PendingRevenueFactsAfter(ctx, "consumer", 0, 200)
			require.NoError(t, err)
			require.Len(t, restarted, 200)
			require.Equal(t, found[0].ID, restarted[0].ID)
			tail, err := svc.PendingRevenueFactsAfter(ctx, "consumer", 200, 200)
			require.NoError(t, err)
			require.Len(t, tail, 4)
		})
	}
}
