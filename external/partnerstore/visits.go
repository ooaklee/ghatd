package partnerstore

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const kindVisitReceipt = "partner_referral_visit_receipt"
const kindVisitDay = "partner_referral_visit_day"

// WithVisitTransaction serializes the first measurement/receipt and every
// duplicate observation for one link. The callback receives only a bound
// adapter; nested calls reuse the same transactional storage capability.
func (r *ReferralRepository) WithVisitTransaction(ctx context.Context, link string, fn func(referral.Repository) error) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if link == "" || fn == nil {
		return referral.ErrInvalid
	}
	if _, nested := r.store.(partitionStore); nested {
		return referral.ErrInvalid
	}
	partition := clicksPartition(link)
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		return fn(&ReferralRepository{store: partitionStore{tx: tx, partition: partition}})
	}))
}

func (r *ReferralRepository) GetClick(ctx context.Context, link, id string) (referral.Click, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.Click{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.Click{}, referral.ErrUnavailable
	}
	if link == "" || id == "" {
		return referral.Click{}, referral.ErrInvalid
	}
	var out referral.Click
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = referral.Click{}
		v, row, err := loadRecord[referral.Click](ctx, tx, kindClick, id, clicksPartition(link))
		if err != nil {
			return err
		}
		if v.ID != row.ID || v.LinkID != link || row.Revision != 1 || row.State != visitDayKey(v.OccurredAt) || row.ExpiresAt == nil || row.ExpiresAt.Before(v.OccurredAt.Add(24*time.Hour)) || row.ExpiresAt.After(v.OccurredAt.Add(365*24*time.Hour+time.Millisecond)) {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	if err != nil {
		return referral.Click{}, referralError(err)
	}
	return out, nil
}

func (r *ReferralRepository) GetVisitReceipt(ctx context.Context, link, digest string) (referral.VisitReceipt, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.VisitReceipt{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.VisitReceipt{}, referral.ErrUnavailable
	}
	if link == "" || digest == "" {
		return referral.VisitReceipt{}, referral.ErrInvalid
	}
	var out referral.VisitReceipt
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = referral.VisitReceipt{}
		v, row, err := loadRecord[referral.VisitReceipt](ctx, tx, kindVisitReceipt, identity(referral.ProgramID, link, digest), clicksPartition(link))
		if err != nil {
			return err
		}
		if v.LinkID != link || v.Digest != digest || v.MeasuredClickID == "" || v.ExpiresAt.IsZero() || row.Revision != 1 || row.ExpiresAt == nil || row.ExpiresAt.Before(v.ExpiresAt) || row.ExpiresAt.Sub(v.ExpiresAt) >= time.Millisecond {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	if err != nil {
		return referral.VisitReceipt{}, referralError(err)
	}
	return out, nil
}

func (r *ReferralRepository) InsertVisitReceipt(ctx context.Context, v referral.VisitReceipt) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if v.LinkID == "" || v.Digest == "" || v.MeasuredClickID == "" || v.ExpiresAt.IsZero() {
		return referral.ErrInvalid
	}
	return referralError(r.store.Transact(ctx, clicksPartition(v.LinkID), func(tx recordstore.Tx) error {
		row, err := recordstore.NewRecord(kindVisitReceipt, identity(referral.ProgramID, v.LinkID, v.Digest), clicksPartition(v.LinkID), 1, v)
		if err != nil {
			return err
		}
		row, err = row.WithExpiration(v.ExpiresAt)
		if err != nil {
			return err
		}
		return tx.Insert(ctx, row)
	}))
}

func visitDayKey(at time.Time) string { return at.UTC().Format("2006-01-02") }
func validVisitDay(at time.Time) bool {
	return !at.IsZero() && at.Equal(at.UTC().Truncate(24*time.Hour))
}

// GetVisitDay reads a persistent anonymous bucket within the same link guard
// used for observations and receipt deduplication.
func (r *ReferralRepository) GetVisitDay(ctx context.Context, link string, day time.Time) (referral.VisitDay, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.VisitDay{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.VisitDay{}, referral.ErrUnavailable
	}
	if link == "" || !validVisitDay(day) {
		return referral.VisitDay{}, referral.ErrInvalid
	}
	var out referral.VisitDay
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = referral.VisitDay{}
		v, row, err := loadRecord[referral.VisitDay](ctx, tx, kindVisitDay, identity(referral.ProgramID, link, visitDayKey(day)), clicksPartition(link))
		if err != nil {
			return err
		}
		if v.Validate() != nil || v.LinkID != link || !v.Day.Equal(day) || v.Revision != row.Revision || row.State != visitDayKey(day) || row.ExpiresAt != nil {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	if err != nil {
		return referral.VisitDay{}, referralError(err)
	}
	return out, nil
}

// PutVisitDay uses conditional revision receipts. The service calls it in the
// observation transaction; failed writes roll back raw data and receipt too.
func (r *ReferralRepository) PutVisitDay(ctx context.Context, v referral.VisitDay, expected int64) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if v.Validate() != nil || expected < 0 || v.Revision-1 != expected {
		return referral.ErrInvalid
	}
	return referralError(r.store.Transact(ctx, clicksPartition(v.LinkID), func(tx recordstore.Tx) error {
		row, err := recordstore.NewRecord(kindVisitDay, identity(referral.ProgramID, v.LinkID, visitDayKey(v.Day)), clicksPartition(v.LinkID), v.Revision, v)
		if err != nil {
			return err
		}
		row.State = visitDayKey(v.Day)
		if expected == 0 {
			return tx.Insert(ctx, row)
		}
		return tx.Replace(ctx, row, expected)
	}))
}
