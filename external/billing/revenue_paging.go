package billing

import "context"

// RevenuePagingRepository separates a bounded sweep position from durable
// consumer acceptance. Advancing a read position does not acknowledge a fact or
// resolve a source. Unresolved items remain available to every later sweep.
// Fact sequence is assigned at acceptance, not provider-effective time; source
// IDs are retained and ordered lexically. Begin each complete sweep at zero/"".
type RevenuePagingRepository interface {
	// PendingRevenueFactsAfter returns unacknowledged facts after the given
	// sequence for a consumer as a bounded sweep read; the service implementation
	// validates consumer, non-negative sequence and 1–200 limit. It never
	// acknowledges returned facts.
	PendingRevenueFactsAfter(context.Context, string, int64, int) ([]RevenueFact, error)
	// UnresolvedRevenueObservationsAfter returns unresolved observations ordered
	// after the given source ID as a bounded sweep; the service implementation
	// validates the optional after ID and 1–200 limit. Unresolved items remain
	// available to later sweeps.
	UnresolvedRevenueObservationsAfter(context.Context, string, int) ([]RevenueObservation, error)
}

// PendingRevenueFactsAfter validates consumer, sequence and 1–200 limit,
// requires the optional paging extension, and forwards the bounded sweep read;
// it never acknowledges returned facts.
func (s *RevenueService) PendingRevenueFactsAfter(ctx context.Context, consumer string, afterSequence int64, limit int) ([]RevenueFact, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if !validRevenueIdentity(consumer) || afterSequence < 0 || limit < 1 || limit > 200 {
		return nil, ErrRevenueInvalid
	}
	repo, ok := s.repo.(RevenuePagingRepository)
	if !ok {
		return nil, ErrRevenueUnavailable
	}
	return repo.PendingRevenueFactsAfter(ctx, consumer, afterSequence, limit)
}

// UnresolvedRevenueObservationsAfter validates the optional after ID and 1–200
// limit, requires the optional paging extension, and forwards the bounded
// unresolved-observation sweep.
func (s *RevenueService) UnresolvedRevenueObservationsAfter(ctx context.Context, afterID string, limit int) ([]RevenueObservation, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if (afterID != "" && !validRevenueIdentity(afterID)) || limit < 1 || limit > 200 {
		return nil, ErrRevenueInvalid
	}
	repo, ok := s.repo.(RevenuePagingRepository)
	if !ok {
		return nil, ErrRevenueUnavailable
	}
	return repo.UnresolvedRevenueObservationsAfter(ctx, afterID, limit)
}
