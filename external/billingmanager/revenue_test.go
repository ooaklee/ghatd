package billingmanager

import (
	"context"
	"errors"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Audit disposition: new named cases verify orchestration only; authenticated
// provider and replica-set atomic acceptance have separate owning-package tests.
type revenueBoundaryProvider struct {
	e         *paymentprovider.RevenueEvidence
	err       error
	invoice   *paymentprovider.RevenueInvoiceEvidence
	lookupErr error
	lookups   []paymentprovider.RevenueInvoiceRequest
}

func (p *revenueBoundaryProvider) ResolveRevenueWebhook(context.Context, *http.Request) (*paymentprovider.RevenueEvidence, error) {
	return p.e, p.err
}
func (p *revenueBoundaryProvider) LookupRevenueInvoice(_ context.Context, r paymentprovider.RevenueInvoiceRequest) (*paymentprovider.RevenueInvoiceEvidence, error) {
	p.lookups = append(p.lookups, r)
	return p.invoice, p.lookupErr
}

type revenueBoundaryRegistry struct {
	p   paymentprovider.RevenueProvider
	err error
}

func (r revenueBoundaryRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	return r.p, r.err
}

type revenueBoundaryFeed struct {
	requests                 []billing.VerifiedRevenueRequest
	original                 []billing.RevenueFact
	lookupErr, errorOnAccept error
}

func (f *revenueBoundaryFeed) AcceptVerified(_ context.Context, r billing.VerifiedRevenueRequest) (billing.RevenueObservation, error) {
	f.requests = append(f.requests, r)
	return billing.RevenueObservation{ID: "durable-observation"}, f.errorOnAccept
}
func (f *revenueBoundaryFeed) FindPaymentRevenueFacts(context.Context, billing.RevenueScope, string) ([]billing.RevenueFact, error) {
	return f.original, f.lookupErr
}
func (f *revenueBoundaryFeed) GetRevenueObservation(context.Context, string) (billing.RevenueObservation, error) {
	return billing.RevenueObservation{}, billing.ErrRevenueNotFound
}
func (f *revenueBoundaryFeed) ResolveQuarantinedRevenue(context.Context, billing.ResolveRevenueRequest) (billing.RevenueObservation, error) {
	return billing.RevenueObservation{}, billing.ErrRevenueUnavailable
}

type revenueBoundaryAssociation struct {
	requests []RevenueAssociationRequest
	err      error
}

func (a *revenueBoundaryAssociation) ResolveRevenueAssociation(_ context.Context, r RevenueAssociationRequest) (RevenueAssociation, error) {
	a.requests = append(a.requests, r)
	return RevenueAssociation{PrincipalID: "owning-account", PlanID: "historical-plan", CostID: "historical-cost"}, a.err
}

func TestRevenueWebhookOwningBoundary(t *testing.T) {
	type testCase struct {
		name       string
		kind       string
		original   bool
		mutate     func(*revenueBoundaryProvider, *revenueBoundaryFeed, *revenueBoundaryAssociation)
		quarantine string
		want       error
		accepted   bool
	}
	cases := []testCase{
		{name: "first_payment_resolves_server_owned_historical_principal", accepted: true},
		{name: "economic_replay_preserves_original_identity_despite_current_plan", original: true, accepted: true},
		{name: "missing_historical_association_is_durable_quarantine", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			a.err = billing.ErrRevenueNotFound
		}, quarantine: "historical_payer_plan_pending", accepted: true},
		{name: "owning_identity_outage_is_retryable", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			a.err = billing.ErrRevenueUnavailable
		}, want: billing.ErrRevenueUnavailable},
		{name: "joined_absence_and_identity_outage_is_not_quarantined", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			a.err = errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUnavailable)
		}, want: billing.ErrRevenueUnavailable},
		{name: "joined_unassessable_and_identity_outage_is_not_quarantined", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			a.err = errors.Join(billing.ErrRevenueUnassessable, billing.ErrRevenueUnavailable)
		}, want: billing.ErrRevenueUnavailable},
		{name: "provider_outage_is_retryable", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			p.err = paymentprovider.ErrPaymentProviderAPIRequestFailed
		}, want: paymentprovider.ErrPaymentProviderAPIRequestFailed},
		{name: "feed_uncertain_commit_is_not_acknowledged", mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			f.errorOnAccept = billing.ErrRevenueUncertain
		}, want: billing.ErrRevenueUncertain},
		{name: "full_refund_uses_original_payer_and_exact_net", kind: billing.RevenueRefund, original: true, accepted: true},
		{name: "refund_before_original_acceptance_remains_pending", kind: billing.RevenueRefund, quarantine: "original_payment_pending", accepted: true},
		{name: "unallocated_refund_has_no_estimated_fact", kind: billing.RevenueRefund, original: true, mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			p.invoice.Lines[0].CumulativeRefundedMinor = 0
		}, quarantine: "refund_allocation_pending", accepted: true},
		{name: "full_dispute_uses_original_economics", kind: billing.RevenueDisputeHold, original: true, accepted: true},
		{name: "partial_dispute_without_line_evidence_remains_pending", kind: billing.RevenueDisputeHold, original: true, mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			p.e.AffectedMinor = 100
		}, quarantine: "partial_dispute_allocation_pending", accepted: true},
		{name: "changed_original_paid_amount_is_quarantined", original: true, mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			p.invoice.Lines[0].NetPaidMinor++
		}, quarantine: "original_economics_changed", accepted: true},
		{name: "wrong_adjustment_currency_is_quarantined", kind: billing.RevenueRefund, original: true, mutate: func(p *revenueBoundaryProvider, f *revenueBoundaryFeed, a *revenueBoundaryAssociation) {
			p.e.Currency = "USD"
		}, quarantine: "adjustment_currency_mismatch", accepted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
			at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			invoice := &paymentprovider.RevenueInvoiceEvidence{Scope: scope, InvoiceID: "in_invoice", PaymentID: "pi_payment", CustomerID: "cus_payer", SubscriptionID: "sub_recurring", Currency: "GBP", CurrencyExponent: 2, PaidAt: at, GrossPaidMinor: 840, Lines: []paymentprovider.RevenueLineEvidence{{ID: "il_one", SubscriptionID: "sub_recurring", PriceID: "price_frozen", NetPaidMinor: 700, CumulativeRefundedMinor: 700}}}
			kind := tc.kind
			if kind == "" {
				kind = billing.RevenuePayment
			}
			p := &revenueBoundaryProvider{invoice: invoice, e: &paymentprovider.RevenueEvidence{Scope: scope, EnvelopeID: "evt_fixture", Kind: kind, PaymentID: "pi_payment", EffectiveAt: at.Add(time.Hour), Currency: "GBP", AffectedMinor: 840, CumulativeRefundedGrossMinor: 840, AdjustmentID: "adjustment"}}
			if kind == billing.RevenuePayment {
				p.e.Invoice = invoice
			}
			feed := &revenueBoundaryFeed{}
			association := &revenueBoundaryAssociation{}
			if tc.original {
				feed.original = []billing.RevenueFact{{Scope: billingRevenueScope(scope), Kind: billing.RevenuePayment, PaymentID: "pi_payment", InvoiceID: "in_invoice", AllocationID: "il_one", PrincipalID: "original-payer", SubscriptionID: "sub_recurring", PlanID: "original-plan", CostID: "original-cost", ProviderPriceID: "price_frozen", ProviderCustomerID: "cus_payer", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 700, EffectiveAt: at}}
			}
			if tc.mutate != nil {
				tc.mutate(p, feed, association)
			}
			s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: p}, feed, association)
			require.NoError(t, err)
			accepted, err := s.acceptRevenueWebhook(context.Background(), "stripe", httptest.NewRequest(http.MethodPost, "/webhook", nil))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, accepted)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.accepted, accepted)
			require.Len(t, feed.requests, 1)
			req := feed.requests[0]
			require.Equal(t, tc.quarantine, req.QuarantineReason)
			if tc.quarantine != "" {
				require.Empty(t, req.Facts)
				return
			}
			require.Len(t, req.Facts, 1)
			fact := req.Facts[0]
			require.EqualValues(t, 700, fact.PaidMinor)
			require.Equal(t, kind, fact.Kind)
			require.Equal(t, "price_frozen", fact.ProviderPriceID)
			if tc.original {
				require.Empty(t, association.requests)
				require.Equal(t, "original-payer", fact.PrincipalID)
				require.Equal(t, "original-plan", fact.PlanID)
			} else {
				require.Len(t, association.requests, 1)
				require.Equal(t, "owning-account", fact.PrincipalID)
				require.Equal(t, at, association.requests[0].PaidAt)
			}
			if kind == billing.RevenueRefund {
				require.EqualValues(t, 700, fact.CumulativeRefundedMinor)
				require.True(t, p.lookups[0].IncludeRefunds)
				require.EqualValues(t, 840, p.lookups[0].ExpectedCumulativeRefundedGrossMinor)
			}
		})
	}
}

func TestRevenueOptionalCapabilities(t *testing.T) {
	type testCase struct {
		name          string
		service       *Service
		registryError error
		providerError error
		wantAccepted  bool
		wantError     error
	}
	cases := []testCase{{name: "feature_not_opted_in", service: &Service{}}, {name: "webhook_only_provider", registryError: paymentprovider.ErrPaymentProviderUnsupportedProvider}, {name: "stripe_revenue_not_enabled", providerError: paymentprovider.ErrRevenueNotEnabled}, {name: "nonfinancial_event", providerError: paymentprovider.ErrRevenueEventNotRelevant}, {name: "missing_registered_provider", registryError: paymentprovider.ErrPaymentProviderNotFound, wantError: paymentprovider.ErrPaymentProviderNotFound}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.service
			if s == nil {
				s = &Service{revenueRegistry: revenueBoundaryRegistry{p: &revenueBoundaryProvider{err: tc.providerError}, err: tc.registryError}, revenueFeed: &revenueBoundaryFeed{}, revenueAssociation: &revenueBoundaryAssociation{}}
			}
			accepted, err := s.acceptRevenueWebhook(context.Background(), "stripe", httptest.NewRequest(http.MethodPost, "/", nil))
			require.Equal(t, tc.wantAccepted, accepted)
			if tc.wantError == nil {
				require.NoError(t, err)
			} else {
				require.True(t, errors.Is(err, tc.wantError))
			}
		})
	}
}

func (f *revenueBoundaryFeed) GetRevenueSourceResolution(context.Context, string) (billing.RevenueObservation, error) {
	return billing.RevenueObservation{}, billing.ErrRevenueNotFound
}

func (f *revenueBoundaryFeed) GetRevenueDelivery(context.Context, billing.RevenueScope, string) (billing.RevenueObservation, error) {
	return billing.RevenueObservation{}, billing.ErrRevenueNotFound
}
