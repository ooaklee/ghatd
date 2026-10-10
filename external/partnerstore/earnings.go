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

// identity derives a non-enumerable SHA-256 hex identity from the given parts'
// canonical JSON.
func identity(parts ...string) string {
	body, _ := json.Marshal(parts)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// ledgerPartition derives the per-partner ledger partition key from program,
// partner and currency.
func ledgerPartition(program, partner, currency string) string {
	return "partner-ledger:" + identity(program, partner, currency)
}

// recordReference is the internal stored pointer from a source-identity record
// to its entry ID.
type recordReference struct {
	ID string `json:"id"`
}

// ledgerHead is the internal per-partition sequence head record.
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

// NewEarningsRepository validates the store and configuration and pins one
// program/currency; it opens nothing eagerly.
func NewEarningsRepository(store recordstore.Store, config partnerearnings.Config) (*EarningsRepository, error) {
	if nilStoreDependency(store) {
		return nil, partnerearnings.ErrUnavailable
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &EarningsRepository{store: store, program: config.ProgramID, currency: config.Currency}, nil
}

// earningsError translates recordstore not-found, conflict, uncertain and
// unavailable failures into the earnings domain, wrapping uncertain/unavailable
// with the original cause; other errors pass through unchanged.
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

// read executes fn on the bound transaction or a fresh store read, translating
// failures through earningsError and rejecting nil/expired contexts.
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

// WithTransaction runs fn on a repository bound to the same transaction and the
// requested partner within the pinned program/currency; nesting or a mismatched
// partition fails with ErrInvalid.
func (r *EarningsRepository) WithTransaction(ctx context.Context, program, partner, currency string, fn func(partnerearnings.Repository) error) error {
	if r.tx != nil || program != r.program || currency != r.currency || partner == "" || fn == nil {
		return partnerearnings.ErrInvalid
	}
	return earningsError(r.store.Transact(ctx, ledgerPartition(program, partner, currency), func(tx recordstore.Tx) error {
		return fn(&EarningsRepository{tx: tx, program: r.program, currency: r.currency, boundPartner: partner})
	}))
}

// ListEntries returns the partner's ledger entries sorted by sequence,
// validating each decoded row's identity against its storage key and rejecting
// a partner outside the bound transaction with ErrInvalid.
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

// AppendEntry inserts one sequenced entry and its source-identity record inside
// the bound transaction only; out-of-transaction use or a mismatched
// partner/currency/sequence fails with ErrInvalid.
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

// EntryBySource scans the partner's entries for one matching kind and source
// event ID, returning ErrNotFound when no entry exists.
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

// NextSequence allocates the next ledger sequence number by CAS-advancing the
// ledger head revision inside the bound transaction. It rejects overflow of the
// sequence or revision with ErrInvalid.
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

// GetClaim reads one claim and re-verifies its program, currency, revision,
// partition and (on a bound repository) owning partner; mismatches yield
// ErrUnavailable rather than the decoded value.
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

// claimRevision writes the claim's immutable per-revision copy into the same
// transaction as the head write. It requires an active repository transaction.
func (r *EarningsRepository) claimRevision(ctx context.Context, c partnerearnings.Claim) error {
	row, err := recordstore.NewRecord(kindClaimRevision, identity(c.ID, fmt.Sprint(c.Revision)), ledgerPartition(c.ProgramID, c.PartnerID, c.Currency), 1, c)
	if err != nil {
		return err
	}
	return r.tx.Insert(ctx, row)
}

// InsertClaim stores a first-revision claim and its immutable revision copy in
// the bound transaction, enforcing the repository's program, currency and
// partner pins. Conflict from an existing claim surfaces as an earnings error.
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

// ReplaceClaim advances a claim from expected revision via compare-and-swap and
// records the immutable revision copy in the same transaction. A storage
// conflict maps to ErrStaleWrite.
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

// ListClaims decodes every claim in the program/partition, cross-checking row
// identity, revision and partition, and returns those after the cursor matching
// the optional state filter up to limit. An inconsistent row fails the whole
// list with ErrUnavailable.
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

// receiptID derives the storage identity of an idempotency receipt from its
// full program/partner/actor/use-case/currency/key tuple.
func receiptID(k partnerearnings.ReceiptKey) string {
	return identity(k.ProgramID, k.PartnerID, k.ActorID, k.UseCase, k.Currency, k.Key)
}

// GetReceipt reads an idempotency receipt by key and verifies envelope
// invariants (revision 1, no state/expiry) and full key agreement; any mismatch
// returns ErrUnavailable instead of the decoded value.
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

// PutReceipt inserts a one-time idempotency receipt inside the bound
// transaction; an existing receipt maps to ErrAlreadyExists so retries cannot
// re-apply the operation.
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
