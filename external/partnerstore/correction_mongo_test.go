package partnerstore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func reviewedCorrection(t *testing.T, s *referral.Service, req referral.CorrectionRequest) referral.CorrectionRequest {
	t.Helper()
	state, err := s.GetAttributionSnapshot(context.Background(), req.ReferredCustomer)
	require.NoError(t, err)
	req.Mode = referral.CorrectionProspective
	req.IdempotencyKey = "review-" + req.Partner.PartnerID + "-" + req.Reason
	req.ExpectedSnapshotFingerprint = state.Fingerprint
	req.PreviewFingerprint = state.Fingerprint
	req.SignupID = state.Head.SignupID
	if req.SignupID == "" {
		req.SignupID = "signup-fixture"
	}
	// mongoEarnings clock is fixed in these fixtures; initial cutovers must not
	// predate that owning test signup and later revisions retain its anchor.
	req.SignupCreatedAt = state.Head.LockedAt
	if req.SignupCreatedAt.IsZero() {
		req.SignupCreatedAt = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	}
	if len(state.History) > 0 {
		req.SignupCreatedAt = state.History[0].LockedAt
	}
	return req
}

type correctionFailureStore struct {
	recordstore.Store
	failAt    int
	uncertain bool
}
type correctionFailureTx struct{ *injectedTx }

func (t correctionFailureTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	t.writes++
	if t.writes == t.failAt {
		return injectedFailure
	}
	return t.Tx.Replace(ctx, row, expected)
}
func (s correctionFailureStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	err := s.Store.Transact(ctx, key, func(tx recordstore.Tx) error {
		return fn(correctionFailureTx{injectedTx: &injectedTx{Tx: tx, failAt: s.failAt}})
	})
	if err == nil && s.uncertain {
		return recordstore.ErrUncertain
	}
	return err
}

// Audit disposition: named actual-transaction rollback, lost-acknowledgement
// and race cases. Each fixture uses its own disposable database.
func TestMongoCorrectionReceiptRollbackAndRecovery(t *testing.T) {
	cases := []struct {
		name   string
		failAt int
		lost   bool
		want   error
	}{
		{name: "revision_receipt_insert_failure_rolls_back", failAt: 1, want: injectedFailure},
		{name: "membership_insert_failure_rolls_back_revision_receipt", failAt: 2, want: injectedFailure},
		{name: "head_replace_failure_rolls_back_revision_and_membership", failAt: 3, want: injectedFailure},
		{name: "committed_reply_lost_recovers_immutable_receipt", lost: true, want: referral.ErrUncertain},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "original-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited prospective initial assignment", Terms: frozenTerms()}))
			require.NoError(t, err)
			clock.now = clock.now.Add(time.Hour)
			req := reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "reviewed correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()})
			broken, err := NewReferralRepository(correctionFailureStore{Store: store, failAt: tc.failAt, uncertain: tc.lost})
			require.NoError(t, err)
			bs, err := referral.NewService(broken, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			out, err := bs.AssignAttribution(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			receipt, err := s.FindCorrection(ctx, req.ActorID, req.ReferredCustomer, req.IdempotencyKey)
			if tc.lost {
				require.NoError(t, err)
				require.NotNil(t, receipt.Correction)
			} else {
				require.ErrorIs(t, err, referral.ErrNotFound)
				head, err := s.GetReferralForCustomer(ctx, "customer")
				require.NoError(t, err)
				require.Equal(t, first, head)
				history, err := s.History(ctx, "customer")
				require.NoError(t, err)
				require.Equal(t, []referral.Referral{first}, history)
				members, err := s.ListRelationships(ctx, "new", referral.RelationshipQuery{Limit: 100})
				require.NoError(t, err)
				require.Empty(t, members.Items, "aborted correction cannot retain membership")
			}
			recovered, err := s.AssignAttribution(ctx, req)
			require.NoError(t, err)
			members, err := s.ListRelationships(ctx, "new", referral.RelationshipQuery{Limit: 100})
			require.NoError(t, err)
			require.Len(t, members.Items, 1)
			require.True(t, members.Items[0].Current)
			if tc.lost {
				require.Equal(t, receipt, recovered)
			}
			req.Partner.CanAcquireReferrals = false
			clock.now = clock.now.Add(5 * time.Hour)
			again, err := s.AssignAttribution(ctx, req)
			require.NoError(t, err)
			require.Equal(t, recovered, again)
			wire, err := json.Marshal(again)
			require.NoError(t, err)
			require.NotContains(t, string(wire), again.Correction.RequestFingerprint)
			req.Reason = "changed request"
			_, err = s.AssignAttribution(ctx, req)
			require.ErrorIs(t, err, referral.ErrStaleWrite)
		})
	}
}

func TestMongoCorrectionCompetingRequestKeys(t *testing.T) {
	cases := []struct {
		name    string
		sameKey bool
	}{
		{name: "same_key_competing_replies_recover_one_original", sameKey: true},
		{name: "different_keys_compete_on_reviewed_snapshot"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(store)
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			req := reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited prospective assignment", Terms: frozenTerms()})
			type result struct {
				row referral.Referral
				err error
			}
			out := make(chan result, 2)
			start := make(chan struct{})
			for n := 0; n < 2; n++ {
				copy := req
				if !tc.sameKey && n == 1 {
					copy.IdempotencyKey = "different-key"
				}
				go func() { <-start; v, err := s.AssignAttribution(ctx, copy); out <- result{v, err} }()
			}
			close(start)
			a, b := <-out, <-out
			if tc.sameKey {
				require.NoError(t, a.err)
				require.NoError(t, b.err)
				require.Equal(t, a.row, b.row)
			} else {
				require.True(t, (a.err == nil && errors.Is(b.err, referral.ErrStaleWrite)) || (b.err == nil && errors.Is(a.err, referral.ErrStaleWrite)), "outcomes: %v / %v", a.err, b.err)
			}
			history, err := s.History(ctx, "customer")
			require.NoError(t, err)
			require.Len(t, history, 1)
		})
	}
}

func TestMongoProspectiveCorrectionPreservesPaidEconomics(t *testing.T) {
	// One paid-commission lifecycle is the invariant: an owner correction must
	// retain every original journal/claim/payment row and future frozen binding.
	earnings, _, store, _, clock, ctx := mongoEarnings(t)
	refs, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(refs, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited prospective assignment", Terms: frozenTerms()}))
	require.NoError(t, err)
	paidBinding, err := s.BindPayment(ctx, "customer", "original-payment", clock.Now())
	require.NoError(t, err)
	futureBinding, err := s.BindPayment(ctx, "customer", "future-payment", clock.Now().Add(5*time.Hour))
	require.NoError(t, err)
	_, err = earnings.Accrue(ctx, partnerearnings.AccrualRequest{PartnerID: paidBinding.PartnerID, PaymentID: paidBinding.PaymentID, PaymentMinor: 10000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: paidBinding.EffectiveAt, ReferralID: first.ID, TermsVersion: "fixture-terms", PolicyID: "fixture-policy"})
	require.NoError(t, err)
	claim, err := earnings.RequestClaim(ctx, claimRequest(1500, "claim"))
	require.NoError(t, err)
	process(t, earnings, ctx, claim)
	paid, err := earnings.RecordPayment(ctx, paymentRequest(claim, clock, "record"))
	require.NoError(t, err)
	before, err := earnings.ListJournal(ctx, "partner")
	require.NoError(t, err)
	balances, err := earnings.Balances(ctx, "partner")
	require.NoError(t, err)
	report, err := earnings.GetPaymentReport(ctx, "partner", partnerearnings.PaymentQuery{Limit: 100, ReferralID: first.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1500, report.CohortAmounts.GrossPaidBackingMinor)
	clock.now = clock.now.Add(time.Hour)
	req := reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "reviewed prospective correction", ExpectedRevision: first.Revision, ExpectedReferralID: first.ID, Terms: frozenTerms()})
	corrected, err := s.AssignAttribution(ctx, req)
	require.NoError(t, err)
	after, err := earnings.ListJournal(ctx, "partner")
	require.NoError(t, err)
	require.Equal(t, before, after)
	current, err := earnings.Balances(ctx, "partner")
	require.NoError(t, err)
	require.Equal(t, balances, current)
	retained, err := earnings.GetPaymentReport(ctx, "partner", partnerearnings.PaymentQuery{Limit: 100, ReferralID: first.ID})
	require.NoError(t, err)
	require.Equal(t, report.Revision, retained.Revision)
	require.Equal(t, report.Items, retained.Items)
	require.Equal(t, report.CohortAmounts, retained.CohortAmounts)
	require.Equal(t, report.Balances, retained.Balances)
	claimNow, err := earnings.GetClaim(ctx, paid.ID)
	require.NoError(t, err)
	require.Equal(t, paid, claimNow)
	for _, binding := range []referral.PaymentAttribution{paidBinding, futureBinding} {
		replay, err := s.BindPayment(ctx, "customer", binding.PaymentID, binding.EffectiveAt)
		require.NoError(t, err)
		require.Equal(t, binding, replay)
	}
	late, err := s.BindPayment(ctx, "customer", "late-original-payment", first.LockedAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, first.ID, late.ReferralID)
	renewal, err := s.BindPayment(ctx, "customer", "new-renewal", corrected.LockedAt.Add(time.Minute))
	require.NoError(t, err)
	require.Equal(t, corrected.ID, renewal.ReferralID)
}
