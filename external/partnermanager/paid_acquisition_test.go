package partnermanager

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// paidRecordsStub returns owning pages and records the exact customer query.
type paidRecordsStub struct {
	rows         []billing.Subscription
	err          error
	requests     []*billing.GetSubscriptionsRequest
	changedTotal bool
}

// GetSubscriptions models complete pages, with an optional concurrent inventory change.
func (s *paidRecordsStub) GetSubscriptions(_ context.Context, req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	start := (req.Page - 1) * req.PerPage
	end := min(start+req.PerPage, len(s.rows))
	if start > len(s.rows) {
		return nil, ErrUnavailable
	}
	total := len(s.rows)
	if s.changedTotal && req.Page > 1 {
		total++
	}
	return &billing.GetSubscriptionsResponse{Subscriptions: s.rows[start:end], Total: total, Page: req.Page, PerPage: req.PerPage}, nil
}

// paidEvidenceStub returns authenticated-contract fixtures without provider I/O.
type paidEvidenceStub struct {
	proof paymentprovider.PaidSubscriptionEvidence
	err   error
	calls int
}

// LookupPaidSubscription records use of the fresh evidence port.
func (s *paidEvidenceStub) LookupPaidSubscription(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.PaidSubscriptionEvidence, error) {
	s.calls++
	return s.proof, s.err
}

// paidAcquisitionFixture supplies one owned, mapped and currently paid weekly plan.
func paidAcquisitionFixture() (*paidRecordsStub, *paidEvidenceStub, managerClock, PaidAcquisitionConfig) {
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
	store := &paidRecordsStub{rows: []billing.Subscription{{ID: "native", UserID: "customer", Integrator: "stripe", IntegratorSubscriptionID: "sub_owned", IntegratorCustomerID: "cus_owned", PlanID: "plan_weekly", ProviderPriceID: "price_weekly", BillingKind: "recurring"}}}
	provider := &paidEvidenceStub{proof: paymentprovider.PaidSubscriptionEvidence{Scope: scope, SubscriptionID: "sub_owned", CustomerID: "cus_owned", Status: "active", PaidAt: at.Add(-time.Hour), PaidLines: []paymentprovider.PaidSubscriptionLine{{PriceID: "price_weekly", Interval: "week", IntervalCount: 1, PeriodStart: at.Add(-time.Hour), PeriodEnd: at.Add(167 * time.Hour), NetPaidMinor: 292}}}}
	return store, provider, managerClock{at}, PaidAcquisitionConfig{Scope: scope, EligiblePlanIDs: []string{"plan_weekly", "plan_monthly", "plan_annual"}, Intervals: []string{"week", "month", "year"}}
}

// TestPaidAcquisitionEvidenceBoundary covers commercial decisions, exact time
// boundaries, mismatched ownership and dependency failures without access flags.
func TestPaidAcquisitionEvidenceBoundary(t *testing.T) {
	for _, tc := range []struct {
		name     string
		mutate   func(*paidRecordsStub, *paidEvidenceStub, managerClock)
		eligible bool
		want     error
	}{
		{name: "current paid weekly", eligible: true},
		{name: "monthly cadence", eligible: true, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.PaidLines[0].Interval = "month" }},
		{name: "annual cadence", eligible: true, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.PaidLines[0].Interval = "year" }},
		{name: "period starts now inclusive", eligible: true, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, c managerClock) {
			p.proof.PaidLines[0].PeriodStart = c.Now()
		}},
		{name: "period ends now exclusive", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, c managerClock) {
			p.proof.PaidLines[0].PeriodEnd = c.Now()
		}},
		{name: "future period", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, c managerClock) {
			p.proof.PaidLines[0].PeriodStart = c.Now().Add(time.Second)
		}},
		{name: "future paid timestamp", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, c managerClock) {
			p.proof.PaidAt = c.Now().Add(time.Second)
		}},
		{name: "free account", mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows = nil }},
		{name: "trial", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.Status = "trialing" }},
		{name: "past due", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.Status = "past_due" }},
		{name: "unpaid", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.Status = "unpaid" }},
		{name: "fully refunded or free invoice", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.PaidLines = nil }},
		{name: "access flag does not substitute proof", mutate: func(s *paidRecordsStub, p *paidEvidenceStub, _ managerClock) {
			s.rows[0].Status = "active"
			p.proof.PaidLines = nil
		}},
		{name: "fresh proof survives stale local lifecycle", eligible: true, mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows[0].Status = "past_due" }},
		{name: "unapproved plan", mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows[0].PlanID = "plan_free" }},
		{name: "unsupported cadence", mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.PaidLines[0].Interval = "day" }},
		{name: "one off record", mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows[0].IsOneOff = true }},
		{name: "provider price mismatch", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) {
			p.proof.PaidLines[0].PriceID = "price_other"
		}},
		{name: "provider owner mismatch", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.CustomerID = "cus_other" }},
		{name: "provider subscription mismatch", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.SubscriptionID = "sub_other" }},
		{name: "provider mode mismatch", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.Scope.LiveMode = true }},
		{name: "native foreign owner", want: ErrUnavailable, mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows[0].UserID = "other" }},
		{name: "missing price mapping", want: ErrUnavailable, mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.rows[0].ProviderPriceID = "" }},
		{name: "provider unavailable", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.err = context.DeadlineExceeded }},
		{name: "billing unavailable", want: ErrUnavailable, mutate: func(s *paidRecordsStub, _ *paidEvidenceStub, _ managerClock) { s.err = context.DeadlineExceeded }},
		{name: "malformed positive proof", want: ErrUnavailable, mutate: func(_ *paidRecordsStub, p *paidEvidenceStub, _ managerClock) { p.proof.PaidLines[0].NetPaidMinor = -1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, c, cfg := paidAcquisitionFixture()
			if tc.mutate != nil {
				tc.mutate(s, p, c)
			}
			a, err := NewPaidAcquisition(s, p, c, cfg)
			require.NoError(t, err)
			got, err := a.CanAcquirePartnerReferrals(context.Background(), "customer")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.NotErrorIs(t, err, ErrIneligible)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.eligible, got)
			for _, req := range s.requests {
				require.Equal(t, []string{"customer"}, req.ForUserIDs)
				require.Empty(t, req.ForEmails)
			}
		})
	}
}

// TestPaidAcquisitionScansAllOwnedRecords prevents a one-off or old record from
// hiding a qualifying subscription on another page, and refuses changing pages.
func TestPaidAcquisitionScansAllOwnedRecords(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint("inventory changed=", changed), func(t *testing.T) {
			s, p, c, cfg := paidAcquisitionFixture()
			paid := s.rows[0]
			s.rows = nil
			for n := 0; n < 100; n++ {
				s.rows = append(s.rows, billing.Subscription{ID: fmt.Sprint("old-", n), UserID: "customer", IsOneOff: true})
			}
			s.rows = append(s.rows, paid)
			s.changedTotal = changed
			a, err := NewPaidAcquisition(s, p, c, cfg)
			require.NoError(t, err)
			got, err := a.CanAcquirePartnerReferrals(context.Background(), "customer")
			if changed {
				require.ErrorIs(t, err, ErrUnavailable)
				require.False(t, got)
				require.Zero(t, p.calls)
			} else {
				require.NoError(t, err)
				require.True(t, got)
				require.Equal(t, 1, p.calls)
			}
			require.Len(t, s.requests, 2)
		})
	}
}
