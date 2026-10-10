package partnerstore

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	kindLink             = "partner_referral_link"
	kindLinkRevision     = "partner_referral_link_revision"
	kindLinkCode         = "partner_referral_code"
	kindLinkHead         = "partner_referral_link_head"
	kindReferralHead     = "partner_referral_head"
	kindReferralRevision = "partner_referral_revision"
	kindPaymentBinding   = "partner_referral_payment_binding"
	kindClick            = "partner_referral_click"
)

// ReferralRepository persists referral links, corrections, payment attributions
// and click evidence over the shared transactional store. It stores private
// correction receipts that transport JSON omits.
type ReferralRepository struct{ store recordstore.Store }

// storedReferral retains private correction receipts despite the domain
// Referral's transport JSON omission. All storage reads decode this envelope.
type storedReferral struct {
	referral.Referral
	Receipt *referral.CorrectionReceipt `json:"correction_receipt,omitempty"`
}

// referralRecord copies a domain Referral into the stored envelope, preserving
// the correction receipt alongside the transport projection.
func referralRecord(v referral.Referral) storedReferral {
	return storedReferral{Referral: v, Receipt: v.Correction}
}

// value restores the domain Referral from the stored envelope, reinstating the
// private correction receipt into the Correction field.
func (v storedReferral) value() referral.Referral {
	v.Referral.Correction = v.Receipt
	return v.Referral
}

// NewReferralRepository binds referral-only schemas to the shared transactional
// storage capability. Codes retain case and remain reserved after retirement.
func NewReferralRepository(store recordstore.Store) (*ReferralRepository, error) {
	if nilStoreDependency(store) {
		return nil, referral.ErrUnavailable
	}
	return &ReferralRepository{store}, nil
}

// referralPartition derives the per-customer partition holding a customer's
// referral revisions and payment bindings.
func referralPartition(customer string) string {
	return "partner-referral:" + identity(referral.ProgramID, customer)
}

// linksPartition derives the per-partner partition holding that partner's
// referral links and their revision history.
func linksPartition(partner string) string {
	return "partner-links:" + identity(referral.ProgramID, partner)
}

// clicksPartition derives the per-link partition holding click evidence, visit
// receipts and visit-day buckets for one link.
func clicksPartition(link string) string {
	return "partner-clicks:" + identity(referral.ProgramID, link)
}

// referralProgramPartition derives the program-wide partition holding code
// reservations, referral heads and payment bindings.
func referralProgramPartition() string {
	return "partner-referral-program:" + identity(referral.ProgramID)
}

// referralError maps storage sentinel causes to referral domain errors,
// wrapping uncertain, unavailable and invalid causes to preserve
// classification; other errors pass through.
func referralError(err error) error {
	switch {
	case singleCauseIs(err, recordstore.ErrNotFound):
		return referral.ErrNotFound
	case singleCauseIs(err, recordstore.ErrConflict):
		return referral.ErrStaleWrite
	case singleCauseIs(err, recordstore.ErrInvalid):
		return fmt.Errorf("%w: %w", referral.ErrInvalid, err)
	case errors.Is(err, recordstore.ErrUncertain):
		return fmt.Errorf("%w: %w", referral.ErrUncertain, err)
	case errors.Is(err, recordstore.ErrUnavailable):
		return fmt.Errorf("%w: %w", referral.ErrUnavailable, err)
	default:
		return err
	}
}

// GetLinkByCode resolves a case-sensitive code reservation to its link in one
// read transaction, verifying code, program and partner partition agree. Codes
// longer than 128 bytes are rejected as ErrInvalid.
func (r *ReferralRepository) GetLinkByCode(ctx context.Context, code string) (referral.Link, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.Link{}, referralError(err)
	}
	if code == "" || len(code) > 128 {
		return referral.Link{}, referral.ErrInvalid
	}
	var out referral.Link
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		ref, _, err := loadRecord[recordReference](ctx, tx, kindLinkCode, identity(referral.ProgramID, code), referralProgramPartition())
		if err != nil {
			return err
		}
		row, err := tx.Get(ctx, kindLink, ref.ID)
		if err != nil {
			return err
		}
		var link referral.Link
		if err := row.Decode(&link); err != nil {
			return err
		}
		if link.ID != row.ID || link.ProgramID != referral.ProgramID || link.Code != code || row.Partition != linksPartition(link.PartnerID) {
			return recordstore.ErrUnavailable
		}
		out = link
		return nil
	})
	return out, referralError(err)
}

// InsertLink reserves the code, stores the link, its first revision and the
// partner's active-link head in one transaction. An active head, taken code or
// racing insert returns ErrAlreadyExists or ErrCodeTaken.
func (r *ReferralRepository) InsertLink(ctx context.Context, l referral.Link) error {
	if l.ProgramID != referral.ProgramID || l.ID == "" || l.Code == "" || len(l.Code) > 128 || l.PartnerID == "" || l.CreatedAt.IsZero() || l.RetiredAt != nil {
		return referral.ErrInvalid
	}
	partition := linksPartition(l.PartnerID)
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		headID := identity(l.ProgramID, l.PartnerID)
		active, head, err := loadRecord[recordReference](ctx, tx, kindLinkHead, headID, partition)
		if err == nil && active.ID != "" {
			return referral.ErrAlreadyExists
		}
		if err != nil && !singleCauseIs(err, recordstore.ErrNotFound) {
			return err
		}
		expected := head.Revision
		_, err = tx.Get(ctx, kindLinkCode, identity(l.ProgramID, l.Code))
		if err == nil {
			return referral.ErrCodeTaken
		}
		if !singleCauseIs(err, recordstore.ErrNotFound) {
			return err
		}
		if err := insertRecord(ctx, tx, kindLinkCode, identity(l.ProgramID, l.Code), referralProgramPartition(), 1, recordReference{l.ID}); err != nil {
			if singleCauseIs(err, recordstore.ErrConflict) {
				return referral.ErrCodeTaken
			}
			return err
		}
		if err := insertRecord(ctx, tx, kindLink, l.ID, partition, 1, l); err != nil {
			return err
		}
		if err := insertRecord(ctx, tx, kindLinkRevision, identity(l.ID, "1"), partition, 1, l); err != nil {
			return err
		}
		return writeReference(ctx, tx, kindLinkHead, headID, partition, l.ID, expected)
	}))
}

// RetireLink marks the currently active link retired with reason, actor and UTC
// timestamp, appending a revision and clearing the head in one transaction.
// Retrying the same actor/reason is idempotent; other concurrent retires return
// ErrStaleWrite.
func (r *ReferralRepository) RetireLink(ctx context.Context, id string, at time.Time, reason, actor string) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if at.IsZero() || actor == "" || reason == "" || len(reason) > 1000 {
		return referral.ErrInvalid
	}
	var old referral.Link
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		row, err := tx.Get(ctx, kindLink, id)
		if err != nil {
			return err
		}
		if err := row.Decode(&old); err != nil {
			return err
		}
		if old.ID != id || old.ProgramID != referral.ProgramID || row.Partition != linksPartition(old.PartnerID) {
			return recordstore.ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return referralError(err)
	}
	partition := linksPartition(old.PartnerID)
	return referralError(r.store.Transact(ctx, partition, func(tx recordstore.Tx) error {
		link, row, err := loadRecord[referral.Link](ctx, tx, kindLink, id, partition)
		if err != nil {
			return err
		}
		if link.RetiredAt != nil {
			if link.RetireReason == reason && link.RetiredBy == actor {
				return nil
			}
			return referral.ErrStaleWrite
		}
		if row.Revision == math.MaxInt64 {
			return referral.ErrInvalid
		}
		headID := identity(referral.ProgramID, link.PartnerID)
		active, head, err := loadRecord[recordReference](ctx, tx, kindLinkHead, headID, partition)
		if err != nil {
			return err
		}
		if active.ID != id {
			return referral.ErrStaleWrite
		}
		utc := at.UTC()
		link.RetiredAt = &utc
		link.RetireReason = reason
		link.RetiredBy = actor
		if err := replaceRecord(ctx, tx, kindLink, id, partition, row.Revision+1, link); err != nil {
			return err
		}
		if err := insertRecord(ctx, tx, kindLinkRevision, identity(id, fmt.Sprint(row.Revision+1)), partition, 1, link); err != nil {
			return err
		}
		return writeReference(ctx, tx, kindLinkHead, headID, partition, "", head.Revision)
	}))
}

// ListLinksByPartner reads every link in the partner's partition, verifying
// decoded identity, and returns them sorted by creation time then ID. Any
// inconsistent row fails the whole read.
func (r *ReferralRepository) ListLinksByPartner(ctx context.Context, program, partner string) ([]referral.Link, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, referralError(err)
	}
	if program != referral.ProgramID || partner == "" {
		return nil, referral.ErrInvalid
	}
	var out []referral.Link
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindLink, Partition: linksPartition(partner)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var link referral.Link
			if err := row.Decode(&link); err != nil {
				return err
			}
			if link.ID != row.ID || link.PartnerID != partner || link.ProgramID != program {
				return recordstore.ErrUnavailable
			}
			out = append(out, link)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, referralError(err)
}

// GetReferralByCustomer reads the customer's referral head, restores its
// private correction receipt and verifies program, customer, revision and state
// agreement before returning it.
func (r *ReferralRepository) GetReferralByCustomer(ctx context.Context, program, customer string) (referral.Referral, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.Referral{}, referralError(err)
	}
	if program != referral.ProgramID || customer == "" {
		return referral.Referral{}, referral.ErrInvalid
	}
	var out referral.Referral
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		stored, row, err := loadRecord[storedReferral](ctx, tx, kindReferralHead, identity(program, customer), referralProgramPartition())
		if err != nil {
			return err
		}
		v := stored.value()
		if v.ProgramID != program || v.ReferredCustomer != customer || v.Revision != row.Revision || row.State != v.PartnerID {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	return out, referralError(err)
}

// InsertReferral stores a correction revision, retains the partner relationship
// and CAS-advances the customer's referral head in one transaction. Expected-
// revision or correction-chain mismatch returns ErrStaleWrite.
func (r *ReferralRepository) InsertReferral(ctx context.Context, v referral.Referral, expected int64) error {
	if v.ProgramID != referral.ProgramID || v.ReferredCustomer == "" || v.PartnerID == "" || expected < 0 || expected == math.MaxInt64 || v.Revision != expected+1 {
		return referral.ErrInvalid
	}
	return referralError(r.store.Transact(ctx, referralPartition(v.ReferredCustomer), func(tx recordstore.Tx) error {
		id := identity(v.ProgramID, v.ReferredCustomer)
		stored, head, err := loadRecord[storedReferral](ctx, tx, kindReferralHead, id, referralProgramPartition())
		prior := stored.value()
		if singleCauseIs(err, recordstore.ErrNotFound) {
			if expected != 0 {
				return referral.ErrStaleWrite
			}
		} else if err != nil {
			return err
		} else if head.Revision != expected || v.CorrectionOf != prior.ID {
			return referral.ErrStaleWrite
		}
		if err := insertRecord(ctx, tx, kindReferralRevision, v.ID, referralPartition(v.ReferredCustomer), 1, referralRecord(v)); err != nil {
			return err
		}
		if err := retainReferralRelationship(ctx, tx, v); err != nil {
			return err
		}
		row, err := recordstore.NewRecord(kindReferralHead, id, referralProgramPartition(), v.Revision, referralRecord(v))
		if err != nil {
			return err
		}
		row.State = v.PartnerID
		if expected == 0 {
			return tx.Insert(ctx, row)
		}
		return tx.Replace(ctx, row, expected)
	}))
}

// ListReferralHistory reads every stored referral revision for a customer,
// restoring correction receipts, and returns them sorted by revision. An
// inconsistent row fails the whole list.
func (r *ReferralRepository) ListReferralHistory(ctx context.Context, program, customer string) ([]referral.Referral, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, referralError(err)
	}
	if program != referral.ProgramID || customer == "" {
		return nil, referral.ErrInvalid
	}
	var out []referral.Referral
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindReferralRevision, Partition: referralPartition(customer)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var stored storedReferral
			if err := row.Decode(&stored); err != nil {
				return err
			}
			v := stored.value()
			if v.ID != row.ID || v.ProgramID != program || v.ReferredCustomer != customer {
				return recordstore.ErrUnavailable
			}
			out = append(out, v)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Revision < out[j].Revision })
	return out, referralError(err)
}

// ListReferralsByPartner pages current referral heads by partner state with a
// validated limit of 1-100. The after cursor is itself verified to belong to
// the partner before it is used.
func (r *ReferralRepository) ListReferralsByPartner(ctx context.Context, program, partner string, limit int, after string) ([]referral.Referral, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, referralError(err)
	}
	if program != referral.ProgramID || partner == "" || limit < 1 || limit > 100 {
		return nil, referral.ErrInvalid
	}
	var out []referral.Referral
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		afterID := ""
		if after != "" {
			row, err := tx.Get(ctx, kindReferralRevision, after)
			if err != nil {
				return err
			}
			var stored storedReferral
			if err := row.Decode(&stored); err != nil {
				return err
			}
			v := stored.value()
			if v.ProgramID != program || v.PartnerID != partner || row.Partition != referralPartition(v.ReferredCustomer) {
				return referral.ErrInvalid
			}
			afterID = identity(program, v.ReferredCustomer)
		}
		rows, err := tx.Find(ctx, recordstore.Query{Kind: kindReferralHead, Partition: referralProgramPartition(), State: partner, Limit: limit, AfterID: afterID})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var stored storedReferral
			if err := row.Decode(&stored); err != nil {
				return err
			}
			v := stored.value()
			if v.PartnerID != partner || v.ProgramID != program || v.Revision != row.Revision || row.ID != identity(program, v.ReferredCustomer) {
				return recordstore.ErrUnavailable
			}
			out = append(out, v)
		}
		return nil
	})
	return out, referralError(err)
}

// GetPaymentAttribution loads the payment binding for a program/payment ID pair
// and verifies decoded agreement, including that the row state names the owning
// referred customer.
func (r *ReferralRepository) GetPaymentAttribution(ctx context.Context, program, payment string) (referral.PaymentAttribution, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.PaymentAttribution{}, referralError(err)
	}
	if program != referral.ProgramID || payment == "" {
		return referral.PaymentAttribution{}, referral.ErrInvalid
	}
	var out referral.PaymentAttribution
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		v, row, err := loadRecord[referral.PaymentAttribution](ctx, tx, kindPaymentBinding, identity(program, payment), referralProgramPartition())
		if err != nil {
			return err
		}
		if v.ProgramID != program || v.PaymentID != payment || row.State != v.ReferredCustomer {
			return recordstore.ErrUnavailable
		}
		out = v
		return nil
	})
	return out, referralError(err)
}

// InsertPaymentAttribution records the globally unique payment binding once
// inside the referred customer's partition; an existing binding returns
// ErrAlreadyExists.
func (r *ReferralRepository) InsertPaymentAttribution(ctx context.Context, v referral.PaymentAttribution) error {
	if v.ProgramID != referral.ProgramID || v.PaymentID == "" || v.ID == "" || v.ReferredCustomer == "" || v.PartnerID == "" || v.ReferralID == "" {
		return referral.ErrInvalid
	}
	err := r.store.Transact(ctx, referralPartition(v.ReferredCustomer), func(tx recordstore.Tx) error {
		row, err := recordstore.NewRecord(kindPaymentBinding, identity(v.ProgramID, v.PaymentID), referralProgramPartition(), 1, v)
		if err != nil {
			return err
		}
		row.State = v.ReferredCustomer
		return tx.Insert(ctx, row)
	})
	if singleCauseIs(err, recordstore.ErrConflict) {
		return referral.ErrAlreadyExists
	}
	return referralError(err)
}

// ListPaymentAttributionsByCustomer uses the indexed customer projection on
// globally unique payment records. It never scans another customer's payloads.
func (r *ReferralRepository) ListPaymentAttributionsByCustomer(ctx context.Context, program, customer string) ([]referral.PaymentAttribution, error) {
	if err := validStoreContext(ctx); err != nil {
		return nil, referralError(err)
	}
	if program != referral.ProgramID || customer == "" || len(customer) > 256 {
		return nil, referral.ErrInvalid
	}
	var out []referral.PaymentAttribution
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindPaymentBinding, Partition: referralProgramPartition(), State: customer})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var v referral.PaymentAttribution
			if err := row.Decode(&v); err != nil {
				return err
			}
			if v.ProgramID != program || v.ReferredCustomer != customer || row.State != customer || row.ID != identity(program, v.PaymentID) {
				return recordstore.ErrUnavailable
			}
			out = append(out, v)
		}
		return nil
	})
	if err != nil {
		return nil, referralError(err)
	}
	return out, nil
}

// RecordClick stores one click observation in the link's clicks partition with
// a 24h-365h expiry window relative to occurrence. A blank user agent or out-
// of-window expiry is rejected as ErrInvalid.
func (r *ReferralRepository) RecordClick(ctx context.Context, c referral.Click, expires time.Time) error {
	if err := validStoreContext(ctx); err != nil {
		return referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ErrUnavailable
	}
	if c.ID == "" || c.LinkID == "" || c.UserAgent != "" || c.OccurredAt.IsZero() || expires.Sub(c.OccurredAt) < 24*time.Hour || expires.Sub(c.OccurredAt) > 365*24*time.Hour {
		return referral.ErrInvalid
	}
	return referralError(r.store.Transact(ctx, clicksPartition(c.LinkID), func(tx recordstore.Tx) error {
		row, err := recordstore.NewRecord(kindClick, c.ID, clicksPartition(c.LinkID), 1, c)
		if err != nil {
			return err
		}
		row.State = visitDayKey(c.OccurredAt)
		row, err = row.WithExpiration(expires)
		if err != nil {
			return err
		}
		return tx.Insert(ctx, row)
	}))
}

// CountClicks scans the link's clicks and counts occurrences in [from,to). It
// fails on decode or identity inconsistency and rejects counts that would
// overflow int64.
func (r *ReferralRepository) CountClicks(ctx context.Context, link string, from, to time.Time) (int64, error) {
	if err := validStoreContext(ctx); err != nil {
		return 0, referralError(err)
	}
	if link == "" || from.IsZero() || !to.After(from) {
		return 0, referral.ErrInvalid
	}
	var count int64
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		count = 0
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindClick, Partition: clicksPartition(link)})
		if err != nil {
			return err
		}
		for _, row := range rows {
			var c referral.Click
			if err := row.Decode(&c); err != nil {
				return err
			}
			if c.LinkID != link || c.ID != row.ID {
				return recordstore.ErrUnavailable
			}
			if !c.OccurredAt.Before(from) && c.OccurredAt.Before(to) {
				if count == math.MaxInt64 {
					return referral.ErrInvalid
				}
				count++
			}
		}
		return nil
	})
	return count, referralError(err)
}

var _ referral.Repository = (*ReferralRepository)(nil)
