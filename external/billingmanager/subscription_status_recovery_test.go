package billingmanager

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

type checkoutStatusManagerClock struct{ at time.Time }

func (c *checkoutStatusManagerClock) Now() time.Time { return c.at }

// The wrapper adds boundary failures around actual owning operations. Fresh
// native codecs/services run over isolated atomic records per named case;
// encrypted Mongo and platform admission require their own qualification.
type checkoutStatusManagerOwner struct {
	*billing.RevenueService
	prepareCalls, validateCalls, captureCalls, readCalls int
	failure                                              error
	failStage                                            string
	badPrepare, badRead                                  bool
	hook                                                 func(string)
}

func (o *checkoutStatusManagerOwner) stage(name string, err error) error {
	if o.hook != nil {
		o.hook(name)
	}
	if o.failStage == name {
		return o.failure
	}
	return err
}
func (o *checkoutStatusManagerOwner) PrepareSubscriptionStatusForCheckout(ctx context.Context, actor string, scope billing.RevenueScope, sub string) (billing.SubscriptionStatusPreparation, error) {
	o.prepareCalls++
	p, err := o.RevenueService.PrepareSubscriptionStatusForCheckout(ctx, actor, scope, sub)
	if o.badPrepare {
		p.ActorID = "different-author"
	}
	return p, o.stage("prepare", err)
}
func (o *checkoutStatusManagerOwner) ValidateSubscriptionStatusPreparation(ctx context.Context, p billing.SubscriptionStatusPreparation) error {
	o.validateCalls++
	return o.stage("validate", o.RevenueService.ValidateSubscriptionStatusPreparation(ctx, p))
}
func (o *checkoutStatusManagerOwner) CaptureVerifiedSubscriptionStatus(ctx context.Context, p billing.SubscriptionStatusPreparation, e billing.VerifiedSubscriptionStatusEvidence) (billing.SubscriptionStatus, error) {
	o.captureCalls++
	v, err := o.RevenueService.CaptureVerifiedSubscriptionStatus(ctx, p, e)
	return v, o.stage("capture", err)
}
func (o *checkoutStatusManagerOwner) GetSubscriptionStatusForCheckout(ctx context.Context, scope billing.RevenueScope, sub string, age time.Duration) (billing.SubscriptionStatus, error) {
	o.readCalls++
	v, err := o.RevenueService.GetSubscriptionStatusForCheckout(ctx, scope, sub, age)
	if o.badRead {
		v.Status = "invalid"
	}
	return v, o.stage("read", err)
}

type checkoutStatusManagerRegistry struct {
	p    paymentprovider.RevenueProvider
	err  error
	hook func()
}

func (r *checkoutStatusManagerRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	if r.hook != nil {
		r.hook()
	}
	return r.p, r.err
}

type checkoutStatusManagerProvider struct {
	*statusManagerProvider
	hook func()
}

func (p *checkoutStatusManagerProvider) LookupRevenueSubscription(ctx context.Context, scope paymentprovider.RevenueScope, sub string) (paymentprovider.RevenueSubscriptionEvidence, error) {
	e, err := p.statusManagerProvider.LookupRevenueSubscription(ctx, scope, sub)
	if p.hook != nil {
		p.hook()
	}
	return e, err
}

func retainedStatusManagerFixture(t *testing.T) (*Service, *checkoutStatusManagerOwner, *checkoutStatusManagerProvider, *statusManagerAuthority, *statusManagerStore, *checkoutStatusManagerClock, billing.CheckoutIntent) {
	t.Helper()
	_, checkout, checkoutProvider, _, records, i := completionManagerFixture(t)
	_, err := checkout.CheckoutService.CaptureCheckoutLifecycleEvidence(t.Context(), i, checkoutProvider.e)
	require.NoError(t, err)
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	clock := &checkoutStatusManagerClock{i.CreatedAt.Add(time.Minute)}
	native, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	owner := &checkoutStatusManagerOwner{RevenueService: native}
	provider := &checkoutStatusManagerProvider{statusManagerProvider: &statusManagerProvider{revenueBoundaryProvider: &revenueBoundaryProvider{}, evidence: paymentprovider.RevenueSubscriptionEvidence{Scope: checkoutProvider.e.Scope, SubscriptionID: checkoutProvider.e.SubscriptionID, CustomerID: checkoutProvider.e.CustomerID, Status: "trialing"}}}
	s, err := (&Service{}).WithRevenueServices(&checkoutStatusManagerRegistry{p: provider}, owner, checkout)
	require.NoError(t, err)
	a := &statusManagerAuthority{}
	_, err = s.WithSubscriptionStatusAuthority(a)
	require.NoError(t, err)
	return s, owner, provider, a, records, clock, i
}

func TestRetainedSubscriptionStatusManagerStages(t *testing.T) {
	denied := errors.New("current selected refresh denied")
	outage := errors.New("owning or provider outage")
	for _, tc := range []struct {
		name, stage, state string
		denyAt             int
		want               error
	}{
		{"prepare_trial_before_payment", "prepare", "", 0, nil},
		{"validate_original_with_replacement_actor", "validate", "", 0, nil},
		{"lookup_prepaid_trial", "lookup", "", 0, nil},
		{"capture_trial_without_paid_fact", "capture", "", 0, nil},
		{"read_shared_checkout_head", "read", "seed", 0, nil},
		{"read_later_paid_head_through_checkout", "read", "paid", 0, nil},
		{"prepare_global_permission_before_owner", "prepare", "", 1, denied},
		{"prepare_selected_permission_before_disclosure", "prepare", "", 2, denied},
		{"prepare_owner_outage_preserved", "prepare", "outage", 0, outage},
		{"prepare_error_rechecks_global_authority", "prepare", "outage", 2, denied},
		{"malformed_prepared_result_rechecks_global", "prepare", "bad", 2, denied},
		{"malformed_prepared_result_rejected", "prepare", "bad", 0, billing.ErrRevenueConflict},
		{"validate_selected_permission_before_owner", "validate", "", 2, denied},
		{"validate_error_rechecks_selected_permission", "validate", "outage", 3, denied},
		{"validate_owner_outage_preserved", "validate", "outage", 0, outage},
		{"malformed_original_before_any_owner_io", "validate", "malformed", 0, billing.ErrRevenueConflict},
		{"lookup_selected_permission_before_validation", "lookup", "", 2, denied},
		{"lookup_current_permission_before_provider", "lookup", "", 3, denied},
		{"lookup_revoked_after_provider", "lookup", "", 4, denied},
		{"lookup_registry_error_rechecks_permission", "lookup", "registry-outage", 4, denied},
		{"lookup_missing_provider_rechecks_permission", "lookup", "no-provider", 4, denied},
		{"lookup_provider_error_preserves_cause", "lookup", "provider-outage", 0, outage},
		{"lookup_provider_error_rechecks_permission", "lookup", "provider-outage", 4, denied},
		{"lookup_wrong_customer_rechecks_permission", "lookup", "bad-provider", 4, denied},
		{"lookup_cancel_after_original_validation_no_get", "lookup", "cancel-validate", 0, context.Canceled},
		{"lookup_cancel_after_registry_no_get", "lookup", "cancel-registry", 0, context.Canceled},
		{"lookup_cancel_after_provider_zero_evidence", "lookup", "cancel-provider", 0, context.Canceled},
		{"capture_selected_permission_before_validation", "capture", "", 2, denied},
		{"capture_current_permission_before_commit", "capture", "", 3, denied},
		{"capture_postcommit_revocation_withholds_truth", "capture", "", 4, denied},
		{"capture_unknown_commit_preserves_original", "capture", "uncertain", 0, billing.ErrRevenueUncertain},
		{"capture_unknown_commit_and_revocation_conserve_causes", "capture", "uncertain", 4, denied},
		{"capture_unknown_commit_and_cancel_conserve_causes", "capture", "uncertain-cancel", 0, context.Canceled},
		{"capture_evidence_mismatch_rechecks_permission", "capture", "bad-evidence", 4, denied},
		{"read_global_permission_before_owner", "read", "seed", 1, denied},
		{"read_selected_permission_before_disclosure", "read", "seed", 2, denied},
		{"read_absence_rechecks_global_permission", "read", "", 2, denied},
		{"read_absence_is_unknown", "read", "", 0, billing.ErrRevenueNotFound},
		{"read_stale_rechecks_global_permission", "read", "stale", 2, denied},
		{"read_stale_is_unknown", "read", "stale", 0, billing.ErrSubscriptionStatusStale},
		{"read_malformed_native_output_rechecks_global", "read", "bad", 2, denied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, provider, a, records, clock, i := retainedStatusManagerFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			var p billing.SubscriptionStatusPreparation
			var e billing.VerifiedSubscriptionStatusEvidence
			var v billing.SubscriptionStatus
			var err error
			sub := provider.evidence.SubscriptionID
			if tc.stage != "prepare" {
				p, err = s.PrepareSubscriptionStatusForCheckout(ctx, "original-worker", i.Scope, sub)
				require.NoError(t, err)
				e = billing.VerifiedSubscriptionStatusEvidence{Scope: i.Scope, SubscriptionID: sub, ProviderCustomerID: provider.evidence.CustomerID, Status: "trialing"}
			}
			if tc.stage == "read" && (tc.state == "seed" || tc.state == "stale" || tc.state == "bad") {
				_, err = s.CaptureSubscriptionStatus(ctx, "current-worker", p, e)
				require.NoError(t, err)
			}
			if tc.state == "paid" {
				fact := billing.RevenueFact{Scope: i.Scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: i.Request.UserID, ProviderCustomerID: e.ProviderCustomerID, SubscriptionID: sub, PlanID: i.Request.PlanID, CostID: i.Request.CostID, Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: clock.at}
				observed, paidErr := owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: i.Scope, EnvelopeID: "evt_first", Facts: []billing.RevenueFact{fact}})
				require.NoError(t, paidErr)
				paid, prepErr := s.PrepareSubscriptionStatus(ctx, "paid-author", observed.FactIDs[0])
				require.NoError(t, prepErr)
				e.Status = "active"
				v, err = s.CaptureSubscriptionStatus(ctx, "current-worker", paid, e)
				require.NoError(t, err)
				require.Empty(t, v.Preparation.Source)
			}
			a.actors = nil
			a.actions = nil
			a.targets = nil
			a.denyAt = tc.denyAt
			a.err = denied
			owner.prepareCalls = 0
			owner.validateCalls = 0
			owner.captureCalls = 0
			owner.readCalls = 0
			if tc.state == "outage" {
				owner.failStage = tc.stage
				owner.failure = outage
			}
			if tc.state == "bad" {
				owner.badPrepare = tc.stage == "prepare"
				owner.badRead = tc.stage == "read"
			}
			if tc.state == "malformed" {
				p.CaptureID = "invalid"
			}
			if tc.state == "registry-outage" {
				s.revenueRegistry = &checkoutStatusManagerRegistry{err: outage}
			}
			if tc.state == "no-provider" {
				s.revenueRegistry = &checkoutStatusManagerRegistry{}
			}
			if tc.state == "provider-outage" {
				provider.failure = outage
			}
			if tc.state == "bad-provider" {
				provider.evidence.CustomerID = "cus_wrong"
			}
			if tc.state == "cancel-validate" {
				owner.hook = func(stage string) {
					if stage == "validate" {
						cancel()
					}
				}
			}
			if tc.state == "cancel-registry" {
				s.revenueRegistry = &checkoutStatusManagerRegistry{p: provider, hook: cancel}
			}
			if tc.state == "cancel-provider" {
				provider.hook = cancel
			}
			if tc.state == "uncertain" || tc.state == "uncertain-cancel" {
				records.uncertain = true
			}
			if tc.state == "uncertain-cancel" {
				owner.hook = func(stage string) {
					if stage == "capture" {
						cancel()
					}
				}
			}
			if tc.state == "bad-evidence" {
				e.ProviderCustomerID = "cus_wrong"
			}
			if tc.state == "stale" {
				clock.at = clock.at.Add(2 * time.Minute)
			}
			before := maps.Clone(records.records)
			switch tc.stage {
			case "prepare":
				p, err = s.PrepareSubscriptionStatusForCheckout(ctx, "current-worker", i.Scope, sub)
			case "validate":
				err = s.ValidateSubscriptionStatusPreparation(ctx, "current-worker", p)
			case "lookup":
				e, err = s.LookupSubscriptionStatus(ctx, "current-worker", p)
			case "capture":
				v, err = s.CaptureSubscriptionStatus(ctx, "current-worker", p, e)
			case "read":
				v, err = s.GetSubscriptionStatusForCheckout(ctx, "current-worker", i.Scope, sub, time.Minute)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				if tc.stage == "prepare" {
					require.Zero(t, p)
				}
				if tc.stage == "lookup" {
					require.Zero(t, e)
				}
				if tc.stage == "capture" || tc.stage == "read" {
					require.Zero(t, v)
				}
			} else {
				require.NoError(t, err)
			}
			if tc.state == "uncertain" || tc.state == "uncertain-cancel" {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				records.uncertain = false
				owner.hook = nil
				a.denyAt = 0
				v, err = s.CaptureSubscriptionStatus(context.WithoutCancel(ctx), "replacement-worker", p, e)
				require.NoError(t, err)
				require.Equal(t, "original-worker", v.Preparation.ActorID)
			}
			if tc.stage != "capture" || owner.captureCalls == 0 {
				require.Equal(t, before, records.records)
			}
			if tc.state == "malformed" {
				require.Empty(t, a.targets)
				require.Zero(t, owner.validateCalls)
			}
			if tc.denyAt == 1 {
				require.Zero(t, owner.prepareCalls+owner.validateCalls+owner.captureCalls+owner.readCalls)
			}
			if tc.denyAt == 2 && (tc.stage == "validate" || tc.stage == "lookup" || tc.stage == "capture") {
				require.Zero(t, owner.validateCalls)
				require.Zero(t, owner.captureCalls)
			}
			if tc.stage != "lookup" || tc.denyAt == 2 || tc.denyAt == 3 || tc.state == "cancel-validate" || tc.state == "cancel-registry" {
				require.Zero(t, provider.calls)
			}
			for n, target := range a.targets {
				require.Equal(t, "current-worker", a.actors[n])
				if tc.state == "uncertain" || tc.state == "uncertain-cancel" {
					break
				} // replay actor asserted independently above
				if target != (SubscriptionStatusTarget{}) {
					require.Equal(t, i.Scope, target.Scope)
					require.Equal(t, i.Request.UserID, target.PrincipalID)
					require.Equal(t, sub, target.SubscriptionID)
				}
				wantAction := SubscriptionStatusRefresh
				if tc.stage == "read" {
					wantAction = SubscriptionStatusRead
				}
				require.Equal(t, wantAction, a.actions[n])
			}
			if tc.state != "paid" {
				for _, r := range records.records {
					require.NotEqual(t, "billing_revenue_fact", r.Kind)
				}
			}
		})
	}
}

// Payment-only custom adapters keep the original required capability. The
// optional checkout interface must be obtained from the current feed each time.
type legacyStatusManagerFeed struct {
	RevenueFeedService
	SubscriptionStatusService
}

func TestRetainedSubscriptionStatusManagerCapabilities(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"legacy_paid_adapter_remains_usable", "legacy"},
		{"changed_feed_does_not_reuse_old_optional_owner", "changed"},
		{"typed_nil_feed_is_unavailable", "nil-feed"},
		{"missing_authority_is_unavailable", "authority"},
		{"typed_nil_authority_is_unavailable", "nil-authority"},
		{"missing_provider_registry_is_unavailable", "registry"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, _, _, provider, fact := statusManagerFixture(t)
			if tc.state == "legacy" || tc.state == "changed" {
				s.revenueFeed = &legacyStatusManagerFeed{owner, owner}
				_, err := s.WithSubscriptionStatusAuthority(&statusManagerAuthority{})
				require.NoError(t, err)
				p, err := s.PrepareSubscriptionStatus(t.Context(), "paid-worker", fact)
				require.NoError(t, err)
				require.Empty(t, p.Source)
			}
			if tc.state == "nil-feed" {
				s.revenueFeed = (*legacyStatusManagerFeed)(nil)
			}
			if tc.state == "authority" {
				s.subscriptionStatusAuthority = nil
			}
			if tc.state == "nil-authority" {
				s.subscriptionStatusAuthority = (*statusManagerAuthority)(nil)
			}
			if tc.state == "registry" {
				s.revenueRegistry = nil
			}
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_primary"}
			p, err := s.PrepareSubscriptionStatusForCheckout(t.Context(), "current-worker", scope, "sub_original")
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, p)
			v, err := s.GetSubscriptionStatusForCheckout(t.Context(), "current-worker", scope, "sub_original", time.Minute)
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, v)
			require.Zero(t, provider.calls)
		})
	}
}

func TestRetainedSubscriptionStatusManagerInputs(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"nil_context_before_owner", "nil"}, {"canceled_context_before_owner", "cancel"},
		{"blank_current_actor_before_owner", "actor"}, {"invalid_scope_before_owner", "scope"},
		{"blank_subscription_before_owner", "empty"}, {"padded_subscription_before_owner", "padded"},
		{"control_subscription_before_owner", "control"}, {"oversized_subscription_before_owner", "oversized"},
		{"invalid_read_age_before_owner", "age"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, provider, a, _, _, i := retainedStatusManagerFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			actor, sub, age := "current-worker", provider.evidence.SubscriptionID, time.Minute
			scope := i.Scope
			want := billing.ErrRevenueInvalid
			switch tc.state {
			case "nil":
				ctx = nil
			case "cancel":
				cancel()
				want = context.Canceled
			case "actor":
				actor = ""
			case "scope":
				scope.Provider = ""
			case "empty":
				sub = ""
			case "padded":
				sub = " sub"
			case "control":
				sub = "sub\x00"
			case "oversized":
				sub = string(make([]byte, 257))
			case "age":
				age = 0
			}
			if tc.state != "age" {
				p, err := s.PrepareSubscriptionStatusForCheckout(ctx, actor, scope, sub)
				require.ErrorIs(t, err, want)
				require.Zero(t, p)
			}
			v, err := s.GetSubscriptionStatusForCheckout(ctx, actor, scope, sub, age)
			require.ErrorIs(t, err, want)
			require.Zero(t, v)
			require.Zero(t, owner.prepareCalls+owner.validateCalls+owner.captureCalls+owner.readCalls)
			require.Zero(t, provider.calls)
			require.Empty(t, a.targets)
		})
	}
}

func TestRetainedSubscriptionStatusManagerOriginalReplay(t *testing.T) {
	for _, tc := range []struct {
		name      string
		laterHead bool
	}{
		{"replacement_actor_preserves_original_capture_and_clock", false},
		{"original_receipt_recovers_after_later_current_head", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, provider, _, _, clock, i := retainedStatusManagerFixture(t)
			p, err := s.PrepareSubscriptionStatusForCheckout(t.Context(), "original-worker", i.Scope, provider.evidence.SubscriptionID)
			require.NoError(t, err)
			e, err := s.LookupSubscriptionStatus(t.Context(), "replacement-worker", p)
			require.NoError(t, err)
			first, err := s.CaptureSubscriptionStatus(t.Context(), "replacement-worker", p, e)
			require.NoError(t, err)
			clock.at = clock.at.Add(time.Minute)
			if tc.laterHead {
				next, err := s.PrepareSubscriptionStatusForCheckout(t.Context(), "later-worker", i.Scope, p.SubscriptionID)
				require.NoError(t, err)
				later := e
				later.Status = "active"
				updated, err := s.CaptureSubscriptionStatus(t.Context(), "later-worker", next, later)
				require.NoError(t, err)
				require.EqualValues(t, 2, updated.Revision)
			}
			require.NoError(t, s.ValidateSubscriptionStatusPreparation(t.Context(), "current-worker", p))
			replayed, err := s.CaptureSubscriptionStatus(t.Context(), "current-worker", p, e)
			require.NoError(t, err)
			require.Equal(t, first, replayed)
			require.Equal(t, "original-worker", replayed.Preparation.ActorID)
			require.Equal(t, p.RequestedAt, replayed.Preparation.RequestedAt)
			require.Equal(t, 1, provider.calls)
			current, err := s.GetSubscriptionStatusForCheckout(t.Context(), "current-worker", i.Scope, p.SubscriptionID, 2*time.Minute)
			require.NoError(t, err)
			if tc.laterHead {
				require.Equal(t, "active", current.Status)
				require.EqualValues(t, 2, current.Revision)
			} else {
				require.Equal(t, first, current)
			}
		})
	}
}
