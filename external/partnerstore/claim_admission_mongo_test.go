package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// This uses actual owning user, program, destination, encrypted financial
// repositories and native receipts. Fixture authority/region/groups are narrow
// admission ports; this is not authenticated host, provider or worker proof.
func nativeClaimManager(t *testing.T, f *workerFixture, earnings partnermanager.EarningsService, minimum int64, paused bool) *partnermanager.Manager {
	t.Helper()
	m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: earnings, Identity: f.identity, Authority: f.authority, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock, Controls: partnermanager.Controls{Claims: !paused}, Claims: partnermanager.ClaimsConfig{MinimumMinor: minimum}})
	require.NoError(t, err)
	return m
}

func TestMongoManagerClaimAdmissionAndLostResponseRecovery(t *testing.T) {
	type testCase struct {
		name, state  string
		replay, lost bool
		want         error
	}
	for _, tc := range []testCase{
		{name: "minimum_boundary_reserves_once"},
		{name: "new_amount_below_minimum_does_not_write", state: "below", want: partnermanager.ErrInvalid},
		{name: "new_stale_destination_does_not_write", state: "stale", want: partnerearnings.ErrStaleWrite},
		{name: "committed_receipt_recovers_after_pause_minimum_raise_destination_edit", replay: true},
		{name: "lost_commit_ack_recovers_original_receipt", replay: true, lost: true},
		{name: "replay_changed_amount_conflicts", replay: true, state: "amount", want: partnerearnings.ErrConflict},
		{name: "replay_changed_destination_conflicts", replay: true, state: "destination", want: partnerearnings.ErrConflict},
		{name: "replay_current_permission_revocation_denies", replay: true, state: "revoked", want: partnermanager.ErrDenied},
		{name: "terminal_rejected_receipt_recovers_without_new_reservation", replay: true, state: "rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			p := enrollCorrectionPartner(t, f, "withdrawal-owner@example.test")
			_, err := f.earnings.Accrue(f.ctx, partnerearnings.AccrualRequest{PartnerID: p.ID, PaymentID: "accepted-fixture-payment", PaymentMinor: 10000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: f.clock.Now().Add(-8 * 24 * time.Hour), ReferralID: "accepted-fixture-referral", TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
			require.NoError(t, err)
			_, err = f.earnings.Mature(f.ctx, p.ID)
			require.NoError(t, err)
			d, err := f.program.UpdatePayoutDestination(f.ctx, partnerprogram.DestinationRequest{CustomerID: p.CustomerID, Method: "paypal", PayPalEmail: "original@example.test"})
			require.NoError(t, err)
			require.EqualValues(t, 1, d.Version)
			m := nativeClaimManager(t, f, f.earnings, 100, false)
			req := partnermanager.SelfClaimRequest{AmountMinor: 100, ExpectedDestinationVersion: d.Version, IdempotencyKey: "original-intent"}
			before, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			var original partnerearnings.Claim
			if tc.replay {
				creator := m
				if tc.lost {
					repo, err := NewEarningsRepository(injectedStore{Store: f.store, uncertain: true}, f.earnings.Config())
					require.NoError(t, err)
					owner, err := partnerearnings.NewService(repo, f.clock, randomIDs{}, f.earnings.Config())
					require.NoError(t, err)
					creator = nativeClaimManager(t, f, owner, 100, false)
				}
				original, err = creator.RequestClaimWithDestination(f.ctx, p.CustomerID, req)
				if tc.lost {
					require.ErrorIs(t, err, partnerearnings.ErrUncertain)
				} else {
					require.NoError(t, err)
				}
				original, err = f.earnings.FindClaimRequest(f.ctx, p.CustomerID, p.ID, req.IdempotencyKey)
				require.NoError(t, err)
				if tc.state == "rejected" {
					original, err = f.earnings.DecideClaim(f.ctx, partnerearnings.ClaimDecision{ClaimID: original.ID, NewState: partnerearnings.ClaimRejected, ActorID: "fixture-operator", Reason: "fixture review", ExpectedRevision: original.Revision})
					require.NoError(t, err)
				}
				_, err = f.program.UpdatePayoutDestination(f.ctx, partnerprogram.DestinationRequest{CustomerID: p.CustomerID, Method: "paypal", PayPalEmail: "changed@example.test", ExpectedVersion: d.Version})
				require.NoError(t, err)
				m = nativeClaimManager(t, f, f.earnings, 1000, true)
				before, err = f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
				require.NoError(t, err)
			}
			switch tc.state {
			case "below":
				m = nativeClaimManager(t, f, f.earnings, 101, false)
			case "stale":
				req.ExpectedDestinationVersion = 2
			case "amount":
				req.AmountMinor++
			case "destination":
				req.ExpectedDestinationVersion++
			case "revoked":
				f.authority.revoked.Store(true)
			}
			out, err := m.RequestClaimWithDestination(f.ctx, p.CustomerID, req)
			require.ErrorIs(t, err, tc.want)
			after, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			if tc.replay || tc.want != nil {
				require.Equal(t, before, after, "recovery/rejection cannot mutate the owning obligation")
				if tc.want == nil {
					require.Equal(t, original, out)
					require.Equal(t, "original@example.test", out.DestinationSnapshot["email"])
				}
			} else {
				require.EqualValues(t, 100, after.Balances.ReservedMinor)
				require.EqualValues(t, 1900, after.Balances.AvailableMinor)
				prepared, err := m.AdminPaymentClaim(f.ctx, "fixture-operator", out.ID)
				require.NoError(t, err)
				require.Equal(t, out, prepared)
				require.Len(t, after.Lines, len(before.Lines)+1)
				require.Equal(t, partnerearnings.EntryAllocated, after.Lines[0].Entry.Kind)
				require.EqualValues(t, 100, after.Lines[0].Entry.AmountMinor)
				require.Equal(t, out.ID, after.Lines[0].Entry.SourceEventID)
				require.Equal(t, before.Lines, after.Lines[1:], "existing credits remain unchanged")
				require.Equal(t, before.Balances.MatchedMinor, after.Balances.MatchedMinor)
				require.Equal(t, before.Balances.PaidOutMinor, after.Balances.PaidOutMinor, "reservation is not a payout debit")
			}
		})
	}
}
