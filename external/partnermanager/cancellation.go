package partnermanager

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/partnerearnings"
)

// SelfCancellationRequest freezes the selected claim, revision and reason for
// original-key recovery. Actor and partner are resolved from current owners.
type SelfCancellationRequest struct {
	ClaimID          string
	ExpectedRevision int64
	Reason           string
	IdempotencyKey   string
}

// CancelClaimWithReceipt is the retry-safe customer cancellation path. It never
// supplies confirmed-unsent authority or cancels a processing/review claim.
// Admission controls for new withdrawals do not erase cancellation/recovery.
func (m *Manager) CancelClaimWithReceipt(ctx context.Context, actor string, req SelfCancellationRequest) (partnerearnings.Claim, error) {
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	selected, err := m.GetClaim(ctx, actor, req.ClaimID)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if selected.PartnerID != p.ID {
		return partnerearnings.Claim{}, ErrDenied
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerearnings.Claim{}, err
	}
	claim, err := m.deps.Earnings.CancelRequestedClaim(ctx, partnerearnings.CancelClaimRequest{PartnerID: p.ID, ActorID: actor, ClaimID: req.ClaimID, ExpectedRevision: req.ExpectedRevision, Reason: req.Reason, IdempotencyKey: req.IdempotencyKey})
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if err := m.authorize(ctx, actor, CapabilitySelf, actor); err != nil {
		return partnerearnings.Claim{}, fmt.Errorf("%w: %w", partnerearnings.ErrUncertain, err)
	}
	return claim, nil
}
