package partnermanager

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// These narrow ports observe orchestration only. They implement no receipt,
// reservation or payout algorithm; native owning tests prove durable recovery.
type admissionClaimEarnings struct {
	*claimEarningsStub
	reads int
}

func (e *admissionClaimEarnings) FindClaimRequest(ctx context.Context, actor, partner, key string) (partnerearnings.Claim, error) {
	e.reads++
	return e.claimEarningsStub.FindClaimRequest(ctx, actor, partner, key)
}

func TestCustomerClaimAdmissionAndFrozenRecovery(t *testing.T) {
	type testCase struct {
		name, state string
		want        error
	}
	for _, tc := range []testCase{
		{name: "minimum_boundary_admits_owning_destination", state: "new"},
		{name: "zero_minimum_preserves_positive_amount", state: "zero-minimum"},
		{name: "below_minimum_has_no_financial_request", state: "below-minimum", want: ErrInvalid},
		{name: "zero_amount_invalid", state: "zero-amount", want: ErrInvalid},
		{name: "empty_key_invalid", state: "empty-key", want: ErrInvalid},
		{name: "missing_destination_revision_invalid", state: "zero-version", want: ErrInvalid},
		{name: "negative_destination_revision_invalid", state: "negative-version", want: ErrInvalid},
		{name: "stale_owning_destination", state: "stale-version", want: partnerearnings.ErrStaleWrite},
		{name: "wrong_destination_owner_unavailable", state: "wrong-destination", want: ErrUnavailable},
		{name: "destination_outage_retained", state: "destination-outage", want: partnerprogram.ErrUnavailable},
		{name: "new_claim_paused", state: "paused", want: ErrDenied},
		{name: "new_claim_partner_suspended", state: "suspended", want: ErrDenied},
		{name: "receipt_recovers_before_new_minimum_pause_and_destination", state: "recover"},
		{name: "same_key_changed_amount_conflicts", state: "replay-amount", want: partnerearnings.ErrConflict},
		{name: "same_key_changed_destination_conflicts", state: "replay-version", want: partnerearnings.ErrConflict},
		{name: "wrong_receipt_actor_conflicts", state: "replay-actor", want: partnerearnings.ErrConflict},
		{name: "wrong_receipt_partner_conflicts", state: "replay-partner", want: partnerearnings.ErrConflict},
		{name: "wrong_receipt_currency_conflicts", state: "replay-currency", want: partnerearnings.ErrConflict},
		{name: "missing_frozen_revision_conflicts", state: "replay-no-version", want: partnerearnings.ErrConflict},
		{name: "revoked_after_receipt_read_denies_recovery", state: "replay-revoked", want: ErrDenied},
		{name: "receipt_joined_absence_outage_never_admits", state: "joined-absence", want: ErrUnavailable},
		{name: "receipt_outage_never_admits", state: "receipt-outage", want: partnerearnings.ErrUnavailable},
		{name: "legacy_api_enforces_minimum", state: "legacy-minimum", want: ErrInvalid},
		{name: "legacy_receipt_recovers_after_minimum_raise", state: "legacy-recover"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, base, _, _, a, _, _ := managerFixture(t)
			m.deps.Claims = ClaimsConfig{MinimumMinor: 100}
			p := &claimProgramStub{programStub: base, destination: partnerprogram.Destination{ID: "destination", CustomerID: "owner", Method: "paypal", Email: "owner@example.test", Version: 2}}
			p.destinationErr = nil
			m.deps.Program = p
			e := &admissionClaimEarnings{claimEarningsStub: &claimEarningsStub{lookupErr: partnerearnings.ErrNotFound}}
			m.deps.Earnings = e
			req := SelfClaimRequest{AmountMinor: 100, ExpectedDestinationVersion: 2, IdempotencyKey: "intent"}
			replay := tc.state == "recover" || tc.state == "legacy-recover" || len(tc.state) >= 7 && tc.state[:7] == "replay-"
			if replay {
				e.lookupErr = nil
				e.claim = partnerearnings.Claim{ID: "original", PartnerID: "partner", RequestedBy: "owner", AmountMinor: 100, Currency: "EUR", DestinationID: "destination", DestinationSnapshot: map[string]string{"version": "2", "email": "original@example.test"}}
				m.deps.Claims.MinimumMinor = 1000
				m.deps.Controls.Claims = false
				p.partner.CanRequestPayouts = false
				p.destinationErr = partnerprogram.ErrUnavailable
			}
			switch tc.state {
			case "zero-minimum":
				m.deps.Claims.MinimumMinor = 0
				req.AmountMinor = 1
			case "below-minimum", "legacy-minimum":
				req.AmountMinor = 99
			case "zero-amount":
				req.AmountMinor = 0
			case "empty-key":
				req.IdempotencyKey = ""
			case "zero-version":
				req.ExpectedDestinationVersion = 0
			case "negative-version":
				req.ExpectedDestinationVersion = -1
			case "stale-version":
				req.ExpectedDestinationVersion = 1
			case "wrong-destination":
				p.destination.CustomerID = "other"
			case "destination-outage":
				p.destinationErr = partnerprogram.ErrUnavailable
			case "paused":
				m.deps.Controls.Claims = false
			case "suspended":
				p.partner.CanRequestPayouts = false
			case "replay-amount":
				req.AmountMinor++
			case "replay-version":
				req.ExpectedDestinationVersion++
			case "replay-actor":
				e.claim.RequestedBy = "other"
			case "replay-partner":
				e.claim.PartnerID = "other"
			case "replay-currency":
				e.claim.Currency = "USD"
			case "replay-no-version":
				delete(e.claim.DestinationSnapshot, "version")
			case "replay-revoked":
				e.afterLookup = func() { a.deny = true }
			case "joined-absence":
				e.lookupErr = errors.Join(partnerearnings.ErrNotFound, ErrUnavailable)
			case "receipt-outage":
				e.lookupErr = partnerearnings.ErrUnavailable
			}
			var out partnerearnings.Claim
			var err error
			if tc.state == "legacy-minimum" || tc.state == "legacy-recover" {
				out, err = m.RequestClaim(t.Context(), "owner", req.AmountMinor, req.IdempotencyKey)
			} else {
				out, err = m.RequestClaimWithDestination(t.Context(), "owner", req)
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil || replay {
				require.Nil(t, e.request)
				if tc.want == nil {
					require.Equal(t, e.claim, out)
				}
				return
			}
			require.Equal(t, 1, e.reads)
			require.Equal(t, "new-claim", out.ID)
			require.Equal(t, "owner", e.request.ActorID)
			require.Equal(t, "partner", e.request.PartnerID)
			require.Equal(t, req.AmountMinor, e.request.AmountMinor)
			require.Equal(t, "intent", e.request.IdempotencyKey)
			require.Equal(t, "owner@example.test", e.request.DestinationSnapshot["email"])
			require.Equal(t, "2", e.request.DestinationSnapshot["version"])
		})
	}
}

type paymentClaimReadPort struct {
	EarningsService
	claim partnerearnings.Claim
	err   error
	reads int
	after func()
}

func (p *paymentClaimReadPort) GetClaim(context.Context, string) (partnerearnings.Claim, error) {
	p.reads++
	if p.after != nil {
		p.after()
	}
	return p.claim, p.err
}

type paymentClaimAuthority struct {
	permission, target string
	revoked            bool
	checks             int
}

func (a *paymentClaimAuthority) CheckPartners(_ context.Context, actor, capability, target string) error {
	a.checks++
	if actor != "operator" || a.revoked || capability != a.permission || target != a.target {
		return ErrDenied
	}
	return nil
}

func TestAdminPaymentPreparationUsesExactCurrentClaimAuthority(t *testing.T) {
	type testCase struct {
		name, state string
		want        error
		reads       int
	}
	for _, tc := range []testCase{
		{name: "recording_capability_resolves_fixed_obligation", reads: 1},
		{name: "manual_recording_pause_preserves_authorized_read", state: "paused", reads: 1},
		{name: "reporting_does_not_prepare_payment", state: "reporting", want: ErrDenied},
		{name: "processing_does_not_prepare_payment", state: "processing", want: ErrDenied},
		{name: "other_claim_grant_does_not_prepare_payment", state: "wrong-target", want: ErrDenied},
		{name: "other_actor_denied", state: "wrong-actor", want: ErrDenied},
		{name: "read_revocation_denies_return", state: "revoked", want: ErrDenied, reads: 1},
		{name: "owner_absence_retained", state: "absent", want: partnerearnings.ErrNotFound, reads: 1},
		{name: "owner_outage_retained", state: "outage", want: partnerearnings.ErrUnavailable, reads: 1},
		{name: "wrong_claim_unavailable", state: "wrong-id", want: ErrUnavailable, reads: 1},
		{name: "wrong_program_unavailable", state: "wrong-program", want: ErrUnavailable, reads: 1},
		{name: "wrong_currency_unavailable", state: "wrong-currency", want: ErrUnavailable, reads: 1},
		{name: "missing_partner_unavailable", state: "no-partner", want: ErrUnavailable, reads: 1},
		{name: "zero_amount_unavailable", state: "no-amount", want: ErrUnavailable, reads: 1},
		{name: "zero_revision_unavailable", state: "no-revision", want: ErrUnavailable, reads: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			a := &paymentClaimAuthority{permission: CapabilityRecordPayment, target: "claim"}
			e := &paymentClaimReadPort{claim: partnerearnings.Claim{ID: "claim", ProgramID: partnerprogram.ProgramID, PartnerID: "partner", AmountMinor: 100, Currency: "EUR", Revision: 1}}
			m.deps.Authority = a
			m.deps.Earnings = e
			actor := "operator"
			switch tc.state {
			case "paused":
				m.deps.Controls.ManualRecording = false
			case "reporting":
				a.permission = CapabilityReporting
			case "processing":
				a.permission = CapabilityProcessing
			case "wrong-target":
				a.target = "other"
			case "wrong-actor":
				actor = "other"
			case "revoked":
				e.after = func() { a.revoked = true }
			case "absent":
				e.err = partnerearnings.ErrNotFound
			case "outage":
				e.err = partnerearnings.ErrUnavailable
			case "wrong-id":
				e.claim.ID = "other"
			case "wrong-program":
				e.claim.ProgramID = "other"
			case "wrong-currency":
				e.claim.Currency = "USD"
			case "no-partner":
				e.claim.PartnerID = ""
			case "no-amount":
				e.claim.AmountMinor = 0
			case "no-revision":
				e.claim.Revision = 0
			}
			out, err := m.AdminPaymentClaim(t.Context(), actor, "claim")
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.reads, e.reads)
			if tc.want == nil {
				require.Equal(t, e.claim, out)
				require.Equal(t, 2, a.checks)
			} else {
				require.Empty(t, out.ID)
			}
		})
	}
}

func TestClaimMinimumConfiguration(t *testing.T) {
	type testCase struct {
		name    string
		minimum int64
		want    error
	}
	for _, tc := range []testCase{{name: "compatible_zero"}, {name: "explicit_positive", minimum: 100}, {name: "negative_invalid", minimum: -1, want: ErrInvalid}} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			deps := m.deps
			deps.Claims = ClaimsConfig{MinimumMinor: tc.minimum}
			_, err := NewManager(deps)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestAdminClaimMinimumAppliesOnlyToNewAdmission(t *testing.T) {
	type testCase struct {
		name     string
		minimum  int64
		accepted bool
		want     error
	}
	for _, tc := range []testCase{
		{name: "new_boundary_admits", minimum: 100},
		{name: "new_below_minimum_refuses", minimum: 101, want: ErrInvalid},
		{name: "original_operator_receipt_recovers_below_raised_minimum", minimum: 1000, accepted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, base, _, _, _, _, _ := managerFixture(t)
			m.deps.Claims = ClaimsConfig{MinimumMinor: tc.minimum}
			p := &claimProgramStub{programStub: base, destination: partnerprogram.Destination{ID: "destination", CustomerID: "owner", Method: "paypal", Email: "owner@example.test", Version: 1}}
			p.destinationErr = nil
			m.deps.Program = p
			m.deps.Authority = &claimAuthorityStub{permitted: CapabilityCreateClaimOnBehalf}
			e := &claimEarningsStub{lookupErr: partnerearnings.ErrNotFound}
			m.deps.Earnings = e
			if tc.accepted {
				e.lookupErr = nil
				e.claim = partnerearnings.Claim{ID: "original", PartnerID: "partner", RequestedBy: "operator", RequestedReason: "owner requested assistance", AmountMinor: 100, Currency: "EUR", DestinationSnapshot: map[string]string{"version": "1"}}
				m.deps.Controls.Claims = false
				p.destinationErr = partnerprogram.ErrUnavailable
			}
			out, err := m.AdminRequestClaim(t.Context(), ClaimOnBehalfRequest{ActorID: "operator", PartnerID: "partner", AmountMinor: 100, ExpectedDestinationVersion: 1, IdempotencyKey: "intent", Reason: "owner requested assistance"})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil || tc.accepted {
				require.Nil(t, e.request)
				if tc.want == nil {
					require.Equal(t, e.claim, out)
				}
			} else {
				require.NotNil(t, e.request)
			}
		})
	}
}
