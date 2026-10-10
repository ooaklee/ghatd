package partnerearnings

import (
	"context"
	"time"
)

// ReferralAmountGroup joins immutable accrual referral revisions for one
// retained relationship. IDs are supplied by the owning referral service via
// the manager, never inferred from payment dates or mutable current ownership.
// A revision may belong to only one requested group.
type ReferralAmountGroup struct {
	ID          string
	ReferralIDs []string
}

// MaxReferralAmountRevisions bounds an internal grouped query. A caller with a
// larger selected history receives ErrReportTooLarge. Reducing the page helps
// only when several groups exceed the bound together; a single larger group
// needs an explicit history-capacity extension. History is never truncated.
const MaxReferralAmountRevisions = 10000

// ReferralAmountQuery selects up to 100 groups and 10000 revision IDs. From/To are an original-payment
// cohort, inclusive/exclusive. Complete owned revision lists can grow with
// history; this domain query is not a customer transport request.
type ReferralAmountQuery struct {
	Groups   []ReferralAmountGroup
	From, To *time.Time
}

// ReferralAmounts is one requested group's aggregate: accepted journal payment
// count and bounds, whether review is required, and the group's PaymentAmounts
// portions.
type ReferralAmounts struct {
	ID                            string
	AcceptedPayments              int
	FirstPaymentAt, LastPaymentAt *time.Time
	ReviewRequired                bool
	Amounts                       PaymentAmounts
}

// ReferralAmountReport includes every requested group, even with no accepted
// accrual. AcceptedPayments is an accepted journal count, not a source-complete
// entitlement decision, renewal count or subscription-state observation.
type ReferralAmountReport struct {
	ProgramID, PartnerID, Currency, Revision string
	AsOf                                     time.Time
	LedgerSequence                           int64
	Balances                                 Balances
	Items                                    []ReferralAmounts
}

// GetReferralAmounts aggregates all selected ownership periods in one owning
// financial snapshot. A whole claim is never copied to every group: validated
// per-payment settlements and typed returns supply its exact backing portions.
// Current global balances remain separate from filtered referral amounts.
func (s *Service) GetReferralAmounts(ctx context.Context, partner string, q ReferralAmountQuery) (ReferralAmountReport, error) {
	if err := s.checkContext(ctx); err != nil {
		return ReferralAmountReport{}, err
	}
	if cleaned, ok := cleanPlain(partner, maxIDLength); !ok || cleaned != partner || len(q.Groups) > 100 || (q.From != nil && q.From.IsZero()) || (q.To != nil && q.To.IsZero()) || (q.From != nil && q.To != nil && !q.To.After(*q.From)) {
		return ReferralAmountReport{}, ErrInvalid
	}
	groups := map[string]bool{}
	revisions := map[string]int{}
	for i, group := range q.Groups {
		if cleaned, ok := cleanPlain(group.ID, maxIDLength); !ok || cleaned != group.ID || groups[group.ID] || len(group.ReferralIDs) == 0 {
			return ReferralAmountReport{}, ErrInvalid
		}
		groups[group.ID] = true
		for _, id := range group.ReferralIDs {
			if err := ctx.Err(); err != nil {
				return ReferralAmountReport{}, err
			}
			if cleaned, ok := cleanPlain(id, maxIDLength); !ok || cleaned != id {
				return ReferralAmountReport{}, ErrInvalid
			}
			if len(revisions) >= MaxReferralAmountRevisions {
				return ReferralAmountReport{}, ErrReportTooLarge
			}
			if _, duplicate := revisions[id]; duplicate {
				return ReferralAmountReport{}, ErrInvalid
			}
			revisions[id] = i
		}
	}
	var out ReferralAmountReport
	err := s.withPaymentSnapshot(ctx, partner, func(snapshot paymentSnapshot) error {
		out = ReferralAmountReport{ProgramID: s.programID, PartnerID: partner, Currency: s.currency, Revision: snapshot.revision, AsOf: snapshot.asOf, LedgerSequence: snapshot.sequence, Balances: snapshot.balances, Items: make([]ReferralAmounts, len(q.Groups))}
		for i, group := range q.Groups {
			out.Items[i].ID = group.ID
		}
		for _, lot := range snapshot.lots {
			if err := ctx.Err(); err != nil {
				return err
			}
			i, selected := revisions[lot.ReferralID]
			if !selected || (q.From != nil && lot.OccurredAt.Before(*q.From)) || (q.To != nil && !lot.OccurredAt.Before(*q.To)) {
				continue
			}
			row := &out.Items[i]
			row.AcceptedPayments++
			row.ReviewRequired = row.ReviewRequired || lot.ReviewRequired
			if row.FirstPaymentAt == nil || lot.OccurredAt.Before(*row.FirstPaymentAt) {
				at := lot.OccurredAt
				row.FirstPaymentAt = &at
			}
			if row.LastPaymentAt == nil || lot.OccurredAt.After(*row.LastPaymentAt) {
				at := lot.OccurredAt
				row.LastPaymentAt = &at
			}
			if err := addPaymentAmounts(&row.Amounts, lot.Amounts); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return ReferralAmountReport{}, err
	}
	return out, nil
}
