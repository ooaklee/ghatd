package partnermanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
)

// ReferralTerms is the customer-visible frozen commercial description. Policy
// IDs, acquisition evidence, provider references and internal targets are not
// part of this transport projection.
type ReferralTerms struct {
	RateBasisPoints  int        `json:"rate_basis_points"`
	HoldDurationDays int        `json:"hold_duration_days"`
	Currency         string     `json:"currency"`
	CurrencyExponent int        `json:"currency_exponent"`
	TermsVersion     string     `json:"terms_version"`
	RecurrenceEndsAt *time.Time `json:"recurrence_ends_at,omitempty"`
}

// ReferralPeriod is one contiguous ownership interval with the frozen terms
// snapshot in force. Until is nil while the period is open; Corrected marks a
// period ended by correction rather than natural expiry.
type ReferralPeriod struct {
	ID        string        `json:"id"`
	From      time.Time     `json:"from"`
	Until     *time.Time    `json:"until,omitempty"`
	Terms     ReferralTerms `json:"terms"`
	Corrected bool          `json:"corrected"`
}

// CustomerReferral is the customer-facing projection of one referred
// relationship: stable ID, first ownership time, current flag and ordered
// periods. Internal customer and attribution identities are absent.
type CustomerReferral struct {
	ID           string           `json:"id"`
	FirstOwnedAt time.Time        `json:"first_owned_at"`
	Current      bool             `json:"current"`
	Periods      []ReferralPeriod `json:"periods"`
}

// CustomerReferralPage is one bounded page of customer referral projections
// with cursor continuation; it is not a complete relationship set.
type CustomerReferralPage struct {
	Items     []CustomerReferral `json:"items"`
	HasMore   bool               `json:"has_more"`
	NextAfter string             `json:"next_after,omitempty"`
}

// relationshipPage reads and strictly validates one owning relationship page:
// identity fields, ordering, derived IDs, non-overlapping contiguous periods
// with in-range frozen terms, and consistency between Current and the last
// period's open end. Any malformed projection fails with ErrUnavailable.
func (m *Manager) relationshipPage(ctx context.Context, partner string, q referral.RelationshipQuery) (referral.RelationshipPage, error) {
	page, err := m.deps.Referral.ListRelationships(ctx, partner, q)
	if err != nil {
		return referral.RelationshipPage{}, err
	}
	if page.ProgramID != referral.ProgramID || page.PartnerID != partner || len(page.Items) > q.Limit || (page.HasMore && len(page.Items) != q.Limit) || (!page.HasMore && page.NextAfter != "") {
		return referral.RelationshipPage{}, ErrUnavailable
	}
	previous := q.After
	for _, item := range page.Items {
		if item.ProgramID != referral.ProgramID || item.PartnerID != partner || !validWorkText(item.ReferredCustomer, 256) || item.ID != referral.RelationshipReferenceID(referral.ProgramID, partner, item.ReferredCustomer) || item.ID <= previous || item.FirstOwnedAt.IsZero() || len(item.Periods) == 0 {
			return referral.RelationshipPage{}, ErrUnavailable
		}
		previous = item.ID
		seen := map[string]bool{}
		for i, period := range item.Periods {
			if period.ReferralID == "" || seen[period.ReferralID] || period.From.IsZero() || (period.Until != nil && period.Until.Before(period.From)) || period.Terms.Currency != m.deps.Program.Config().Currency || period.Terms.CurrencyExponent != m.deps.Program.Config().CurrencyExponent || period.Terms.TermsVersion == "" || period.Terms.RateBasisPoints < 0 || period.Terms.RateBasisPoints > 10000 || period.Terms.HoldDurationDays < 0 || period.Terms.HoldDurationDays > partnerprogram.MaxSupportedHoldDays {
				return referral.RelationshipPage{}, ErrUnavailable
			}
			if i == 0 && !period.From.Equal(item.FirstOwnedAt) {
				return referral.RelationshipPage{}, ErrUnavailable
			}
			if i > 0 && (item.Periods[i-1].Until == nil || period.From.Before(*item.Periods[i-1].Until)) {
				return referral.RelationshipPage{}, ErrUnavailable
			}
			seen[period.ReferralID] = true
		}
		if item.Current != (item.Periods[len(item.Periods)-1].Until == nil) {
			return referral.RelationshipPage{}, ErrUnavailable
		}
	}
	if page.HasMore && page.NextAfter != previous {
		return referral.RelationshipPage{}, ErrUnavailable
	}
	return page, nil
}

// ReferralHistory exposes current and retained relationships to the verified
// owner. A correction never removes earned history from this view. Authority
// is checked again after the owning read, before returning private data.
func (m *Manager) ReferralHistory(ctx context.Context, actor string, q referral.RelationshipQuery) (CustomerReferralPage, error) {
	if q.Limit < 1 || q.Limit > 100 {
		return CustomerReferralPage{}, ErrInvalid
	}
	p, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return CustomerReferralPage{}, err
	}
	if p.ProgramID != partnerprogram.ProgramID {
		return CustomerReferralPage{}, ErrUnavailable
	}
	page, err := m.relationshipPage(ctx, p.ID, q)
	if err != nil {
		return CustomerReferralPage{}, err
	}
	out := CustomerReferralPage{Items: []CustomerReferral{}, HasMore: page.HasMore, NextAfter: page.NextAfter}
	for _, item := range page.Items {
		row := projectCustomerReferral(item)
		out.Items = append(out.Items, row)
	}
	current, err := m.self(ctx, actor, CapabilitySelf)
	if err != nil {
		return CustomerReferralPage{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID {
		return CustomerReferralPage{}, ErrDenied
	}
	return out, nil
}

// projectCustomerReferral never exposes internal attribution or customer IDs.
func projectCustomerReferral(item referral.Relationship) CustomerReferral {
	row := CustomerReferral{ID: item.ID, FirstOwnedAt: item.FirstOwnedAt, Current: item.Current, Periods: []ReferralPeriod{}}
	for _, period := range item.Periods {
		// Namespace the revision ID by this relationship; do not publish the
		// raw revision ID used by privileged attribution/payment joins.
		digest := sha256.Sum256([]byte(item.ID + ":" + period.ReferralID))
		terms := period.Terms
		row.Periods = append(row.Periods, ReferralPeriod{ID: hex.EncodeToString(digest[:]), From: period.From, Until: period.Until, Corrected: period.Corrected, Terms: ReferralTerms{RateBasisPoints: terms.RateBasisPoints, HoldDurationDays: terms.HoldDurationDays, Currency: terms.Currency, CurrencyExponent: terms.CurrencyExponent, TermsVersion: terms.TermsVersion, RecurrenceEndsAt: terms.RecurrenceEndsAt}})
	}
	return row
}

// AdminReferralHistory is the reporting-scoped domain view for one selected
// partner. The host explicitly projects any permitted customer identity; this
// view still contains no other owner's history or raw correction reasons.
func (m *Manager) AdminReferralHistory(ctx context.Context, actor, partner string, q referral.RelationshipQuery) (referral.RelationshipPage, error) {
	if !validWorkText(partner, 256) || q.Limit < 1 || q.Limit > 100 {
		return referral.RelationshipPage{}, ErrInvalid
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return referral.RelationshipPage{}, err
	}
	p, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return referral.RelationshipPage{}, err
	}
	if p.ID != partner || p.ProgramID != partnerprogram.ProgramID {
		return referral.RelationshipPage{}, ErrUnavailable
	}
	page, err := m.relationshipPage(ctx, partner, q)
	if err != nil {
		return referral.RelationshipPage{}, err
	}
	if err := m.authorize(ctx, actor, CapabilityReporting, partner); err != nil {
		return referral.RelationshipPage{}, err
	}
	current, err := m.deps.Program.GetPartner(ctx, partner)
	if err != nil {
		return referral.RelationshipPage{}, err
	}
	if current.ID != p.ID || current.ProgramID != p.ProgramID || current.CustomerID != p.CustomerID {
		return referral.RelationshipPage{}, ErrDenied
	}
	return page, nil
}
