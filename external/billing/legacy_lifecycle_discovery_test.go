package billing

import (
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named boundary cases validate historical provenance before
// binding-only filtering; each case owns its receipt and invoice-line slice.
func legacyDiscoveryCandidate(t *testing.T) (LifecycleDiscoveryQuery, LifecycleDiscoveryCandidate) {
	t.Helper()
	_, _, _, f := statusFixture(t)
	q := LifecycleDiscoveryQuery{Scope: f.Scope, Kind: LifecycleSubscriptionSources, Limit: 1}
	created := f.AcceptedAt.Add(-time.Hour)
	a := CheckoutAssociation{Scope: f.Scope, SessionID: "cs_legacy", CustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID, ProviderPriceID: "price_legacy", Currency: f.Currency, PrincipalID: f.PrincipalID, PlanID: f.PlanID, CostID: f.CostID, CheckoutCreatedAt: created, LinkedAt: f.AcceptedAt}
	providerScope := paymentprovider.RevenueScope{Provider: f.Scope.Provider, AccountID: f.Scope.AccountID, LiveMode: f.Scope.LiveMode}
	r := LegacyCheckoutMigrationReceipt{ID: LegacyCheckoutMigrationID(f.Scope, a.SessionID), Version: LegacyCheckoutAssociationVersion, MigratedAt: a.LinkedAt, ActorID: "reviewed-operator", Reason: "reviewed_history", SourceID: "original-source", EnvelopeID: "original-envelope", SourceObservationFingerprint: "observation-fingerprint", SourceFingerprint: "source-fingerprint", RequestFingerprint: strings.Repeat("a", 64), Association: a,
		Checkout: paymentprovider.RevenueCheckoutEvidence{Scope: providerScope, SessionID: a.SessionID, ClientReferenceID: a.PrincipalID, CustomerID: a.CustomerID, SubscriptionID: a.SubscriptionID, PriceID: a.ProviderPriceID, Currency: a.Currency, Mode: "subscription", Status: "complete", CreatedAt: created, UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month"},
		Invoice:  paymentprovider.RevenueInvoiceEvidence{Scope: providerScope, InvoiceID: "in_legacy", PaymentID: "pi_legacy", CustomerID: a.CustomerID, SubscriptionID: a.SubscriptionID, Currency: a.Currency, CurrencyExponent: 2, PaidAt: created.Add(time.Minute), GrossPaidMinor: 1000, Lines: []paymentprovider.RevenueLineEvidence{{ID: "li_legacy", SubscriptionID: a.SubscriptionID, PriceID: a.ProviderPriceID, NetPaidMinor: 1000}}},
	}
	r.ProofFingerprint = r.ProofDigest()
	require.NoError(t, r.Validate())
	return q, LifecycleDiscoveryCandidate{ID: LifecycleDiscoverySourceID(q.Scope, q.Kind, f.SubscriptionID), Revision: 1, Scope: f.Scope, PrincipalID: f.PrincipalID, CustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID, Fact: f, HasPaidOwner: true, PaidOwner: a, LegacyPaidOwner: &r}
}

func TestLegacyLifecycleDiscoveryProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
		items        int
	}{
		{name: "accepted_renewal_fact_is_refreshable", items: 1},
		{name: "binding_only_advances_cursor", change: "binding-only"},
		{name: "missing_receipt", change: "missing", want: ErrRevenueUnavailable},
		{name: "corrupt_proof_before_binding_filter", change: "proof", want: ErrRevenueUnavailable},
		{name: "different_association", change: "association", want: ErrRevenueUnavailable},
		{name: "dangling_receipt", change: "dangling", want: ErrRevenueUnavailable},
		{name: "modern_owner_and_historical_receipt", change: "mixed-owner", want: ErrRevenueUnavailable},
		{name: "modern_intent_and_historical_receipt", change: "mixed-intent", want: ErrRevenueUnavailable},
		{name: "hidden_modern_request_and_historical_receipt", change: "mixed-request", want: ErrRevenueUnavailable},
		{name: "wrong_selected_owner", change: "owner", want: ErrRevenueConflict},
		{name: "returned_receipt_is_detached", change: "clone", items: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, c := legacyDiscoveryCandidate(t)
			switch tc.change {
			case "binding-only":
				c.Fact = RevenueFact{}
			case "missing":
				c.LegacyPaidOwner = nil
			case "proof":
				c.Fact = RevenueFact{}
				c.LegacyPaidOwner.ProofFingerprint = strings.Repeat("b", 64)
			case "association":
				c.PaidOwner.PlanID = "other-plan"
			case "dangling":
				c.HasPaidOwner = false
			case "mixed-owner":
				c.PaidOwner.IntentID = "modern-intent"
			case "mixed-intent":
				c.PaidOwnerIntent.ID = "modern-intent"
			case "mixed-request":
				c.PaidOwnerIntent.Request.PriceID = "modern-price"
			case "owner":
				c.PrincipalID = "other-payer"
			}
			repo := &discoveryReadRepo{snapshot: LifecycleDiscoverySnapshot{Prepared: true, Items: []LifecycleDiscoveryCandidate{c}}}
			page, err := (&RevenueService{repo: repo}).DiscoverLifecycleSources(t.Context(), q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, page)
				return
			}
			require.Len(t, page.Items, tc.items)
			require.False(t, page.ReachedEnd)
			require.Equal(t, q.CursorFor(c.ID), page.NextCursor)
			if tc.change == "clone" {
				page.Items[0].LegacyPaidOwner.ActorID = "changed"
				page.Items[0].LegacyPaidOwner.Invoice.Lines[0].NetPaidMinor++
				require.Equal(t, "reviewed-operator", c.LegacyPaidOwner.ActorID)
				require.EqualValues(t, 1000, c.LegacyPaidOwner.Invoice.Lines[0].NetPaidMinor)
			}
		})
	}
}
