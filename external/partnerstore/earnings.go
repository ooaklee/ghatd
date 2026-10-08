// Package partnerstore adapts typed partner services to encrypted shared
// recordstore transactions. It owns persistence, guards, CAS and schema identity;
// rates, balances and state transitions remain in their owning services.
package partnerstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindEntry         = "partner_journal"
	kindEntrySource   = "partner_journal_source"
	kindLedgerHead    = "partner_ledger_head"
	kindClaim         = "partner_claim"
	kindClaimRevision = "partner_claim_revision"
	kindReceipt       = "partner_earnings_receipt"
)

func identity(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
func ledgerPartition(program, partner, currency string) string {
	return "partner-ledger:" + identity(program, partner, currency)
}

type recordReference struct {
	ID string `json:"id"`
}
type ledgerHead struct {
	Sequence int64 `json:"sequence"`
}

// EarningsRepository pins one program/currency, never blends currencies or opens
// a nested transaction. A callback receives a bound repository with the same Tx.
type EarningsRepository struct {
	store             recordstore.Store
	tx                recordstore.Tx
	program, currency string
	boundPartner      string
}

func NewEarningsRepository(store recordstore.Store, config partnerearnings.Config) (*EarningsRepository, error) {
	if nilStoreDependency(store) {
		return nil, partnerearnings.ErrUnavailable
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &EarningsRepository{store: store, program: config.ProgramID, currency: config.Currency}, nil
}
func earningsError(err error) error {
	switch {
	case singleCauseIs(err, recordstore.ErrNotFound):
		return partnerearnings.ErrNotFound
	case singleCauseIs(err, recordstore.ErrConflict):
		return partnerearnings.ErrConflict
	case errors.Is(err, recordstore.ErrUncertain):
		return fmt.Errorf("%w: %w", partnerearnings.ErrUncertain, err)
	case errors.Is(err, recordstore.ErrUnavailable):
		return fmt.Errorf("%w: %w", partnerearnings.ErrUnavailable, err)
	}
	return err
}
func (r *EarningsRepository) read(ctx context.Context, fn func(recordstore.Tx) error) error {
	if ctx == nil {
		return partnerearnings.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r.tx != nil {
		return earningsError(fn(r.tx))
	}
	return earningsError(r.store.Read(ctx, fn))
}
func (r *EarningsRepository) WithTransaction(ctx context.Context, program, partner, currency string, fn func(partnerearnings.Repository) error) error {
	if r.tx != nil || program != r.program || currency != r.currency || partner == "" || fn == nil {
		return partnerearnings.ErrInvalid
	}
	return earningsError(r.store.Transact(ctx, ledgerPartition(program, partner, currency), func(tx recordstore.Tx) error {
		return fn(&EarningsRepository{tx: tx, program: r.program, currency: r.currency, boundPartner: partner})
	}))
}
func (r *EarningsRepository) ListEntries(ctx context.Context, program, partner string) ([]partnerearnings.Entry, error) {
	if program != r.program || partner == "" || (r.tx != nil && partner != r.boundPartner) {
		return nil, partnerearnings.ErrInvalid
	}
	out := []partnerearnings.Entry{}
	err := r.read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindEntry, Partition: ledgerPartition(program, partner, r.currency)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var e partnerearnings.Entry
			if err := row.Decode(&e); err != nil {
				return err
			}
			if e.ID != row.ID || e.ProgramID != program || e.PartnerID != partner || e.Currency != r.currency || e.Sequence != row.Sequence {
				return partnerearnings.ErrUnavailable
			}
			out = append(out, e)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Sequence < out[j].Sequence })
	return out, err
}
func (r *EarningsRepository) AppendEntry(ctx context.Context, e partnerearnings.Entry) error {
	if r.tx == nil || e.ProgramID != r.program || e.Currency != r.currency || e.Sequence < 1 || e.PartnerID != r.boundPartner {
		return partnerearnings.ErrInvalid
	}
	partition := ledgerPartition(e.ProgramID, e.PartnerID, e.Currency)
	row, err := recordstore.NewRecord(kindEntry, e.ID, partition, 1, e)
	if err != nil {
		return err
	}
	row.Sequence = e.Sequence
	if err := r.tx.Insert(ctx, row); err != nil {
		return earningsError(err)
	}
	source, err := recordstore.NewRecord(kindEntrySource, identity(e.ProgramID, e.PartnerID, e.Kind, e.SourceEventID, e.SourceRef), partition, 1, recordReference{e.ID})
	if err != nil {
		return err
	}
	return earningsError(r.tx.Insert(ctx, source))
}
func (r *EarningsRepository) EntryBySource(ctx context.Context, program, partner, kind, source string) (partnerearnings.Entry, error) {
	rows, err := r.ListEntries(ctx, program, partner)
	if err != nil {
		return partnerearnings.Entry{}, err
	}
	for _, e := range rows {
		if e.Kind == kind && e.SourceEventID == source {
			return e, nil
		}
	}
	return partnerearnings.Entry{}, partnerearnings.ErrNotFound
}
func (r *EarningsRepository) NextSequence(ctx context.Context, program, partner string) (int64, error) {
	if r.tx == nil || program != r.program || partner != r.boundPartner {
		return 0, partnerearnings.ErrInvalid
	}
	id := identity(program, partner, r.currency)
	partition := ledgerPartition(program, partner, r.currency)
	var head ledgerHead
	old, err := r.tx.Get(ctx, kindLedgerHead, id)
	revision := int64(1)
	if err == nil {
		if err := old.Decode(&head); err != nil {
			return 0, err
		}
		if old.Revision == math.MaxInt64 {
			return 0, partnerearnings.ErrInvalid
		}
		revision = old.Revision + 1
	} else if !singleCauseIs(err, recordstore.ErrNotFound) {
		return 0, earningsError(err)
	}
	if head.Sequence == math.MaxInt64 {
		return 0, partnerearnings.ErrInvalid
	}
	head.Sequence++
	row, err := recordstore.NewRecord(kindLedgerHead, id, partition, revision, head)
	if err != nil {
		return 0, err
	}
	if revision == 1 {
		err = r.tx.Insert(ctx, row)
	} else {
		err = r.tx.Replace(ctx, row, revision-1)
	}
	return head.Sequence, earningsError(err)
}
func (r *EarningsRepository) GetClaim(ctx context.Context, program, id string) (partnerearnings.Claim, error) {
	if program != r.program {
		return partnerearnings.Claim{}, partnerearnings.ErrInvalid
	}
	var c partnerearnings.Claim
	err := r.read(ctx, func(tx recordstore.Tx) error {
		row, err := tx.Get(ctx, kindClaim, id)
		if err != nil {
			return err
		}
		if err := row.Decode(&c); err != nil {
			return err
		}
		if c.ID != row.ID || c.ProgramID != program || c.Currency != r.currency || c.Revision != row.Revision || row.Partition != ledgerPartition(program, c.PartnerID, c.Currency) || (r.tx != nil && c.PartnerID != r.boundPartner) {
			return partnerearnings.ErrUnavailable
		}
		return nil
	})
	return c, err
}
func (r *EarningsRepository) claimRevision(ctx context.Context, c partnerearnings.Claim) error {
	row, err := recordstore.NewRecord(kindClaimRevision, identity(c.ID, fmt.Sprint(c.Revision)), ledgerPartition(c.ProgramID, c.PartnerID, c.Currency), 1, c)
	if err != nil {
		return err
	}
	return r.tx.Insert(ctx, row)
}
func (r *EarningsRepository) InsertClaim(ctx context.Context, c partnerearnings.Claim) error {
	if r.tx == nil || c.ProgramID != r.program || c.Currency != r.currency || c.Revision != 1 || c.PartnerID != r.boundPartner {
		return partnerearnings.ErrInvalid
	}
	row, err := recordstore.NewRecord(kindClaim, c.ID, ledgerPartition(c.ProgramID, c.PartnerID, c.Currency), c.Revision, c)
	if err != nil {
		return err
	}
	row.State = c.State
	if err := r.tx.Insert(ctx, row); err != nil {
		return earningsError(err)
	}
	return earningsError(r.claimRevision(ctx, c))
}
func (r *EarningsRepository) ReplaceClaim(ctx context.Context, c partnerearnings.Claim, expected int64) (partnerearnings.Claim, error) {
	if r.tx == nil || c.ProgramID != r.program || c.Currency != r.currency || c.Revision != expected+1 || c.PartnerID != r.boundPartner {
		return partnerearnings.Claim{}, partnerearnings.ErrInvalid
	}
	row, err := recordstore.NewRecord(kindClaim, c.ID, ledgerPartition(c.ProgramID, c.PartnerID, c.Currency), c.Revision, c)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	row.State = c.State
	err = r.tx.Replace(ctx, row, expected)
	if errors.Is(err, recordstore.ErrConflict) {
		return partnerearnings.Claim{}, partnerearnings.ErrStaleWrite
	}
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if err := r.claimRevision(ctx, c); err != nil {
		return partnerearnings.Claim{}, earningsError(err)
	}
	return c, nil
}
func (r *EarningsRepository) ListClaims(ctx context.Context, program, partner string, states []string, limit int, after string) ([]partnerearnings.Claim, error) {
	if program != r.program || limit < 0 || (r.tx != nil && partner != r.boundPartner) {
		return nil, partnerearnings.ErrInvalid
	}
	out := []partnerearnings.Claim{}
	err := r.read(ctx, func(tx recordstore.Tx) error {
		out = nil
		query := recordstore.Query{Kind: kindClaim}
		if partner != "" {
			query.Partition = ledgerPartition(program, partner, r.currency)
		}
		rows, err := recordstore.FindAll(ctx, tx, query)
		if err != nil {
			return err
		}
		stateSet := map[string]bool{}
		for _, s := range states {
			stateSet[s] = true
		}
		for _, row := range rows {
			if row.ID <= after {
				continue
			}
			var c partnerearnings.Claim
			if err := row.Decode(&c); err != nil {
				return err
			}
			if c.ProgramID != program || c.Currency != r.currency {
				continue
			}
			if c.ID != row.ID || c.Revision != row.Revision || c.State != row.State || row.Partition != ledgerPartition(program, c.PartnerID, c.Currency) {
				return partnerearnings.ErrUnavailable
			}
			if len(stateSet) > 0 && !stateSet[c.State] {
				continue
			}
			out = append(out, c)
			if limit > 0 && len(out) == limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, err
}
func receiptID(k partnerearnings.ReceiptKey) string {
	return identity(k.ProgramID, k.PartnerID, k.ActorID, k.UseCase, k.Currency, k.Key)
}
func (r *EarningsRepository) GetReceipt(ctx context.Context, k partnerearnings.ReceiptKey) (partnerearnings.Receipt, error) {
	if k.ProgramID != r.program || k.Currency != r.currency || (r.tx != nil && k.PartnerID != r.boundPartner) {
		return partnerearnings.Receipt{}, partnerearnings.ErrInvalid
	}
	var out partnerearnings.Receipt
	err := r.read(ctx, func(tx recordstore.Tx) error {
		row, err := tx.Get(ctx, kindReceipt, receiptID(k))
		if err != nil {
			return err
		}
		if err := row.Decode(&out); err != nil {
			return err
		}
		if row.Kind != kindReceipt || row.ID != receiptID(k) || row.Revision != 1 || row.Sequence != 0 || row.State != "" || row.ExpiresAt != nil || out.ProgramID != k.ProgramID || out.PartnerID != k.PartnerID || out.Currency != k.Currency || out.ActorID != k.ActorID || out.UseCase != k.UseCase || out.Key != k.Key || row.Partition != ledgerPartition(k.ProgramID, k.PartnerID, k.Currency) {
			return partnerearnings.ErrUnavailable
		}
		return nil
	})
	return out, err
}
func (r *EarningsRepository) PutReceipt(ctx context.Context, v partnerearnings.Receipt) error {
	if r.tx == nil || v.ProgramID != r.program || v.Currency != r.currency || v.PartnerID != r.boundPartner {
		return partnerearnings.ErrInvalid
	}
	key := partnerearnings.ReceiptKey{ProgramID: v.ProgramID, PartnerID: v.PartnerID, ActorID: v.ActorID, UseCase: v.UseCase, Currency: v.Currency, Key: v.Key}
	row, err := recordstore.NewRecord(kindReceipt, receiptID(key), ledgerPartition(v.ProgramID, v.PartnerID, v.Currency), 1, v)
	if err != nil {
		return err
	}
	err = r.tx.Insert(ctx, row)
	if errors.Is(err, recordstore.ErrConflict) {
		return partnerearnings.ErrAlreadyExists
	}
	return earningsError(err)
}

var _ partnerearnings.Repository = (*EarningsRepository)(nil)
