package billingmanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

type statusResolutionManagerOwner struct {
	*checkoutStatusManagerOwner
	calls   int
	failure error
	bad     bool
	after   func()
}

func (o *statusResolutionManagerOwner) ResolveSubscriptionStatus(ctx context.Context, p billing.SubscriptionStatusPreparation) (billing.SubscriptionStatusResolution, error) {
	o.calls++
	out, err := o.RevenueService.ResolveSubscriptionStatus(ctx, p)
	if o.bad {
		out.State = "unknown"
	}
	if o.after != nil {
		o.after()
	}
	if o.failure != nil {
		return billing.SubscriptionStatusResolution{}, o.failure
	}
	return out, err
}

// Exposing only the existing port deliberately hides the optional resolver.
type legacyStatusResolutionOwner struct {
	SubscriptionStatusService
	RevenueFeedService
}

func TestSubscriptionStatusResolutionManagerAuthority(t *testing.T) {
	denied := errors.New("current refresh denied")
	outage := errors.New("owning resolution outage")
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"pending", "pending", nil}, {"captured", "captured", nil}, {"superseded", "superseded", nil},
		{"deny_global_before_owner", "", denied}, {"deny_selected_before_owner", "", denied},
		{"deny_selected_after_resolution", "", denied}, {"deny_global_after_resolution", "", denied},
		{"outage_preserved", "", outage}, {"uncertainty_preserved", "", billing.ErrRevenueUncertain},
		{"uncertainty_then_denied", "", billing.ErrRevenueUncertain}, {"uncertainty_then_canceled", "", billing.ErrRevenueUncertain},
		{"validation_uncertainty_then_denied", "", billing.ErrRevenueUncertain},
		{"malformed_result", "", billing.ErrRevenueConflict}, {"malformed_result_then_denied", "", denied},
		{"legacy_optional_unavailable", "", billing.ErrRevenueUnavailable}, {"cancel_after_read", "", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, base, provider, authority, records, clock, i := retainedStatusManagerFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p, err := m.PrepareSubscriptionStatusForCheckout(ctx, "original-author", i.Scope, provider.evidence.SubscriptionID)
			require.NoError(t, err)
			if tc.state == "captured" {
				e := billing.VerifiedSubscriptionStatusEvidence{Scope: p.Scope, SubscriptionID: p.SubscriptionID, ProviderCustomerID: p.ProviderCustomerID, Status: "trialing"}
				_, err = base.RevenueService.CaptureVerifiedSubscriptionStatus(ctx, p, e)
				require.NoError(t, err)
			}
			if tc.state == "superseded" {
				clock.at = clock.at.Add(time.Second)
				other, e := base.RevenueService.PrepareSubscriptionStatusForCheckout(ctx, "later-author", p.Scope, p.SubscriptionID)
				require.NoError(t, e)
				_, e = base.RevenueService.CaptureVerifiedSubscriptionStatus(ctx, other, billing.VerifiedSubscriptionStatusEvidence{Scope: p.Scope, SubscriptionID: p.SubscriptionID, ProviderCustomerID: p.ProviderCustomerID, Status: "active"})
				require.NoError(t, e)
			}
			authority.actors, authority.actions, authority.targets = nil, nil, nil
			base.validateCalls = 0
			owner := &statusResolutionManagerOwner{checkoutStatusManagerOwner: base}
			m.revenueFeed = owner
			switch tc.name {
			case "deny_global_before_owner":
				authority.denyAt, authority.err = 1, denied
			case "deny_selected_before_owner":
				authority.denyAt, authority.err = 2, denied
			case "deny_selected_after_resolution", "malformed_result_then_denied", "uncertainty_then_denied":
				owner.after = func() { authority.denyAt, authority.err = len(authority.actors)+1, denied }
			case "deny_global_after_resolution":
				owner.after = func() { authority.denyAt, authority.err = len(authority.actors)+2, denied }
			case "cancel_after_read", "uncertainty_then_canceled":
				owner.after = cancel
			case "validation_uncertainty_then_denied":
				base.failStage, base.failure = "validate", billing.ErrRevenueUncertain
				base.hook = func(string) { authority.denyAt, authority.err = len(authority.actors)+1, denied }
			case "legacy_optional_unavailable":
				m.revenueFeed = &legacyStatusResolutionOwner{SubscriptionStatusService: base, RevenueFeedService: base}
			}
			if tc.name == "outage_preserved" {
				owner.failure = outage
			}
			if tc.name == "uncertainty_preserved" || tc.name == "uncertainty_then_denied" || tc.name == "uncertainty_then_canceled" {
				owner.failure = billing.ErrRevenueUncertain
			}
			if tc.name == "malformed_result" || tc.name == "malformed_result_then_denied" {
				owner.bad = true
			}
			before := len(records.records)
			out, err := m.ResolveSubscriptionStatus(ctx, "current-worker", p)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.state, out.State)
				require.NoError(t, out.Validate())
				require.Equal(t, "original-author", out.Preparation.ActorID)
			}
			require.Zero(t, provider.calls)
			require.Equal(t, before, len(records.records))
			for _, actor := range authority.actors {
				require.Equal(t, "current-worker", actor)
			}
			if tc.name == "deny_global_before_owner" || tc.name == "deny_selected_before_owner" {
				require.Zero(t, base.validateCalls)
				require.Zero(t, owner.calls)
			}
			encoded, e := json.Marshal(out)
			require.NoError(t, e)
			require.Equal(t, "{}", string(encoded))
		})
	}
}
