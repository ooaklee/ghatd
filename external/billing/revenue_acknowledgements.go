package billing

import "context"

// RevenueAcknowledgementRepository supplies immutable owning receipts for
// recovery after a consumer decision or feed acknowledgement loses its reply.
// Reading an earlier actor's receipt never grants that actor's current authority.
type RevenueAcknowledgementRepository interface {
	GetRevenueAcknowledgement(context.Context, string, string) (RevenueAcknowledgement, error)
}

func (s *RevenueService) GetRevenueAcknowledgement(ctx context.Context, consumer, fact string) (RevenueAcknowledgement, error) {
	if err := revenueContext(ctx); err != nil {
		return RevenueAcknowledgement{}, err
	}
	if !validRevenueIdentity(consumer) || !validRevenueIdentity(fact) {
		return RevenueAcknowledgement{}, ErrRevenueInvalid
	}
	if s == nil || revenueNil(s.repo) {
		return RevenueAcknowledgement{}, ErrRevenueUnavailable
	}
	repo, ok := s.repo.(RevenueAcknowledgementRepository)
	if !ok || revenueNil(repo) {
		return RevenueAcknowledgement{}, ErrRevenueUnavailable
	}
	return repo.GetRevenueAcknowledgement(ctx, consumer, fact)
}
