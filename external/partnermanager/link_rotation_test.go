package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type rotationManagerReferral struct {
	ReferralService
	request referral.RotateLinkRequest
	calls   int
	revoke  *authorityStub
}

func (r *rotationManagerReferral) RotateLink(_ context.Context, req referral.RotateLinkRequest) (referral.Link, error) {
	r.calls++
	r.request = req
	if r.revoke != nil {
		r.revoke.deny = true
	}
	return referral.Link{ID: "owning-result"}, nil
}

type rotationRevokingProgram struct {
	ProgramService
	authority *authorityStub
}

func (p rotationRevokingProgram) GetPartnerForCustomer(ctx context.Context, actor string) (partnerprogram.Partner, error) {
	out, err := p.ProgramService.GetPartnerForCustomer(ctx, actor)
	p.authority.deny = true
	return out, err
}

func TestRotateLinkChecksRevocationBeforeAndAfterOwningReceipt(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls int
		want  error
	}{{"revoked_during_identity_reads", 0, ErrDenied}, {"revoked_after_committed_receipt", 1, referral.ErrUncertain}} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, a, _, _ := managerFixture(t)
			r := &rotationManagerReferral{}
			m.deps.Referral = r
			if tc.calls == 0 {
				m.deps.Program = rotationRevokingProgram{ProgramService: m.deps.Program, authority: a}
			} else {
				r.revoke = a
			}
			out, err := m.RotateLink(context.Background(), "owner", referral.RotateLinkRequest{ExpectedLinkCode: "original", Reason: "reviewed reason", IdempotencyKey: "original-key"})
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out.ID)
			require.Equal(t, tc.calls, r.calls)
		})
	}
}

// These cases verify facade binding and current admission, not datastore
// atomicity. Native owner tests prove receipt recovery and pause behavior.
func TestRotateLinkBindsCurrentOwnerAndDoesNotTrustTransportActor(t *testing.T) {
	for _, tc := range []struct {
		name            string
		denied, acquire bool
	}{
		{"current_owner", false, true}, {"attribution_paused", false, false},
		{"partner_acquisition_paused", false, false}, {"revoked_permission", true, true},
		{"inactive_account", true, true}, {"wrong_owning_customer", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, _, a, i, _ := managerFixture(t)
			r := &rotationManagerReferral{}
			m.deps.Referral = r
			switch tc.name {
			case "attribution_paused":
				m.deps.Controls.Attribution = false
			case "partner_acquisition_paused":
				p.partner.CanAcquireReferrals = false
			case "revoked_permission":
				a.deny = true
			case "inactive_account":
				i.principal.Active = false
			case "wrong_owning_customer":
				p.partner.CustomerID = "different"
			}
			req := referral.RotateLinkRequest{Partner: referral.PartnerState{PartnerID: "forged-partner", CustomerID: "forged-customer", CanAcquireReferrals: true}, ActorID: "forged-actor", ExpectedLinkCode: "observed-original", Reason: "reviewed reason", IdempotencyKey: "original-key"}
			_, err := m.RotateLink(context.Background(), "owner", req)
			if tc.denied {
				require.ErrorIs(t, err, ErrDenied)
				require.Zero(t, r.calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, r.calls)
				require.Equal(t, "owner", r.request.ActorID)
				require.Equal(t, "partner", r.request.Partner.PartnerID)
				require.Equal(t, "owner", r.request.Partner.CustomerID)
				require.Equal(t, tc.acquire, r.request.Partner.CanAcquireReferrals)
				require.Equal(t, req.ExpectedLinkCode, r.request.ExpectedLinkCode)
				require.Equal(t, req.IdempotencyKey, r.request.IdempotencyKey)
				require.Equal(t, req.Reason, r.request.Reason)
			}
		})
	}
}
