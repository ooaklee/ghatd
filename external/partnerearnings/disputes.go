package partnerearnings

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"sort"
	"time"
)

func financialFingerprint(value any) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func strictFinancialAbsence(err error) bool {
	for i := 0; err != nil && i < 32; i++ {
		if err == ErrNotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// Dispute freezes original unrefunded commission, including pending credit.
// Won releases that dispute's hold. Lost reverses only the original commission
// not already refunded/lost; a late hold cannot re-freeze a terminal dispute.
// All evidence, backing changes and its replay decision commit together.
func (s *Service) Dispute(ctx context.Context, req DisputeRequest) (DisputeResult, error) {
	if err := s.checkContext(ctx); err != nil {
		return DisputeResult{}, err
	}
	for _, id := range []string{req.PartnerID, req.PaymentID, req.DisputeID, req.OperationID, req.ActorID, req.Reason} {
		if _, ok := cleanPlain(id, maxIDLength); !ok {
			return DisputeResult{}, ErrInvalid
		}
	}
	if req.Currency != s.currency {
		return DisputeResult{}, ErrCurrencyMismatch
	}
	if req.OccurredAt.IsZero() {
		return DisputeResult{}, ErrInvalid
	}
	if req.Action != "hold" && req.Action != "won" && req.Action != "lost" {
		return DisputeResult{}, ErrInvalid
	}
	fp := financialFingerprint(struct {
		Program string
		Request DisputeRequest
		Actor   string
	}{s.programID, req, req.ActorID})
	var result DisputeResult
	err := s.repo.WithTransaction(ctx, s.programID, req.PartnerID, s.currency, func(tx Repository) error {
		result = DisputeResult{}
		old, err := tx.EntryBySource(ctx, s.programID, req.PartnerID, EntryDisputeDecision, req.OperationID)
		if err == nil {
			if old.Fingerprint != fp {
				return ErrConflict
			}
			result.Entries, err = s.disputeOperationEntries(ctx, tx, req.PartnerID, req.OperationID)
			return err
		}
		if !strictFinancialAbsence(err) {
			return err
		}
		accrual, err := tx.EntryBySource(ctx, s.programID, req.PartnerID, EntryAccrued, req.PaymentID)
		if strictFinancialAbsence(err) {
			return ErrUnresolved
		}
		if err != nil {
			return err
		}
		entries, err := tx.ListEntries(ctx, s.programID, req.PartnerID)
		if err != nil {
			return err
		}
		var reversed, lost, held big.Int
		terminal := ""
		for _, e := range entries {
			if e.SourceRef != req.PaymentID {
				continue
			}
			switch e.Kind {
			case EntryReversed:
				reversed.Add(&reversed, negBigInt(e.AmountMinor))
			case EntryDisputeLost:
				lost.Add(&lost, negBigInt(e.AmountMinor))
			}
			if e.DisputeID == req.DisputeID {
				switch e.Kind {
				case EntryDisputeHold:
					held.Add(&held, big.NewInt(e.AmountMinor))
				case EntryDisputeReleased, EntryDisputeWon:
					held.Sub(&held, big.NewInt(e.AmountMinor))
				case EntryDisputeDecision:
					if e.Note == "won" || e.Note == "lost" {
						terminal = e.Note
					}
				}
			}
		}
		if terminal != "" && req.Action != "hold" && req.Action != terminal {
			return ErrConflict
		}
		appendOne := func(kind string, amount int64, note string) error {
			e := Entry{Kind: kind, SourceEventID: req.OperationID, SourceRef: req.PaymentID, DisputeID: req.DisputeID, AmountMinor: amount, Currency: s.currency, OccurredAt: req.OccurredAt, CreatedAt: s.clock.Now(), ActorID: req.ActorID, Note: note, Fingerprint: fp}
			if err := s.append(ctx, tx, req.PartnerID, &e); err != nil {
				return err
			}
			result.Entries = append(result.Entries, e)
			return nil
		}
		if terminal == "" {
			remaining := new(big.Int).Sub(big.NewInt(accrual.CommissionMinor), &reversed)
			remaining.Sub(remaining, &lost)
			if remaining.Sign() < 0 {
				remaining.SetInt64(0)
			}
			switch req.Action {
			case "hold":
				// One payment cannot have more frozen commission than remains. Existing
				// active dispute holds against the payment consume the same liability.
				allHeld := activeDisputeHolds(entries, req.PaymentID)
				for _, v := range allHeld {
					remaining.Sub(remaining, v)
				}
				if remaining.Sign() < 0 {
					remaining.SetInt64(0)
				}
				amount, err := bigToInt64(remaining)
				if err != nil {
					return err
				}
				if err := appendOne(EntryDisputeHold, amount, req.Reason); err != nil {
					return err
				}
				if amount > 0 {
					if err := s.recomputeBacking(ctx, tx, req.PartnerID, req.PaymentID); err != nil {
						return err
					}
				}
			case "won":
				amount, err := bigToInt64(&held)
				if err != nil {
					return err
				}
				if err := appendOne(EntryDisputeWon, amount, req.Reason); err != nil {
					return err
				}
			case "lost":
				amount, err := bigToInt64(remaining)
				if err != nil {
					return err
				}
				if err := s.reduceDisputeHolds(ctx, tx, req.PartnerID, req.PaymentID, req.OperationID, accrual.CommissionMinor, req.OccurredAt); err != nil {
					return err
				}
				if err := appendOne(EntryDisputeLost, -amount, req.Reason); err != nil {
					return err
				}
				if amount > 0 {
					if err := s.recomputeBacking(ctx, tx, req.PartnerID, req.PaymentID); err != nil {
						return err
					}
				}
			}
		}
		if err := appendOne(EntryDisputeDecision, 0, req.Action); err != nil {
			return err
		}
		if err := s.validateLedgerRepresentable(ctx, tx, req.PartnerID); err != nil {
			return err
		}
		result.Entries, err = s.disputeOperationEntries(ctx, tx, req.PartnerID, req.OperationID)
		return err
	})
	if err != nil {
		return DisputeResult{}, err
	}
	return result, nil
}
func activeDisputeHolds(entries []Entry, payment string) map[string]*big.Int {
	holds := map[string]*big.Int{}
	for _, e := range entries {
		if e.SourceRef != payment || e.DisputeID == "" {
			continue
		}
		if holds[e.DisputeID] == nil {
			holds[e.DisputeID] = new(big.Int)
		}
		switch e.Kind {
		case EntryDisputeHold:
			holds[e.DisputeID].Add(holds[e.DisputeID], big.NewInt(e.AmountMinor))
		case EntryDisputeReleased, EntryDisputeWon:
			holds[e.DisputeID].Sub(holds[e.DisputeID], big.NewInt(e.AmountMinor))
		}
	}
	return holds
}
func (s *Service) reduceDisputeHolds(ctx context.Context, tx Repository, partner, payment, operation string, amount int64, at time.Time) error {
	entries, err := tx.ListEntries(ctx, s.programID, partner)
	if err != nil {
		return err
	}
	holds := activeDisputeHolds(entries, payment)
	ids := make([]string, 0, len(holds))
	for id := range holds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	remaining := big.NewInt(amount)
	for _, id := range ids {
		if remaining.Sign() <= 0 {
			break
		}
		held := holds[id]
		if held.Sign() <= 0 {
			continue
		}
		reduce := new(big.Int).Set(held)
		if reduce.Cmp(remaining) > 0 {
			reduce.Set(remaining)
		}
		value, err := bigToInt64(reduce)
		if err != nil {
			return err
		}
		e := Entry{Kind: EntryDisputeReleased, SourceEventID: operation + ":" + id, SourceRef: payment, DisputeID: id, AmountMinor: value, Currency: s.currency, OccurredAt: at, CreatedAt: s.clock.Now(), Note: "original liability reduced"}
		if err := s.append(ctx, tx, partner, &e); err != nil {
			return err
		}
		remaining.Sub(remaining, reduce)
	}
	return nil
}

// Both initial acceptance and replay return the same complete dispute operation,
// including hold releases. Other source namespaces cannot contaminate the result.
func (s *Service) disputeOperationEntries(ctx context.Context, tx Repository, partner, operation string) ([]Entry, error) {
	entries, err := tx.ListEntries(ctx, s.programID, partner)
	if err != nil {
		return nil, err
	}
	result := []Entry{}
	for _, entry := range entries {
		if entry.SourceEventID != operation {
			continue
		}
		switch entry.Kind {
		case EntryDisputeDecision, EntryDisputeHold, EntryDisputeWon, EntryDisputeLost, EntryDisputeReleased:
			result = append(result, entry)
		}
	}
	return result, nil
}
