package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named current-source economics, provenance, privacy,
// cancellation and complete-history cases. Every case owns its mutable fixture.
type historyTestRepo struct {
	*revenueTestRepo
	snapshot *RevenueHistorySnapshot
	readErr  error
	cancel   context.CancelFunc
}

func (r *historyTestRepo) ReadRevenueHistory(ctx context.Context) (RevenueHistorySnapshot, error) {
	if r.cancel != nil {
		r.cancel()
	}
	if r.readErr != nil {
		return RevenueHistorySnapshot{}, r.readErr
	}
	if r.snapshot != nil {
		return *r.snapshot, nil
	}
	out := RevenueHistorySnapshot{Facts: []RevenueFact{}, Observations: []RevenueObservation{}, Sequence: r.sequence, HeadRevision: r.sequence}
	for _, f := range r.facts {
		out.Facts = append(out.Facts, f)
	}
	for _, o := range r.observations {
		out.Observations = append(out.Observations, o)
	}
	return out, nil
}
func historyFixture(t *testing.T) (*RevenueService, *historyTestRepo, RevenueFact, RevenueHistoryQuery) {
	t.Helper()
	_, base, request := revenueFixture(t)
	request.Facts[0].ProviderCustomerID = "private_customer"
	r := &historyTestRepo{revenueTestRepo: base}
	s, err := NewRevenueService(r, revenueTestClock{request.Facts[0].EffectiveAt.Add(time.Minute)})
	require.NoError(t, err)
	o, err := s.AcceptVerified(context.Background(), request)
	require.NoError(t, err)
	f, err := s.GetRevenueFact(context.Background(), o.FactIDs[0])
	require.NoError(t, err)
	return s, r, f, RevenueHistoryQuery{Scopes: []RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}}
}
func acceptHistoryFact(t *testing.T, s *RevenueService, f RevenueFact, envelope string) RevenueObservation {
	t.Helper()
	o, err := s.AcceptVerified(context.Background(), VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: envelope, Facts: []RevenueFact{f}})
	require.NoError(t, err)
	return o
}
func TestRevenueHistoryCurrentAdjustmentEconomics(t *testing.T) {
	type testCase struct {
		name                                    string
		refunds                                 []int64
		disputeKinds                            []string
		wantRefund, wantNet, wantLost, wantHold int64
		want                                    error
	}
	cases := []testCase{
		{name: "verified_original_payment", wantNet: 10000},
		{name: "partial_refund", refunds: []int64{2000}, wantRefund: 2000, wantNet: 8000},
		{name: "cumulative_split_refunds_are_not_summed", refunds: []int64{2000, 6000}, wantRefund: 6000, wantNet: 4000},
		{name: "late_smaller_cumulative_refund_cannot_restore_revenue", refunds: []int64{6000, 2000}, wantRefund: 6000, wantNet: 4000},
		{name: "different_deliveries_same_cumulative_evidence_no_double_debit", refunds: []int64{2000, 2000}, wantRefund: 2000, wantNet: 8000},
		{name: "full_refund", refunds: []int64{10000}, wantRefund: 10000},
		{name: "hold_is_not_loss", disputeKinds: []string{RevenueDisputeHold}, wantNet: 10000, wantHold: 10000},
		{name: "won_supersedes_hold", disputeKinds: []string{RevenueDisputeHold, RevenueDisputeWon}, wantNet: 10000},
		{name: "late_hold_cannot_reopen_won", disputeKinds: []string{RevenueDisputeWon, RevenueDisputeHold}, wantNet: 10000},
		{name: "lost_supersedes_hold", disputeKinds: []string{RevenueDisputeHold, RevenueDisputeLost}, wantLost: 10000},
		{name: "lost_consumes_remaining_after_refund", refunds: []int64{2000}, disputeKinds: []string{RevenueDisputeLost}, wantRefund: 2000, wantLost: 8000},
		{name: "hold_tracks_remaining_after_refund", refunds: []int64{2000}, disputeKinds: []string{RevenueDisputeHold}, wantRefund: 2000, wantHold: 8000, wantNet: 8000},
		{name: "contradictory_terminal_results_are_unassessable", disputeKinds: []string{RevenueDisputeLost, RevenueDisputeWon}, want: ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, f, q := historyFixture(t)
			for i, n := range tc.refunds {
				adjustment := f
				adjustment.Kind = RevenueRefund
				adjustment.AdjustmentID = fmt.Sprintf("refund-%d", i)
				adjustment.CumulativeRefundedMinor = n
				acceptHistoryFact(t, s, adjustment, fmt.Sprintf("refund-envelope-%d", i))
			}
			for i, kind := range tc.disputeKinds {
				adjustment := f
				adjustment.Kind = kind
				adjustment.AdjustmentID = "same-dispute"
				acceptHistoryFact(t, s, adjustment, fmt.Sprintf("dispute-envelope-%d", i))
			}
			out, err := s.GetPaymentRevenueHistory(context.Background(), q)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
				return
			}
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			row := out.Items[0]
			require.NoError(t, row.Validate())
			require.Equal(t, f, row.Original)
			require.Zero(t, row.Original.CumulativeRefundedMinor)
			require.Equal(t, tc.wantRefund, row.RefundedMinor)
			require.Equal(t, tc.wantNet, row.NetMinor)
			require.Equal(t, tc.wantLost, row.DisputeLostMinor)
			require.Equal(t, tc.wantHold, row.DisputeHoldMinor)
			require.Equal(t, r.sequence, out.AcceptanceSequence)
			require.NotEmpty(t, out.Revision)
			require.Zero(t, out.ScopedUnresolvedSources)
			for _, value := range []any{q, out, row} {
				data, err := json.Marshal(value)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(data))
			}
		})
	}
}

func TestRevenueHistoryDisputeAllocationAndIngestBoundaries(t *testing.T) {
	type testCase struct {
		name, mode string
		want       error
	}
	cases := []testCase{
		{name: "distinct_dispute_hold_and_loss_conserve_remaining", mode: "combined"},
		{name: "refund_cannot_exceed_original", mode: "overflow", want: ErrRevenueInvalid},
		{name: "refund_requires_adjustment_identity", mode: "refund_id", want: ErrRevenueInvalid},
		{name: "dispute_requires_adjustment_identity", mode: "dispute_id", want: ErrRevenueInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, f, q := historyFixture(t)
			raw := f
			raw.Kind = RevenueRefund
			raw.AdjustmentID = "refund"
			raw.CumulativeRefundedMinor = 2000
			if tc.mode == "combined" {
				acceptHistoryFact(t, s, raw, "partial-refund")
				raw.CumulativeRefundedMinor = 0
				raw.Kind = RevenueDisputeHold
				raw.AdjustmentID = "held-dispute"
				acceptHistoryFact(t, s, raw, "held-delivery")
				raw.Kind = RevenueDisputeLost
				raw.AdjustmentID = "lost-dispute"
				acceptHistoryFact(t, s, raw, "lost-delivery")
				out, err := s.GetPaymentRevenueHistory(context.Background(), q)
				require.NoError(t, err)
				require.Len(t, out.Items, 1)
				p := out.Items[0]
				require.True(t, p.DisputeHeld)
				require.True(t, p.DisputeLost)
				require.Zero(t, p.NetMinor)
				require.Zero(t, p.DisputeHoldMinor)
				require.EqualValues(t, 8000, p.DisputeLostMinor)
				require.NoError(t, p.Validate())
			} else {
				switch tc.mode {
				case "overflow":
					raw.CumulativeRefundedMinor = f.PaidMinor + 1
				case "refund_id":
					raw.AdjustmentID = ""
				case "dispute_id":
					raw.Kind = RevenueDisputeHold
					raw.AdjustmentID = ""
					raw.CumulativeRefundedMinor = 0
				}
				before, err := s.GetPaymentRevenueHistory(context.Background(), q)
				require.NoError(t, err)
				out, err := s.AcceptVerified(context.Background(), VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "invalid-adjustment", Facts: []RevenueFact{raw}})
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
				after, err := s.GetPaymentRevenueHistory(context.Background(), q)
				require.NoError(t, err)
				require.Equal(t, before, after)
			}
		})
	}
}

func TestRevenueHistoryQueryAndContextBoundaries(t *testing.T) {
	type testCase struct {
		name, mode string
		want       error
	}
	cases := []testCase{
		{name: "explicit_scoped_principal_query"},
		{name: "maximum_scopes_and_principals", mode: "maximum"},
		{name: "missing_scope", mode: "scopes", want: ErrRevenueInvalid},
		{name: "duplicate_scope", mode: "duplicate_scope", want: ErrRevenueInvalid},
		{name: "scope_capacity", mode: "scope_capacity", want: ErrRevenueInvalid},
		{name: "unclean_scope", mode: "unclean_scope", want: ErrRevenueInvalid},
		{name: "missing_principal", mode: "principals", want: ErrRevenueInvalid},
		{name: "duplicate_principal", mode: "duplicate_principal", want: ErrRevenueInvalid},
		{name: "principal_capacity", mode: "principal_capacity", want: ErrRevenueInvalid},
		{name: "unclean_principal", mode: "unclean_principal", want: ErrRevenueInvalid},
		{name: "nil_context", mode: "nil_context", want: ErrRevenueInvalid},
		{name: "canceled_before_read", mode: "canceled", want: context.Canceled},
		{name: "canceled_during_read", mode: "during", want: context.Canceled},
		{name: "missing_optional_same_repo_capability", mode: "legacy", want: ErrRevenueUnavailable},
		{name: "typed_nil_owner", mode: "nil_owner", want: ErrRevenueUnavailable},
		{name: "rollback_clock_cannot_certify_future_acceptance", mode: "rollback", want: ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, _, q := historyFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc.mode {
			case "maximum", "scope_capacity", "principal_capacity":
				q.Scopes = []RevenueScope{}
				for i := 0; i < 10; i++ {
					q.Scopes = append(q.Scopes, RevenueScope{Provider: "stripe", AccountID: fmt.Sprintf("acct_%d", i)})
				}
				q.Principals = []string{}
				for i := 0; i < RevenueHistoryCapacity; i++ {
					q.Principals = append(q.Principals, fmt.Sprintf("principal_%d", i))
				}
				if tc.mode == "scope_capacity" {
					q.Scopes = append(q.Scopes, RevenueScope{Provider: "stripe", AccountID: "acct_extra"})
				}
				if tc.mode == "principal_capacity" {
					q.Principals = append(q.Principals, "principal_extra")
				}
			case "scopes":
				q.Scopes = nil
			case "duplicate_scope":
				q.Scopes = append(q.Scopes, q.Scopes[0])
			case "unclean_scope":
				q.Scopes[0].AccountID = "\naccount"
			case "principals":
				q.Principals = nil
			case "duplicate_principal":
				q.Principals = append(q.Principals, q.Principals[0])
			case "unclean_principal":
				q.Principals[0] = "\nprincipal"
			case "nil_context":
				ctx = nil
			case "canceled":
				cancel()
			case "during":
				r.cancel = cancel
			case "legacy":
				s.repo = r.revenueTestRepo
			case "nil_owner":
				s = nil
			case "rollback":
				s.clock = revenueTestClock{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
			}
			out, err := s.GetPaymentRevenueHistory(ctx, q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
			} else {
				require.NotNil(t, out.Items)
				require.NotEmpty(t, out.Revision)
			}
		})
	}
}

func TestPaymentRevenueBoundaryConservation(t *testing.T) {
	type testCase struct {
		name, mode string
		want       error
	}
	cases := []testCase{
		{name: "positive_net"}, {name: "fully_refunded", mode: "refund"}, {name: "confirmed_loss", mode: "loss"}, {name: "temporary_hold", mode: "hold"},
		{name: "refund_over_original", mode: "overflow", want: ErrRevenueUnassessable}, {name: "negative_refund", mode: "negative", want: ErrRevenueUnassessable},
		{name: "invented_net", mode: "net", want: ErrRevenueUnassessable}, {name: "loss_is_not_second_full_original", mode: "double_loss", want: ErrRevenueUnassessable},
		{name: "hold_must_overlap_current_net", mode: "bad_hold", want: ErrRevenueUnassessable}, {name: "hold_without_hold_state", mode: "missing_hold_state", want: ErrRevenueUnassessable},
		{name: "original_identity_cannot_change", mode: "identity", want: ErrRevenueUnassessable}, {name: "accepted_time_required", mode: "time", want: ErrRevenueUnassessable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, original, _ := historyFixture(t)
			p := PaymentRevenue{Original: original, NetMinor: original.PaidMinor}
			switch tc.mode {
			case "refund":
				p.RefundedMinor = original.PaidMinor
				p.NetMinor = 0
			case "loss":
				p.DisputeLost = true
				p.DisputeLostMinor = original.PaidMinor
				p.NetMinor = 0
			case "hold":
				p.DisputeHeld = true
				p.DisputeHoldMinor = p.NetMinor
			case "overflow":
				p.RefundedMinor = original.PaidMinor + 1
			case "negative":
				p.RefundedMinor = -1
			case "net":
				p.NetMinor++
			case "double_loss":
				p.RefundedMinor = 2000
				p.DisputeLost = true
				p.NetMinor = 0
				p.DisputeLostMinor = original.PaidMinor
			case "bad_hold":
				p.DisputeHeld = true
				p.DisputeHoldMinor = 1
			case "missing_hold_state":
				p.DisputeHoldMinor = p.NetMinor
			case "identity":
				p.Original.ID = "changed"
			case "time":
				p.Original.AcceptedAt = time.Time{}
			}
			require.ErrorIs(t, p.Validate(), tc.want)
		})
	}
}
func TestRevenueHistoryAdjustmentProvenance(t *testing.T) {
	type testCase struct {
		name, field     string
		missingOriginal bool
	}
	cases := []testCase{{name: "missing_original_never_guesses_positive", missingOriginal: true}, {name: "wrong_currency", field: "currency"}, {name: "wrong_principal_outside_filter_still_rejected", field: "principal"}, {name: "wrong_subscription", field: "subscription"}, {name: "wrong_original_amount", field: "amount"}, {name: "wrong_provider_customer", field: "customer"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, f, q := historyFixture(t)
			if tc.missingOriginal {
				r.facts = map[string]RevenueFact{}
				r.observations = map[string]RevenueObservation{}
				r.sequence = 0
			}
			f.Kind = RevenueRefund
			f.AdjustmentID = "refund"
			f.CumulativeRefundedMinor = 2000
			switch tc.field {
			case "currency":
				f.Currency = "GBP"
			case "principal":
				f.PrincipalID = "other-principal"
			case "subscription":
				f.SubscriptionID = "other-subscription"
			case "amount":
				f.PaidMinor++
			case "customer":
				f.ProviderCustomerID = "other-customer"
			}
			acceptHistoryFact(t, s, f, "refund")
			out, err := s.GetPaymentRevenueHistory(context.Background(), q)
			require.ErrorIs(t, err, ErrRevenueUnassessable)
			require.Zero(t, out)
		})
	}
}
func TestRevenueHistoryScopeFilteringAndQuarantine(t *testing.T) {
	type testCase struct {
		name                                              string
		changeScope, changePrincipal, quarantine, resolve bool
	}
	cases := []testCase{{name: "other_merchant_same_payment_is_isolated", changeScope: true}, {name: "other_principal_is_filtered_before_return", changePrincipal: true}, {name: "scoped_unknown_delivery_remains_coverage", quarantine: true}, {name: "reasoned_resolution_clears_unknown_without_payment", quarantine: true, resolve: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, f, q := historyFixture(t)
			if tc.changeScope || tc.changePrincipal {
				if tc.changeScope {
					f.Scope.AccountID = "other-account"
				} else {
					f.PrincipalID = "other-principal"
					f.PaymentID = "other-payment"
				}
				acceptHistoryFact(t, s, f, "other")
			}
			if tc.quarantine {
				o, err := s.AcceptVerified(context.Background(), VerifiedRevenueRequest{Scope: q.Scopes[0], EnvelopeID: "unknown-delivery", QuarantineReason: "historical_payer_pending"})
				require.NoError(t, err)
				if tc.resolve {
					_, err = s.ResolveQuarantinedRevenue(context.Background(), ResolveRevenueRequest{ObservationID: o.ID, ExpectedFingerprint: o.Fingerprint, Reason: "verified_non_subscription", ActorID: "current-worker"})
					require.NoError(t, err)
				}
			}
			out, err := s.GetPaymentRevenueHistory(context.Background(), q)
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			require.Equal(t, q.Principals[0], out.Items[0].Original.PrincipalID)
			require.Equal(t, q.Scopes[0], out.Items[0].Original.Scope)
			want := 0
			if tc.quarantine && !tc.resolve {
				want = 1
			}
			require.Equal(t, want, out.ScopedUnresolvedSources)
		})
	}
}
func TestRevenueHistorySnapshotIntegrity(t *testing.T) {
	type testCase struct {
		name, field string
		want        error
	}
	cases := []testCase{{name: "missing_sequence_head", field: "head", want: ErrRevenueUnassessable}, {name: "head_gap", field: "gap", want: ErrRevenueUnassessable}, {name: "duplicate_sequence", field: "sequence", want: ErrRevenueUnassessable}, {name: "changed_fact_fingerprint", field: "fact", want: ErrRevenueUnassessable}, {name: "missing_original_reception", field: "receipt", want: ErrRevenueUnassessable}, {name: "changed_reception_fingerprint", field: "observation", want: ErrRevenueUnassessable}, {name: "future_acceptance_clock", field: "clock", want: ErrRevenueUnassessable}, {name: "zero_value_is_not_explicit_empty_history", field: "nil", want: ErrRevenueUnassessable}, {name: "global_capacity_not_reduced_by_filter", field: "capacity", want: ErrRevenueHistoryTooLarge}, {name: "joined_absence_outage_not_empty", field: "outage", want: ErrRevenueUnavailable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, _, q := historyFixture(t)
			snap, err := r.ReadRevenueHistory(context.Background())
			require.NoError(t, err)
			switch tc.field {
			case "head":
				snap.HeadRevision = 0
			case "gap":
				snap.Sequence++
			case "sequence":
				snap.Facts = append(snap.Facts, snap.Facts[0])
				snap.Sequence = 2
				snap.HeadRevision = 2
			case "fact":
				snap.Facts[0].Fingerprint = "changed"
			case "receipt":
				snap.Observations = []RevenueObservation{}
			case "observation":
				snap.Observations[0].Fingerprint = "changed"
			case "clock":
				snap.Facts[0].AcceptedAt = snap.Facts[0].AcceptedAt.Add(time.Second)
			case "nil":
				snap = RevenueHistorySnapshot{}
			case "capacity":
				snap.Observations = make([]RevenueObservation, RevenueHistoryCapacity)
			case "outage":
				r.readErr = errors.Join(ErrRevenueNotFound, ErrRevenueUnavailable)
			}
			r.snapshot = &snap
			out, err := s.GetPaymentRevenueHistory(context.Background(), q)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, out)
		})
	}
}
