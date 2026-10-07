package partnermanager

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

// ClaimOnBehalfRequest distinguishes the verified operator from the selected
// partner. Destination is chosen from the owner, never supplied in a body.
type ClaimOnBehalfRequest struct {
	ActorID                    string `json:"-"`
	PartnerID                  string
	AmountMinor                int64
	ExpectedDestinationVersion int64
	IdempotencyKey             string
	Reason                     string
}

func (m *Manager) AdminRequestClaim(ctx context.Context, req ClaimOnBehalfRequest) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, req.ActorID, CapabilityCreateClaimOnBehalf, req.PartnerID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if !validWorkText(req.PartnerID, 256) || req.AmountMinor <= 0 || !validWorkText(req.IdempotencyKey, 256) || !validWorkText(req.Reason, 1024) || req.ExpectedDestinationVersion < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	p, err := m.deps.Program.GetPartner(ctx, req.PartnerID)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if p.ID != req.PartnerID || p.CustomerID == "" {
		return partnerearnings.Claim{}, ErrUnavailable
	}
	old, err := m.deps.Earnings.FindClaimRequest(ctx, req.ActorID, p.ID, req.IdempotencyKey)
	if err == nil {
		if old.PartnerID != p.ID || old.RequestedBy != req.ActorID || old.RequestedReason != req.Reason || old.AmountMinor != req.AmountMinor || old.Currency != m.deps.Program.Config().Currency || old.DestinationSnapshot["version"] != fmt.Sprint(req.ExpectedDestinationVersion) {
			return partnerearnings.Claim{}, partnerearnings.ErrConflict
		}
		if err := m.authorize(ctx, req.ActorID, CapabilityCreateClaimOnBehalf, req.PartnerID); err != nil {
			return partnerearnings.Claim{}, err
		}
		return old, nil
	}
	if !singleManagerAbsence(err, partnerearnings.ErrNotFound) {
		return partnerearnings.Claim{}, err
	}
	if !m.deps.Controls.Claims || !p.CanRequestPayouts {
		return partnerearnings.Claim{}, ErrDenied
	}
	if req.AmountMinor < m.deps.Claims.MinimumMinor {
		return partnerearnings.Claim{}, ErrInvalid
	}
	principal, err := m.deps.Identity.GetPartnerPrincipal(ctx, p.CustomerID)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if principal.ID != p.CustomerID || !principal.Active || !principal.EmailVerified || !principal.Individual {
		return partnerearnings.Claim{}, ErrDenied
	}
	d, err := m.deps.Program.GetPayoutDestination(ctx, p.CustomerID)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if d.CustomerID != p.CustomerID || d.Version != req.ExpectedDestinationVersion || d.ID == "" {
		return partnerearnings.Claim{}, partnerearnings.ErrStaleWrite
	}
	if err := m.authorize(ctx, req.ActorID, CapabilityCreateClaimOnBehalf, req.PartnerID); err != nil {
		return partnerearnings.Claim{}, err
	}
	return m.deps.Earnings.RequestClaim(ctx, partnerearnings.ClaimRequest{ActorID: req.ActorID, PartnerID: p.ID, AmountMinor: req.AmountMinor, Currency: m.deps.Program.Config().Currency, DestinationID: d.ID, DestinationSnapshot: map[string]string{"destination_id": d.ID, "method": d.Method, "email": d.Email, "version": fmt.Sprint(d.Version)}, IdempotencyKey: req.IdempotencyKey, Reason: req.Reason})
}

// AdminPaymentClaim resolves the selected financial obligation under the exact
// payment-recording capability, before and after the owning read. A host uses
// its immutable amount/currency to prepare a full manual attestation without
// borrowing reporting/processing authority or accepting those fields in a body.
// The returned domain view needs an explicit permission-appropriate host DTO.
func (m *Manager) AdminPaymentClaim(ctx context.Context, actor, claimID string) (partnerearnings.Claim, error) {
	if err := m.authorize(ctx, actor, CapabilityRecordPayment, claimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	if !validWorkText(claimID, 256) {
		return partnerearnings.Claim{}, ErrInvalid
	}
	claim, err := m.deps.Earnings.GetClaim(ctx, claimID)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if claim.ID != claimID || claim.ProgramID != partnerprogram.ProgramID || !validWorkText(claim.PartnerID, 256) || claim.AmountMinor <= 0 || claim.Revision < 1 || claim.Currency != m.deps.Program.Config().Currency {
		return partnerearnings.Claim{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilityRecordPayment, claimID); err != nil {
		return partnerearnings.Claim{}, err
	}
	return claim, nil
}
