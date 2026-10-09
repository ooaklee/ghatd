package billing

import (
	"context"
	"errors"
	"sort"
	"time"
)

// RevenueHistoryCapacity bounds the COMPLETE owning history (facts plus
// reception/resolution receipts). Filtering never hides a capacity failure.
const RevenueHistoryCapacity = 10000

// ErrRevenueHistoryTooLarge requires an explicitly extended operational read
// budget or equivalent indexed projection, never a silently truncated report.
var ErrRevenueHistoryTooLarge = errors.New("billing/revenue-history-too-large")

// RevenueHistorySnapshot is private repository evidence from one owning read.
// The economic sequence head certifies the retained accepted fact count;
// complete reception/resolution evidence proves each fact's original reception.
type RevenueHistorySnapshot struct {
	Facts                  []RevenueFact        `json:"-"`
	Observations           []RevenueObservation `json:"-"`
	Sequence, HeadRevision int64                `json:"-"`
}

// RevenueHistoryRepository supplies a complete bounded native snapshot using
// the same revenue owner. It performs no provider I/O or money calculation.
type RevenueHistoryRepository interface {
	ReadRevenueHistory(context.Context) (RevenueHistorySnapshot, error)
}

// RevenueHistoryQuery is private trusted reporting scope. Principals originate
// in owning referral relationships, never customer-entered payer identities.
// Scopes originate in host-reviewed provider configuration, never HTTP input.
// At most RevenueHistoryCapacity principals fit; the separate COMPLETE global
// facts-plus-receipts budget still applies before filtering.
type RevenueHistoryQuery struct {
	Scopes     []RevenueScope `json:"-"`
	Principals []string       `json:"-"`
}

// PaymentRevenue is the current verified economics of one original allocation.
// Original remains immutable. Refund/loss/hold derive from SEPARATE facts;
// temporary hold is not a confirmed debit. It creates no commission entitlement.
type PaymentRevenue struct {
	Original                           RevenueFact `json:"-"`
	RefundedMinor, NetMinor            int64       `json:"-"`
	DisputeLostMinor, DisputeHoldMinor int64       `json:"-"`
	DisputeHeld, DisputeLost           bool        `json:"-"`
}

// PaymentRevenueHistory is confirmed accepted-source coverage, NOT certification
// of unseen provider deliveries. Unresolved scoped sources cannot be assigned
// to a principal without historical association. Customer projections must not
// disclose unrelated merchant source counts or raw provider/payer identities.
type PaymentRevenueHistory struct {
	Items                   []PaymentRevenue `json:"-"`
	AsOf                    time.Time        `json:"-"`
	Revision                string           `json:"-"`
	AcceptanceSequence      int64            `json:"-"`
	ScopedUnresolvedSources int              `json:"-"`
}

func (q RevenueHistoryQuery) Validate() error {
	if ValidateRevenueHistoryScopes(q.Scopes) != nil || len(q.Principals) < 1 || len(q.Principals) > RevenueHistoryCapacity {
		return ErrRevenueInvalid
	}
	principals := map[string]bool{}
	for _, p := range q.Principals {
		if !cleanStatusID(p) || principals[p] {
			return ErrRevenueInvalid
		}
		principals[p] = true
	}
	return nil
}

// ValidateRevenueHistoryScopes validates an explicit host-reviewed scope set
// without inventing a paying principal or accepting an implicit default account.
func ValidateRevenueHistoryScopes(scopes []RevenueScope) error {
	if len(scopes) < 1 || len(scopes) > 10 {
		return ErrRevenueInvalid
	}
	seen := map[RevenueScope]bool{}
	for _, s := range scopes {
		if !validRevenueScope(s) || !cleanStatusID(s.Provider) || !cleanStatusID(s.AccountID) || seen[s] {
			return ErrRevenueInvalid
		}
		seen[s] = true
	}
	return nil
}

// Validate verifies one private allocation's provenance and conserved current
// revenue. Held exposure overlaps net; confirmed loss consumes only the amount
// remaining after cumulative refunds, not a second original payment amount.
func (p PaymentRevenue) Validate() error {
	f := p.Original
	canonical, err := canonicalRevenueFact(f)
	if err != nil || canonical.ID != f.ID || canonical.Fingerprint != f.Fingerprint || f.Kind != RevenuePayment || f.Sequence < 1 || f.AcceptedAt.IsZero() || p.RefundedMinor < 0 || p.RefundedMinor > f.PaidMinor || p.NetMinor < 0 || p.DisputeLostMinor < 0 || p.DisputeHoldMinor < 0 {
		return ErrRevenueUnassessable
	}
	remaining := f.PaidMinor - p.RefundedMinor
	if p.DisputeLost {
		if p.NetMinor != 0 || p.DisputeLostMinor != remaining {
			return ErrRevenueUnassessable
		}
	} else if p.NetMinor != remaining || p.DisputeLostMinor != 0 {
		return ErrRevenueUnassessable
	}
	if p.DisputeHeld {
		if p.DisputeHoldMinor != p.NetMinor {
			return ErrRevenueUnassessable
		}
	} else if p.DisputeHoldMinor != 0 {
		return ErrRevenueUnassessable
	}
	return nil
}

func canonicalObservation(v RevenueObservation, facts map[string]RevenueFact, all map[string]RevenueObservation) (string, error) {
	if !validRevenueScope(v.Scope) || !cleanStatusID(v.ID) || !cleanStatusID(v.EnvelopeID) || v.AcceptedAt.IsZero() || len(v.FactIDs) > 200 || (v.SourceFingerprint != "" && !cleanStatusID(v.SourceFingerprint)) || (v.RecoveryFingerprint != "" && !validRevenueIdentity(v.RecoveryFingerprint)) {
		return "", ErrRevenueUnassessable
	}
	values := make([]RevenueFact, 0, len(v.FactIDs))
	previous := ""
	for _, id := range v.FactIDs {
		f, ok := facts[id]
		if !ok || id <= previous || f.Scope != v.Scope || f.AcceptedAt.After(v.AcceptedAt) {
			return "", ErrRevenueUnassessable
		}
		previous = id
		canonical, err := canonicalRevenueFact(f)
		if err != nil {
			return "", ErrRevenueUnassessable
		}
		values = append(values, canonical)
	}
	if v.ResolutionOf == "" {
		if v.ID != revenueID(v.Scope, "delivery", v.EnvelopeID, "", "", "") || v.ResolutionBy != "" || v.ResolutionReason != "" || v.RecoveryFingerprint != "" || (len(values) == 0 && v.QuarantineReason == "") || (len(values) > 0 && v.QuarantineReason != "") || len(v.QuarantineReason) > 128 {
			return "", ErrRevenueUnassessable
		}
		return subscriptionDigest(struct {
			Facts                     []RevenueFact
			Reason, SourceFingerprint string
		}{values, v.QuarantineReason, v.SourceFingerprint}), nil
	}
	original, ok := all[v.ResolutionOf]
	if !ok || original.ResolutionOf != "" || original.QuarantineReason == "" || len(original.FactIDs) != 0 || original.Scope != v.Scope || original.EnvelopeID != v.EnvelopeID || original.SourceFingerprint != v.SourceFingerprint || v.AcceptedAt.Before(original.AcceptedAt) || v.QuarantineReason != "" || !cleanStatusID(v.ResolutionBy) || !cleanStatusID(v.ResolutionReason) || v.ID != revenueID(v.Scope, "resolution", original.ID, "", "", "") {
		return "", ErrRevenueUnassessable
	}
	// Match ResolveQuarantinedRevenue's stored fingerprint exactly. Omitting
	// an empty recovery identity preserves existing legacy resolution hashes.
	return subscriptionDigest(struct {
		Original            string
		Fingerprint         string
		Facts               []RevenueFact
		Reason              string
		Actor               string
		RecoveryFingerprint string `json:",omitempty"`
	}{Original: original.ID, Fingerprint: original.Fingerprint, Facts: values, Reason: v.ResolutionReason, Actor: v.ResolutionBy, RecoveryFingerprint: v.RecoveryFingerprint}), nil
}

func validateRevenueHistory(ctx context.Context, snapshot RevenueHistorySnapshot, at time.Time) (map[string]RevenueFact, map[string]RevenueObservation, error) {
	if len(snapshot.Facts)+len(snapshot.Observations) > RevenueHistoryCapacity {
		return nil, nil, ErrRevenueHistoryTooLarge
	}
	if snapshot.Facts == nil || snapshot.Observations == nil || at.IsZero() || snapshot.Sequence != int64(len(snapshot.Facts)) || snapshot.HeadRevision != snapshot.Sequence {
		return nil, nil, ErrRevenueUnassessable
	}
	facts := map[string]RevenueFact{}
	sequences := map[int64]bool{}
	observations := map[string]RevenueObservation{}
	for _, f := range snapshot.Facts {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		canonical, err := canonicalRevenueFact(f)
		if err != nil || canonical.ID != f.ID || canonical.Fingerprint != f.Fingerprint || f.AcceptedAt.IsZero() || f.AcceptedAt.After(at) || f.Sequence < 1 || f.Sequence > snapshot.Sequence || sequences[f.Sequence] || facts[f.ID].ID != "" {
			return nil, nil, ErrRevenueUnassessable
		}
		sequences[f.Sequence] = true
		facts[f.ID] = f
	}
	for _, v := range snapshot.Observations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if observations[v.ID].ID != "" || v.AcceptedAt.After(at) {
			return nil, nil, ErrRevenueUnassessable
		}
		observations[v.ID] = v
	}
	created := map[string]bool{}
	for _, v := range snapshot.Observations {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		fp, err := canonicalObservation(v, facts, observations)
		if err != nil || v.Fingerprint != fp {
			return nil, nil, ErrRevenueUnassessable
		}
		for _, id := range v.FactIDs {
			if facts[id].AcceptedAt.Equal(v.AcceptedAt) {
				created[id] = true
			}
		}
	}
	if len(created) != len(facts) {
		return nil, nil, ErrRevenueUnassessable
	}
	return facts, observations, nil
}

func sameOriginalEconomics(original, adjustment RevenueFact) bool {
	return original.Kind == RevenuePayment && original.ID == adjustment.PaymentFactID() && original.Scope == adjustment.Scope && original.PaymentID == adjustment.PaymentID && original.InvoiceID == adjustment.InvoiceID && original.AllocationID == adjustment.AllocationID && original.PrincipalID == adjustment.PrincipalID && original.ProviderCustomerID == adjustment.ProviderCustomerID && original.SubscriptionID == adjustment.SubscriptionID && original.PlanID == adjustment.PlanID && original.CostID == adjustment.CostID && original.ProviderPriceID == adjustment.ProviderPriceID && original.Currency == adjustment.Currency && original.CurrencyExponent == adjustment.CurrencyExponent && original.PaidMinor == adjustment.PaidMinor
}

// GetPaymentRevenueHistory derives net revenue from complete accepted evidence.
// Unknown originals/contradictions and capacity/cancellation produce no partial
// report. Independent status/financial reads must expose their own freshness;
// this report neither calls a provider nor establishes referral ownership.
func (s *RevenueService) GetPaymentRevenueHistory(ctx context.Context, q RevenueHistoryQuery) (PaymentRevenueHistory, error) {
	if err := revenueContext(ctx); err != nil {
		return PaymentRevenueHistory{}, err
	}
	if s == nil || revenueNil(s.repo) || revenueNil(s.clock) {
		return PaymentRevenueHistory{}, ErrRevenueUnavailable
	}
	if err := q.Validate(); err != nil {
		return PaymentRevenueHistory{}, err
	}
	repo, ok := s.repo.(RevenueHistoryRepository)
	if !ok || revenueNil(repo) {
		return PaymentRevenueHistory{}, ErrRevenueUnavailable
	}
	scopes := map[RevenueScope]bool{}
	principals := map[string]bool{}
	for _, scope := range q.Scopes {
		scopes[scope] = true
	}
	for _, p := range q.Principals {
		principals[p] = true
	}
	snapshot, err := repo.ReadRevenueHistory(ctx)
	if err != nil {
		return PaymentRevenueHistory{}, err
	}
	at := s.clock.Now().UTC()
	facts, observations, err := validateRevenueHistory(ctx, snapshot, at)
	if err != nil {
		return PaymentRevenueHistory{}, err
	}
	snapshot.Facts = append([]RevenueFact{}, snapshot.Facts...)
	snapshot.Observations = append([]RevenueObservation{}, snapshot.Observations...)
	rows := map[string]*PaymentRevenue{}
	disputes := map[string]map[string]string{}
	for _, f := range facts {
		if scopes[f.Scope] && principals[f.PrincipalID] && f.Kind == RevenuePayment {
			rows[f.ID] = &PaymentRevenue{Original: f, NetMinor: f.PaidMinor}
		}
	}
	for _, f := range facts {
		if err := ctx.Err(); err != nil {
			return PaymentRevenueHistory{}, err
		}
		original := facts[f.PaymentFactID()]
		if f.Kind == RevenuePayment || !scopes[f.Scope] || (!principals[f.PrincipalID] && !principals[original.PrincipalID]) {
			continue
		}
		row := rows[f.PaymentFactID()]
		if row == nil || !sameOriginalEconomics(row.Original, f) {
			return PaymentRevenueHistory{}, ErrRevenueUnassessable
		}
		if f.Kind == RevenueRefund {
			if f.CumulativeRefundedMinor > row.RefundedMinor {
				row.RefundedMinor = f.CumulativeRefundedMinor
			}
			continue
		}
		if disputes[row.Original.ID] == nil {
			disputes[row.Original.ID] = map[string]string{}
		}
		old := disputes[row.Original.ID][f.AdjustmentID]
		if (old == RevenueDisputeLost && f.Kind == RevenueDisputeWon) || (old == RevenueDisputeWon && f.Kind == RevenueDisputeLost) {
			return PaymentRevenueHistory{}, ErrRevenueUnassessable
		}
		if old == "" || old == RevenueDisputeHold {
			disputes[row.Original.ID][f.AdjustmentID] = f.Kind
		}
	}
	out := PaymentRevenueHistory{Items: []PaymentRevenue{}, AsOf: at, AcceptanceSequence: snapshot.Sequence}
	for _, row := range rows {
		for _, state := range disputes[row.Original.ID] {
			if state == RevenueDisputeLost {
				row.DisputeLost = true
			}
			if state == RevenueDisputeHold {
				row.DisputeHeld = true
			}
		}
		row.NetMinor = row.Original.PaidMinor - row.RefundedMinor
		if row.DisputeLost {
			// A verified full-allocation loss consumes the remaining exposure after
			// cumulative refunds, not another original amount for each envelope.
			row.DisputeLostMinor = row.NetMinor
			row.NetMinor = 0
		}
		if row.DisputeHeld {
			row.DisputeHoldMinor = row.NetMinor
		}
		if err := row.Validate(); err != nil {
			return PaymentRevenueHistory{}, err
		}
		out.Items = append(out.Items, *row)
	}
	resolved := map[string]bool{}
	for _, v := range observations {
		if v.ResolutionOf != "" {
			resolved[v.ResolutionOf] = true
		}
	}
	for _, v := range observations {
		if scopes[v.Scope] && v.ResolutionOf == "" && v.QuarantineReason != "" && !resolved[v.ID] {
			out.ScopedUnresolvedSources++
		}
	}
	sort.Slice(out.Items, func(i, j int) bool { return out.Items[i].Original.Sequence < out.Items[j].Original.Sequence })
	// Fingerprint explicit private source fields; public result JSON is empty.
	sort.Slice(snapshot.Facts, func(i, j int) bool { return snapshot.Facts[i].Sequence < snapshot.Facts[j].Sequence })
	sort.Slice(snapshot.Observations, func(i, j int) bool { return snapshot.Observations[i].ID < snapshot.Observations[j].ID })
	sourceProof := []any{snapshot.Sequence}
	for _, f := range snapshot.Facts {
		sourceProof = append(sourceProof, []any{f.ID, f.Fingerprint, f.Sequence, f.AcceptedAt})
	}
	for _, v := range snapshot.Observations {
		sourceProof = append(sourceProof, []any{v.ID, v.Fingerprint, v.AcceptedAt})
	}
	out.Revision = subscriptionDigest(sourceProof)
	if out.Revision == "" {
		return PaymentRevenueHistory{}, ErrRevenueUnassessable
	}
	if err := ctx.Err(); err != nil {
		return PaymentRevenueHistory{}, err
	}
	return out, nil
}
