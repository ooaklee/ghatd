package partnermanager

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named current permission, actor/target and frozen receipt
// cases cover manager boundaries; Mongo financial recovery has separate proof.
type claimEarningsStub struct {
	EarningsService
	claim       partnerearnings.Claim
	lookupErr   error
	request     *partnerearnings.ClaimRequest
	afterLookup func()
}

func (e *claimEarningsStub) FindClaimRequest(context.Context, string, string, string) (partnerearnings.Claim, error) {
	if e.afterLookup != nil {
		e.afterLookup()
	}
	return e.claim, e.lookupErr
}
func (e *claimEarningsStub) RequestClaim(_ context.Context, req partnerearnings.ClaimRequest) (partnerearnings.Claim, error) {
	e.request = &req
	return partnerearnings.Claim{ID: "new-claim"}, nil
}

type claimProgramStub struct {
	*programStub
	destination partnerprogram.Destination
}

func (p *claimProgramStub) GetPayoutDestination(context.Context, string) (partnerprogram.Destination, error) {
	return p.destination, p.destinationErr
}

type claimAuthorityStub struct {
	permitted     string
	revoked       bool
	actor, target string
}

func (a *claimAuthorityStub) CheckPartners(_ context.Context, actor, cap, target string) error {
	a.actor = actor
	a.target = target
	if a.revoked || cap != a.permitted || target != "partner" {
		return ErrDenied
	}
	return nil
}

func TestClaimRecoveryUsesFrozenDestinationBeforeNewAdmission(t *testing.T) {
	cases := []struct {
		name                          string
		amount                        int64
		lookupErr                     error
		revokedDuringRead, wrongOwner bool
		want                          error
	}{
		{name: "committed_request_recovers_after_destination_change_and_pause", amount: 100},
		{name: "changed_amount_conflicts", amount: 101, want: partnerearnings.ErrConflict},
		{name: "wrong_receipt_owner_conflicts", amount: 100, wrongOwner: true, want: partnerearnings.ErrConflict},
		{name: "revocation_during_receipt_read_denies_return", amount: 100, revokedDuringRead: true, want: ErrDenied},
		{name: "fresh_request_remains_paused", amount: 100, lookupErr: partnerearnings.ErrNotFound, want: ErrDenied},
		{name: "joined_absence_outage_is_not_new_admission", amount: 100, lookupErr: errors.Join(partnerearnings.ErrNotFound, ErrUnavailable), want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, _, a, _, _ := managerFixture(t)
			m.deps.Controls.Claims = false
			p.partner.CanRequestPayouts = false
			p.destinationErr = partnerprogram.ErrUnavailable
			original := partnerearnings.Claim{ID: "original", PartnerID: "partner", RequestedBy: "owner", AmountMinor: 100, Currency: "EUR", DestinationID: "old-destination", DestinationSnapshot: map[string]string{"version": "1"}}
			e := &claimEarningsStub{claim: original, lookupErr: tc.lookupErr}
			if tc.wrongOwner {
				e.claim.RequestedBy = "other"
			}
			if tc.revokedDuringRead {
				e.afterLookup = func() { a.deny = true }
			}
			m.deps.Earnings = e
			out, err := m.RequestClaim(context.Background(), "owner", tc.amount, "request-key")
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, e.request)
			if tc.want == nil {
				require.Equal(t, original, out)
			}
		})
	}
}

func TestAdminClaimOnBehalfUsesScopedOperatorAndOwningDestination(t *testing.T) {
	cases := []struct {
		name, permission                               string
		staleDestination, wrongOwner, paused, accepted bool
		want                                           error
	}{
		{name: "explicit_scoped_claim_creation", permission: CapabilityCreateClaimOnBehalf},
		{name: "reporting_does_not_create_claim", permission: CapabilityReporting, want: ErrDenied},
		{name: "processing_does_not_create_claim", permission: CapabilityProcessing, want: ErrDenied},
		{name: "stale_destination_version", permission: CapabilityCreateClaimOnBehalf, staleDestination: true, want: partnerearnings.ErrStaleWrite},
		{name: "wrong_owning_principal", permission: CapabilityCreateClaimOnBehalf, wrongOwner: true, want: ErrDenied},
		{name: "fresh_claim_paused", permission: CapabilityCreateClaimOnBehalf, paused: true, want: ErrDenied},
		{name: "committed_operator_claim_recovers_under_pause", permission: CapabilityCreateClaimOnBehalf, paused: true, accepted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, base, _, _, _, identity, _ := managerFixture(t)
			p := &claimProgramStub{programStub: base, destination: partnerprogram.Destination{ID: "destination", CustomerID: "owner", Method: "paypal", Email: "owner@example.test", Version: 1}}
			p.destinationErr = nil
			if tc.staleDestination {
				p.destination.Version = 2
			}
			m.deps.Program = p
			if tc.wrongOwner {
				identity.principal.ID = "unrelated"
			}
			m.deps.Controls.Claims = !tc.paused
			a := &claimAuthorityStub{permitted: tc.permission}
			m.deps.Authority = a
			e := &claimEarningsStub{lookupErr: partnerearnings.ErrNotFound}
			if tc.accepted {
				e.lookupErr = nil
				e.claim = partnerearnings.Claim{ID: "original", PartnerID: "partner", RequestedBy: "operator", RequestedReason: "owner requested assistance", AmountMinor: 100, Currency: "EUR", DestinationSnapshot: map[string]string{"version": "1"}}
				p.destinationErr = partnerprogram.ErrUnavailable
			}
			m.deps.Earnings = e
			out, err := m.AdminRequestClaim(context.Background(), ClaimOnBehalfRequest{ActorID: "operator", PartnerID: "partner", AmountMinor: 100, ExpectedDestinationVersion: 1, IdempotencyKey: "request-key", Reason: "owner requested assistance"})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, e.request)
				return
			}
			require.Equal(t, "operator", a.actor)
			require.Equal(t, "partner", a.target)
			if tc.accepted {
				require.Equal(t, "original", out.ID)
				require.Nil(t, e.request)
				return
			}
			require.NotNil(t, e.request)
			require.Equal(t, "operator", e.request.ActorID)
			require.Equal(t, "partner", e.request.PartnerID)
			require.Equal(t, "owner@example.test", e.request.DestinationSnapshot["email"])
		})
	}
}
