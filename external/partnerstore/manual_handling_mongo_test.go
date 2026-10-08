package partnerstore

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Exact selected authority is a narrow concurrent-safe boundary fixture. Native
// users/program/destination/financial repositories and transactions are real.
// Controlled setup credits/attestations prove no provider transfer or browser
// authentication; host tests separately exercise current persistent grants.
type manualHandlingAuthority struct {
	claim   string
	revoked atomic.Bool
}

func (a *manualHandlingAuthority) CheckPartners(ctx context.Context, actor, capability, target string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor != "fixture-operator" || target != a.claim || a.revoked.Load() {
		return partnermanager.ErrDenied
	}
	switch capability {
	case partnermanager.CapabilityProcessing, partnermanager.CapabilityRecordPayment, partnermanager.CapabilityAmendPayment:
		return nil
	default:
		return partnermanager.ErrDenied
	}
}

func TestMongoManualHandlingPauseAndAlreadyAttemptedRecovery(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"new_requested_handling_denied_without_writes", "requested-admission"},
		{"resuming_review_handling_denied_without_writes", "review-admission"},
		{"already_assigned_transfer_records_during_pause", "full"},
		{"already_assigned_review_records_during_pause", "review-full"},
		{"unknown_attempt_keeps_reserved_obligation", "unknown"},
		{"partial_attempt_keeps_reserved_obligation", "partial"},
		{"mismatched_attempt_keeps_reserved_obligation", "mismatched"},
		{"unknown_attempt_can_later_record_partial_evidence", "repeat-partial"},
		{"unknown_attempt_can_later_record_mismatched_evidence", "repeat-mismatched"},
		{"original_receipt_recovers_after_pause", "replay"},
		{"original_receipt_recovers_after_amendment", "amended-replay"},
		{"lost_commit_ack_recovers_after_pause", "lost"},
		{"changed_original_payload_conflicts", "changed"},
		{"revoked_authority_denies_original_receipt", "revoked"},
		{"another_assigned_actor_cannot_record", "other-actor"},
		{"unassigned_review_cannot_record", "unassigned"},
		{"stale_new_record_cannot_settle", "stale"},
		{"confirmed_unsent_cancellation_remains_available", "cancel"},
		{"unconfirmed_cancellation_preserves_reservation", "cancel-unconfirmed"},
		{"concurrent_original_replays_settle_once", "concurrent-same"},
		{"concurrent_distinct_records_settle_once", "concurrent-distinct"},
		{"concurrent_confirmed_unsent_cancellation_and_record_have_one_winner", "concurrent-cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			p := enrollCorrectionPartner(t, f, "manual-pause-owner@example.test")
			_, err := f.earnings.Accrue(f.ctx, partnerearnings.AccrualRequest{PartnerID: p.ID, PaymentID: "fixture-credit", PaymentMinor: 10000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: f.clock.Now().Add(-8 * 24 * time.Hour)})
			require.NoError(t, err)
			_, err = f.earnings.Mature(f.ctx, p.ID)
			require.NoError(t, err)
			d, err := f.program.UpdatePayoutDestination(f.ctx, partnerprogram.DestinationRequest{CustomerID: p.CustomerID, Method: "paypal", PayPalEmail: "manual-pause-owner@example.test"})
			require.NoError(t, err)
			claim, err := f.earnings.RequestClaim(f.ctx, partnerearnings.ClaimRequest{ActorID: p.CustomerID, PartnerID: p.ID, AmountMinor: 100, Currency: "EUR", DestinationID: d.ID, DestinationSnapshot: map[string]string{"method": d.Method, "email": d.Email, "version": fmt.Sprint(d.Version)}, IdempotencyKey: "claim"})
			require.NoError(t, err)
			a := &manualHandlingAuthority{claim: claim.ID}
			manager := func(earnings partnermanager.EarningsService, enabled bool) *partnermanager.Manager {
				m, e := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: earnings, Identity: f.identity, Authority: a, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock, Controls: partnermanager.Controls{ManualRecording: enabled}})
				require.NoError(t, e)
				return m
			}
			setup := manager(f.earnings, true)
			actor := "fixture-operator"
			if tc.state == "other-actor" {
				actor = "other-operator"
			}
			if tc.state != "requested-admission" && tc.state != "unassigned" {
				// Trusted setup begins handling before the host pause. The assigned
				// actor is not evidence that money was actually sent.
				claim, err = f.earnings.DecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: actor, ClaimID: claim.ID, NewState: partnerearnings.ClaimProcessing, ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			if tc.state == "review-admission" || tc.state == "review-full" || tc.state == "unassigned" {
				claim, err = setup.AdminDecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: "fixture-operator", ClaimID: claim.ID, NewState: partnerearnings.ClaimNeedsReview, Reason: "check previous attempt", ExpectedRevision: claim.Revision})
				require.NoError(t, err)
			}
			req := partnerearnings.RecordPaymentRequest{ActorID: "fixture-operator", ClaimID: claim.ID, Method: "bank", Reference: "controlled-attestation", PaidAt: f.clock.Now(), AmountMinor: claim.AmountMinor, Currency: claim.Currency, State: partnerearnings.PaymentStateFull, ExpectedRevision: claim.Revision, IdempotencyKey: "original-record"}
			replayed := tc.state == "replay" || tc.state == "amended-replay" || tc.state == "changed" || tc.state == "revoked" || tc.state == "lost"
			if replayed {
				creator := setup
				if tc.state == "lost" {
					repo, e := NewEarningsRepository(injectedStore{Store: f.store, uncertain: true}, f.earnings.Config())
					require.NoError(t, e)
					owner, e := partnerearnings.NewService(repo, f.clock, randomIDs{}, f.earnings.Config())
					require.NoError(t, e)
					creator = manager(owner, false)
				}
				_, err = creator.AdminRecordPayment(f.ctx, req)
				if tc.state == "lost" {
					require.ErrorIs(t, err, partnerearnings.ErrUncertain)
				} else {
					require.NoError(t, err)
				}
				claim, err = f.earnings.GetClaim(f.ctx, claim.ID)
				require.NoError(t, err)
				if tc.state == "amended-replay" {
					claim, err = manager(f.earnings, false).AdminAmendPayment(f.ctx, partnerearnings.AmendPaymentRequest{ActorID: "fixture-operator", ClaimID: claim.ID, Method: "corrected-bank", Reference: "corrected-reference", PaidAt: req.PaidAt, Reason: "corrected attestation", ExpectedRevision: claim.Revision, IdempotencyKey: "amend"})
					require.NoError(t, err)
				}
			}
			m := manager(f.earnings, false)
			require.False(t, m.ManualHandlingAdmitted())
			before, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			if tc.state == "requested-admission" || tc.state == "review-admission" {
				_, err = m.AdminDecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: "fixture-operator", ClaimID: claim.ID, NewState: partnerearnings.ClaimProcessing, ExpectedRevision: claim.Revision})
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			} else if tc.state == "cancel" || tc.state == "cancel-unconfirmed" {
				_, err = m.AdminDecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: "fixture-operator", ClaimID: claim.ID, NewState: partnerearnings.ClaimCancelled, Reason: "verified unsent", ConfirmedUnsent: tc.state == "cancel", ExpectedRevision: claim.Revision})
				if tc.state == "cancel" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, partnerearnings.ErrDenied)
				}
			} else if tc.state == "concurrent-same" || tc.state == "concurrent-distinct" || tc.state == "concurrent-cancel" {
				start, results := make(chan struct{}), make(chan error, 2)
				go func() { <-start; _, e := m.AdminRecordPayment(f.ctx, req); results <- e }()
				go func() {
					<-start
					var e error
					if tc.state == "concurrent-cancel" {
						_, e = m.AdminDecideClaim(f.ctx, partnerearnings.ClaimDecision{ActorID: "fixture-operator", ClaimID: claim.ID, NewState: partnerearnings.ClaimCancelled, Reason: "verified unsent", ConfirmedUnsent: true, ExpectedRevision: claim.Revision})
					} else {
						other := req
						if tc.state == "concurrent-distinct" {
							other.IdempotencyKey = "distinct-record"
						}
						_, e = m.AdminRecordPayment(f.ctx, other)
					}
					results <- e
				}()
				close(start)
				one, two := <-results, <-results
				if tc.state == "concurrent-same" {
					require.NoError(t, one)
					require.NoError(t, two)
				} else {
					require.True(t, one == nil && errors.Is(two, partnerearnings.ErrStaleWrite) || two == nil && errors.Is(one, partnerearnings.ErrStaleWrite), "one CAS winner required: %v / %v", one, two)
				}
			} else {
				var want error
				switch tc.state {
				case "unknown", "repeat-partial", "repeat-mismatched":
					req.State, req.AmountMinor, req.Currency = partnerearnings.PaymentStateUnknown, 0, ""
				case "partial":
					req.State, req.AmountMinor = partnerearnings.PaymentStatePartial, 50
				case "mismatched":
					req.Currency = "USD"
				case "changed":
					req.Reference = "different-original-payload"
					want = partnerearnings.ErrConflict
				case "revoked":
					a.revoked.Store(true)
					want = partnermanager.ErrDenied
				case "other-actor":
					want = partnerearnings.ErrConflict
				case "unassigned":
					want = partnerearnings.ErrInvalidState
				case "stale":
					req.ExpectedRevision--
					want = partnerearnings.ErrStaleWrite
				}
				out, e := m.AdminRecordPayment(f.ctx, req)
				require.ErrorIs(t, e, want)
				if tc.state == "repeat-partial" || tc.state == "repeat-mismatched" {
					original := req
					req.ExpectedRevision = out.Revision
					req.IdempotencyKey = "later-observation"
					req.State, req.AmountMinor, req.Currency = partnerearnings.PaymentStatePartial, 50, "EUR"
					if tc.state == "repeat-mismatched" {
						req.State, req.AmountMinor, req.Currency = partnerearnings.PaymentStateMismatched, 100, "USD"
					}
					later, e := m.AdminRecordPayment(f.ctx, req)
					require.NoError(t, e, "distinct later observation must retain new evidence")
					for _, retry := range []partnerearnings.RecordPaymentRequest{original, req} {
						recovered, e := m.AdminRecordPayment(f.ctx, retry)
						require.NoError(t, e)
						require.Equal(t, later, recovered, "both original receipts survive later evidence")
					}
				}
				if replayed && want == nil {
					require.Equal(t, claim, out, "original revision/key recovers the owning current result")
				}
			}
			after, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			stored, err := f.earnings.GetClaim(f.ctx, claim.ID)
			require.NoError(t, err)
			paid := 0
			for _, line := range after.Lines {
				if line.Entry.Kind == partnerearnings.EntryPaid {
					paid++
				}
			}
			switch {
			case tc.state == "requested-admission" || tc.state == "review-admission" || tc.state == "cancel-unconfirmed" || tc.state == "other-actor" || tc.state == "unassigned" || tc.state == "stale" || replayed:
				require.Equal(t, before, after, "denial/recovery cannot mutate financial history")
				require.Equal(t, claim, stored)
			case tc.state == "unknown" || tc.state == "partial" || tc.state == "mismatched" || tc.state == "repeat-partial" || tc.state == "repeat-mismatched":
				require.Equal(t, partnerearnings.ClaimNeedsReview, stored.State)
				observations := 1
				if tc.state == "repeat-partial" || tc.state == "repeat-mismatched" {
					observations = 2
				}
				require.Len(t, stored.PaymentObservations, observations)
				require.Nil(t, stored.Payment)
				require.EqualValues(t, 100, after.Balances.ReservedMinor+after.Balances.ReviewHoldMinor)
				require.EqualValues(t, 1900, after.Balances.AvailableMinor)
				require.Zero(t, paid)
				if observations == 2 {
					sources := make(map[string]bool)
					for _, line := range after.Lines {
						if line.Entry.Kind == partnerearnings.EntryPaymentObserved {
							require.NotEmpty(t, line.Entry.SourceRef)
							sources[line.Entry.SourceRef] = true
						}
					}
					require.Len(t, sources, 2, "separate accepted evidence must remain separately anchored")
					report, e := f.earnings.GetPaymentReport(f.ctx, p.ID, partnerearnings.PaymentQuery{Limit: 100})
					require.NoError(t, e, "later evidence cannot break payment reporting")
					require.Equal(t, after.Balances, report.Balances)
					require.EqualValues(t, 100, report.CohortAmounts.ReviewBackingMinor)
					require.Zero(t, report.CohortAmounts.NetPaidBackingMinor)
				}
			default:
				require.Zero(t, after.Balances.ReservedMinor+after.Balances.ReviewHoldMinor)
				if stored.State == partnerearnings.ClaimCancelled {
					require.EqualValues(t, 2000, after.Balances.AvailableMinor)
					require.Zero(t, paid)
				} else {
					require.Equal(t, partnerearnings.ClaimPaid, stored.State)
					require.EqualValues(t, 100, after.Balances.PaidOutMinor)
					require.EqualValues(t, 1900, after.Balances.AvailableMinor)
					require.Equal(t, 1, paid)
				}
			}
		})
	}
}
