package billing

import "context"

// RevenuePagingRepository separates a bounded sweep position from durable
// consumer acceptance. Advancing a read position does not acknowledge a fact or
// resolve a source. Unresolved items remain available to every later sweep.
// Fact sequence is assigned at acceptance, not provider-effective time; source
// IDs are retained and ordered lexically. Begin each complete sweep at zero/"".
type RevenuePagingRepository interface {
	PendingRevenueFactsAfter(context.Context, string, int64, int) ([]RevenueFact, error)
	UnresolvedRevenueObservationsAfter(context.Context, string, int) ([]RevenueObservation, error)
}

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
