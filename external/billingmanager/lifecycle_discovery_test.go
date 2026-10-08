package billingmanager

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

type discoveryManagerAuthority struct {
	actors, actions []string
	targets         []LifecycleDiscoveryTarget
	denyAt          int
	err             error
	cancelAt        int
	cancel          context.CancelFunc
}

func (a *discoveryManagerAuthority) AuthorizeLifecycleDiscovery(_ context.Context, actor, action string, target LifecycleDiscoveryTarget) error {
	a.actors = append(a.actors, actor)
	a.actions = append(a.actions, action)
	a.targets = append(a.targets, target)
	if len(a.actors) == a.cancelAt && a.cancel != nil {
		a.cancel()
	}
	if len(a.actors) == a.denyAt {
		return a.err
	}
	return nil
}

type discoveryManagerOwner struct {
	RevenueFeedService
	page   billing.LifecycleDiscoveryPage
	err    error
	calls  int
	onRead func()
	query  billing.LifecycleDiscoveryQuery
}

func (o *discoveryManagerOwner) DiscoverLifecycleSources(_ context.Context, q billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoveryPage, error) {
	o.calls++
	o.query = q
	if o.onRead != nil {
		o.onRead()
	}
	return o.page, o.err
}

// The fixture obtains canonical evidence from the actual configured billing
// owner; the narrow proxy below changes only deliberate boundary-test results.
func discoveryManagerPaidFixture(t *testing.T) (billing.LifecycleDiscoveryQuery, billing.LifecycleDiscoveryCandidate) {
	t.Helper()
	_, owner, _, _, _, id := statusManagerFixture(t)
	f, err := owner.GetRevenueFact(context.Background(), id)
	require.NoError(t, err)
	q := billing.LifecycleDiscoveryQuery{Scope: f.Scope, Kind: billing.LifecycleSubscriptionSources, Limit: 2}
	return q, billing.LifecycleDiscoveryCandidate{ID: billing.LifecycleDiscoverySourceID(q.Scope, q.Kind, f.SubscriptionID), Revision: 1, Scope: f.Scope, PrincipalID: f.PrincipalID, CustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID, Fact: f}
}
func TestLifecycleDiscoveryManagerAuthority(t *testing.T) {
	denied := errors.New("current discovery authority denied")
	outage := errors.New("owning discovery snapshot unavailable")
	for _, tc := range []struct {
		name, change string
		denyAt       int
		want         error
	}{
		{name: "current_scope_payer_and_scope_disclosure"},
		{name: "scope_denied_before_owner_read", denyAt: 1, want: denied},
		{name: "payer_denied_withholds_entire_page", denyAt: 2, want: denied},
		{name: "post_read_scope_revocation_withholds_cursor", denyAt: 3, want: denied},
		{name: "empty_page_still_checks_scope_twice", change: "empty"},
		{name: "empty_page_post_read_revocation", change: "empty", denyAt: 2, want: denied},
		{name: "invisible_binding_tail_preserves_cursor", change: "tail"},
		{name: "zero_items_continuation_preserves_progress", change: "empty-tail"},
		{name: "owner_failure_withholds_partial_page_and_cursor", change: "outage", want: outage},
		{name: "unprepared_is_distinct_with_current_scope", change: "unprepared", want: billing.ErrLifecycleDiscoveryUnprepared},
		{name: "unprepared_post_read_revocation_is_denied", change: "unprepared", denyAt: 2, want: denied},
		{name: "joined_absence_and_outage_is_preserved", change: "joined", want: outage},
		{name: "nil_context_before_io", change: "nil-context", want: billing.ErrRevenueInvalid},
		{name: "empty_actor_before_io", change: "actor-empty", want: billing.ErrRevenueInvalid},
		{name: "trimmed_actor_not_accepted", change: "actor-space", want: billing.ErrRevenueInvalid},
		{name: "control_character_actor_rejected", change: "actor-control", want: billing.ErrRevenueInvalid},
		{name: "oversized_actor_rejected", change: "actor-long", want: billing.ErrRevenueInvalid},
		{name: "canceled_before_read", change: "cancel-before", want: context.Canceled},
		{name: "canceled_by_initial_authority_before_read", change: "cancel-authority", want: context.Canceled},
		{name: "canceled_after_owner_read", change: "cancel-read", want: context.Canceled},
		{name: "canceled_before_disclosure", change: "cancel-final", want: context.Canceled},
		{name: "wrong_scope_source_withholds", change: "scope", want: billing.ErrRevenueUnavailable},
		{name: "wrong_payer_source_withholds", change: "payer", want: billing.ErrRevenueUnavailable},
		{name: "malformed_cursor_withholds", change: "cursor", want: billing.ErrRevenueUnavailable},
		{name: "terminal_flag_contradicts_cursor", change: "terminal", want: billing.ErrRevenueUnavailable},
		{name: "invalid_query_scope_before_io", change: "query-scope", want: billing.ErrRevenueInvalid},
		{name: "invalid_query_kind_before_io", change: "query-kind", want: billing.ErrRevenueInvalid},
		{name: "invalid_query_limit_before_io", change: "query-limit", want: billing.ErrRevenueInvalid},
		{name: "query_cursor_other_mode_before_io", change: "query-cursor", want: billing.ErrRevenueInvalid},
		{name: "missing_authority_is_unavailable", change: "nil-authority", want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_authority_is_unavailable", change: "typed-authority", want: billing.ErrRevenueUnavailable},
		{name: "missing_owner_is_unavailable", change: "nil-owner", want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_owner_is_unavailable", change: "typed-owner", want: billing.ErrRevenueUnavailable},
		{name: "reconfigured_legacy_feed_cannot_fall_back", change: "legacy", want: billing.ErrRevenueUnavailable},
		{name: "nil_manager_is_unavailable", change: "nil-manager", want: billing.ErrRevenueUnavailable},
		{name: "read_only_discovery_requires_no_provider_registry", change: "no-registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, c := discoveryManagerPaidFixture(t)
			owner := &discoveryManagerOwner{page: billing.LifecycleDiscoveryPage{Items: []billing.LifecycleDiscoveryCandidate{c}, ReachedEnd: true}}
			auth := &discoveryManagerAuthority{denyAt: tc.denyAt, err: denied}
			s, err := (&Service{revenueFeed: owner}).WithLifecycleDiscoveryAuthority(auth)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			auth.cancel = cancel
			actor := "current-worker"
			switch tc.change {
			case "empty":
				owner.page.Items = nil
			case "tail", "empty-tail":
				owner.page.ReachedEnd = false
				owner.page.NextCursor = q.CursorFor(strings.Repeat("f", 64))
				if tc.change == "empty-tail" {
					owner.page.Items = nil
				}
			case "outage":
				owner.err = outage
				owner.page.NextCursor = q.CursorFor(c.ID)
			case "unprepared":
				owner.err = billing.ErrLifecycleDiscoveryUnprepared
			case "joined":
				owner.err = errors.Join(billing.ErrRevenueNotFound, outage)
			case "nil-context":
				ctx = nil
			case "actor-empty":
				actor = ""
			case "actor-space":
				actor = " current-worker"
			case "actor-control":
				actor = "worker\n"
			case "actor-long":
				actor = strings.Repeat("a", 257)
			case "cancel-before":
				cancel()
			case "cancel-authority":
				auth.cancelAt = 1
			case "cancel-read":
				owner.onRead = cancel
			case "cancel-final":
				auth.cancelAt = 3
			case "scope":
				owner.page.Items[0].Scope.AccountID = "other-account"
			case "payer":
				owner.page.Items[0].PrincipalID = "other-payer"
			case "cursor":
				owner.page.ReachedEnd = false
				owner.page.NextCursor = "wrong-cursor"
			case "terminal":
				owner.page.NextCursor = q.CursorFor(c.ID)
			case "query-scope":
				q.Scope.AccountID = ""
			case "query-kind":
				q.Kind = "other-kind"
			case "query-limit":
				q.Limit = 201
			case "query-cursor":
				other := q
				other.Scope.LiveMode = !q.Scope.LiveMode
				q.Cursor = other.CursorFor(c.ID)
			case "nil-authority":
				s.lifecycleDiscoveryAuthority = nil
			case "typed-authority":
				var a *discoveryManagerAuthority
				s.lifecycleDiscoveryAuthority = a
			case "nil-owner":
				s.revenueFeed = nil
			case "typed-owner":
				var o *discoveryManagerOwner
				s.revenueFeed = o
			case "legacy":
				s.revenueFeed = &revenueBoundaryFeed{}
			case "nil-manager":
				s = nil
			case "no-registry":
				s.revenueRegistry = nil
			}
			page, err := s.DiscoverLifecycleSources(ctx, actor, q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, page)
			} else {
				require.Equal(t, owner.page, page)
			}
			if len(auth.targets) > 0 {
				require.Equal(t, LifecycleDiscoveryTarget{Scope: q.Scope, Kind: q.Kind}, auth.targets[0])
				for n, action := range auth.actions {
					require.Equal(t, LifecycleDiscovery, action)
					require.Equal(t, actor, auth.actors[n])
				}
			}
			if tc.denyAt == 1 || strings.HasPrefix(tc.change, "actor-") || strings.HasPrefix(tc.change, "query-") || tc.change == "nil-context" || tc.change == "cancel-before" || tc.change == "cancel-authority" {
				require.Zero(t, owner.calls)
			}
			if tc.change == "unprepared" || tc.change == "empty" {
				require.Len(t, auth.targets, 2)
			}
			if tc.change == "" && tc.denyAt == 0 {
				require.Len(t, auth.targets, 3)
				require.Equal(t, LifecycleDiscoveryTarget{Scope: q.Scope, Kind: q.Kind, PrincipalID: c.PrincipalID, SubscriptionID: c.SubscriptionID}, auth.targets[1])
				require.Equal(t, auth.targets[0], auth.targets[2])
				require.Equal(t, q, owner.query)
			}
		})
	}
}
func TestLifecycleDiscoveryManagerCapability(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		want         error
	}{
		{name: "same_feed_optional_capability_installs"},
		{name: "nil_authority_rejected", change: "authority", want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_authority_rejected", change: "typed-authority", want: billing.ErrRevenueUnavailable},
		{name: "nil_owner_rejected", change: "owner", want: billing.ErrRevenueUnavailable},
		{name: "typed_nil_owner_rejected", change: "typed-owner", want: billing.ErrRevenueUnavailable},
		{name: "legacy_owner_rejected", change: "legacy", want: billing.ErrRevenueUnavailable},
		{name: "nil_service_rejected", change: "service", want: billing.ErrRevenueUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var auth LifecycleDiscoveryAuthority = &discoveryManagerAuthority{}
			owner := &discoveryManagerOwner{}
			s := &Service{revenueFeed: owner}
			switch tc.change {
			case "authority":
				auth = nil
			case "typed-authority":
				auth = (*discoveryManagerAuthority)(nil)
			case "owner":
				s.revenueFeed = nil
			case "typed-owner":
				s.revenueFeed = (*discoveryManagerOwner)(nil)
			case "legacy":
				s.revenueFeed = &revenueBoundaryFeed{}
			case "service":
				s = nil
			}
			result, err := s.WithLifecycleDiscoveryAuthority(auth)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, result)
			} else {
				require.Same(t, s, result)
				require.Same(t, owner, s.revenueFeed)
			}
		})
	}
}

// Compile-time assertion verifies the actual owning capability stays optional
// on the existing revenue owner rather than requiring another injected ledger.
var _ LifecycleDiscoveryService = (*billing.RevenueService)(nil)
