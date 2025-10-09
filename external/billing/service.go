package billing

import (
	"context"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// billingEventsRepository is the expected methods needed to
// interact with the database
type billingEventsRepository interface {
	GetTotalBillingEvents(ctx context.Context, req *GetTotalBillingEventsRequest) (int64, error)
	GetBillingEvents(ctx context.Context, req *GetBillingEventsRequest) ([]BillingEvent, error)
	CreateBillingEvent(ctx context.Context, newEvent *BillingEvent) (*BillingEvent, error)
	GetBillingEventByID(ctx context.Context, eventID string) (*BillingEvent, error)
}

// subscriptionRepository is the expected methods needed to
// interact with the database
type subscriptionRepository interface {
	GetTotalSubscriptions(ctx context.Context, req *GetTotalSubscriptionsRequest) (int64, error)
	GetSubscriptions(ctx context.Context, req *GetSubscriptionsRequest) ([]Subscription, error)
	CreateSubscription(ctx context.Context, newSubscription *Subscription) (*Subscription, error)
	GetSubscriptionByID(ctx context.Context, subscriptionID string) (*Subscription, error)
	GetSubscriptionByIntegratorID(ctx context.Context, integratorName, integratorSubscriptionID string) (*Subscription, error)
	UpdateSubscription(ctx context.Context, subscription *Subscription) (*Subscription, error)
	DeleteSubscription(ctx context.Context, subscriptionID string) error
}

// Service represents the billing service
type Service struct {
	billingEventsRepository billingEventsRepository
	subscriptionRepository  subscriptionRepository
}

// NewService returns a new instance of the billing service
func NewService(billingEventsRepository billingEventsRepository, subscriptionRepository subscriptionRepository) *Service {
	return &Service{
		billingEventsRepository: billingEventsRepository,
		subscriptionRepository:  subscriptionRepository,
	}
}

// CreateSubscription creates a new subscription
func (s *Service) CreateSubscription(ctx context.Context, req *CreateSubscriptionRequest) (*CreateSubscriptionResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		newSubscription = &Subscription{
			UserID:                   req.UserID,
			Email:                    toolbox.StringStandardisedToLower(req.Email),
			Status:                   req.Status,
			Integrator:               req.Integrator,
			IntegratorSubscriptionID: req.IntegratorSubscriptionID,
			IntegratorCustomerID:     req.IntegratorCustomerID,
			PlanName:                 req.PlanName,
			PlanID:                   req.PlanID,
			Amount:                   req.Amount,
			Currency:                 req.Currency,
			BillingInterval:          req.BillingInterval,
			NextBillingDate:          req.NextBillingDate,
			AvailableUntilDate:       req.AvailableUntilDate,
			ProviderTrialEndsAt:      req.TrialEndsAt,
			CancelURL:                req.CancelURL,
			UpdateURL:                req.UpdateURL,
			Metadata:                 req.Metadata,
		}
	)

	log.Debug("initiating-create-subscription-request", zap.Any("request", req))

	// Generate ID and timestamps
	newSubscription.GenerateId().SetCreatedAtTimeToNow().SetUpdatedAtTimeToNow()

	createdSubscription, err := s.subscriptionRepository.CreateSubscription(ctx, newSubscription)
	if err != nil {
		log.Error("failed-to-create-subscription-error-creating-subscription", zap.Any("request", req), zap.Error(err))
		return &CreateSubscriptionResponse{}, err
	}

	log.Debug("create-subscription-request-successful", zap.Any("request", req), zap.Any("created-subscription", createdSubscription))

	return &CreateSubscriptionResponse{
		Subscription: createdSubscription,
	}, nil
}

// UpdateSubscription updates an existing subscription
func (s *Service) UpdateSubscription(ctx context.Context, req *UpdateSubscriptionRequest) (*UpdateSubscriptionResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Debug("initiating-update-subscription-request", zap.Any("request", req))

	// Get existing subscription
	subscription, err := s.subscriptionRepository.GetSubscriptionByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-update-subscription-error-getting-subscription", zap.Any("request", req), zap.Error(err))
		return &UpdateSubscriptionResponse{}, err
	}

	// Update fields if provided
	if req.Status != nil {
		subscription.Status = *req.Status
	}
	if req.PlanName != nil {
		subscription.PlanName = *req.PlanName
	}
	if req.PlanID != nil {
		subscription.PlanID = *req.PlanID
	}
	if req.Amount != nil {
		subscription.Amount = *req.Amount
	}
	if req.Currency != nil {
		subscription.Currency = *req.Currency
	}
	if req.BillingInterval != nil {
		subscription.BillingInterval = *req.BillingInterval
	}
	if req.NextBillingDate != nil {
		subscription.NextBillingDate = req.NextBillingDate
	}
	if req.AvailableUntilDate != nil {
		subscription.AvailableUntilDate = req.AvailableUntilDate
	}
	if req.TrialEndsAt != nil {
		subscription.ProviderTrialEndsAt = req.TrialEndsAt
	}
	if req.CancelledAt != nil {
		subscription.ProviderCancelledAt = req.CancelledAt
	}
	if req.CancelURL != nil {
		subscription.CancelURL = *req.CancelURL
	}
	if req.UpdateURL != nil {
		subscription.UpdateURL = *req.UpdateURL
	}
	if req.Metadata != nil {
		subscription.Metadata = req.Metadata
	}

	subscription.SetUpdatedAtTimeToNow()

	updatedSubscription, err := s.subscriptionRepository.UpdateSubscription(ctx, subscription)
	if err != nil {
		log.Error("failed-to-update-subscription-error-updating-subscription", zap.Any("request", req), zap.Error(err))
		return &UpdateSubscriptionResponse{}, err
	}

	log.Debug("update-subscription-request-successful", zap.Any("request", req), zap.Any("updated-subscription", updatedSubscription))

	return &UpdateSubscriptionResponse{
		Subscription: updatedSubscription,
	}, nil
}

// GetSubscriptions returns a list of subscriptions
func (s *Service) GetSubscriptions(ctx context.Context, req *GetSubscriptionsRequest) (*GetSubscriptionsResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// Set defaults
	if req.Order == "" {
		req.Order = "created_at_desc"
	}

	if req.PerPage == 0 {
		req.PerPage = 25
	}

	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count of subscriptions
	getTotalSubscriptionsRequest := &GetTotalSubscriptionsRequest{
		IntegratorName:           req.IntegratorName,
		IntegratorSubscriptionID: req.IntegratorSubscriptionID,
		IntegratorCustomerID:     req.IntegratorCustomerID,
		UserIDs:                  req.ForUserIDs,
		Emails:                   standardisedEmails(req.ForEmails),
		Statuses:                 req.Statuses,
		PlanNameContains:         req.PlanNameContains,
		Currency:                 req.Currency,
		BillingInterval:          req.BillingInterval,
		CreatedAtFrom:            req.CreatedAtFrom,
		CreatedAtTo:              req.CreatedAtTo,
		NextBillingDateFrom:      req.NextBillingDateFrom,
		NextBillingDateTo:        req.NextBillingDateTo,
	}

	totalSubscriptions, err := s.subscriptionRepository.GetTotalSubscriptions(ctx, getTotalSubscriptionsRequest)
	if err != nil {
		log.Error("failed-to-get-subscriptions-request-error-getting-total-subscriptions", zap.Any("request", req), zap.Any("get-total-subscriptions-request", getTotalSubscriptionsRequest), zap.Error(err))
		return &GetSubscriptionsResponse{}, err
	}

	req.TotalCount = int(totalSubscriptions)
	log.Debug("handling-get-subscriptions-request-total-subscriptions-found", zap.Int64("total", totalSubscriptions), zap.Any("request", req))

	subscriptions, err := s.subscriptionRepository.GetSubscriptions(ctx, req)
	if err != nil {
		log.Error("failed-to-get-subscriptions-request-error-getting-subscriptions", zap.Any("request", req), zap.Error(err))
		return &GetSubscriptionsResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, subscriptions, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetSubscriptionsResponse{
		Total:         paginatedResponse.Total,
		TotalPages:    paginatedResponse.TotalPages,
		Subscriptions: paginatedResponse.Resources,
		Page:          paginatedResponse.Page,
		PerPage:       paginatedResponse.ResourcePerPage,
	}, nil
}

// GetSubscriptionByID retrieves a subscription by its internal ID
func (s *Service) GetSubscriptionByID(ctx context.Context, req *GetSubscriptionByIDRequest) (*GetSubscriptionByIDResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Debug("initiating-get-subscription-by-id-request", zap.Any("request", req))

	subscription, err := s.subscriptionRepository.GetSubscriptionByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-get-subscription-by-id-error-getting-subscription", zap.Any("request", req), zap.Error(err))
		return &GetSubscriptionByIDResponse{}, err
	}

	log.Debug("get-subscription-by-id-request-successful", zap.Any("request", req), zap.Any("subscription", subscription))

	return &GetSubscriptionByIDResponse{
		Subscription: subscription,
	}, nil
}

// GetSubscriptionByIntegratorID retrieves a subscription by integrator subscription ID
func (s *Service) GetSubscriptionByIntegratorID(ctx context.Context, req *GetSubscriptionByIntegratorIDRequest) (*GetSubscriptionByIntegratorIDResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Debug("initiating-get-subscription-by-integrator-id-request", zap.Any("request", req))

	subscription, err := s.subscriptionRepository.GetSubscriptionByIntegratorID(ctx, req.IntegratorName, req.IntegratorSubscriptionID)
	if err != nil {
		log.Error("failed-to-get-subscription-by-integrator-id-error-getting-subscription", zap.Any("request", req), zap.Error(err))
		return &GetSubscriptionByIntegratorIDResponse{}, err
	}

	log.Debug("get-subscription-by-integrator-id-request-successful", zap.Any("request", req), zap.Any("subscription", subscription))

	return &GetSubscriptionByIntegratorIDResponse{
		Subscription: subscription,
	}, nil
}

// CancelSubscription cancels a subscription
func (s *Service) CancelSubscription(ctx context.Context, req *CancelSubscriptionRequest) (*CancelSubscriptionResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Debug("initiating-cancel-subscription-request", zap.Any("request", req))

	// Get existing subscription
	subscription, err := s.subscriptionRepository.GetSubscriptionByID(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-cancel-subscription-error-getting-subscription", zap.Any("request", req), zap.Error(err))
		return &CancelSubscriptionResponse{}, err
	}

	// Update status and cancelled at time
	subscription.Status = req.Status
	if req.CancelledAt != nil {
		subscription.ProviderCancelledAt = req.CancelledAt
	}
	subscription.SetUpdatedAtTimeToNow()

	updatedSubscription, err := s.subscriptionRepository.UpdateSubscription(ctx, subscription)
	if err != nil {
		log.Error("failed-to-cancel-subscription-error-updating-subscription", zap.Any("request", req), zap.Error(err))
		return &CancelSubscriptionResponse{}, err
	}

	log.Debug("cancel-subscription-request-successful", zap.Any("request", req), zap.Any("cancelled-subscription", updatedSubscription))

	return &CancelSubscriptionResponse{
		Subscription: updatedSubscription,
	}, nil
}

// DeleteSubscription deletes a subscription
func (s *Service) DeleteSubscription(ctx context.Context, req *DeleteSubscriptionRequest) (*DeleteSubscriptionResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Debug("initiating-delete-subscription-request", zap.Any("request", req))

	err := s.subscriptionRepository.DeleteSubscription(ctx, req.ID)
	if err != nil {
		log.Error("failed-to-delete-subscription-error-deleting-subscription", zap.Any("request", req), zap.Error(err))
		return &DeleteSubscriptionResponse{}, err
	}

	log.Debug("delete-subscription-request-successful", zap.Any("request", req))

	return &DeleteSubscriptionResponse{
		Success: true,
	}, nil
}

// CreateBillingEvent creates a new billing event
func (s *Service) CreateBillingEvent(ctx context.Context, req *CreateBillingEventRequest) (*CreateBillingEventResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		newEvent = &BillingEvent{
			SubscriptionID:           req.SubscriptionID,
			UserID:                   req.UserID,
			EventType:                req.EventType,
			Integrator:               req.Integrator,
			IntegratorEventID:        req.IntegratorEventID,
			IntegratorSubscriptionID: req.IntegratorSubscriptionID,
			Status:                   req.Status,
			Amount:                   req.Amount,
			Currency:                 req.Currency,
			PlanName:                 req.PlanName,
			ReceiptURL:               req.ReceiptURL,
			RawPayload:               req.RawPayload,
			ProviderEventTime:        req.EventTime,
		}
	)

	log.Debug("initiating-create-billing-event-request", zap.Any("request", req))

	// Generate ID and timestamps
	newEvent.GenerateId().SetCreatedAtTimeToNow().SetUpdatedAtTimeToNow()

	createdEvent, err := s.billingEventsRepository.CreateBillingEvent(ctx, newEvent)
	if err != nil {
		log.Error("failed-to-create-billing-event-error-creating-event", zap.Any("request", req), zap.Error(err))
		return &CreateBillingEventResponse{}, err
	}

	log.Debug("create-billing-event-request-successful", zap.Any("request", req), zap.Any("created-event", createdEvent))

	return &CreateBillingEventResponse{
		BillingEvent: createdEvent,
	}, nil
}

// GetBillingEvents returns a list of billing events
func (s *Service) GetBillingEvents(ctx context.Context, req *GetBillingEventsRequest) (*GetBillingEventsResponse, error) {
	var (
		log = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// Set defaults
	if req.Order == "" {
		req.Order = "created_at_desc"
	}

	if req.PerPage == 0 {
		req.PerPage = 25
	}

	if req.Page == 0 {
		req.Page = 1
	}

	// Get total count of billing events
	getTotalBillingEventsRequest := &GetTotalBillingEventsRequest{
		IntegratorName:           req.IntegratorName,
		IntegratorUserID:         req.IntegratorUserID,
		IntegratorSubscriptionID: req.IntegratorSubscriptionID,
		UserIDs:                  req.ForUserIDs,
		EventTypes:               req.EventTypes,
		PlanNameContains:         req.PlanNameContains,
		Currency:                 req.Currency,
		Statuses:                 req.Statuses,
		CreatedAtFrom:            req.CreatedAtFrom,
		CreatedAtTo:              req.CreatedAtTo,
		EventTimeFrom:            req.EventTimeFrom,
		EventTimeTo:              req.EventTimeTo,
	}

	totalEvents, err := s.billingEventsRepository.GetTotalBillingEvents(ctx, getTotalBillingEventsRequest)
	if err != nil {
		log.Error("failed-to-get-billing-events-request-error-getting-total-events", zap.Any("request", req), zap.Any("get-total-events-request", getTotalBillingEventsRequest), zap.Error(err))
		return &GetBillingEventsResponse{}, err
	}

	req.TotalCount = int(totalEvents)
	log.Debug("handling-get-billing-events-request-total-events-found", zap.Int64("total", totalEvents), zap.Any("request", req))

	events, err := s.billingEventsRepository.GetBillingEvents(ctx, req)
	if err != nil {
		log.Error("failed-to-get-billing-events-request-error-getting-events", zap.Any("request", req), zap.Error(err))
		return &GetBillingEventsResponse{}, err
	}

	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{
		PerPage: req.PerPage,
		Page:    req.Page,
	}, events, req.TotalCount)

	if err != nil {
		return nil, err
	}

	return &GetBillingEventsResponse{
		Total:         paginatedResponse.Total,
		TotalPages:    paginatedResponse.TotalPages,
		BillingEvents: paginatedResponse.Resources,
		Page:          paginatedResponse.Page,
		PerPage:       paginatedResponse.ResourcePerPage,
	}, nil
}
