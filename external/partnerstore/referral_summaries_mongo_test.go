package partnerstore

import (
	"context"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

func summaryManager(t *testing.T, f *workerFixture, earnings partnermanager.EarningsService) *partnermanager.Manager {
	t.Helper()
	m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: earnings, Identity: f.identity, Authority: f.authority, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock})
	require.NoError(t, err)
	return m
}

// Audit disposition: named retained/reacquired lifecycles use actual owning
// identity/program/referral/financial services and encrypted Mongo transactions.
func TestMongoReferralSummaryRetainsFrozenPaymentEconomics(t *testing.T) {
	for _, reacquire := range []bool{false, true} {
		name := "former_owner"
		if reacquire {
			name = "reacquired_owner"
		}
		t.Run(name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			original := enrollCorrectionPartner(t, f, "summary-owner@example.test")
			other := enrollCorrectionPartner(t, f, "other-owner@example.test")
			customer := correctionAccount(t, f, "summary-customer@example.test")
			first, err := f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: original.ID, CustomerID: original.CustomerID, CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "initial reviewed assignment", Terms: frozenTerms()}))
			require.NoError(t, err)
			paid, err := f.referrals.BindPayment(f.ctx, customer, "original", f.clock.Now())
			require.NoError(t, err)
			future, err := f.referrals.BindPayment(f.ctx, customer, "future-frozen", f.clock.Now().Add(5*time.Hour))
			require.NoError(t, err)
			f.clock.now = f.clock.now.Add(time.Hour)
			second, err := f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: other.ID, CustomerID: other.CustomerID, CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "reviewed prospective transfer", Terms: frozenTerms(), ExpectedRevision: first.Revision, ExpectedReferralID: first.ID}))
			require.NoError(t, err)
			bindings := []referral.PaymentAttribution{paid, future}
			amounts := []int64{5000, 3000}
			if reacquire {
				f.clock.now = f.clock.now.Add(time.Hour)
				_, err = f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: original.ID, CustomerID: original.CustomerID, CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "reviewed prospective reacquisition", Terms: frozenTerms(), ExpectedRevision: second.Revision, ExpectedReferralID: second.ID}))
				require.NoError(t, err)
				newBinding, err := f.referrals.BindPayment(f.ctx, customer, "reacquired-payment", f.clock.Now().Add(time.Hour))
				require.NoError(t, err)
				bindings = append(bindings, newBinding)
				amounts = append(amounts, 2000)
			}
			for i, binding := range bindings {
				_, err = f.earnings.Accrue(f.ctx, partnerearnings.AccrualRequest{PartnerID: binding.PartnerID, PaymentID: binding.PaymentID, PaymentMinor: amounts[i], RateBasisPoints: binding.Terms.RateBasisPoints, HoldDuration: time.Duration(binding.Terms.HoldDurationDays) * 24 * time.Hour, Currency: "EUR", OccurredAt: binding.EffectiveAt, ReferralID: binding.ReferralID, PlanID: "plan-" + binding.PaymentID, TermsVersion: binding.Terms.TermsVersion, PolicyID: binding.Terms.PolicyVersionIDs[0]})
				require.NoError(t, err)
			}
			// This binding has no accepted commission yet; reporting cannot
			// infer that pending source processing grants zero entitlement.
			_, err = f.referrals.BindPayment(f.ctx, customer, "awaiting-acceptance", f.clock.Now().Add(6*time.Hour))
			require.NoError(t, err)
			f.clock.now = f.clock.now.Add(8 * 24 * time.Hour)
			_, err = f.earnings.Mature(f.ctx, original.ID)
			require.NoError(t, err)
			claim, err := f.earnings.RequestClaim(f.ctx, partnerearnings.ClaimRequest{PartnerID: original.ID, ActorID: original.CustomerID, AmountMinor: 1500, Currency: "EUR", DestinationID: "fixture-destination", IdempotencyKey: "claim"})
			require.NoError(t, err)
			claim, err = f.earnings.DecideClaim(f.ctx, partnerearnings.ClaimDecision{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, NewState: partnerearnings.ClaimProcessing})
			require.NoError(t, err)
			claim, err = f.earnings.RecordPayment(f.ctx, partnerearnings.RecordPaymentRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: claim.AmountMinor, Currency: "EUR", State: partnerearnings.PaymentStateFull, Method: "bank", Reference: "controlled-manual-record", PaidAt: f.clock.Now(), IdempotencyKey: "record"})
			require.NoError(t, err)
			_, err = f.earnings.RecordReturnedTransfer(f.ctx, partnerearnings.ReturnRequest{ClaimID: claim.ID, ActorID: "operator", ExpectedRevision: claim.Revision, AmountMinor: 700, Currency: "EUR", ReturnedAt: f.clock.Now(), Reference: "controlled-return", Reason: "verified fixture return", IdempotencyKey: "return"})
			require.NoError(t, err)
			out, err := summaryManager(t, f, f.earnings).ReferralSummaries(f.ctx, original.CustomerID, partnermanager.ReferralSummaryQuery{Limit: 1})
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			row := out.Items[0]
			periods, paymentRows, accrued := 1, 2, int64(1600)
			if reacquire {
				periods, paymentRows, accrued = 2, 3, 2000
			}
			require.Equal(t, reacquire, row.Current)
			require.Len(t, row.Periods, periods)
			require.Equal(t, paymentRows, row.AcceptedPaymentRows)
			require.EqualValues(t, accrued, row.Commission.AccruedMinor)
			require.EqualValues(t, 1500, row.Commission.GrossPaidBackingMinor)
			require.EqualValues(t, 700, row.Commission.ReturnedBackingMinor)
			require.EqualValues(t, 800, row.Commission.NetPaidBackingMinor)
			require.EqualValues(t, accrued-800, out.CurrentPartnerBalances.AvailableMinor)
			require.Equal(t, "not_evaluated", out.SourceCoverage)
			payments, err := f.earnings.GetPaymentReport(f.ctx, original.ID, partnerearnings.PaymentQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, payments.Revision, out.FinancialRevision)
			require.Equal(t, first.ID, future.ReferralID, "future old-owner binding remains immutable after cutover")
			financial, err := summaryManager(t, f, f.earnings).FinancialSummary(f.ctx, original.CustomerID, partnermanager.FinancialSummaryQuery{})
			require.NoError(t, err)
			require.Equal(t, paymentRows, financial.AcceptedAllocationRows)
			require.Equal(t, out.FinancialRevision, financial.FinancialRevision)
			require.Equal(t, *row.Commission, *financial.CohortCommission)
			require.EqualValues(t, accrued-800, financial.RemainingCommissionMinor)
			require.EqualValues(t, 1500, financial.PeriodMovements.PaidMinor)
			require.EqualValues(t, 700, financial.PeriodMovements.ReturnedMinor)
			require.Equal(t, 1, financial.CurrentClaims.Paid)
			require.Equal(t, "not_evaluated", financial.SourceCoverage)
		})
	}
}

type summarySnapshotGate struct {
	partnermanager.EarningsService
	ready  chan struct{}
	resume chan struct{}
	once   sync.Once
}

func (g *summarySnapshotGate) GetReferralAmounts(ctx context.Context, partner string, q partnerearnings.ReferralAmountQuery) (partnerearnings.ReferralAmountReport, error) {
	out, err := g.EarningsService.GetReferralAmounts(ctx, partner, q)
	if err != nil {
		return partnerearnings.ReferralAmountReport{}, err
	}
	g.once.Do(func() { close(g.ready) })
	select {
	case <-g.resume:
		return out, nil
	case <-ctx.Done():
		return partnerearnings.ReferralAmountReport{}, ctx.Err()
	}
}

// Audit disposition: related scheduled cross-domain races use one explicit
// read barrier after the actual financial snapshot; each case has its own DB.
func TestMongoReferralSummaryCrossDomainRaces(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "on_page_reacquisition_invalidates_join", mode: "reacquire", want: referral.ErrStaleWrite},
		{name: "new_membership_before_cursor_does_not_certify_program_totals", mode: "before"},
		{name: "authority_revoked_after_financial_snapshot", mode: "revoked", want: partnermanager.ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			original := enrollCorrectionPartner(t, f, "race-owner@example.test")
			other := enrollCorrectionPartner(t, f, "race-other-owner@example.test")
			customers := []string{correctionAccount(t, f, "race-a@example.test"), correctionAccount(t, f, "race-b@example.test"), correctionAccount(t, f, "race-c@example.test")}
			sort.Slice(customers, func(i, j int) bool {
				return referral.RelationshipReferenceID(referral.ProgramID, original.ID, customers[i]) < referral.RelationshipReferenceID(referral.ProgramID, original.ID, customers[j])
			})
			state := referral.PartnerState{PartnerID: original.ID, CustomerID: original.CustomerID, CanAcquireReferrals: true}
			assign := func(customer, reason string) referral.Referral {
				row, err := f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: state, ReferredCustomer: customer, ActorID: "operator", Reason: reason, Terms: frozenTerms()}))
				require.NoError(t, err)
				return row
			}
			assign(customers[1], "cursor membership")
			first := assign(customers[2], "selected membership")
			if tc.mode == "reacquire" {
				f.clock.now = f.clock.now.Add(time.Hour)
				var err error
				first, err = f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: other.ID, CustomerID: other.CustomerID, CanAcquireReferrals: true}, ReferredCustomer: customers[2], ActorID: "operator", Reason: "correct away", Terms: frozenTerms(), ExpectedRevision: first.Revision, ExpectedReferralID: first.ID}))
				require.NoError(t, err)
			}
			gate := &summarySnapshotGate{EarningsService: f.earnings, ready: make(chan struct{}), resume: make(chan struct{})}
			var resume sync.Once
			t.Cleanup(func() { resume.Do(func() { close(gate.resume) }) })
			m := summaryManager(t, f, gate)
			q := partnermanager.ReferralSummaryQuery{Limit: 1, After: referral.RelationshipReferenceID(referral.ProgramID, original.ID, customers[1])}
			type result struct {
				out partnermanager.ReferralSummaryPage
				err error
			}
			done := make(chan result, 1)
			go func() { out, err := m.ReferralSummaries(f.ctx, original.CustomerID, q); done <- result{out, err} }()
			select {
			case <-gate.ready:
			case <-time.After(20 * time.Second):
				t.Fatal("financial snapshot did not reach barrier")
			}
			switch tc.mode {
			case "reacquire":
				f.clock.now = f.clock.now.Add(time.Hour)
				_, err := f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: state, ReferredCustomer: customers[2], ActorID: "operator", Reason: "reacquire during join", Terms: frozenTerms(), ExpectedRevision: first.Revision, ExpectedReferralID: first.ID}))
				require.NoError(t, err)
			case "before":
				assign(customers[0], "new membership before cursor")
			case "revoked":
				f.authority.revoked.Store(true)
			}
			resume.Do(func() { close(gate.resume) })
			var got result
			select {
			case got = <-done:
			case <-time.After(20 * time.Second):
				t.Fatal("manager join did not finish")
			}
			require.ErrorIs(t, got.err, tc.want)
			if tc.want != nil {
				require.Empty(t, got.out)
				return
			}
			require.Equal(t, "visible_relationships", got.out.Scope)
			require.Len(t, got.out.Items, 1)
			require.Equal(t, referral.RelationshipReferenceID(referral.ProgramID, original.ID, customers[2]), got.out.Items[0].ID)
			require.Nil(t, got.out.Items[0].Commission)
			all, err := f.referrals.ListRelationships(f.ctx, original.ID, referral.RelationshipQuery{Limit: 100})
			require.NoError(t, err)
			require.Len(t, all.Items, 3, "the unchanged page cannot certify the complete program set")
		})
	}
}
