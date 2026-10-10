package partnermanager

import (
	"context"
	"fmt"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

// PaidSubscriptionRecords is the billing owner's customer-bound read port.
// It supplies ownership and plan mappings, never proof of current payment.
type PaidSubscriptionRecords interface {
	// GetSubscriptions returns paginated records for the exact selected user.
	GetSubscriptions(context.Context, *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error)
}

// PaidAcquisitionConfig confines admission to one authenticated merchant/mode
// and host-approved plans and recurring intervals. These are host configuration,
// never fields accepted from an enrollment request.
type PaidAcquisitionConfig struct {
	// Scope selects the provider account and test/live mode for every lookup.
	Scope paymentprovider.RevenueScope
	// EligiblePlanIDs identifies native billing plans, not browser labels or slugs.
	EligiblePlanIDs []string
	// Intervals contains exact provider cadence names: day, week, month or year.
	Intervals []string
}

// PaidAcquisition verifies fresh paid periods over existing billing ownership.
// It does not associate records by email, cache access flags, ingest commissions
// or modify a provider subscription. One qualifying current plan is sufficient.
type PaidAcquisition struct {
	// records owns durable customer, subscription and plan relationships.
	records PaidSubscriptionRecords
	// provider authenticates payment and current subscription evidence.
	provider paymentprovider.PaidSubscriptionProvider
	// scope restricts every provider read to the configured merchant and mode.
	scope paymentprovider.RevenueScope
	// plans and intervals are immutable admission allowlists copied at construction.
	plans, intervals map[string]bool
	// clock evaluates exclusive period endings after external I/O.
	clock Clock
}

// NewPaidAcquisition validates complete, explicit commercial admission wiring.
// Missing scope, dependencies or allowlists fail at startup rather than silently
// allowing unpaid enrollment. Construction performs no provider or storage I/O.
func NewPaidAcquisition(records PaidSubscriptionRecords, provider paymentprovider.PaidSubscriptionProvider, clock Clock, cfg PaidAcquisitionConfig) (*PaidAcquisition, error) {
	if nilManagerDependency(records) || nilManagerDependency(provider) || nilManagerDependency(clock) {
		return nil, ErrUnavailable
	}
	if billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{{Provider: cfg.Scope.Provider, AccountID: cfg.Scope.AccountID, LiveMode: cfg.Scope.LiveMode}}) != nil || len(cfg.EligiblePlanIDs) == 0 || len(cfg.EligiblePlanIDs) > 1000 || len(cfg.Intervals) == 0 || len(cfg.Intervals) > 4 {
		return nil, ErrInvalid
	}
	plans, intervals := make(map[string]bool), make(map[string]bool)
	for _, id := range cfg.EligiblePlanIDs {
		if strings.TrimSpace(id) != id || id == "" || len(id) > 128 || plans[id] {
			return nil, ErrInvalid
		}
		plans[id] = true
	}
	for _, interval := range cfg.Intervals {
		switch interval {
		case "day", "week", "month", "year":
		default:
			return nil, ErrInvalid
		}
		if intervals[interval] {
			return nil, ErrInvalid
		}
		intervals[interval] = true
	}
	return &PaidAcquisition{records: records, provider: provider, scope: cfg.Scope, plans: plans, intervals: intervals, clock: clock}, nil
}

// CanAcquirePartnerReferrals accepts only current active, positively paid
// recurring periods bound to the selected customer's native plan and provider
// price. Trial/free/expired/unpaid states return false; incomplete or unavailable
// evidence returns ErrUnavailable. Previously earned commissions are unaffected.
func (a *PaidAcquisition) CanAcquirePartnerReferrals(ctx context.Context, customer string) (bool, error) {
	if a == nil || ctx == nil || strings.TrimSpace(customer) != customer || customer == "" || len(customer) > 256 {
		return false, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	rows, err := a.subscriptions(ctx, customer)
	if err != nil {
		return false, err
	}
	var unavailable error
	for _, record := range rows {
		if record.Integrator != a.scope.Provider || !a.plans[record.PlanID] || !record.IsRecurring() {
			continue
		}
		if record.IntegratorSubscriptionID == "" || record.IntegratorCustomerID == "" || record.ProviderPriceID == "" {
			unavailable = ErrUnavailable
			continue
		}
		proof, err := a.provider.LookupPaidSubscription(ctx, a.scope, record.IntegratorSubscriptionID)
		if err != nil {
			unavailable = fmt.Errorf("%w: %w", ErrUnavailable, err)
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if proof.Scope != a.scope || proof.CustomerID != record.IntegratorCustomerID || proof.SubscriptionID != record.IntegratorSubscriptionID {
			return false, ErrUnavailable
		}
		switch proof.Status {
		case "active":
		case "trialing", "incomplete", "incomplete_expired", "past_due", "unpaid", "canceled", "paused":
			continue
		default:
			return false, ErrUnavailable
		}
		now := a.clock.Now().UTC()
		if len(proof.PaidLines) != 0 && (proof.PaidAt.IsZero() || proof.PaidAt.After(now)) {
			return false, ErrUnavailable
		}
		matchedPrice := false
		for _, line := range proof.PaidLines {
			if line.NetPaidMinor <= 0 || line.PeriodStart.IsZero() || !line.PeriodEnd.After(line.PeriodStart) || line.IntervalCount <= 0 {
				return false, ErrUnavailable
			}
			if line.PriceID != record.ProviderPriceID {
				continue
			}
			matchedPrice = true
			if a.intervals[line.Interval] && !now.Before(line.PeriodStart) && now.Before(line.PeriodEnd) {
				return true, nil
			}
		}
		// A paid provider price without the owning billing mapping can reflect
		// delayed plan reconciliation. Do not label that customer unpaid.
		if len(proof.PaidLines) > 0 && !matchedPrice {
			unavailable = ErrUnavailable
		}
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return false, unavailable
}

// subscriptions reads the complete bounded owning inventory. A changed total,
// duplicate identity or foreign owner is unavailable, not absence or paidness.
func (a *PaidAcquisition) subscriptions(ctx context.Context, customer string) ([]billing.Subscription, error) {
	const pageSize, maximum = 100, 10000
	var out []billing.Subscription
	seen := make(map[string]bool)
	total := -1
	for page := 1; page <= maximum/pageSize; page++ {
		response, err := a.records.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{ForUserIDs: []string{customer}, PerPage: pageSize, Page: page, Order: "created_at_desc"})
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if response == nil || response.Total < 0 || response.Total > maximum || len(response.Subscriptions) > pageSize || (total >= 0 && response.Total != total) {
			return nil, ErrUnavailable
		}
		total = response.Total
		for _, row := range response.Subscriptions {
			if row.ID == "" || seen[row.ID] || row.UserID != customer {
				return nil, ErrUnavailable
			}
			seen[row.ID] = true
			out = append(out, row)
		}
		if len(out) > total {
			return nil, ErrUnavailable
		}
		if len(out) == total {
			return out, nil
		}
		if len(response.Subscriptions) != pageSize {
			return nil, ErrUnavailable
		}
	}
	return nil, ErrUnavailable
}

var _ AcquisitionEligibility = (*PaidAcquisition)(nil)
