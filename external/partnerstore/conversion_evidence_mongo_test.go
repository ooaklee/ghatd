package partnerstore

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

func nativeConversionFixture(t *testing.T) (*workerFixture, string, billing.RevenueScope) {
	t.Helper()
	f := owningWorkerFixture(t)
	f.signer = mongoVisitSigner(t, f.clock)
	link, err := f.referrals.IssueLink(f.ctx, referral.PartnerState{PartnerID: f.partner.ID, CustomerID: f.partner.CustomerID, CanAcquireReferrals: true})
	require.NoError(t, err)
	m := f.manager(t, false)
	visit, err := m.PrepareVisit(f.ctx, partnermanager.PrepareVisitRequest{Code: link.Code, Consented: true})
	require.NoError(t, err)
	customer := f.signup(t, "conversion-fixture@example.test", visit.Evidence)
	_, err = m.ConsumeSignup(f.ctx, "worker", customer)
	require.NoError(t, err)
	f.clock.now = f.clock.now.Add(time.Minute)
	fact := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "conversion-account"}, Kind: billing.RevenuePayment, PaymentID: "conversion-payment", InvoiceID: "conversion-invoice", AllocationID: "conversion-line", PrincipalID: customer, ProviderCustomerID: "conversion-provider-customer", SubscriptionID: "conversion-subscription", PlanID: "fixture-plan", CostID: "fixture-cost", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: f.clock.Now()}
	o, err := f.revenue.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: "conversion-envelope", Facts: []billing.RevenueFact{fact}})
	require.NoError(t, err)
	_, err = f.referrals.BindPayment(f.ctx, customer, o.FactIDs[0], fact.EffectiveAt)
	require.NoError(t, err)
	return f, customer, fact.Scope
}

// Audit disposition: named actual identity/signup/visit/payment/native-read
// journeys verify same-snapshot coupling, retry reset and late binding failure.
func TestMongoConversionEvidenceAndReport(t *testing.T) {
	for _, tc := range []struct {
		name, failKind string
		retry          bool
	}{
		{"complete_native_cohort_report", "", false},
		{"reentered_callback_keeps_one_binding", "", true},
		{"late_binding_failure_discards_traffic", kindPaymentBinding, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, scope := nativeConversionFixture(t)
			reader := &analyticsReadStore{Store: f.store, retry: tc.retry, failKind: tc.failKind}
			repo, err := NewReferralRepository(reader)
			require.NoError(t, err)
			s, err := referral.NewService(repo, f.clock, randomIDs{}, time.Hour)
			require.NoError(t, err)
			f.referrals = s
			m := summaryManager(t, f, f.earnings)
			_, err = m.WithRevenueReporting(partnermanager.RevenueReportingConfig{Scopes: []billing.RevenueScope{scope}, StatusMaxAge: time.Hour})
			require.NoError(t, err)
			out, err := m.AdminConversionMetrics(f.ctx, "operator", f.partner.ID, partnermanager.AdminConversionQuery{AnalyticsQuery: referral.AnalyticsQuery{Limit: 1}, PlanLimit: 1})
			if tc.failKind != "" {
				require.ErrorIs(t, err, referral.ErrUnavailable)
				require.Zero(t, out)
				require.Equal(t, 1, reader.reads)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 2, reader.reads, "one native complete read plus one complete recheck")
			require.EqualValues(t, 1, out.Summary.Cohorts.MeasuredSignups.ConfirmedConverted)
			require.EqualValues(t, 1, out.Summary.Cohorts.MeasuredVisits.ConfirmedConverted)
			require.EqualValues(t, 1, out.Summary.Cohorts.MeasuredVisits.Denominator)
			require.Len(t, out.Plans, 1)
		})
	}
}

// Standalone audit exception: one gated persisted overlap pins traffic and
// ownership before a new visit and binding commit. This read/write sequence is
// the behavior, with channel cleanup preventing a leaked reader on failure.
func TestMongoConversionSnapshotPinsTrafficAndBindings(t *testing.T) {
	f, customer, _ := nativeConversionFixture(t)
	gate := &relationshipSnapshotStore{Store: f.store, selected: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(gate.release) }) })
	r, err := NewReferralRepository(gate)
	require.NoError(t, err)
	type result struct {
		value referral.ConversionSnapshot
		err   error
	}
	done := make(chan result, 1)
	go func() {
		value, err := r.ReadConversionSnapshot(f.ctx, referral.ProgramID, f.partner.ID, referral.AnalyticsQuery{Limit: 1})
		done <- result{value, err}
	}()
	select {
	case <-gate.selected:
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	_, err = f.referrals.BindPayment(f.ctx, customer, "later-frozen-allocation", f.clock.Now())
	require.NoError(t, err)
	// Resolve the real stored share code for the new observation.
	page, err := f.referrals.GetAnalytics(f.ctx, f.partner.ID, referral.AnalyticsQuery{Limit: 1})
	require.NoError(t, err)
	_, err = f.manager(t, false).PrepareVisit(f.ctx, partnermanager.PrepareVisitRequest{Code: page.Links[0].Code, Consented: true})
	require.NoError(t, err)
	release.Do(func() { close(gate.release) })
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Len(t, got.value.Bindings[customer], 1)
		require.EqualValues(t, 1, got.value.Analytics.Days[0].Counts.Observations)
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	confirm, err := f.referrals.GetConversionEvidence(context.Background(), f.partner.ID, referral.AnalyticsQuery{Limit: 1})
	require.NoError(t, err)
	require.Len(t, confirm.Relationships.Items[0].Attribution.Bindings, 2)
	require.EqualValues(t, 2, confirm.Analytics.Visits.Observations)
}
