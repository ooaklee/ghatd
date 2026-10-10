package partnerstore

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindMaturitySource = "partner_maturity_source"

// persistedMaturity is encrypted storage, separate from the private service
// projection whose JSON deliberately omits every financial/source identity.
type persistedMaturity struct {
	ID, ProgramID, PartnerID, Currency, PaymentID string
	AccruedEntryID, AccruedFingerprint            string
	AccruedSequence, AccruedAmountMinor           int64
	AvailableAt, CreatedAt                        time.Time
	Fingerprint, State                            string
	Revision                                      int64
	MaturedEntryID                                string
	MaturedAt                                     time.Time
}

// maturityPartition derives the per program/currency storage partition for
// maturity sources; partner identity is kept inside the record, not the key.
func maturityPartition(program, currency string) string {
	return "partner-maturity:" + identity(program, currency)
}

// maturityEnvelope copies a MaturitySource into the persisted encrypted
// envelope, preserving all financial and source identity fields.
func maturityEnvelope(v partnerearnings.MaturitySource) persistedMaturity {
	return persistedMaturity{ID: v.ID, ProgramID: v.ProgramID, PartnerID: v.PartnerID, Currency: v.Currency, PaymentID: v.PaymentID, AccruedEntryID: v.AccruedEntryID, AccruedFingerprint: v.AccruedFingerprint, AccruedSequence: v.AccruedSequence, AccruedAmountMinor: v.AccruedAmountMinor, AvailableAt: v.AvailableAt, CreatedAt: v.CreatedAt, Fingerprint: v.Fingerprint, State: v.State, Revision: v.Revision, MaturedEntryID: v.MaturedEntryID, MaturedAt: v.MaturedAt}
}

// decodeMaturity decodes a persisted maturity envelope and re-validates model
// constraints plus row kind, ID, partition, state and revision agreement.
// Corrupt or inconsistent rows return ErrUnavailable, never a partial value.
func decodeMaturity(row recordstore.Record) (partnerearnings.MaturitySource, error) {
	var stored persistedMaturity
	if err := row.Decode(&stored); err != nil {
		return partnerearnings.MaturitySource{}, partnerearnings.ErrUnavailable
	}
	v := partnerearnings.MaturitySource{ID: stored.ID, ProgramID: stored.ProgramID, PartnerID: stored.PartnerID, Currency: stored.Currency, PaymentID: stored.PaymentID, AccruedEntryID: stored.AccruedEntryID, AccruedFingerprint: stored.AccruedFingerprint, AccruedSequence: stored.AccruedSequence, AccruedAmountMinor: stored.AccruedAmountMinor, AvailableAt: stored.AvailableAt, CreatedAt: stored.CreatedAt, Fingerprint: stored.Fingerprint, State: stored.State, Revision: stored.Revision, MaturedEntryID: stored.MaturedEntryID, MaturedAt: stored.MaturedAt}
	if v.Validate() != nil || row.Kind != kindMaturitySource || row.ID != v.ID || row.Partition != maturityPartition(v.ProgramID, v.Currency) || row.State != v.State || row.Revision != v.Revision || row.Sequence != 0 || row.ExpiresAt != nil {
		return partnerearnings.MaturitySource{}, partnerearnings.ErrUnavailable
	}
	return v, nil
}

// maturityRecord builds the storage row for a maturity source, carrying the
// service state in the row envelope and the source's own revision.
func maturityRecord(v partnerearnings.MaturitySource) (recordstore.Record, error) {
	row, err := recordstore.NewRecord(kindMaturitySource, v.ID, maturityPartition(v.ProgramID, v.Currency), v.Revision, maturityEnvelope(v))
	row.State = v.State
	return row, err
}

// GetMaturitySource reads the configured financial scope. Bound transaction
// repositories additionally enforce the selected partner's ledger ownership.
func (r *EarningsRepository) GetMaturitySource(ctx context.Context, program, id string) (partnerearnings.MaturitySource, error) {
	if program != r.program || id == "" {
		return partnerearnings.MaturitySource{}, partnerearnings.ErrInvalid
	}
	var out partnerearnings.MaturitySource
	err := r.read(ctx, func(tx recordstore.Tx) error {
		out = partnerearnings.MaturitySource{}
		row, err := tx.Get(ctx, kindMaturitySource, id)
		if err != nil {
			return err
		}
		v, err := decodeMaturity(row)
		if err != nil {
			return err
		}
		if v.ProgramID != r.program || v.Currency != r.currency || (r.tx != nil && v.PartnerID != r.boundPartner) {
			return partnerearnings.ErrUnavailable
		}
		out = v
		return nil
	})
	if err != nil {
		return partnerearnings.MaturitySource{}, err
	}
	return out, nil
}

// InsertMaturitySource persists a service-admitted pending obligation in the
// same Tx as its accrued entry, although discovery uses a global scope index.
func (r *EarningsRepository) InsertMaturitySource(ctx context.Context, v partnerearnings.MaturitySource) error {
	if r.tx == nil || v.ProgramID != r.program || v.Currency != r.currency || v.PartnerID != r.boundPartner || v.Validate() != nil || v.State != partnerearnings.MaturityPending {
		return partnerearnings.ErrInvalid
	}
	row, err := maturityRecord(v)
	if err != nil {
		return err
	}
	return earningsError(r.tx.Insert(ctx, row))
}

// ReplaceMaturitySource fences completion by revision and immutable source
// evidence. Only the service selects the journal receipt being committed.
func (r *EarningsRepository) ReplaceMaturitySource(ctx context.Context, v partnerearnings.MaturitySource, expected int64) error {
	if r.tx == nil || v.ProgramID != r.program || v.Currency != r.currency || v.PartnerID != r.boundPartner || v.Validate() != nil || v.State != partnerearnings.MaturityCompleted || expected != 1 {
		return partnerearnings.ErrInvalid
	}
	old, err := r.GetMaturitySource(ctx, v.ProgramID, v.ID)
	if err != nil {
		return err
	}
	if old.Revision != expected || old.Fingerprint != v.Fingerprint {
		return partnerearnings.ErrConflict
	}
	row, err := maturityRecord(v)
	if err != nil {
		return err
	}
	return earningsError(r.tx.Replace(ctx, row, expected))
}

// PendingMaturitySourcesAfter performs one indexed, bounded source query.
// It does not scan partner journals or infer financial eligibility/completion.
func (r *EarningsRepository) PendingMaturitySourcesAfter(ctx context.Context, program, currency, after string, limit int) ([]partnerearnings.MaturitySource, error) {
	if r.tx != nil || program != r.program || currency != r.currency || limit < 1 || limit > 200 {
		return nil, partnerearnings.ErrInvalid
	}
	var out []partnerearnings.MaturitySource
	err := r.read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := tx.Find(ctx, recordstore.Query{Kind: kindMaturitySource, Partition: maturityPartition(program, currency), State: partnerearnings.MaturityPending, AfterID: after, Limit: limit})
		if err != nil {
			return err
		}
		if len(rows) > limit {
			return partnerearnings.ErrUnavailable
		}
		previous := after
		for _, row := range rows {
			v, err := decodeMaturity(row)
			if err != nil {
				return err
			}
			if v.ProgramID != program || v.Currency != currency || v.State != partnerearnings.MaturityPending || v.ID <= previous {
				return partnerearnings.ErrUnavailable
			}
			out = append(out, v)
			previous = v.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
