package partnermanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/referral"
)

// ReferralEvidenceCapacity bounds the combined ownership revisions and frozen
// payment bindings joined for one visible page, independently of billing's
// source budget. A smaller page helps only if each relationship fits the bound.
const ReferralEvidenceCapacity = 10000

// ErrReferralEvidenceTooLarge requires a smaller relationship page or an
// explicitly extended owning projection, never a partial financial report.
var ErrReferralEvidenceTooLarge = errors.New("partnermanager/referral-evidence-too-large")

// RevenueReportingConfig is trusted host configuration, never transport input.
// The manager copies Scopes at opt-in and derives all reads from SAME Revenue.
type RevenueReportingConfig struct {
	Scopes       []billing.RevenueScope `json:"-"`
	StatusMaxAge time.Duration          `json:"-"`
}

// revenueReportingSource is the private billing-owner capability required for
// paid evidence: accepted payment history and per-fact subscription status
// reads.
type revenueReportingSource interface {
	// GetPaymentRevenueHistory returns the accepted PaymentRevenueHistory matching
	// the RevenueHistoryQuery, a billing-owner read for paid evidence.
	GetPaymentRevenueHistory(context.Context, billing.RevenueHistoryQuery) (billing.PaymentRevenueHistory, error)
	// GetSubscriptionStatusForFact returns the SubscriptionStatus for the
	// identified revenue fact within the freshness window, a per-fact billing-owner
	// read.
	GetSubscriptionStatusForFact(context.Context, string, time.Duration) (billing.SubscriptionStatus, error)
}

// ReferralPaidEvidence counts one retained relationship's eligible frozen
// original-payment cohort. Paid and NetPositive are distinct; rows are not people.
// Unknown status leaves the exact ActivePaidSubscriptions value absent.
type ReferralPaidEvidence struct {
	Paid                             bool       `json:"paid"`
	NetPositive                      bool       `json:"net_positive"`
	ConfirmedAllocationRows          int        `json:"confirmed_allocation_rows"`
	AwaitingAttributionRows          int        `json:"awaiting_attribution_rows"`
	ActivePaidSubscriptions          *int       `json:"active_paid_subscriptions"`
	ConfirmedActivePaidSubscriptions int        `json:"confirmed_active_paid_subscriptions"`
	ConfirmedTrialingSubscriptions   int        `json:"confirmed_trialing_subscriptions"`
	UnknownSubscriptions             int        `json:"unknown_subscriptions"`
	SubscriptionCoverage             string     `json:"subscription_coverage"`
	SubscriptionObservedFrom         *time.Time `json:"subscription_observed_from,omitempty"`
	SubscriptionObservedTo           *time.Time `json:"subscription_observed_to,omitempty"`
}

// ReferralPaidTotals aggregates exactly the relationship scope stated by its
// containing report. Independent owning snapshots never certify globally atomic
// money/status or unreceived provider deliveries.
type ReferralPaidTotals struct {
	ConfirmedAllocationRows           int        `json:"confirmed_allocation_rows"`
	AwaitingAttributionRows           int        `json:"awaiting_attribution_rows"`
	DeferredStatusReads               int        `json:"deferred_status_reads"`
	ConfirmedPaidRelationships        int        `json:"confirmed_paid_relationships"`
	ConfirmedNetPositiveRelationships int        `json:"confirmed_net_positive_relationships"`
	ActivePaidSubscriptions           *int       `json:"active_paid_subscriptions"`
	ConfirmedActivePaidSubscriptions  int        `json:"confirmed_active_paid_subscriptions"`
	ConfirmedTrialingSubscriptions    int        `json:"confirmed_trialing_subscriptions"`
	UnknownSubscriptions              int        `json:"unknown_subscriptions"`
	SourceAsOf                        *time.Time `json:"source_as_of,omitempty"`
	SourceRevision                    string     `json:"source_revision,omitempty"`
	SubscriptionObservedFrom          *time.Time `json:"subscription_observed_from,omitempty"`
	SubscriptionObservedTo            *time.Time `json:"subscription_observed_to,omitempty"`
	SubscriptionRevision              string     `json:"subscription_revision,omitempty"`
	UpstreamDeliveryCoverage          string     `json:"upstream_delivery_coverage"`
}

// WithRevenueReporting opts summaries into billing/status reads without a
// second owner. Missing capability fails configuration; absent opt-in preserves
// honest not_evaluated coverage. Freshness is explicitly host-approved1s..24h.
func (m *Manager) WithRevenueReporting(config RevenueReportingConfig) (*Manager, error) {
	if m == nil || nilManagerDependency(m.deps.Revenue) {
		return nil, ErrUnavailable
	}
	if billing.ValidateRevenueHistoryScopes(config.Scopes) != nil || config.StatusMaxAge < time.Second || config.StatusMaxAge > 24*time.Hour {
		return nil, ErrInvalid
	}
	owner, ok := m.deps.Revenue.(revenueReportingSource)
	if !ok || nilManagerDependency(owner) {
		return nil, ErrUnavailable
	}
	config.Scopes = append([]billing.RevenueScope(nil), config.Scopes...)
	m.revenueReporting = &config
	return m, nil
}

// sourceCohort reports whether at falls in the half-open cohort [From,To); nil
// bounds are unbounded on that side.
func sourceCohort(at time.Time, q ReferralSummaryQuery) bool {
	return (q.From == nil || !at.Before(*q.From)) && (q.To == nil || at.Before(*q.To))
}

// eligiblePaidBinding reports whether an original payment fact satisfies frozen
// binding terms: positive amount, matching currency/exponent, a plan in
// EligiblePlanIDs, and effective time before any recurring end. Other facts are
// not eligible.
func eligiblePaidBinding(original billing.RevenueFact, binding referral.PaymentAttribution) bool {
	terms := binding.Terms
	if original.PaidMinor <= 0 || original.Currency != terms.Currency || original.CurrencyExponent != terms.CurrencyExponent || (terms.RecurrenceEndsAt != nil && !original.EffectiveAt.Before(*terms.RecurrenceEndsAt)) {
		return false
	}
	for _, id := range terms.EligiblePlanIDs {
		if id == original.PlanID {
			return true
		}
	}
	return false
}

// statusTimeRange widens the [*from,*to) observation window to include at,
// initializing missing bounds and never shrinking existing ones.
func statusTimeRange(from, to **time.Time, at time.Time) {
	if *from == nil || at.Before(**from) {
		value := at
		*from = &value
	}
	if *to == nil || at.After(**to) {
		value := at
		*to = &value
	}
}

// joinReferralPaidEvidence fetches attribution snapshots for the page, computes
// paid evidence, then rereads each snapshot and returns ErrStaleWrite if any
// fingerprint changed during the independent revenue reads.
func (m *Manager) joinReferralPaidEvidence(ctx context.Context, partner string, page referral.RelationshipPage, q ReferralSummaryQuery, out *ReferralSummaryPage) error {
	snapshots := make([]referral.AttributionSnapshot, len(page.Items))
	for i, item := range page.Items {
		snapshot, err := m.deps.Referral.GetAttributionSnapshot(ctx, item.ReferredCustomer)
		if err != nil {
			return err
		}
		snapshots[i] = snapshot
	}
	if err := m.calculateReferralPaidEvidence(ctx, partner, page, q, snapshots, out); err != nil {
		return err
	}
	for i, item := range page.Items {
		confirm, err := m.deps.Referral.GetAttributionSnapshot(ctx, item.ReferredCustomer)
		if err != nil {
			return err
		}
		if confirm.ReferredCustomer != item.ReferredCustomer || confirm.Fingerprint != snapshots[i].Fingerprint {
			return referral.ErrStaleWrite
		}
	}
	return ctx.Err()
}

// SubscriptionStatusReadCapacity bounds optional retained status I/O per read.
// Further candidates stay unknown; paid-source counts still cover the full set.
const SubscriptionStatusReadCapacity = 200

// calculateReferralPaidEvidence joins frozen attribution snapshots and binding
// terms to confirmed revenue history within configured scopes, enforcing per-
// item capacity budgets and rejecting duplicate principals, malformed facts or
// mismatched bindings with ErrUnavailable/ErrStaleWrite. Subscription status is
// read for at most SubscriptionStatusReadCapacity candidates; failures beyond
// capacity or on error keep the subscription unknown rather than failing the
// report, and coverage fields plus a SHA-256 revision over the status proofs
// describe the outcome.
func (m *Manager) calculateReferralPaidEvidence(ctx context.Context, partner string, page referral.RelationshipPage, q ReferralSummaryQuery, snapshots []referral.AttributionSnapshot, out *ReferralSummaryPage) error {
	owner, ok := m.deps.Revenue.(revenueReportingSource)
	if !ok || nilManagerDependency(owner) {
		return ErrUnavailable
	}
	config := m.revenueReporting
	if config == nil {
		return ErrUnavailable
	}
	allowedScopes := map[billing.RevenueScope]bool{}
	for _, scope := range config.Scopes {
		allowedScopes[scope] = true
	}
	byPrincipal := map[string]int{}
	if len(snapshots) != len(page.Items) || len(out.Items) != len(page.Items) {
		return ErrUnavailable
	}
	budget := 0
	for i, item := range page.Items {
		if _, duplicate := byPrincipal[item.ReferredCustomer]; duplicate {
			return ErrUnavailable
		}
		byPrincipal[item.ReferredCustomer] = i
		snapshot := snapshots[i]
		if snapshot.ReferredCustomer != item.ReferredCustomer || snapshot.Fingerprint == "" || (snapshot.Head.PartnerID == partner) != item.Current {
			return referral.ErrStaleWrite
		}
		budget += len(snapshot.History) + len(snapshot.Bindings)
		if budget > ReferralEvidenceCapacity {
			return ErrReferralEvidenceTooLarge
		}
		snapshots[i] = snapshot
		out.Items[i].PaidEvidence = &ReferralPaidEvidence{SubscriptionCoverage: "no_current_candidates"}
	}
	metrics := &ReferralPaidTotals{UpstreamDeliveryCoverage: "unreceived_deliveries_not_assessed"}
	out.PaidCoverage = metrics
	if len(page.Items) == 0 {
		zero := 0
		metrics.ActivePaidSubscriptions = &zero
		out.SourceCoverage = "no_visible_relationships"
		out.SubscriptionCoverage = "no_current_candidates"
		return ctx.Err()
	}
	query := billing.RevenueHistoryQuery{Scopes: append([]billing.RevenueScope(nil), config.Scopes...), Principals: []string{}}
	for _, item := range page.Items {
		query.Principals = append(query.Principals, item.ReferredCustomer)
	}
	history, err := owner.GetPaymentRevenueHistory(ctx, query)
	if err != nil {
		return err
	}
	if history.Items == nil || history.AsOf.IsZero() || history.Revision == "" || history.AcceptanceSequence < 0 || history.ScopedUnresolvedSources < 0 || len(history.Items) > billing.RevenueHistoryCapacity {
		return ErrUnavailable
	}
	metrics.SourceAsOf, metrics.SourceRevision = &history.AsOf, history.Revision
	out.SourceCoverage = "confirmed_accepted_history"
	// Unassigned quarantines have no verified relationship owner. Do not expose
	// their counts or presence through partner reporting. Confirmed history
	// never certifies completeness of unreceived or unassigned deliveries.
	bindings := make([]map[string]referral.PaymentAttribution, len(snapshots))
	periods := make([]map[string]referral.OwnershipPeriod, len(page.Items))
	for i, snapshot := range snapshots {
		bindings[i] = map[string]referral.PaymentAttribution{}
		periods[i] = map[string]referral.OwnershipPeriod{}
		for _, period := range page.Items[i].Periods {
			periods[i][period.ReferralID] = period
		}
		for _, b := range snapshot.Bindings {
			if b.ProgramID != referral.ProgramID || b.ReferredCustomer != page.Items[i].ReferredCustomer || b.PaymentID == "" || bindings[i][b.PaymentID].PaymentID != "" {
				return ErrUnavailable
			}
			bindings[i][b.PaymentID] = b
		}
	}
	type subscriptionCandidate struct {
		item     int
		original billing.RevenueFact
	}
	candidates := map[string]subscriptionCandidate{}
	seenFacts := map[string]bool{}
	for _, payment := range history.Items {
		if err := ctx.Err(); err != nil {
			return err
		}
		f := payment.Original
		i, exists := byPrincipal[f.PrincipalID]
		if !exists || !allowedScopes[f.Scope] || payment.Validate() != nil || seenFacts[f.ID] {
			return ErrUnavailable
		}
		seenFacts[f.ID] = true
		if !sourceCohort(f.EffectiveAt, q) {
			continue
		}
		evidence := out.Items[i].PaidEvidence
		binding, exists := bindings[i][f.ID]
		if !exists {
			if f.PaidMinor > 0 {
				evidence.AwaitingAttributionRows++
			}
			continue
		}
		if binding.PartnerID != partner {
			continue
		}
		period, exists := periods[i][binding.ReferralID]
		if !exists || binding.ReferredCustomer != f.PrincipalID || !binding.EffectiveAt.Equal(f.EffectiveAt) || !reflect.DeepEqual(binding.Terms, period.Terms) {
			return ErrUnavailable
		}
		if !eligiblePaidBinding(f, binding) {
			continue
		}
		evidence.Paid = true
		evidence.ConfirmedAllocationRows++
		if payment.NetMinor <= 0 {
			continue
		}
		evidence.NetPositive = true
		if !page.Items[i].Current {
			continue
		}
		key := billing.SubscriptionStatusIdentity(f.Scope, f.SubscriptionID)
		if old, exists := candidates[key]; exists {
			if old.item != i || old.original.PrincipalID != f.PrincipalID || old.original.ProviderCustomerID != f.ProviderCustomerID {
				return billing.ErrRevenueUnassessable
			}
			if old.original.Sequence > f.Sequence {
				continue
			}
		}
		candidates[key] = subscriptionCandidate{item: i, original: f}
	}
	keys := make([]string, 0, len(candidates))
	for key := range candidates {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	statusProof := []any{}
	for index, key := range keys {
		candidate := candidates[key]
		evidence := out.Items[candidate.item].PaidEvidence
		f := candidate.original
		if index >= SubscriptionStatusReadCapacity {
			evidence.UnknownSubscriptions++
			metrics.DeferredStatusReads++
			statusProof = append(statusProof, []any{key, "deferred"})
			continue
		}
		status, err := owner.GetSubscriptionStatusForFact(ctx, f.ID, config.StatusMaxAge)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Every failed optional lifecycle read stays unknown; even an outage
			// preserves separately verified source/commission history.
			evidence.UnknownSubscriptions++
			statusProof = append(statusProof, []any{key, "unknown"})
			continue
		}
		p := status.Preparation
		if status.Validate() != nil || p.Scope != f.Scope || p.PrincipalID != f.PrincipalID || p.SubscriptionID != f.SubscriptionID || p.ProviderCustomerID != f.ProviderCustomerID {
			return ErrUnavailable
		}
		statusTimeRange(&evidence.SubscriptionObservedFrom, &evidence.SubscriptionObservedTo, status.ObservedAt)
		statusTimeRange(&metrics.SubscriptionObservedFrom, &metrics.SubscriptionObservedTo, status.ObservedAt)
		if status.Status == "active" {
			evidence.ConfirmedActivePaidSubscriptions++
		}
		if status.Status == "trialing" {
			evidence.ConfirmedTrialingSubscriptions++
		}
		statusProof = append(statusProof, []any{key, status.Fingerprint, status.Revision, status.ObservedAt})
	}
	for i, item := range out.Items {
		e := item.PaidEvidence
		metrics.ConfirmedAllocationRows += e.ConfirmedAllocationRows
		metrics.AwaitingAttributionRows += e.AwaitingAttributionRows
		if e.Paid {
			metrics.ConfirmedPaidRelationships++
		}
		if e.NetPositive {
			metrics.ConfirmedNetPositiveRelationships++
		}
		metrics.ConfirmedActivePaidSubscriptions += e.ConfirmedActivePaidSubscriptions
		metrics.UnknownSubscriptions += e.UnknownSubscriptions
		metrics.ConfirmedTrialingSubscriptions += e.ConfirmedTrialingSubscriptions
		if !page.Items[i].Current {
			e.SubscriptionCoverage = "retained_history_only"
			continue
		}
		if e.UnknownSubscriptions > 0 {
			e.SubscriptionCoverage = "unknown_current_status"
		} else {
			value := e.ConfirmedActivePaidSubscriptions
			e.ActivePaidSubscriptions = &value
			if e.SubscriptionObservedFrom != nil {
				e.SubscriptionCoverage = "fresh_retained_status"
			}
		}
	}
	if metrics.UnknownSubscriptions == 0 {
		value := metrics.ConfirmedActivePaidSubscriptions
		metrics.ActivePaidSubscriptions = &value
		out.SubscriptionCoverage = "fresh_or_no_current_candidates"
	} else {
		out.SubscriptionCoverage = "unknown_current_status"
	}
	statusJSON, err := json.Marshal(struct {
		Program, Partner string
		Evidence         []any
	}{referral.ProgramID, partner, statusProof})
	if err != nil {
		return ErrUnavailable
	}
	digest := sha256.Sum256(statusJSON)
	metrics.SubscriptionRevision = hex.EncodeToString(digest[:])
	return ctx.Err()
}
