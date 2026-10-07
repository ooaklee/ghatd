package partnerearnings

import (
	"context"
	"math/big"
	"sort"
)

// RecordReturnedTransfer restores a proven returned payout as an obligation.
// It preserves the paid record and adds an audited adjustment, capped by the
// original settlement net of earlier returns. Backing and money commit together.
func (s *Service) RecordReturnedTransfer(ctx context.Context, req ReturnRequest) (ReturnResult, error) {
	if err := s.checkContext(ctx); err != nil {
		return ReturnResult{}, err
	}
	if err := validateActor(req.ActorID); err != nil {
		return ReturnResult{}, err
	}
	if err := validateExpectedRevision(req.ExpectedRevision); err != nil {
		return ReturnResult{}, err
	}
	for _, v := range []string{req.ClaimID, req.IdempotencyKey, req.Reference, req.Reason} {
		if _, ok := cleanPlain(v, maxIDLength); !ok {
			return ReturnResult{}, ErrInvalid
		}
	}
	if req.AmountMinor <= 0 || req.ReturnedAt.IsZero() || req.ReturnedAt.After(s.clock.Now()) {
		return ReturnResult{}, ErrInvalid
	}
	if req.Currency != s.currency {
		return ReturnResult{}, ErrCurrencyMismatch
	}
	pre, err := s.repo.GetClaim(ctx, s.programID, req.ClaimID)
	if err != nil {
		return ReturnResult{}, err
	}
	fp := financialFingerprint(struct {
		Program string
		Request ReturnRequest
		Actor   string
	}{s.programID, req, req.ActorID})
	var result ReturnResult
	err = s.repo.WithTransaction(ctx, s.programID, pre.PartnerID, s.currency, func(tx Repository) error {
		result = ReturnResult{}
		claim, err := tx.GetClaim(ctx, s.programID, req.ClaimID)
		if err != nil {
			return err
		}
		key := ReceiptKey{ProgramID: s.programID, PartnerID: claim.PartnerID, ActorID: req.ActorID, UseCase: UseCaseReturn, Currency: s.currency, Key: req.IdempotencyKey}
		receipt, err := tx.GetReceipt(ctx, key)
		if err == nil {
			if receipt.Fingerprint != fp {
				return ErrConflict
			}
			entries, err := tx.ListEntries(ctx, s.programID, claim.PartnerID)
			if err != nil {
				return err
			}
			result.Claim = claim
			for _, e := range entries {
				if e.Kind == EntryReturned && e.SourceEventID == receipt.OperationID {
					result.Entries = append(result.Entries, e)
				}
			}
			return nil
		}
		if !strictFinancialAbsence(err) {
			return err
		}
		if claim.Revision != req.ExpectedRevision {
			return ErrStaleWrite
		}
		if claim.State != ClaimPaid || claim.Payment == nil {
			return ErrInvalidState
		}
		entries, err := tx.ListEntries(ctx, s.programID, claim.PartnerID)
		if err != nil {
			return err
		}
		restored, err := returnedAllocationBacking(entries, claim.ID)
		if err != nil {
			return err
		}
		remaining := big.NewInt(claim.Payment.AmountMinor)
		for _, e := range entries {
			if e.Kind == EntryReturned && e.SourceRef == claim.ID {
				remaining.Sub(remaining, big.NewInt(e.AmountMinor))
			}
		}
		if remaining.Cmp(big.NewInt(req.AmountMinor)) < 0 {
			return ErrInvalid
		}
		operation := s.ids.NewID()
		now := s.clock.Now()
		returned := Entry{Kind: EntryReturned, SourceEventID: operation, SourceRef: claim.ID, AmountMinor: req.AmountMinor, Currency: s.currency, OccurredAt: req.ReturnedAt, CreatedAt: now, ActorID: req.ActorID, Fingerprint: fp, Note: req.Reason}
		if err := s.append(ctx, tx, claim.PartnerID, &returned); err != nil {
			return err
		}
		// Restore original settled backing in its oldest allocation order. Each
		// original payment remains identifiable; future claims still allocate oldest.
		allocations := []Entry{}
		for _, e := range entries {
			if e.Kind == EntryAllocated && e.SourceEventID == claim.ID {
				allocations = append(allocations, e)
			}
		}
		sort.Slice(allocations, func(i, j int) bool { return allocations[i].Sequence < allocations[j].Sequence })
		toRestore := req.AmountMinor
		for _, a := range allocations {
			if toRestore == 0 {
				break
			}
			amount := a.AmountMinor - restored[a.SourceRef]
			if amount <= 0 {
				continue
			}
			if amount > toRestore {
				amount = toRestore
			}
			release := Entry{Kind: EntryAllocationReleased, SourceEventID: operation, SourceRef: a.SourceRef, AmountMinor: -amount, Currency: s.currency, OccurredAt: req.ReturnedAt, CreatedAt: now, ActorID: req.ActorID, Note: "returned backing: " + claim.ID}
			if err := s.append(ctx, tx, claim.PartnerID, &release); err != nil {
				return err
			}
			toRestore -= amount
		}
		if toRestore != 0 {
			return ErrUnavailable
		}
		claim.ReturnedAdjustments = append(claim.ReturnedAdjustments, ReturnedAdjustment{OperationID: operation, AmountMinor: req.AmountMinor, Currency: req.Currency, Reference: req.Reference, ReturnedAt: req.ReturnedAt, Reason: req.Reason, By: req.ActorID, At: now, IdempotencyKey: req.IdempotencyKey})
		claim.UpdatedAt = now
		claim.UpdatedBy = req.ActorID
		claim.Revision++
		if _, err := tx.ReplaceClaim(ctx, claim, claim.Revision-1); err != nil {
			return err
		}
		if err := tx.PutReceipt(ctx, Receipt{ProgramID: s.programID, PartnerID: claim.PartnerID, ActorID: req.ActorID, UseCase: UseCaseReturn, Currency: s.currency, Key: req.IdempotencyKey, Fingerprint: fp, ClaimID: claim.ID, OperationID: operation, CreatedAt: now}); err != nil {
			return err
		}
		result = ReturnResult{Claim: claim, Entries: []Entry{returned}}
		return s.validateLedgerRepresentable(ctx, tx, claim.PartnerID)
	})
	if err != nil {
		return ReturnResult{}, err
	}
	return result, nil
}
func restoredBacking(prior, negative int64) (int64, error) {
	return bigToInt64(new(big.Int).Add(big.NewInt(prior), negBigInt(negative)))
}

// returnedAllocationBacking joins release rows to typed returned-transfer
// operations, never to their human-readable Note. It checks complete operation
// totals and original allocation caps before another return can restore money.
func returnedAllocationBacking(entries []Entry, claim string) (map[string]int64, error) {
	operations := map[string]Entry{}
	allocations := map[string]*big.Int{}
	for _, e := range entries {
		if e.Kind == EntryAllocated && e.SourceEventID == claim {
			if e.SourceRef == "" || e.AmountMinor <= 0 {
				return nil, ErrConflict
			}
			if allocations[e.SourceRef] == nil {
				allocations[e.SourceRef] = new(big.Int)
			}
			allocations[e.SourceRef].Add(allocations[e.SourceRef], big.NewInt(e.AmountMinor))
		}
		if e.Kind == EntryReturned && e.SourceRef == claim {
			if e.SourceEventID == "" || e.SourceEventID == claim || e.AmountMinor <= 0 {
				return nil, ErrConflict
			}
			if _, duplicate := operations[e.SourceEventID]; duplicate {
				return nil, ErrConflict
			}
			operations[e.SourceEventID] = e
		}
	}
	restored := map[string]int64{}
	totals := map[string]*big.Int{}
	for _, e := range entries {
		operation, ok := operations[e.SourceEventID]
		if !ok || e.Kind != EntryAllocationReleased {
			continue
		}
		if e.AmountMinor >= 0 || allocations[e.SourceRef] == nil || e.Currency != operation.Currency || e.ProgramID != operation.ProgramID || e.PartnerID != operation.PartnerID || e.Sequence <= operation.Sequence {
			return nil, ErrConflict
		}
		value, err := restoredBacking(restored[e.SourceRef], e.AmountMinor)
		if err != nil {
			return nil, err
		}
		restored[e.SourceRef] = value
		if big.NewInt(value).Cmp(allocations[e.SourceRef]) > 0 {
			return nil, ErrConflict
		}
		if totals[e.SourceEventID] == nil {
			totals[e.SourceEventID] = new(big.Int)
		}
		totals[e.SourceEventID].Add(totals[e.SourceEventID], negBigInt(e.AmountMinor))
	}
	for id, operation := range operations {
		if totals[id] == nil || totals[id].Cmp(big.NewInt(operation.AmountMinor)) != 0 {
			return nil, ErrConflict
		}
	}
	return restored, nil
}

// AcceptedAccrual reads original durable financial acceptance before current
// admission flags or provider state are consulted by a retrying worker.
func (s *Service) AcceptedAccrual(ctx context.Context, partner, payment string) (Entry, error) {
	if err := s.checkContext(ctx); err != nil {
		return Entry{}, err
	}
	for _, v := range []string{partner, payment} {
		if _, ok := cleanPlain(v, maxIDLength); !ok {
			return Entry{}, ErrInvalid
		}
	}
	return s.repo.EntryBySource(ctx, s.programID, partner, EntryAccrued, payment)
}
