package partnerearnings

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// StatementQuery selects newest-first rows. Dates use economic OccurredAt,
// inclusive From/exclusive To. Filters never change the running matured balance.
type StatementQuery struct {
	Limit          int
	BeforeSequence int64
	Kinds          []string
	From, To       *time.Time
}

// StatementLine is one rendered journal line with its running matured balance.
// Payment is the administrator-recorded presentation at record time, not
// provider proof; PaymentVersion reflects later amended details retained
// separately as claim audit.
type StatementLine struct {
	Entry               Entry
	RunningMaturedMinor int64
	// Payment is an administrator-recorded presentation, not provider proof.
	// RecordedAt/By refer to the original record; later amended details are
	// represented by PaymentVersion and their own claim audit remains retained.
	Payment        *ManualPayment
	PaymentVersion int64
}

// Statement is one owning snapshot of journal, claims and current balances.
// Revision includes claim revisions because reservation/review changes need
// not append journal lines. Sequence alone is not a complete balance revision.
type Statement struct {
	PartnerID, Currency, Revision string
	AsOf                          time.Time
	LedgerSequence                int64
	Balances                      Balances
	Lines                         []StatementLine
	HasMore                       bool
	NextBeforeSequence            int64
}

// validStatementKind reports whether an entry kind may appear as a statement
// line.
func validStatementKind(kind string) bool {
	switch kind {
	case EntryAccrued, EntryMatured, EntryReversed, EntryAllocated, EntryAllocationReleased, EntryAllocationSettled, EntryPaid, EntryPaymentObserved, EntryReturned, EntryDisputeHold, EntryDisputeReleased, EntryDisputeDecision, EntryDisputeWon, EntryDisputeLost:
		return true
	}
	return false
}

// statementMatches reports whether an entry passes the query's sequence, kind
// and economic OccurredAt (inclusive From, exclusive To) filters.
func statementMatches(e Entry, q StatementQuery, kinds map[string]bool) bool {
	return (q.BeforeSequence == 0 || e.Sequence < q.BeforeSequence) && (len(kinds) == 0 || kinds[e.Kind]) && (q.From == nil || !e.OccurredAt.Before(*q.From)) && (q.To == nil || e.OccurredAt.Before(*q.To))
}

// GetStatement reads a complete owning snapshot, then returns a bounded page.
// Running matured values come from the same canonical financial derivation as
// balances over the full journal prefix, never over a filtered subset.
func (s *Service) GetStatement(ctx context.Context, partner string, q StatementQuery) (Statement, error) {
	if err := s.checkContext(ctx); err != nil {
		return Statement{}, err
	}
	if _, ok := cleanPlain(partner, maxIDLength); !ok {
		return Statement{}, ErrInvalid
	}
	if q.Limit < 1 || q.Limit > 100 || q.BeforeSequence < 0 || len(q.Kinds) > 14 || (q.From != nil && q.From.IsZero()) || (q.To != nil && q.To.IsZero()) || (q.From != nil && q.To != nil && !q.To.After(*q.From)) {
		return Statement{}, ErrInvalid
	}
	kinds := map[string]bool{}
	for _, kind := range q.Kinds {
		if !validStatementKind(kind) || kinds[kind] {
			return Statement{}, ErrInvalid
		}
		kinds[kind] = true
	}
	var result Statement
	err := s.repo.WithTransaction(ctx, s.programID, partner, s.currency, func(tx Repository) error {
		result = Statement{}
		entries, err := tx.ListEntries(ctx, s.programID, partner)
		if err != nil {
			return err
		}
		claims, err := tx.ListClaims(ctx, s.programID, partner, nil, 0, "")
		if err != nil {
			return err
		}
		entries = append([]Entry(nil), entries...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
		var sequence int64
		for _, entry := range entries {
			if entry.ProgramID != s.programID || entry.PartnerID != partner || entry.Currency != s.currency || entry.Sequence <= sequence || !validStatementKind(entry.Kind) {
				return ErrConflict
			}
			sequence = entry.Sequence
		}
		balances, _, err := derive(entries, claims)
		if err != nil {
			return err
		}
		byClaim := map[string]Claim{}
		type claimRevision struct {
			ID       string
			Revision int64
			State    string
		}
		revisions := make([]claimRevision, 0, len(claims))
		for _, claim := range claims {
			if claim.ID == "" || claim.ProgramID != s.programID || claim.PartnerID != partner || claim.Currency != s.currency || claim.Revision < 1 {
				return ErrConflict
			}
			if _, duplicate := byClaim[claim.ID]; duplicate {
				return ErrConflict
			}
			byClaim[claim.ID] = claim
			revisions = append(revisions, claimRevision{claim.ID, claim.Revision, claim.State})
		}
		sort.Slice(revisions, func(i, j int) bool { return revisions[i].ID < revisions[j].ID })
		encoded, err := json.Marshal(revisions)
		if err != nil {
			return ErrInvalid
		}
		result = Statement{PartnerID: partner, Currency: s.currency, Revision: fingerprint(s.programID, partner, strconv.FormatInt(sequence, 10), string(encoded)), AsOf: s.clock.Now().UTC(), LedgerSequence: sequence, Balances: balances, Lines: []StatementLine{}}
		for i := len(entries) - 1; i >= 0; i-- {
			if err := ctx.Err(); err != nil {
				return err
			}
			entry := entries[i]
			if !statementMatches(entry, q, kinds) {
				continue
			}
			if len(result.Lines) == q.Limit {
				result.HasMore = true
				break
			}
			running, _, err := derive(entries[:i+1], nil)
			if err != nil {
				return err
			}
			line := StatementLine{Entry: entry, RunningMaturedMinor: running.MatchedMinor}
			if entry.Kind == EntryPaid {
				claim, ok := byClaim[entry.SourceEventID]
				if !ok || claim.Payment == nil || claim.Payment.AmountMinor != claim.AmountMinor || claim.Payment.Currency != claim.Currency {
					return ErrConflict
				}
				payment := *claim.Payment
				line.PaymentVersion = 1
				for _, amendment := range claim.PaymentAmendments {
					if amendment.Method == "" || amendment.Reference == "" || amendment.PaidAt.IsZero() || amendment.At.IsZero() || amendment.By == "" || amendment.IdempotencyKey == "" {
						return ErrConflict
					}
					payment.Method = amendment.Method
					payment.Reference = amendment.Reference
					payment.PaidAt = amendment.PaidAt
					line.PaymentVersion++
				}
				line.Payment = &payment
			}
			result.Lines = append(result.Lines, line)
		}
		if result.HasMore {
			result.NextBeforeSequence = result.Lines[len(result.Lines)-1].Entry.Sequence
		}
		return nil
	})
	if err != nil {
		return Statement{}, err
	}
	return result, nil
}
