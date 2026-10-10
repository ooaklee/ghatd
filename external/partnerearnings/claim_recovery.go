package partnerearnings

import "context"

// FindClaimRequest recovers a previously admitted actor/partner/key request in
// one owning transaction. It does not create a claim or consult today's payout
// destination. The manager must check current authority before using this port.
func (s *Service) FindClaimRequest(ctx context.Context, actor, partner, key string) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateActor(actor); err != nil {
		return Claim{}, err
	}
	if _, ok := cleanPlain(partner, maxIDLength); !ok {
		return Claim{}, ErrInvalid
	}
	if _, ok := cleanPlain(key, maxIdempotencyKeyLen); !ok {
		return Claim{}, ErrInvalid
	}
	var result Claim
	err := s.repo.WithTransaction(ctx, s.programID, partner, s.currency, func(tx Repository) error {
		result = Claim{}
		receipt, err := tx.GetReceipt(ctx, ReceiptKey{ProgramID: s.programID, PartnerID: partner, ActorID: actor, UseCase: UseCaseClaim, Currency: s.currency, Key: key})
		if err != nil {
			return err
		}
		if receipt.ProgramID != s.programID || receipt.PartnerID != partner || receipt.ActorID != actor || receipt.UseCase != UseCaseClaim || receipt.Currency != s.currency || receipt.Key != key || receipt.Fingerprint == "" || receipt.ClaimID == "" {
			return ErrConflict
		}
		claim, err := tx.GetClaim(ctx, s.programID, receipt.ClaimID)
		if err != nil {
			return err
		}
		if claim.ID != receipt.ClaimID || claim.ProgramID != s.programID || claim.PartnerID != partner || claim.Currency != s.currency || claim.RequestedBy != actor {
			return ErrConflict
		}
		original := ClaimRequest{ActorID: actor, PartnerID: partner, AmountMinor: claim.AmountMinor, Currency: claim.Currency, DestinationID: claim.DestinationID, DestinationSnapshot: claim.DestinationSnapshot, IdempotencyKey: key, Reason: claim.RequestedReason}
		if receipt.Fingerprint != claimFingerprint(s.programID, original) {
			return ErrConflict
		}
		result = claim
		return nil
	})
	if err != nil {
		return Claim{}, err
	}
	return result, nil
}
