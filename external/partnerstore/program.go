package partnerstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindPartner         = "partner_program_participant"
	kindPartnerCustomer = "partner_program_customer"
	kindPartnerRevision = "partner_program_revision"
	kindPolicy          = "partner_program_policy"
	kindPolicyHead      = "partner_program_policy_head"
	kindDestination     = "partner_program_destination"
	kindDestinationHead = "partner_program_destination_head"
)

// ProgramRepository persists one v1 program through encrypted records. Unique
// enrollment, status history, policy scope heads and destination revisions are
// committed with their references in guarded transactions. It does not resolve
// commercial precedence or choose eligibility.
type ProgramRepository struct{ store recordstore.Store }

// NewProgramRepository returns a program repository over the shared
// transactional store, or ErrUnavailable when the store dependency is nil.
func NewProgramRepository(store recordstore.Store) (*ProgramRepository, error) {
	if nilStoreDependency(store) {
		return nil, partnerprogram.ErrUnavailable
	}
	return &ProgramRepository{store}, nil
}

// programPartition derives the single program-wide partition used for partner
// and policy records.
func programPartition() string { return "partner-program:" + identity(partnerprogram.ProgramID) }

// participantPartition derives the per-customer partition guarding partner
// history and destination heads.
func participantPartition(customer string) string {
	return "partner-program-customer:" + identity(partnerprogram.ProgramID, customer)
}

// singleCauseIs reports whether target appears in err's Unwrap chain, bounded
// to 32 levels, without matching wrapped multi-cause errors.
func singleCauseIs(err, target error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// programError maps storage sentinel causes to program domain errors, wrapping
// uncertain/unavailable/invalid causes so callers can distinguish outcomes;
// other errors pass through unchanged.
func programError(err error) error {
	switch {
	case singleCauseIs(err, recordstore.ErrNotFound):
		return partnerprogram.ErrNotFound
	case singleCauseIs(err, recordstore.ErrConflict):
		return partnerprogram.ErrStaleWrite
	case singleCauseIs(err, recordstore.ErrInvalid):
		return fmt.Errorf("%w: %w", partnerprogram.ErrInvalid, err)
	case errors.Is(err, recordstore.ErrUncertain):
		return fmt.Errorf("%w: %w", partnerprogram.ErrUncertain, err)
	case errors.Is(err, recordstore.ErrUnavailable):
		return fmt.Errorf("%w: %w", partnerprogram.ErrUnavailable, err)
	default:
		return err
	}
}

// validStoreContext rejects a nil context with ErrInvalid and otherwise returns
// the context's current cancellation error.
func validStoreContext(ctx context.Context) error {
	if ctx == nil {
		return recordstore.ErrInvalid
	}
	return ctx.Err()
}

// loadRecord fetches a record by kind and ID, verifies its partition, and
// decodes it into T. A mismatched envelope returns ErrUnavailable without
// decoding.
func loadRecord[T any](ctx context.Context, tx recordstore.Tx, kind, id, partition string) (T, recordstore.Record, error) {
	var value T
	row, err := tx.Get(ctx, kind, id)
	if err != nil {
		return value, row, err
	}
	if row.Kind != kind || row.ID != id || row.Partition != partition {
		return value, row, recordstore.ErrUnavailable
	}
	err = row.Decode(&value)
	return value, row, err
}

// insertRecord builds a first-revision record for value and inserts it via the
// transaction.
func insertRecord(ctx context.Context, tx recordstore.Tx, kind, id, partition string, revision int64, value any) error {
	row, err := recordstore.NewRecord(kind, id, partition, revision, value)
	if err != nil {
		return err
	}
	return tx.Insert(ctx, row)
}

// replaceRecord rewrites a record at the given revision using a compare-and-
// swap on revision-1.
func replaceRecord(ctx context.Context, tx recordstore.Tx, kind, id, partition string, revision int64, value any) error {
	row, err := recordstore.NewRecord(kind, id, partition, revision, value)
	if err != nil {
		return err
	}
	return tx.Replace(ctx, row, revision-1)
}

// writeReference inserts a reference head at revision 1 when expected is zero,
// otherwise CAS-replaces it at expected+1. Revision overflow and egative
// expectations are rejected as ErrInvalid.
func writeReference(ctx context.Context, tx recordstore.Tx, kind, id, partition, target string, expected int64) error {
	if expected == math.MaxInt64 || expected < 0 {
		return recordstore.ErrInvalid
	}
	if expected == 0 {
		return insertRecord(ctx, tx, kind, id, partition, 1, recordReference{target})
	}
	return replaceRecord(ctx, tx, kind, id, partition, expected+1, recordReference{target})
}

// GetPartnerByID loads a partner and verifies its program and decoded revision
// agree with the stored row before returning it.
func (r *ProgramRepository) GetPartnerByID(ctx context.Context, id string) (partnerprogram.Partner, error) {
	if err := validStoreContext(ctx); err != nil {
		return partnerprogram.Partner{}, programError(err)
	}
	var out partnerprogram.Partner
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		p, row, err := loadRecord[partnerprogram.Partner](ctx, tx, kindPartner, id, programPartition())
		if err != nil {
			return err
		}
		if p.ID != id || p.ProgramID != partnerprogram.ProgramID || p.Revision != row.Revision {
			return recordstore.ErrUnavailable
		}
		out = p
		return nil
	})
	return out, programError(err)
}

// GetPartnerByCustomer resolves the customer's reference head to its partner in
// one read transaction and cross-checks customer, program and revision. Missing
// references surface as ErrNotFound.
func (r *ProgramRepository) GetPartnerByCustomer(ctx context.Context, program, customer string) (partnerprogram.Partner, error) {
	if err := validStoreContext(ctx); err != nil {
		return partnerprogram.Partner{}, programError(err)
	}
	if program != partnerprogram.ProgramID || customer == "" {
		return partnerprogram.Partner{}, partnerprogram.ErrInvalid
	}
	var out partnerprogram.Partner
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		ref, _, err := loadRecord[recordReference](ctx, tx, kindPartnerCustomer, identity(program, customer), participantPartition(customer))
		if err != nil {
			return err
		}
		p, row, err := loadRecord[partnerprogram.Partner](ctx, tx, kindPartner, ref.ID, programPartition())
		if err != nil {
			return err
		}
		if p.ID != ref.ID || p.ProgramID != program || p.CustomerID != customer || p.Revision != row.Revision {
			return recordstore.ErrUnavailable
		}
		out = p
		return nil
	})
	return out, programError(err)
}

// InsertPartner enrolls a first-revision partner plus its revision history and
// customer reference in one transaction guarded by the customer partition. An
// existing enrollment returns ErrAlreadyEnrolled and a racing insert maps to
// ErrAlreadyExists.
func (r *ProgramRepository) InsertPartner(ctx context.Context, p partnerprogram.Partner) error {
	if p.ProgramID != partnerprogram.ProgramID || p.ID == "" || p.CustomerID == "" || p.Revision != 1 {
		return partnerprogram.ErrInvalid
	}
	partition := participantPartition(p.CustomerID)
	err := r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		_, err := tx.Get(ctx, kindPartnerCustomer, identity(p.ProgramID, p.CustomerID))
		if err == nil {
			return partnerprogram.ErrAlreadyEnrolled
		}
		if !singleCauseIs(err, recordstore.ErrNotFound) {
			return err
		}
		if err := insertRecord(ctx, tx, kindPartner, p.ID, programPartition(), 1, p); err != nil {
			return err
		}
		if err := insertRecord(ctx, tx, kindPartnerRevision, identity(p.ID, "1"), partition, 1, p); err != nil {
			return err
		}
		return insertRecord(ctx, tx, kindPartnerCustomer, identity(p.ProgramID, p.CustomerID), partition, 1, recordReference{p.ID})
	})
	if singleCauseIs(err, recordstore.ErrConflict) {
		return partnerprogram.ErrAlreadyExists
	}
	return programError(err)
}

// ReplacePartner advances a partner from expected revision via CAS, keeping
// customer, accepted terms version and enrollment time immutable; violations
// return ErrInvalid. The new immutable revision copy is written in the same
// transaction.
func (r *ProgramRepository) ReplacePartner(ctx context.Context, p partnerprogram.Partner, expected int64) (partnerprogram.Partner, error) {
	if p.ProgramID != partnerprogram.ProgramID || p.CustomerID == "" || expected < 1 || expected == math.MaxInt64 || p.Revision != expected+1 {
		return partnerprogram.Partner{}, partnerprogram.ErrInvalid
	}
	partition := participantPartition(p.CustomerID)
	err := r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		old, row, err := loadRecord[partnerprogram.Partner](ctx, tx, kindPartner, p.ID, programPartition())
		if err != nil {
			return err
		}
		if row.Revision != expected {
			return partnerprogram.ErrStaleWrite
		}
		if old.CustomerID != p.CustomerID || old.AcceptedTermsVersion != p.AcceptedTermsVersion || !old.EnrolledAt.Equal(p.EnrolledAt) {
			return partnerprogram.ErrInvalid
		}
		if err := replaceRecord(ctx, tx, kindPartner, p.ID, programPartition(), p.Revision, p); err != nil {
			return err
		}
		return insertRecord(ctx, tx, kindPartnerRevision, identity(p.ID, fmt.Sprint(p.Revision)), partition, 1, p)
	})
	if err != nil {
		return partnerprogram.Partner{}, programError(err)
	}
	return p, nil
}

// ListPolicyVersions reads all policy records in the program partition, failing
// on any row whose decoded identity disagrees, and returns them sorted by
// effective time then ID.
func (r *ProgramRepository) ListPolicyVersions(ctx context.Context, program string) ([]partnerprogram.PolicyVersion, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, programError(err)
	}
	if program != partnerprogram.ProgramID {
		return nil, partnerprogram.ErrInvalid
	}
	var out []partnerprogram.PolicyVersion
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindPolicy, Partition: programPartition()})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var v partnerprogram.PolicyVersion
			if err := row.Decode(&v); err != nil {
				return err
			}
			if v.ID != row.ID || v.ProgramID != program {
				return recordstore.ErrUnavailable
			}
			out = append(out, v)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].EffectiveFrom.Equal(out[j].EffectiveFrom) {
			return out[i].ID < out[j].ID
		}
		return out[i].EffectiveFrom.Before(out[j].EffectiveFrom)
	})
	return out, programError(err)
}

// InsertPolicyVersion appends a policy version and CAS-advances the scoped
// policy head in one transaction; a head revision other than expected returns
// ErrStaleWrite.
func (r *ProgramRepository) InsertPolicyVersion(ctx context.Context, v partnerprogram.PolicyVersion, expected int64) error {
	if v.ProgramID != partnerprogram.ProgramID || expected < 0 || expected == math.MaxInt64 || v.Revision != expected+1 {
		return partnerprogram.ErrInvalid
	}
	scope := identity(v.ProgramID, v.Scope, v.GroupID, v.PartnerCustomer)
	return programError(r.store.Transact(ctx, "partner-policy:"+scope, func(tx recordstore.Tx) error {
		_, head, err := loadRecord[recordReference](ctx, tx, kindPolicyHead, scope, programPartition())
		if singleCauseIs(err, recordstore.ErrNotFound) {
			if expected != 0 {
				return partnerprogram.ErrStaleWrite
			}
		} else if err != nil {
			return err
		} else if head.Revision != expected {
			return partnerprogram.ErrStaleWrite
		}
		if err := insertRecord(ctx, tx, kindPolicy, v.ID, programPartition(), 1, v); err != nil {
			return err
		}
		return writeReference(ctx, tx, kindPolicyHead, scope, programPartition(), v.ID, expected)
	}))
}

// GetDestination follows the customer's destination head in one read
// transaction and verifies the referenced destination's version matches the
// head revision.
func (r *ProgramRepository) GetDestination(ctx context.Context, customer string) (partnerprogram.Destination, error) {
	if err := validStoreContext(ctx); err != nil {
		return partnerprogram.Destination{}, programError(err)
	}
	var out partnerprogram.Destination
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		ref, head, err := loadRecord[recordReference](ctx, tx, kindDestinationHead, identity(partnerprogram.ProgramID, customer), participantPartition(customer))
		if err != nil {
			return err
		}
		d, _, err := loadRecord[partnerprogram.Destination](ctx, tx, kindDestination, ref.ID, participantPartition(customer))
		if err != nil {
			return err
		}
		if d.ID != ref.ID || d.CustomerID != customer || d.Version != head.Revision {
			return recordstore.ErrUnavailable
		}
		out = d
		return nil
	})
	return out, programError(err)
}

// InsertDestination stores a new destination version and CAS-advances the
// customer's destination head in the same transaction. A missing head with
// nonzero expectation, or head revision mismatch, returns ErrStaleWrite.
func (r *ProgramRepository) InsertDestination(ctx context.Context, d partnerprogram.Destination, expected int64) error {
	if d.CustomerID == "" || expected < 0 || expected == math.MaxInt64 || d.Version != expected+1 {
		return partnerprogram.ErrInvalid
	}
	partition := participantPartition(d.CustomerID)
	return programError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		id := identity(partnerprogram.ProgramID, d.CustomerID)
		_, head, err := loadRecord[recordReference](ctx, tx, kindDestinationHead, id, partition)
		if singleCauseIs(err, recordstore.ErrNotFound) {
			if expected != 0 {
				return partnerprogram.ErrStaleWrite
			}
		} else if err != nil {
			return err
		} else if head.Revision != expected {
			return partnerprogram.ErrStaleWrite
		}
		if err := insertRecord(ctx, tx, kindDestination, d.ID, partition, 1, d); err != nil {
			return err
		}
		return writeReference(ctx, tx, kindDestinationHead, id, partition, d.ID, expected)
	}))
}

var _ partnerprogram.Repository = (*ProgramRepository)(nil)
