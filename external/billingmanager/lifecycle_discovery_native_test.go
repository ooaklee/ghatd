package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// The actual billing service/adapter operate over isolated native records.
// This fixture enforces the query contract the older status fixture did not
// need: ordering, state, cursor and limit. Replica-set encryption/CAS/index proof
// remains in the owning adapter suite; this is not full platform/browser E2E.
type discoveryNativeStore struct{ *statusManagerStore }
type discoveryNativeTx struct{ recordstore.Tx }

func (t discoveryNativeTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	rows, err := t.Tx.Find(ctx, q)
	if err != nil {
		return nil, err
	}
	out := []recordstore.Record{}
	for _, row := range rows {
		if row.ID > q.AfterID && (q.State == "" || row.State == q.State) {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}
func (s discoveryNativeStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.statusManagerStore.Read(ctx, func(tx recordstore.Tx) error { return fn(discoveryNativeTx{tx}) })
}
func (s discoveryNativeStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	return s.statusManagerStore.Transact(ctx, key, func(tx recordstore.Tx) error { return fn(discoveryNativeTx{tx}) })
}

type discoveryNativeCheckoutProvider struct {
	e     paymentprovider.RevenueCheckoutEvidence
	calls int
}

func (p *discoveryNativeCheckoutProvider) LookupRevenueCheckout(context.Context, paymentprovider.RevenueScope, string) (paymentprovider.RevenueCheckoutEvidence, error) {
	p.calls++
	return p.e, nil
}
func nativeDiscoveryCheckout(t *testing.T, ctx context.Context, repo billing.CheckoutRepository, clock fixtureBillingStatusClock, p *discoveryNativeCheckoutProvider, scope billing.RevenueScope, key, principal, session, sub string, anchor bool) *billing.CheckoutIntent {
	t.Helper()
	checkout, err := billing.NewCheckoutService(repo, clock, p)
	require.NoError(t, err)
	q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: key, PriceID: "price_original", PlanID: "plan", CostID: "cost", UserID: principal, UserReference: principal, CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: "subscription", ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14}
	i, err := checkout.PrepareCheckout(ctx, scope, q)
	require.NoError(t, err)
	require.NoError(t, checkout.AcknowledgeCheckout(ctx, i, session))
	i, err = checkout.FindCheckoutIntent(ctx, scope, key)
	require.NoError(t, err)
	e := paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID, LiveMode: scope.LiveMode}, SessionID: session, IntentID: i.ID, ClientReferenceID: principal, CustomerID: "cus_" + principal, SubscriptionID: sub, PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
	if anchor {
		_, err = checkout.CaptureCheckoutLifecycleEvidence(ctx, i, e)
		require.NoError(t, err)
	} else {
		p.e = e
		_, err = checkout.ResolveRevenueAssociation(ctx, billing.RevenueAssociationRequest{Scope: scope, SubscriptionID: sub, ProviderPriceID: q.PriceID, Currency: "GBP", CustomerID: e.CustomerID, PaidAt: clock.at, InvoiceID: "in_binding_evidence", PaymentID: "pi_binding_evidence"})
		require.NoError(t, err)
	}
	return &i
}
func prepareNativeDiscovery(t *testing.T, ctx context.Context, owner *billing.RevenueService, scope billing.RevenueScope) {
	t.Helper()
	for n := 0; n < 30; n++ {
		out, err := owner.PrepareLifecycleDiscovery(ctx, scope, 200)
		require.NoError(t, err)
		if out.State.Phase == billing.LifecyclePreparationComplete {
			return
		}
	}
	t.Fatal("fixture preparation incomplete")
}
func TestLifecycleDiscoveryManagerOwningIntegration(t *testing.T) {
	denied := errors.New("selected native discovery source denied")
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "acknowledged_original_checkout_is_selected", change: "checkout"},
		{name: "first_trial_anchor_before_paid_fact"},
		{name: "accepted_payment_shares_existing_checkout_owner", change: "paid"},
		{name: "short_page_preserves_binding_only_tail_cursor", change: "tail"},
		{name: "binding_only_page_preserves_no_refreshable_items", change: "binding-only"},
		{name: "independent_account_prepared_empty", change: "account"},
		{name: "unprepared_scope_withholds_native_page", change: "unprepared", want: billing.ErrLifecycleDiscoveryUnprepared},
		{name: "current_selected_ack_permission_required", change: "checkout-denied", want: denied},
		{name: "current_selected_subscription_permission_required", change: "subscription-denied", want: denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			records := &statusManagerStore{records: map[string]recordstore.Record{}}
			store := discoveryNativeStore{records}
			repo, err := revenuestore.NewRepository(store)
			require.NoError(t, err)
			clock := fixtureBillingStatusClock{time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
			owner, err := billing.NewRevenueService(repo, clock)
			require.NoError(t, err)
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			p := &discoveryNativeCheckoutProvider{}
			original := nativeDiscoveryCheckout(t, ctx, repo, clock, p, scope, "original", "native-payer", "cs_original", "sub_original", tc.change != "binding-only")
			if tc.change == "paid" {
				_, err = owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "evt_original", Facts: []billing.RevenueFact{{Scope: scope, Kind: billing.RevenuePayment, PaymentID: "pi_original", InvoiceID: "in_original", AllocationID: "il_original", PrincipalID: original.Request.UserID, ProviderCustomerID: "cus_native-payer", SubscriptionID: "sub_original", PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: clock.at}}})
				require.NoError(t, err)
			}
			var tailID string
			if tc.change == "tail" {
				firstID := billing.LifecycleDiscoverySourceID(scope, billing.LifecycleSubscriptionSources, "sub_original")
				var sub string
				for n := 0; n < 100; n++ {
					sub = fmt.Sprintf("sub_binding_%d", n)
					tailID = billing.LifecycleDiscoverySourceID(scope, billing.LifecycleSubscriptionSources, sub)
					if tailID > firstID {
						break
					}
				}
				require.Greater(t, tailID, firstID)
				nativeDiscoveryCheckout(t, ctx, repo, clock, p, scope, "binding-tail", "other-native-payer", "cs_bound", sub, false)
			}
			q := billing.LifecycleDiscoveryQuery{Scope: scope, Kind: billing.LifecycleSubscriptionSources, Limit: 200}
			if tc.change == "checkout" || tc.change == "checkout-denied" {
				q.Kind = billing.LifecycleCheckoutSources
			}
			if tc.change == "tail" {
				q.Limit = 2
			}
			if tc.change == "binding-only" {
				q.Limit = 1
			}
			if tc.change == "account" {
				q.Scope.AccountID = "acct_other"
			}
			if tc.change != "unprepared" {
				prepareNativeDiscovery(t, ctx, owner, q.Scope)
			}
			auth := &discoveryManagerAuthority{err: denied}
			if tc.change == "checkout-denied" || tc.change == "subscription-denied" {
				auth.denyAt = 2
			}
			manager, err := (&Service{revenueFeed: owner}).WithLifecycleDiscoveryAuthority(auth)
			require.NoError(t, err)
			before := map[string]recordstore.Record{}
			for key, row := range records.records {
				before[key] = row
			}
			p.calls = 0
			page, err := manager.DiscoverLifecycleSources(ctx, "current-instance-worker", q)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, before, records.records, "discovery is read-only across all native records")
			require.Zero(t, p.calls)
			if tc.want != nil {
				require.Zero(t, page)
				return
			}
			if tc.change == "account" || tc.change == "binding-only" {
				require.Empty(t, page.Items)
			} else {
				require.Len(t, page.Items, 1)
				target := auth.targets[1]
				require.Equal(t, original.Request.UserID, target.PrincipalID)
				if q.Kind == billing.LifecycleCheckoutSources {
					require.Equal(t, original.ID, target.IntentID)
					require.Empty(t, target.SubscriptionID)
				} else {
					require.Equal(t, "sub_original", target.SubscriptionID)
					require.Empty(t, target.IntentID)
				}
			}
			if tc.change == "tail" || tc.change == "binding-only" {
				require.False(t, page.ReachedEnd)
				require.NotEmpty(t, page.NextCursor)
				if tc.change == "tail" {
					next := q
					next.Cursor = page.NextCursor
					id, err := next.AfterID()
					require.NoError(t, err)
					require.Equal(t, tailID, id)
					require.Greater(t, id, page.Items[0].ID)
				}
				q.Cursor = page.NextCursor
				auth.targets = nil
				auth.actors = nil
				auth.actions = nil
				done, err := manager.DiscoverLifecycleSources(ctx, "current-instance-worker", q)
				require.NoError(t, err)
				require.Empty(t, done.Items)
				require.True(t, done.ReachedEnd)
				require.Len(t, auth.targets, 2)
			}
		})
	}
}

func TestLifecycleDiscoveryManagerWholePageDenial(t *testing.T) {
	denied := errors.New("native payer or final scope revoked")
	for _, tc := range []struct {
		name   string
		denyAt int
	}{{name: "second_payer_denial_withholds_first_authorized_payer", denyAt: 3}, {name: "final_scope_denial_withholds_both_authorized_payers", denyAt: 4}} {
		t.Run(tc.name, func(t *testing.T) {
			_, owner, _, _, _, id := statusManagerFixture(t)
			ctx := context.Background()
			f, err := owner.GetRevenueFact(ctx, id)
			require.NoError(t, err)
			second := f
			second.ID = ""
			second.Sequence = 0
			second.AcceptedAt = time.Time{}
			second.Fingerprint = ""
			second.PaymentID = "second-payment"
			second.InvoiceID = "second-invoice"
			second.SubscriptionID = "sub_second"
			second.PrincipalID = "other-native-payer"
			o, err := owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "second-native-payment", Facts: []billing.RevenueFact{second}})
			require.NoError(t, err)
			second, err = owner.GetRevenueFact(ctx, o.FactIDs[0])
			require.NoError(t, err)
			q := billing.LifecycleDiscoveryQuery{Scope: f.Scope, Kind: billing.LifecycleSubscriptionSources, Limit: 2}
			items := []billing.LifecycleDiscoveryCandidate{}
			for _, fact := range []billing.RevenueFact{f, second} {
				items = append(items, billing.LifecycleDiscoveryCandidate{ID: billing.LifecycleDiscoverySourceID(q.Scope, q.Kind, fact.SubscriptionID), Revision: 1, Scope: fact.Scope, PrincipalID: fact.PrincipalID, CustomerID: fact.ProviderCustomerID, SubscriptionID: fact.SubscriptionID, Fact: fact})
			}
			sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
			proxy := &discoveryManagerOwner{page: billing.LifecycleDiscoveryPage{Items: items, NextCursor: q.CursorFor(items[1].ID)}}
			auth := &discoveryManagerAuthority{denyAt: tc.denyAt, err: denied}
			manager, err := (&Service{revenueFeed: proxy}).WithLifecycleDiscoveryAuthority(auth)
			require.NoError(t, err)
			page, err := manager.DiscoverLifecycleSources(ctx, "current-worker", q)
			require.ErrorIs(t, err, denied)
			require.Zero(t, page)
			require.Equal(t, tc.denyAt, len(auth.targets))
			require.Equal(t, items[0].PrincipalID, auth.targets[1].PrincipalID)
			require.Equal(t, items[1].PrincipalID, auth.targets[2].PrincipalID)
		})
	}
}
