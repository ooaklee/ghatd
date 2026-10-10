package partnerearnings

import (
	"context"
	"math/big"
	"sort"
	"time"
)

// PaymentQuery selects accrual cohorts, not period journal movements. From is
// inclusive and To exclusive on original payment OccurredAt. Pagination does
// not narrow CohortAmounts or the unfiltered current partner balance.
type PaymentQuery struct {
	Limit                 int
	BeforeAccrualSequence int64
	ReferralID            string
	From, To              *time.Time
}

// PaymentAmounts are current economics of accepted original payment cohorts.
// Backing fields explain provenance; none is an available-to-claim balance.
// A claim can consume several payments and a returned transfer can restore
// portions subsequently used by another claim. Gross/returned/net distinguish
// those cycles without assigning a whole claim debit to every referral.
type PaymentAmounts struct {
	OriginalRevenueMinor, RefundedRevenueMinor           int64
	AccruedMinor, PendingEarnedMinor, MaturedEarnedMinor int64
	ReversedMinor, DisputeLostMinor, DisputeHoldMinor    int64
	ReservedBackingMinor, ReviewBackingMinor             int64
	GrossPaidBackingMinor, ReturnedBackingMinor          int64
	NetPaidBackingMinor                                  int64
}

// PaymentEarning retains internal immutable attribution and policy provenance.
// Hosts aggregate/project this domain view for customer responses; raw economic
// identifiers and per-invoice details are not a customer transport contract.
type PaymentEarning struct {
	PaymentID, ReferralID, PlanID, TermsVersion, PolicyID string
	AccrualSequence                                       int64
	OccurredAt, AvailableAt                               time.Time
	Matured                                               bool
	ReviewRequired                                        bool
	Amounts                                               PaymentAmounts
}

// PaymentReport is one owning payment snapshot projection: partner scope,
// ledger revision, canonical balances and cohort totals, plus a bounded
// original-payment page with continuation evidence.
type PaymentReport struct {
	ProgramID, PartnerID, Currency, Revision string
	AsOf                                     time.Time
	LedgerSequence                           int64
	Balances                                 Balances
	CohortPayments                           int
	CohortAmounts                            PaymentAmounts
	Items                                    []PaymentEarning
	HasMore                                  bool
	NextBeforeAccrualSequence                int64
}

// GetPaymentReport reads the entire owning journal and current claims in one
// transaction, validates conserved allocation/settlement/return evidence, then
// returns a bounded original-payment page. Missing acceptance produces no row;
// it never proves an unprocessed binding has zero entitlement. Global debt and
// availability remain the canonical full-ledger balances, separate from lots.
func (s *Service) GetPaymentReport(ctx context.Context, partner string, q PaymentQuery) (PaymentReport, error) {
	if err := s.checkContext(ctx); err != nil {
		return PaymentReport{}, err
	}
	if _, ok := cleanPlain(partner, maxIDLength); !ok {
		return PaymentReport{}, ErrInvalid
	}
	if q.Limit < 1 || q.Limit > 100 || q.BeforeAccrualSequence < 0 || !boundedOptional(q.ReferralID, maxIDLength) || (q.From != nil && q.From.IsZero()) || (q.To != nil && q.To.IsZero()) || (q.From != nil && q.To != nil && !q.To.After(*q.From)) {
		return PaymentReport{}, ErrInvalid
	}
	var out PaymentReport
	err := s.withPaymentSnapshot(ctx, partner, func(snapshot paymentSnapshot) error {
		out = PaymentReport{ProgramID: s.programID, PartnerID: partner, Currency: s.currency, Revision: snapshot.revision, AsOf: snapshot.asOf, Balances: snapshot.balances, LedgerSequence: snapshot.sequence, Items: []PaymentEarning{}}
		lots := snapshot.lots
		sort.Slice(lots, func(i, j int) bool { return lots[i].AccrualSequence > lots[j].AccrualSequence })
		for _, lot := range lots {
			if err := ctx.Err(); err != nil {
				return err
			}
			if (q.ReferralID != "" && lot.ReferralID != q.ReferralID) || (q.From != nil && lot.OccurredAt.Before(*q.From)) || (q.To != nil && !lot.OccurredAt.Before(*q.To)) {
				continue
			}
			out.CohortPayments++
			if err := addPaymentAmounts(&out.CohortAmounts, lot.Amounts); err != nil {
				return err
			}
			if q.BeforeAccrualSequence != 0 && lot.AccrualSequence >= q.BeforeAccrualSequence {
				continue
			}
			if len(out.Items) == q.Limit {
				out.HasMore = true
				continue
			}
			out.Items = append(out.Items, lot)
		}
		if out.HasMore {
			out.NextBeforeAccrualSequence = out.Items[len(out.Items)-1].AccrualSequence
		}
		return nil
	})
	if err != nil {
		return PaymentReport{}, err
	}
	return out, nil
}

// paymentSnapshot is a single validated owning financial read. It is shared by
// payment pages and grouped referral totals, so groups never join independent
// pages or compute balances from a filtered ledger.
type paymentSnapshot struct {
	repository Repository
	entries    []Entry
	claims     []Claim
	lots       []PaymentEarning
	balances   Balances
	revision   string
	asOf       time.Time
	sequence   int64
}

// withPaymentSnapshot opens one owning transaction, loads and sorts the full
// journal and claims, validates lots and derives balances, then passes the
// shared snapshot to project. The revision fingerprints both entries and claims
// because reservation changes need not append journal lines.
func (s *Service) withPaymentSnapshot(ctx context.Context, partner string, project func(paymentSnapshot) error) error {
	return s.repo.WithTransaction(ctx, s.programID, partner, s.currency, func(tx Repository) error {
		if isNilInterface(tx) {
			return ErrUnavailable
		}
		entries, err := tx.ListEntries(ctx, s.programID, partner)
		if err != nil {
			return err
		}
		claims, err := tx.ListClaims(ctx, s.programID, partner, nil, 0, "")
		if err != nil {
			return err
		}
		entries, claims = append([]Entry(nil), entries...), append([]Claim(nil), claims...)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
		lots, err := s.paymentEarnings(ctx, partner, entries, claims)
		if err != nil {
			return err
		}
		balances, _, err := derive(entries, claims)
		if err != nil {
			return err
		}
		// Claim changes can alter reservations without a new journal sequence.
		sort.Slice(claims, func(i, j int) bool { return claims[i].ID < claims[j].ID })
		snapshot := paymentSnapshot{repository: tx, entries: entries, claims: claims, lots: lots, balances: balances, revision: financialFingerprint(struct {
			Entries []Entry
			Claims  []Claim
		}{entries, claims}), asOf: s.clock.Now().UTC()}
		for _, e := range entries {
			if e.Sequence > snapshot.sequence {
				snapshot.sequence = e.Sequence
			}
		}
		return project(snapshot)
	})
}

// claimPaymentKey identifies one (claim, payment) pair during payment-lot
// validation.
type claimPaymentKey struct{ claim, payment string }

// disputePaymentKey identifies one (dispute, payment) pair during payment-lot
// validation.
type disputePaymentKey struct{ dispute, payment string }

// paymentEarnings validates complete current provenance, including paid claims
// whose subsequent return or amendment changes the claim revision. Original
// payment evidence, not the current revision number, fixes the settled amount.
func (s *Service) paymentEarnings(ctx context.Context, partner string, input []Entry, claims []Claim) ([]PaymentEarning, error) {
	entries := append([]Entry(nil), input...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Sequence < entries[j].Sequence })
	byClaim := map[string]Claim{}
	for _, c := range claims {
		if c.ID == "" || c.ProgramID != s.programID || c.PartnerID != partner || c.Currency != s.currency || c.Revision < 1 || c.AmountMinor <= 0 {
			return nil, ErrConflict
		}
		if _, duplicate := byClaim[c.ID]; duplicate {
			return nil, ErrConflict
		}
		byClaim[c.ID] = c
	}
	lots := map[string]*PaymentEarning{}
	allocated, released, settled := map[claimPaymentKey]int64{}, map[claimPaymentKey]int64{}, map[claimPaymentKey]int64{}
	paid := map[string]Entry{}
	returns := map[string]Entry{}
	disputeHolds := map[disputePaymentKey]int64{}
	seen := map[string]bool{}
	type sourceKey struct{ kind, event, ref string }
	sources := map[sourceKey]bool{}
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if e.ID == "" || seen[e.ID] || e.ProgramID != s.programID || e.PartnerID != partner || e.Currency != s.currency || e.Sequence != int64(i)+1 || !validStatementKind(e.Kind) || e.SourceEventID == "" || e.CreatedAt.IsZero() || e.OccurredAt.IsZero() {
			return nil, ErrConflict
		}
		seen[e.ID] = true
		source := sourceKey{e.Kind, e.SourceEventID, e.SourceRef}
		if sources[source] {
			return nil, ErrConflict
		}
		sources[source] = true
		switch e.Kind {
		case EntryAccrued:
			amount, err := CommissionMinor(e.PaymentMinor, e.RateBasisPoints)
			if err != nil || lots[e.SourceEventID] != nil || e.AmountMinor != amount || e.CommissionMinor != amount || e.AvailableAt == nil || e.HoldDuration < 0 || e.HoldDuration > maxHoldDuration || !e.AvailableAt.Equal(e.OccurredAt.Add(e.HoldDuration)) || !boundedOptional(e.ReferralID, maxIDLength) || (e.PlanID != "" && !canonicalMetricID(e.PlanID)) || !boundedOptional(e.TermsVersion, maxTermsLength) || !boundedOptional(e.PolicyID, maxPolicyLength) {
				return nil, ErrConflict
			}
			lots[e.SourceEventID] = &PaymentEarning{PaymentID: e.SourceEventID, ReferralID: e.ReferralID, PlanID: e.PlanID, TermsVersion: e.TermsVersion, PolicyID: e.PolicyID, AccrualSequence: e.Sequence, OccurredAt: e.OccurredAt, AvailableAt: *e.AvailableAt, Amounts: PaymentAmounts{OriginalRevenueMinor: e.PaymentMinor, AccruedMinor: amount}}
		case EntryMatured:
			lot := lots[e.SourceEventID]
			if lot == nil || lot.Matured || e.AmountMinor != lot.Amounts.AccruedMinor {
				return nil, ErrConflict
			}
			lot.Matured = true
		case EntryReversed, EntryDisputeHold, EntryDisputeReleased, EntryDisputeWon, EntryDisputeLost, EntryDisputeDecision:
			lot := lots[e.SourceRef]
			if lot == nil {
				return nil, ErrConflict
			}
			switch e.Kind {
			case EntryReversed:
				if e.AmountMinor > 0 || e.CumulativeRefundedMinor < 0 || e.CumulativeRefundedMinor > lot.Amounts.OriginalRevenueMinor {
					return nil, ErrConflict
				}
				if e.CumulativeRefundedMinor > lot.Amounts.RefundedRevenueMinor {
					lot.Amounts.RefundedRevenueMinor = e.CumulativeRefundedMinor
				}
				if err := addSignedAmount(&lot.Amounts.ReversedMinor, negBigInt(e.AmountMinor)); err != nil {
					return nil, err
				}
			case EntryDisputeLost:
				if e.AmountMinor > 0 || e.DisputeID == "" {
					return nil, ErrConflict
				}
				if err := addSignedAmount(&lot.Amounts.DisputeLostMinor, negBigInt(e.AmountMinor)); err != nil {
					return nil, err
				}
			case EntryDisputeHold, EntryDisputeReleased, EntryDisputeWon:
				if e.AmountMinor < 0 || e.DisputeID == "" {
					return nil, ErrConflict
				}
				amount := big.NewInt(e.AmountMinor)
				if e.Kind != EntryDisputeHold {
					amount.Neg(amount)
				}
				key := disputePaymentKey{e.DisputeID, e.SourceRef}
				remaining := disputeHolds[key]
				if err := addSignedAmount(&remaining, amount); err != nil {
					return nil, err
				}
				if remaining < 0 {
					return nil, ErrConflict
				}
				disputeHolds[key] = remaining
				if err := addSignedAmount(&lot.Amounts.DisputeHoldMinor, amount); err != nil {
					return nil, err
				}
			case EntryDisputeDecision:
				if e.AmountMinor != 0 || e.DisputeID == "" {
					return nil, ErrConflict
				}
			}
		case EntryAllocated, EntryAllocationSettled, EntryAllocationReleased:
			lot := lots[e.SourceRef]
			if lot == nil || !lot.Matured {
				return nil, ErrConflict
			}
			key := claimPaymentKey{e.SourceEventID, e.SourceRef}
			if e.Kind == EntryAllocationReleased {
				if operation, ok := returns[e.SourceEventID]; ok {
					if _, ambiguous := byClaim[e.SourceEventID]; ambiguous || e.AmountMinor >= 0 || operation.Sequence >= e.Sequence {
						return nil, ErrConflict
					}
					continue // typed return helper validates complete portions below
				}
			}
			if _, ok := byClaim[key.claim]; !ok {
				return nil, ErrConflict
			}
			if e.Kind == EntryAllocationSettled && (paid[key.claim].ID == "" || allocated[key] == 0) {
				return nil, ErrConflict
			}
			if e.Kind == EntryAllocationReleased && allocated[key] == 0 {
				return nil, ErrConflict
			}
			if e.Kind == EntryAllocated && paid[key.claim].ID != "" {
				return nil, ErrConflict
			}
			amount := big.NewInt(e.AmountMinor)
			values := allocated
			if e.Kind != EntryAllocated {
				if e.AmountMinor >= 0 {
					return nil, ErrConflict
				}
				amount.Neg(amount)
				values = settled
				if e.Kind == EntryAllocationReleased {
					values = released
				}
			} else if e.AmountMinor <= 0 {
				return nil, ErrConflict
			}
			value := values[key]
			if err := addSignedAmount(&value, amount); err != nil {
				return nil, err
			}
			values[key] = value
		case EntryPaid:
			c, exists := byClaim[e.SourceEventID]
			if !exists || paid[c.ID].ID != "" || c.State != ClaimPaid || c.Payment == nil || c.Payment.Currency != s.currency || c.Payment.AmountMinor != c.AmountMinor || c.Payment.State != PaymentStateFull || c.Payment.Method == "" || c.Payment.Reference == "" || c.Payment.RecordedBy == "" || c.Payment.RecordedAt.IsZero() || e.AmountMinor != -c.AmountMinor || !e.OccurredAt.Equal(c.Payment.PaidAt) {
				return nil, ErrConflict
			}
			var backing big.Int
			for key, amount := range allocated {
				if key.claim == c.ID {
					backing.Add(&backing, big.NewInt(amount))
				}
			}
			if backing.Cmp(big.NewInt(c.AmountMinor)) != 0 {
				return nil, ErrConflict
			}
			paid[c.ID] = e
		case EntryReturned:
			c, exists := byClaim[e.SourceRef]
			if !exists || c.State != ClaimPaid || paid[c.ID].ID == "" || e.AmountMinor <= 0 || returns[e.SourceEventID].ID != "" {
				return nil, ErrConflict
			}
			if _, ambiguous := byClaim[e.SourceEventID]; ambiguous {
				return nil, ErrConflict
			}
			var backing big.Int
			for key, amount := range settled {
				if key.claim == c.ID {
					backing.Add(&backing, big.NewInt(amount))
				}
			}
			if backing.Cmp(big.NewInt(c.AmountMinor)) != 0 {
				return nil, ErrConflict
			}
			returns[e.SourceEventID] = e
		case EntryPaymentObserved:
			if _, ok := byClaim[e.SourceEventID]; !ok || e.AmountMinor != 0 {
				return nil, ErrConflict
			}
		}
	}
	for key := range released {
		if allocated[key] == 0 {
			return nil, ErrConflict
		}
	}
	for key := range settled {
		if allocated[key] == 0 || paid[key.claim].ID == "" {
			return nil, ErrConflict
		}
	}
	for id, c := range byClaim {
		var total big.Int
		for key, amount := range allocated {
			if key.claim != id {
				continue
			}
			total.Add(&total, big.NewInt(amount))
			lot := lots[key.payment]
			switch c.State {
			case ClaimRequested, ClaimProcessing, ClaimNeedsReview:
				if released[key] != 0 || settled[key] != 0 || paid[id].ID != "" || c.Payment != nil {
					return nil, ErrConflict
				}
				field := &lot.Amounts.ReservedBackingMinor
				if c.State == ClaimNeedsReview {
					field = &lot.Amounts.ReviewBackingMinor
				}
				lot.ReviewRequired = lot.ReviewRequired || c.State == ClaimNeedsReview || c.ReviewReason != ""
				if err := addSignedAmount(field, big.NewInt(amount)); err != nil {
					return nil, err
				}
			case ClaimCancelled, ClaimRejected:
				if released[key] != amount || settled[key] != 0 || paid[id].ID != "" || c.Payment != nil {
					return nil, ErrConflict
				}
			case ClaimPaid:
				if paid[id].ID == "" || settled[key] != amount || released[key] != 0 {
					return nil, ErrConflict
				}
				if err := addSignedAmount(&lot.Amounts.GrossPaidBackingMinor, big.NewInt(amount)); err != nil {
					return nil, err
				}
			default:
				return nil, ErrConflict
			}
		}
		if total.Cmp(big.NewInt(c.AmountMinor)) != 0 {
			return nil, ErrConflict
		}
		if c.State == ClaimPaid {
			backing, err := returnedAllocationBacking(entries, id)
			if err != nil {
				return nil, err
			}
			var restored big.Int
			for payment, amount := range backing {
				restored.Add(&restored, big.NewInt(amount))
				if err := addSignedAmount(&lots[payment].Amounts.ReturnedBackingMinor, big.NewInt(amount)); err != nil {
					return nil, err
				}
			}
			if restored.Cmp(big.NewInt(c.AmountMinor)) > 0 {
				return nil, ErrConflict
			}
			var adjustments big.Int
			seenReturns := map[string]bool{}
			for _, adjustment := range c.ReturnedAdjustments {
				operation, exists := returns[adjustment.OperationID]
				if !exists || seenReturns[adjustment.OperationID] || operation.SourceRef != id || adjustment.AmountMinor <= 0 || adjustment.Currency != s.currency || adjustment.AmountMinor != operation.AmountMinor || adjustment.ReturnedAt.IsZero() || !adjustment.ReturnedAt.Equal(operation.OccurredAt) || adjustment.At.IsZero() || !adjustment.At.Equal(operation.CreatedAt) || adjustment.By == "" || adjustment.By != operation.ActorID {
					return nil, ErrConflict
				}
				seenReturns[adjustment.OperationID] = true
				adjustments.Add(&adjustments, big.NewInt(adjustment.AmountMinor))
			}
			if adjustments.Cmp(&restored) != 0 {
				return nil, ErrConflict
			}
		}
	}
	out := make([]PaymentEarning, 0, len(lots))
	for _, lot := range lots {
		net := new(big.Int).Sub(big.NewInt(lot.Amounts.AccruedMinor), big.NewInt(lot.Amounts.ReversedMinor))
		net.Sub(net, big.NewInt(lot.Amounts.DisputeLostMinor))
		if net.Sign() < 0 || lot.Amounts.DisputeHoldMinor < 0 || big.NewInt(lot.Amounts.DisputeHoldMinor).Cmp(net) > 0 {
			return nil, ErrConflict
		}
		field := &lot.Amounts.PendingEarnedMinor
		if lot.Matured {
			field = &lot.Amounts.MaturedEarnedMinor
		}
		if err := addSignedAmount(field, net); err != nil {
			return nil, err
		}
		lot.Amounts.NetPaidBackingMinor = lot.Amounts.GrossPaidBackingMinor - lot.Amounts.ReturnedBackingMinor
		if lot.Amounts.NetPaidBackingMinor < 0 {
			return nil, ErrConflict
		}
		out = append(out, *lot)
	}
	return out, nil
}

// addSignedAmount adds amount to *target using big-int arithmetic, failing when
// the sum no longer fits int64.
func addSignedAmount(target *int64, amount *big.Int) error {
	value, err := bigToInt64(new(big.Int).Add(big.NewInt(*target), amount))
	if err != nil {
		return err
	}
	*target = value
	return nil
}

// addPaymentAmounts adds each PaymentAmounts field into target with overflow-
// checked accumulation.
func addPaymentAmounts(target *PaymentAmounts, amount PaymentAmounts) error {
	fields := []struct {
		target *int64
		amount int64
	}{
		{&target.OriginalRevenueMinor, amount.OriginalRevenueMinor}, {&target.RefundedRevenueMinor, amount.RefundedRevenueMinor},
		{&target.AccruedMinor, amount.AccruedMinor}, {&target.PendingEarnedMinor, amount.PendingEarnedMinor}, {&target.MaturedEarnedMinor, amount.MaturedEarnedMinor},
		{&target.ReversedMinor, amount.ReversedMinor}, {&target.DisputeLostMinor, amount.DisputeLostMinor}, {&target.DisputeHoldMinor, amount.DisputeHoldMinor},
		{&target.ReservedBackingMinor, amount.ReservedBackingMinor}, {&target.ReviewBackingMinor, amount.ReviewBackingMinor},
		{&target.GrossPaidBackingMinor, amount.GrossPaidBackingMinor}, {&target.ReturnedBackingMinor, amount.ReturnedBackingMinor}, {&target.NetPaidBackingMinor, amount.NetPaidBackingMinor},
	}
	for _, field := range fields {
		if err := addSignedAmount(field.target, big.NewInt(field.amount)); err != nil {
			return err
		}
	}
	return nil
}
