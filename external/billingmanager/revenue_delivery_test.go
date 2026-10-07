package billingmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new named replay cases prove that durable reception does
// not bypass fresh authentication or re-resolve economics during an outage.
type revenueVerifiedProvider struct {
	revenueBoundaryProvider
	identity    paymentprovider.RevenueDeliveryIdentity
	verifyErr   error
	resolutions int
}

func (p *revenueVerifiedProvider) VerifyRevenueDelivery(context.Context, *http.Request) (paymentprovider.RevenueDeliveryIdentity, error) {
	return p.identity, p.verifyErr
}
func (p *revenueVerifiedProvider) ResolveRevenueWebhook(ctx context.Context, r *http.Request) (*paymentprovider.RevenueEvidence, error) {
	p.resolutions++
	return p.revenueBoundaryProvider.ResolveRevenueWebhook(ctx, r)
}

type revenueDeliveryFeed struct {
	revenueBoundaryFeed
	delivery    billing.RevenueObservation
	deliveryErr error
}

func (f *revenueDeliveryFeed) GetRevenueDelivery(context.Context, billing.RevenueScope, string) (billing.RevenueObservation, error) {
	return f.delivery, f.deliveryErr
}

func TestRevenueDeliveryReplay(t *testing.T) {
	type testCase struct {
		name        string
		mutate      func(*revenueVerifiedProvider, *revenueDeliveryFeed)
		want        error
		resolutions int
		writes      int
	}
	cases := []testCase{
		{name: "durable_fact_replay_does_not_depend_on_provider_api"},
		{name: "durable_quarantine_replay_preserves_recovery_obligation", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.delivery.QuarantineReason = "historical_payer_plan_pending"
		}},
		{name: "fresh_signature_required_even_after_acceptance", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			p.verifyErr = paymentprovider.ErrPaymentProviderInvalidWebhookSignature
		}, want: paymentprovider.ErrPaymentProviderInvalidWebhookSignature},
		{name: "same_envelope_with_changed_signed_snapshot_conflicts", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			p.identity.SourceFingerprint = "changed-source"
		}, want: billing.ErrRevenueConflict},
		{name: "missing_digest_is_not_immutable_evidence", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) { p.identity.SourceFingerprint = "" }, want: billing.ErrRevenueInvalid},
		{name: "verifier_cannot_substitute_provider", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) { p.identity.Scope.Provider = "other" }, want: billing.ErrRevenueInvalid},
		{name: "owning_receipt_must_match_scope", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.delivery.Scope.AccountID = "different-account"
		}, want: billing.ErrRevenueConflict},
		{name: "owning_receipt_must_match_envelope", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) { f.delivery.EnvelopeID = "evt_other" }, want: billing.ErrRevenueConflict},
		{name: "owning_read_outage_is_not_absence", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.deliveryErr = billing.ErrRevenueUnavailable
		}, want: billing.ErrRevenueUnavailable},
		{name: "joined_absence_and_outage_is_not_absence", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.deliveryErr = errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUnavailable)
		}, want: billing.ErrRevenueUnavailable},
		{name: "new_delivery_retries_provider_outage", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) { f.deliveryErr = billing.ErrRevenueNotFound }, want: paymentprovider.ErrPaymentProviderAPIRequestFailed, resolutions: 1},
		{name: "new_quarantine_is_durably_accepted", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.deliveryErr = billing.ErrRevenueNotFound
			p.err = nil
		}, resolutions: 1, writes: 1},
		{name: "financial_resolver_cannot_change_verified_snapshot", mutate: func(p *revenueVerifiedProvider, f *revenueDeliveryFeed) {
			f.deliveryErr = billing.ErrRevenueNotFound
			p.err = nil
			p.e.SourceFingerprint = "different"
		}, want: billing.ErrRevenueConflict, resolutions: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope := paymentprovider.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
			identity := paymentprovider.RevenueDeliveryIdentity{Scope: scope, EnvelopeID: "evt_fixture", SourceFingerprint: "immutable-source"}
			p := &revenueVerifiedProvider{identity: identity, revenueBoundaryProvider: revenueBoundaryProvider{err: paymentprovider.ErrPaymentProviderAPIRequestFailed, e: &paymentprovider.RevenueEvidence{Scope: scope, EnvelopeID: identity.EnvelopeID, SourceFingerprint: identity.SourceFingerprint, QuarantineReason: "historical_payer_plan_pending"}}}
			f := &revenueDeliveryFeed{delivery: billing.RevenueObservation{Scope: billingRevenueScope(scope), EnvelopeID: identity.EnvelopeID, SourceFingerprint: identity.SourceFingerprint}}
			if tc.mutate != nil {
				tc.mutate(p, f)
			}
			s, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: p}, f, &revenueBoundaryAssociation{})
			require.NoError(t, err)
			accepted, err := s.acceptRevenueWebhook(context.Background(), "stripe", httptest.NewRequest(http.MethodPost, "/webhook", nil))
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, accepted)
			} else {
				require.NoError(t, err)
				require.True(t, accepted)
			}
			require.Equal(t, tc.resolutions, p.resolutions)
			require.Len(t, f.requests, tc.writes)
			require.Empty(t, p.lookups)
		})
	}
}
