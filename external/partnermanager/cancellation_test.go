package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// This port observes binding and revocation only; native owner tests exercise
// the actual financial transaction, terminal receipt and reservation release.
type cancellationEarningsPort struct {
	EarningsService
	claim                  partnerearnings.Claim
	request                partnerearnings.CancelClaimRequest
	calls                  int
	afterRead, afterCancel func()
}

func (p *cancellationEarningsPort) GetClaim(context.Context, string) (partnerearnings.Claim, error) {
	if p.afterRead != nil {
		p.afterRead()
	}
	return p.claim, nil
}
func (p *cancellationEarningsPort) CancelRequestedClaim(_ context.Context, req partnerearnings.CancelClaimRequest) (partnerearnings.Claim, error) {
	p.request = req
	p.calls++
	if p.afterCancel != nil {
		p.afterCancel()
	}
	return p.claim, nil
}

func TestCustomerCancellationCurrentOwnerAndAuthority(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  error
		calls int
	}{
		{"owner_bound", nil, 1}, {"new_withdrawals_paused", nil, 1},
		{"terminal_receipt_reaches_owner", nil, 1},
		{"foreign_claim", ErrNotFound, 0}, {"inactive_account", ErrDenied, 0},
		{"revoked_before_read", ErrDenied, 0}, {"revoked_during_claim_read", ErrDenied, 0},
		{"revoked_after_owner_commit", partnerearnings.ErrUncertain, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, _, a, identity, _ := managerFixture(t)
			e := &cancellationEarningsPort{claim: partnerearnings.Claim{ID: "claim", PartnerID: "partner", Currency: "EUR", State: partnerearnings.ClaimRequested, Revision: 1}}
			m.deps.Earnings = e
			switch tc.name {
			case "new_withdrawals_paused":
				m.deps.Controls.Claims = false
				p.partner.CanRequestPayouts = false
			case "terminal_receipt_reaches_owner":
				e.claim.State = partnerearnings.ClaimCancelled
				e.claim.Revision = 2
			case "foreign_claim":
				e.claim.PartnerID = "other-partner"
			case "inactive_account":
				identity.principal.Active = false
			case "revoked_before_read":
				a.deny = true
			case "revoked_during_claim_read":
				e.afterRead = func() { a.deny = true }
			case "revoked_after_owner_commit":
				e.afterCancel = func() { a.deny = true }
			}
			req := SelfCancellationRequest{ClaimID: "claim", ExpectedRevision: 1, Reason: "customer changed plans", IdempotencyKey: "original-key"}
			out, err := m.CancelClaimWithReceipt(t.Context(), "owner", req)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, e.calls)
			if tc.calls == 1 {
				require.Equal(t, partnerearnings.CancelClaimRequest{PartnerID: "partner", ActorID: "owner", ClaimID: req.ClaimID, ExpectedRevision: req.ExpectedRevision, Reason: req.Reason, IdempotencyKey: req.IdempotencyKey}, e.request)
			}
			if tc.want == nil {
				require.Equal(t, e.claim, out)
			} else {
				require.Empty(t, out.ID)
			}
		})
	}
}
