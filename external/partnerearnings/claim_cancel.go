package partnerearnings

import (
	"context"
	"math"
	"strconv"
)

// CancelClaimRequest is a verified owner's immutable cancellation intent.
// PartnerID and ActorID are manager-bound, not browser-selected. Only an
// unprocessed requested claim may be cancelled through this self-service path.
type CancelClaimRequest struct {
	PartnerID        string
	ActorID          string `json:"-"`
	ClaimID          string
	ExpectedRevision int64
	Reason           string
	IdempotencyKey   string
}

// cancelClaimFingerprint derives the idempotency fingerprint for a claim
// cancellation from program, currency, partner, actor, claim, expected revision
// and reason.
func cancelClaimFingerprint(program, currency string, req CancelClaimRequest) string {
	return fingerprint(program, UseCaseCancel, req.PartnerID, req.ActorID, currency,
		req.ClaimID, strconv.FormatInt(req.ExpectedRevision, 10), req.Reason)
}

// CancelRequestedClaim commits the release, terminal claim audit and actor/key
// receipt under the existing financial guard. Original-key recovery precedes
// current claim state/revision checks and cannot release a reservation twice.
func (s *Service) CancelRequestedClaim(ctx context.Context, req CancelClaimRequest) (Claim, error) {
	if err := s.checkContext(ctx); err != nil {
		return Claim{}, err
	}
	if err := validateActor(req.ActorID); err != nil {
		return Claim{}, err
	}
	if err := validateExpectedRevision(req.ExpectedRevision); err != nil {
		return Claim{}, err
	}
	if req.ExpectedRevision == math.MaxInt64 {
		return Claim{}, ErrInvalid
	}
	for _, field := range []struct {
		value string
		bound int
	}{{req.PartnerID, maxIDLength}, {req.ActorID, maxIDLength}, {req.ClaimID, maxIDLength}, {req.Reason, maxReasonLength}, {req.IdempotencyKey, maxIdempotencyKeyLen}} {
		if plain, ok := cleanPlain(field.value, field.bound); !ok || plain != field.value {
			return Claim{}, ErrInvalid
		}
	}
	fp := cancelClaimFingerprint(s.programID, s.currency, req)
	var out Claim
	err := s.repo.WithTransaction(ctx, s.programID, req.PartnerID, s.currency, func(tx Repository) error {
		out = Claim{}
		key := ReceiptKey{ProgramID: s.programID, PartnerID: req.PartnerID, ActorID: req.ActorID, UseCase: UseCaseCancel, Currency: s.currency, Key: req.IdempotencyKey}
		receipt, err := tx.GetReceipt(ctx, key)
		if err == nil {
			if receipt.ProgramID != key.ProgramID || receipt.PartnerID != key.PartnerID || receipt.ActorID != key.ActorID || receipt.UseCase != key.UseCase || receipt.Currency != key.Currency || receipt.Key != key.Key || receipt.ClaimID == "" || receipt.Fingerprint == "" || receipt.CreatedAt.IsZero() {
				return ErrUnavailable
			}
			if receipt.Fingerprint != fp || receipt.ClaimID != req.ClaimID {
				return ErrConflict
			}
			claim, err := tx.GetClaim(ctx, s.programID, receipt.ClaimID)
			if err != nil {
				if strictFinancialAbsence(err) {
					return ErrUnavailable
				}
				return err
			}
			if claim.ID != req.ClaimID || claim.ProgramID != s.programID || claim.PartnerID != req.PartnerID || claim.Currency != s.currency || claim.State != ClaimCancelled || claim.Revision != req.ExpectedRevision+1 || claim.UpdatedBy != req.ActorID || claim.Reason != req.Reason || !claim.UpdatedAt.Equal(receipt.CreatedAt) {
				return ErrUnavailable
			}
			out = claim
			return nil
		}
		if !strictFinancialAbsence(err) {
			return err
		}
		claim, err := tx.GetClaim(ctx, s.programID, req.ClaimID)
		if err != nil {
			return err
		}
		if claim.ID != req.ClaimID || claim.ProgramID != s.programID || claim.PartnerID != req.PartnerID || claim.Currency != s.currency {
			return ErrDenied
		}
		if claim.Revision != req.ExpectedRevision {
			return ErrStaleWrite
		}
		if claim.State != ClaimRequested {
			return ErrInvalidState
		}
		updated, err := s.applyDecision(claim, ClaimDecision{ClaimID: req.ClaimID, NewState: ClaimCancelled, Reason: req.Reason, ActorID: req.ActorID, ExpectedRevision: req.ExpectedRevision})
		if err != nil {
			return err
		}
		if updated.claim.UpdatedAt.IsZero() || updated.claim.UpdatedAt.Before(claim.RequestedAt) {
			return ErrInvalid
		}
		if err := s.releaseReservation(ctx, tx, updated.claim, req.ActorID); err != nil {
			return err
		}
		if err := tx.PutReceipt(ctx, Receipt{ProgramID: key.ProgramID, PartnerID: key.PartnerID, ActorID: key.ActorID, UseCase: key.UseCase, Currency: key.Currency, Key: key.Key, ClaimID: req.ClaimID, Fingerprint: fp, CreatedAt: updated.claim.UpdatedAt}); err != nil {
			return err
		}
		if err := s.validateLedgerRepresentable(ctx, tx, req.PartnerID); err != nil {
			return err
		}
		out = updated.claim
		return nil
	})
	if err != nil {
		return Claim{}, err
	}
	return out, nil
}
