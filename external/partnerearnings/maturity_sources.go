package partnerearnings

import (
	"context"
	"encoding/hex"
	"strings"
	"time"
)

const (
	// MaturityPending retains an accrued allocation awaiting a journal movement.
	MaturityPending = "pending"
	// MaturityCompleted retains the actual accepted maturity journal receipt.
	MaturityCompleted = "completed"
)

// MaturitySource is private owning financial evidence for durable discovery.
// Its identity and fingerprint freeze the accepted accrual, including its
// original deadline. Completion retains that evidence and the actual journal
// receipt indefinitely. Never expose this service port as customer JSON.
type MaturitySource struct {
	// ID is a digest of program/currency/partner/payment; the other identities
	// are private scope/evidence fields, never indexed customer/provider data.
	ID, ProgramID, PartnerID, Currency, PaymentID string `json:"-"`
	// Original accepted journal identity and frozen accrual fingerprint.
	AccruedEntryID, AccruedFingerprint string `json:"-"`
	// Stable accrual order and original gross commission, including zero.
	AccruedSequence, AccruedAmountMinor int64 `json:"-"`
	// Original deadline and owning accrual recording time; neither is retimed
	// by refunds, dispute holds, discovery, retries or completion.
	AvailableAt, CreatedAt time.Time `json:"-"`
	// Fingerprint authenticates immutable input; mutable completion is excluded.
	Fingerprint string `json:"-"`
	// State/Revision are pending/1 or completed/2, persisted through guarded CAS.
	State    string `json:"-"`
	Revision int64  `json:"-"`
	// Completed journal entry identity and its owning recording time; these
	// are proof only after verification against the complete financial snapshot.
	MaturedEntryID string    `json:"-"`
	MaturedAt      time.Time `json:"-"`
}

// MaturityRepository persists service-selected financial source transitions.
// Writes use only the repository bound to the existing ledger transaction;
// the adapter never computes a deadline, eligibility, amount or completion.
type MaturityRepository interface {
	// GetMaturitySource selects one source in the configured scope; absence is
	// ErrNotFound, never an assertion that the corresponding ledger is caught up.
	GetMaturitySource(ctx context.Context, programID, id string) (MaturitySource, error)
	// InsertMaturitySource retains a pending source in the accrual transaction.
	InsertMaturitySource(ctx context.Context, source MaturitySource) error
	// ReplaceMaturitySource commits the service-selected receipt with the same
	// immutable input and expected revision; changed input/revision conflicts.
	ReplaceMaturitySource(ctx context.Context, source MaturitySource, expectedRevision int64) error
	// PendingMaturitySourcesAfter selects at most limit records by stable ID.
	// The repository is bound to its configured program and currency. Sources
	// may complete concurrently after selection; their immutable input survives.
	PendingMaturitySourcesAfter(ctx context.Context, programID, currency, afterID string, limit int) ([]MaturitySource, error)
}

func maturityID(program, currency, partner, payment string) string {
	return "maturity_" + financialFingerprint([]string{program, currency, partner, payment})
}

func validMaturityID(id string) bool {
	if !strings.HasPrefix(id, "maturity_") || len(id) != len("maturity_")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, "maturity_"))
	return err == nil && strings.ToLower(id) == id
}

func maturityFingerprint(source MaturitySource) string {
	return financialFingerprint(struct {
		ID, Program, Partner, Currency, Payment, Entry, AccrualFingerprint string
		Sequence, Amount                                                   int64
		AvailableAt, CreatedAt                                             string
	}{source.ID, source.ProgramID, source.PartnerID, source.Currency, source.PaymentID, source.AccruedEntryID, source.AccruedFingerprint, source.AccruedSequence, source.AccruedAmountMinor, source.AvailableAt.UTC().Format(time.RFC3339Nano), source.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func sourceFromAccrual(entry Entry) MaturitySource {
	source := MaturitySource{ID: maturityID(entry.ProgramID, entry.Currency, entry.PartnerID, entry.SourceEventID), ProgramID: entry.ProgramID, PartnerID: entry.PartnerID, Currency: entry.Currency, PaymentID: entry.SourceEventID, AccruedEntryID: entry.ID, AccruedFingerprint: entry.Fingerprint, AccruedSequence: entry.Sequence, AccruedAmountMinor: entry.AmountMinor, CreatedAt: entry.CreatedAt.UTC(), State: MaturityPending, Revision: 1}
	if entry.AvailableAt != nil {
		source.AvailableAt = entry.AvailableAt.UTC()
	}
	source.Fingerprint = maturityFingerprint(source)
	return source
}

// Validate checks internal identity, immutable fingerprint and the only two
// retained states. It does not replace verification against the owning journal.
func (source MaturitySource) Validate() error {
	if (Config{ProgramID: source.ProgramID, Currency: source.Currency}).Validate() != nil || source.ID != maturityID(source.ProgramID, source.Currency, source.PartnerID, source.PaymentID) || !validMaturityID(source.ID) || source.AccruedSequence < 1 || source.AccruedAmountMinor < 0 || source.AvailableAt.IsZero() || source.CreatedAt.IsZero() {
		return ErrConflict
	}
	for _, value := range []string{source.PartnerID, source.PaymentID, source.AccruedEntryID, source.AccruedFingerprint} {
		if _, ok := cleanPlain(value, maxIDLength); !ok {
			return ErrConflict
		}
	}
	if source.Fingerprint != maturityFingerprint(source) {
		return ErrConflict
	}
	switch source.State {
	case MaturityPending:
		if source.Revision != 1 || source.MaturedEntryID != "" || !source.MaturedAt.IsZero() {
			return ErrConflict
		}
	case MaturityCompleted:
		if _, ok := cleanPlain(source.MaturedEntryID, maxIDLength); !ok || source.Revision != 2 || source.MaturedEntryID == source.AccruedEntryID || source.MaturedAt.IsZero() {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	return nil
}

// verifyMaturitySource compares retained source evidence with journal entries
// from the same financial snapshot. Missing/contradictory proof is corruption,
// never a successful no-entitlement decision or permission to rebuild a guess.
func (s *Service) verifyMaturitySource(source MaturitySource, entries []Entry) error {
	if source.Validate() != nil || source.ProgramID != s.programID || source.Currency != s.currency {
		return ErrConflict
	}
	var accrued, matured *Entry
	for i := range entries {
		e := &entries[i]
		if e.SourceEventID != source.PaymentID || e.PartnerID != source.PartnerID {
			continue
		}
		switch e.Kind {
		case EntryAccrued:
			if accrued != nil {
				return ErrConflict
			}
			accrued = e
		case EntryMatured:
			if matured != nil {
				return ErrConflict
			}
			matured = e
		}
	}
	if accrued == nil || source.Fingerprint != sourceFromAccrual(*accrued).Fingerprint {
		return ErrConflict
	}
	if matured == nil {
		if source.State != MaturityPending {
			return ErrConflict
		}
		return nil
	}
	if source.State != MaturityCompleted || source.MaturedEntryID != matured.ID || !source.MaturedAt.Equal(matured.CreatedAt) || matured.ProgramID != s.programID || matured.Currency != s.currency || matured.Sequence <= accrued.Sequence || matured.AmountMinor != accrued.AmountMinor || !matured.OccurredAt.Equal(matured.CreatedAt) || (accrued.HoldDuration > 0 && matured.OccurredAt.Before(source.AvailableAt)) {
		return ErrConflict
	}
	return nil
}

func (s *Service) completeMaturitySource(ctx context.Context, tx Repository, accrued, matured Entry) error {
	source, err := tx.GetMaturitySource(ctx, s.programID, maturityID(s.programID, s.currency, accrued.PartnerID, accrued.SourceEventID))
	if strictFinancialAbsence(err) {
		return ErrUnavailable
	}
	if err != nil {
		return err
	}
	if source.Validate() != nil || source.Fingerprint != sourceFromAccrual(accrued).Fingerprint {
		return ErrConflict
	}
	if source.State != MaturityPending {
		return ErrConflict
	}
	source.State, source.Revision = MaturityCompleted, 2
	source.MaturedEntryID, source.MaturedAt = matured.ID, matured.CreatedAt.UTC()
	if err := s.verifyMaturitySource(source, []Entry{accrued, matured}); err != nil {
		return err
	}
	return tx.ReplaceMaturitySource(ctx, source, 1)
}

// GetMaturitySource returns private accepted evidence verified in one complete
// owning financial snapshot. A preliminary source lookup selects the ledger;
// scope and source are fetched again inside that snapshot before projection.
func (s *Service) GetMaturitySource(ctx context.Context, id string) (MaturitySource, error) {
	if err := s.checkContext(ctx); err != nil {
		return MaturitySource{}, err
	}
	if !validMaturityID(id) {
		return MaturitySource{}, ErrInvalid
	}
	pre, err := s.repo.GetMaturitySource(ctx, s.programID, id)
	if err != nil {
		return MaturitySource{}, err
	}
	if pre.ID != id || pre.ProgramID != s.programID || pre.Currency != s.currency || pre.Validate() != nil {
		return MaturitySource{}, ErrConflict
	}
	var out MaturitySource
	err = s.withPaymentSnapshot(ctx, pre.PartnerID, func(snapshot paymentSnapshot) error {
		out = MaturitySource{}
		current, err := snapshot.repository.GetMaturitySource(ctx, s.programID, id)
		if err != nil {
			return err
		}
		if current.ID != id || current.PartnerID != pre.PartnerID || current.Fingerprint != pre.Fingerprint {
			return ErrConflict
		}
		if err := s.verifyMaturitySource(current, snapshot.entries); err != nil {
			return err
		}
		out = current
		return nil
	})
	if err != nil {
		return MaturitySource{}, err
	}
	return out, nil
}

// PendingMaturitySourcesAfter discovers a bounded page from this configured
// program/currency's private index. Each source is verified against its owning
// current financial snapshot; a concurrent completion preserves its immutable
// candidate and is returned with its actual accepted receipt. No TTL applies.
func (s *Service) PendingMaturitySourcesAfter(ctx context.Context, after string, limit int) ([]MaturitySource, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 || (after != "" && !validMaturityID(after)) {
		return nil, ErrInvalid
	}
	page, err := s.repo.PendingMaturitySourcesAfter(ctx, s.programID, s.currency, after, limit)
	if err != nil {
		return nil, err
	}
	if len(page) > limit {
		return nil, ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]MaturitySource, 0, len(page))
	for _, source := range page {
		if source.ID <= after || source.State != MaturityPending || source.Validate() != nil || source.ProgramID != s.programID || source.Currency != s.currency {
			return nil, ErrConflict
		}
		current, err := s.GetMaturitySource(ctx, source.ID)
		if err != nil {
			return nil, err
		}
		if current.Fingerprint != source.Fingerprint {
			return nil, ErrConflict
		}
		out = append(out, current)
		after = source.ID
	}
	return out, nil
}
