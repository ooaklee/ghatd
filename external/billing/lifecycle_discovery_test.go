package billing

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type discoveryReadRepo struct {
	RevenueRepository
	snapshot LifecycleDiscoverySnapshot
	err      error
	calls    int
	onRead   func()
}

func (r *discoveryReadRepo) ReadLifecycleDiscovery(context.Context, LifecycleDiscoveryQuery) (LifecycleDiscoverySnapshot, error) {
	r.calls++
	if r.onRead != nil {
		r.onRead()
	}
	return r.snapshot, r.err
}

// Fresh narrow-port tables validate complete pages before disclosure. Real
// encrypted snapshot/index/upgrade-marker joins are checked in the adapter suite.
func TestLifecycleDiscoveryServiceBoundaries(t *testing.T) {
	outage := errors.New("owning snapshot unavailable")
	cases := []struct {
		name, change string
		want         error
	}{
		{name: "paid_original_source"},
		{name: "full_page_advances_bound_cursor", change: "full-page"}, {name: "projection_empty_is_prepared_empty", change: "empty"},
		{name: "unprepared_is_not_empty", change: "unprepared", want: ErrLifecycleDiscoveryUnprepared},
		{name: "missing_optional_capability", change: "legacy", want: ErrRevenueUnavailable},
		{name: "typed_nil_repository", change: "nil-repository", want: ErrRevenueUnavailable},
		{name: "nil_context", change: "nil-context", want: ErrRevenueInvalid},
		{name: "cancelled_before_read", change: "cancel-before", want: context.Canceled},
		{name: "cancelled_after_read_withholds_page", change: "cancel-after", want: context.Canceled},
		{name: "repository_failure_withholds_partial_page", change: "outage", want: outage},
		{name: "joined_failure_preserved", change: "joined-outage", want: outage},
		{name: "capacity_overflow_withholds_page", change: "oversized", want: ErrRevenueUnavailable},
		{name: "wrong_native_source_identity", change: "identity", want: ErrRevenueUnavailable},
		{name: "different_scope", change: "scope", want: ErrRevenueUnavailable},
		{name: "different_payer", change: "payer", want: ErrRevenueUnavailable},
		{name: "different_customer", change: "customer", want: ErrRevenueUnavailable},
		{name: "changed_original_fact", change: "fact", want: ErrRevenueUnavailable},
		{name: "refund_is_not_payment_source", change: "refund", want: ErrRevenueUnavailable},
		{name: "bound_checkout_is_not_status_source", change: "bound-only"},
		{name: "binding_only_full_page_still_advances_cursor", change: "bound-full-page"},
		{name: "selected_checkout_binding_disagrees", change: "binding-conflict", want: ErrRevenueConflict},
		{name: "late_bad_item_withholds_earlier_valid_item", change: "bad-tail", want: ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, f := statusFixture(t)
			q := LifecycleDiscoveryQuery{Scope: f.Scope, Kind: LifecycleSubscriptionSources, Limit: 2}
			c := LifecycleDiscoveryCandidate{ID: LifecycleDiscoverySourceID(q.Scope, q.Kind, f.SubscriptionID), Revision: 1, Scope: f.Scope, PrincipalID: f.PrincipalID, CustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID, Fact: f}
			repo := &discoveryReadRepo{snapshot: LifecycleDiscoverySnapshot{Prepared: true, Items: []LifecycleDiscoveryCandidate{c}}}
			svc := &RevenueService{repo: repo}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch tc.change {
			case "empty":
				repo.snapshot.Items = nil
			case "unprepared":
				repo.snapshot.Prepared = false
			case "legacy":
				svc.repo = &revenueTestRepo{}
			case "nil-repository":
				svc.repo = (*discoveryReadRepo)(nil)
			case "nil-context":
				ctx = nil
			case "cancel-before":
				cancel()
			case "cancel-after":
				repo.onRead = cancel
			case "outage":
				repo.err = outage
			case "joined-outage":
				repo.err = errors.Join(ErrRevenueNotFound, outage)
			case "oversized":
				repo.snapshot.Items = []LifecycleDiscoveryCandidate{c, c, c}
			case "identity":
				c.ID = strings.Repeat("f", 64)
			case "scope":
				c.Scope.AccountID = "another-account"
			case "payer":
				c.PrincipalID = "another-payer"
			case "customer":
				c.CustomerID = "another-customer"
			case "fact":
				c.Fact.PaidMinor++
			case "refund":
				c.Fact.Kind = RevenueRefund
			case "full-page":
				q.Limit = 1
			case "bound-only", "bound-full-page":
				if tc.change == "bound-full-page" {
					q.Limit = 1
				}
				c.Fact = RevenueFact{}
				c.HasPaidOwner = true
				request := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "bound", PriceID: "price", PlanID: f.PlanID, CostID: f.CostID, UserID: c.PrincipalID, UserReference: c.PrincipalID, CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: f.Currency, ExpectedAmount: 1000, ExpectedBillingCadence: "month"}
				intentID := checkoutIntentID(q.Scope, request.IdempotencyKey)
				request.Metadata = map[string]string{"checkout_intent_id": intentID}
				c.PaidOwnerIntent = CheckoutIntent{ID: intentID, Scope: q.Scope, Request: request, CreatedAt: f.AcceptedAt, SessionID: "cs_bound", Fingerprint: checkoutRequestFingerprint(q.Scope, request)}
				c.PaidOwner = CheckoutAssociation{IntentID: intentID, SessionID: "cs_bound", Scope: q.Scope, SubscriptionID: c.SubscriptionID, PrincipalID: c.PrincipalID, CustomerID: c.CustomerID, PlanID: f.PlanID, CostID: f.CostID, ProviderPriceID: "price", Currency: f.Currency, CheckoutCreatedAt: f.AcceptedAt, LinkedAt: f.AcceptedAt}
			case "binding-conflict":
				c.HasPaidOwner = true
				c.PaidOwner = CheckoutAssociation{Scope: q.Scope, SubscriptionID: c.SubscriptionID, PrincipalID: "different-owner", CustomerID: c.CustomerID, LinkedAt: f.AcceptedAt}
			case "bad-tail":
				tail := c
				tail.ID = strings.Repeat("f", 64)
				tail.Fact.PaidMinor++
				repo.snapshot.Items = append(repo.snapshot.Items, tail)
			}
			if len(repo.snapshot.Items) == 1 {
				repo.snapshot.Items[0] = c
			}
			page, err := svc.DiscoverLifecycleSources(ctx, q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, page)
				return
			}
			if tc.change == "empty" || tc.change == "bound-only" || tc.change == "bound-full-page" {
				require.Empty(t, page.Items)
			} else {
				require.Len(t, page.Items, 1)
				require.Equal(t, f, page.Items[0].Fact)
			}
			if tc.change == "full-page" || tc.change == "bound-full-page" {
				require.False(t, page.ReachedEnd)
				require.Equal(t, q.CursorFor(c.ID), page.NextCursor)
			} else {
				require.True(t, page.ReachedEnd)
			}
			wire, err := json.Marshal(page)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(wire))
		})
	}
}
func TestLifecycleDiscoveryCursorIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "same_scope_continuation"}, {name: "account_cursor_not_reusable", change: "account", want: ErrRevenueInvalid}, {name: "mode_cursor_not_reusable", change: "mode", want: ErrRevenueInvalid}, {name: "provider_cursor_not_reusable", change: "provider", want: ErrRevenueInvalid}, {name: "kind_cursor_not_reusable", change: "kind", want: ErrRevenueInvalid}, {name: "malformed_position", change: "position", want: ErrRevenueInvalid}, {name: "malformed_version", change: "version", want: ErrRevenueInvalid}, {name: "zero_limit", change: "zero", want: ErrRevenueInvalid}, {name: "oversize_limit", change: "limit", want: ErrRevenueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := LifecycleDiscoveryQuery{Scope: RevenueScope{Provider: "stripe", AccountID: "acct_one"}, Kind: LifecycleSubscriptionSources, Limit: 1}
			id := strings.Repeat("a", 64)
			q.Cursor = q.CursorFor(id)
			switch tc.change {
			case "account":
				q.Scope.AccountID = "acct_two"
			case "mode":
				q.Scope.LiveMode = true
			case "provider":
				q.Scope.Provider = "other"
			case "kind":
				q.Kind = LifecycleCheckoutSources
			case "position":
				q.Cursor = q.CursorFor("sub_raw")
			case "version":
				q.Cursor = strings.Replace(q.Cursor, ".1.", ".2.", 1)
			case "zero":
				q.Limit = 0
			case "limit":
				q.Limit = 201
			}
			got, err := q.AfterID()
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, id, got)
			} else {
				require.Empty(t, got)
			}
		})
	}
}

func TestLifecycleDiscoveryPageContract(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "valid_canonical_paid_page"},
		{name: "short_page_can_continue_after_invisible_binding_tail", change: "tail"},
		{name: "empty_visible_page_can_continue_after_binding_rows", change: "empty-tail"},
		{name: "empty_final_page", change: "empty"},
		{name: "full_item_page_can_continue", change: "full"},
		{name: "nonterminal_requires_cursor", change: "no-cursor", want: ErrRevenueUnavailable},
		{name: "terminal_cannot_have_cursor", change: "terminal-cursor", want: ErrRevenueUnavailable},
		{name: "cursor_for_wrong_account_rejected", change: "cursor-scope", want: ErrRevenueUnavailable},
		{name: "cursor_for_wrong_kind_rejected", change: "cursor-kind", want: ErrRevenueUnavailable},
		{name: "cursor_cannot_rewind_input", change: "rewind", want: ErrRevenueUnavailable},
		{name: "cursor_cannot_precede_visible_item", change: "before-item", want: ErrRevenueUnavailable},
		{name: "oversized_items_rejected", change: "oversized", want: ErrRevenueUnavailable},
		{name: "duplicate_item_ids_rejected", change: "duplicate", want: ErrRevenueUnavailable},
		{name: "wrong_payer_rejected_by_billing", change: "payer", want: ErrRevenueUnavailable},
		{name: "canonical_payment_mutation_rejected", change: "fact", want: ErrRevenueUnavailable},
		{name: "source_from_other_account_rejected", change: "scope", want: ErrRevenueUnavailable},
		{name: "binding_only_cannot_be_refreshable_item", change: "binding", want: ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, fact := statusFixture(t)
			q := LifecycleDiscoveryQuery{Scope: fact.Scope, Kind: LifecycleSubscriptionSources, Limit: 2}
			c := LifecycleDiscoveryCandidate{ID: LifecycleDiscoverySourceID(q.Scope, q.Kind, fact.SubscriptionID), Revision: 1, Scope: fact.Scope, PrincipalID: fact.PrincipalID, CustomerID: fact.ProviderCustomerID, SubscriptionID: fact.SubscriptionID, Fact: fact}
			page := LifecycleDiscoveryPage{Items: []LifecycleDiscoveryCandidate{c}, ReachedEnd: true}
			switch tc.change {
			case "tail":
				page.ReachedEnd = false
				page.NextCursor = q.CursorFor(strings.Repeat("f", 64))
			case "empty-tail":
				page.Items = nil
				page.ReachedEnd = false
				page.NextCursor = q.CursorFor(c.ID)
			case "empty":
				page.Items = nil
			case "full":
				q.Limit = 1
				page.ReachedEnd = false
				page.NextCursor = q.CursorFor(c.ID)
			case "no-cursor":
				page.ReachedEnd = false
			case "terminal-cursor":
				page.NextCursor = q.CursorFor(c.ID)
			case "cursor-scope":
				other := q
				other.Scope.AccountID = "other"
				page.ReachedEnd = false
				page.NextCursor = other.CursorFor(c.ID)
			case "cursor-kind":
				other := q
				other.Kind = LifecycleCheckoutSources
				page.ReachedEnd = false
				page.NextCursor = other.CursorFor(c.ID)
			case "rewind":
				q.Cursor = q.CursorFor(c.ID)
				page.Items = nil
				page.ReachedEnd = false
				page.NextCursor = q.Cursor
			case "before-item":
				page.ReachedEnd = false
				page.NextCursor = q.CursorFor(strings.Repeat("0", 64))
			case "oversized":
				q.Limit = 1
				page.Items = append(page.Items, c)
			case "duplicate":
				page.Items = append(page.Items, c)
			case "payer":
				page.Items[0].PrincipalID = "other-payer"
			case "fact":
				page.Items[0].Fact.PaidMinor++
			case "scope":
				page.Items[0].Scope.AccountID = "other-account"
			case "binding":
				page.Items[0].Fact = RevenueFact{}
			}
			require.ErrorIs(t, page.Validate(q), tc.want)
		})
	}
}
