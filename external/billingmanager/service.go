package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
)

// ProviderRegistry defines the expected methods of a payment provider registry
type ProviderRegistry interface {
	VerifyAndParseWebhookPayload(ctx context.Context, providerName string, req *http.Request) (*paymentprovider.WebhookPayload, error)
}

// AuditService interface for logging billing events (optional)
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// UserService interface for user operations (optional)
type UserService interface {
	GetUserByEmail(ctx context.Context, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error)
	GetUserByID(ctx context.Context, req *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error)
}

// BillingService interface for valid billing service
type BillingService interface {
	GetSubscriptions(ctx context.Context, req *billing.GetSubscriptionsRequest) (*billing.GetSubscriptionsResponse, error)
	GetBillingEvents(ctx context.Context, req *billing.GetBillingEventsRequest) (*billing.GetBillingEventsResponse, error)
	GetSubscriptionByIntegratorID(ctx context.Context, req *billing.GetSubscriptionByIntegratorIDRequest) (*billing.GetSubscriptionByIntegratorIDResponse, error)
	CreateSubscription(ctx context.Context, req *billing.CreateSubscriptionRequest) (*billing.CreateSubscriptionResponse, error)
	UpdateSubscription(ctx context.Context, req *billing.UpdateSubscriptionRequest) (*billing.UpdateSubscriptionResponse, error)
	CreateBillingEvent(ctx context.Context, req *billing.CreateBillingEventRequest) (*billing.CreateBillingEventResponse, error)
	GetSubscriptionsByEmail(ctx context.Context, req *billing.GetSubscriptionsByEmailRequest) (*billing.GetSubscriptionsByEmailResponse, error)
	AssociateSubscriptionsWithUser(ctx context.Context, req *billing.AssociateSubscriptionsWithUserRequest) (*billing.AssociateSubscriptionsWithUserResponse, error)
}

// Service orchestrates webhook processing and billing operations
// It uses paymentprovider for webhook verification and billingstore for persistence
type Service struct {
	ProviderRegistry ProviderRegistry
	BillingService   BillingService
	AuditService     AuditService // Optional audit logging
	UserService      UserService  // Optional user service integration
}

// NewService creates a new billing manager service
func NewService(registry ProviderRegistry, billingService BillingService) *Service {
	return &Service{
		ProviderRegistry: registry,
		BillingService:   billingService,
	}
}

// WithAuditService adds audit logging capability
func (s *Service) WithAuditService(audit AuditService) *Service {
	s.AuditService = audit
	return s
}

// WithUserService adds user service integration
func (s *Service) WithUserService(userSvc UserService) *Service {
	s.UserService = userSvc
	return s
}

// ProcessBillingProviderWebhooks handles incoming webhooks from payment providers
// This is the main entry point for webhook processing
func (s *Service) ProcessBillingProviderWebhooks(ctx context.Context, req *ProcessBillingProviderWebhooksRequest) error {

	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	var (
		subscriptionId string
	)

	payload, err := s.ProviderRegistry.VerifyAndParseWebhookPayload(ctx, req.ProviderName, req.Request)
	if err != nil {
		log.Error("failed-to-verify-and-parse-webhook-payload", zap.String("provider", req.ProviderName), zap.Error(err))
		return err
	}

	userID, err := s.resolveUserID(ctx, payload)
	if err != nil {
		log.Error("failed-to-resolve-user-id", zap.String("provider", req.ProviderName), zap.Error(err))
		return err
	}

	if payload.IsSubscription() {

		subscription, err := s.findOrCreateSubscription(ctx, req.ProviderName, payload, userID)
		if err != nil {
			log.Error("failed-to-find-or-create-subscription", zap.String("provider", req.ProviderName), zap.String("user-id", userID), zap.Any("payload", payload), zap.Error(err))
			return err
		}

		if err := s.updateSubscriptionFromPayload(ctx, subscription, payload); err != nil {
			log.Error("failed-to-update-subscription-from-payload", zap.String("provider", req.ProviderName), zap.String("user-id", userID), zap.String("subscription-id", subscription.ID), zap.Any("payload", payload), zap.Error(err))
			return err
		}

		subscriptionId = subscription.ID
	}

	billingEventSuccessfullyCreated := true
	if err := s.createBillingEvent(ctx, subscriptionId, userID, req.ProviderName, payload); err != nil {
		billingEventSuccessfullyCreated = false
		log.Warn("failed-to-create-billing-event", zap.String("provider", req.ProviderName), zap.String("user-id", userID), zap.String("subscription-id", subscriptionId), zap.Any("payload", payload), zap.Error(err))
	}

	// Optional audit logging
	if s.AuditService != nil {

		eventMessageDetails := ""
		if payload.IsSubscription() {
			eventMessageDetails = fmt.Sprintf("Processed %s webhook for subscription %s", req.ProviderName, payload.SubscriptionID)
		} else {
			eventMessageDetails = fmt.Sprintf("Processed %s webhook for non-subscription event", req.ProviderName)
		}

		event := &AuditEvent{
			EventType:                       payload.EventType,
			UserID:                          userID,
			Details:                         eventMessageDetails,
			OccurredAt:                      time.Now(),
			BillingSubscriptionId:           subscriptionId,
			Provider:                        req.ProviderName,
			BillingEventSuccessfullyCreated: billingEventSuccessfullyCreated,
		}

		// Only include full payload if billing event creation failed
		// This avoids logging sensitive data unnecessarily
		if !billingEventSuccessfullyCreated {
			event.ProviderPayload = payload
		}

		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    audit.AuditActorIdSystem,
			Action:     AuditActionBillingWebhookProcessed,
			TargetId:   payload.EventID,
			TargetType: TargetTypeWebhook,
			Domain:     "billingmanager",
			Details:    event,
		})
	}

	return nil
}

// GetUserSubscriptionStatus retrieves a user's subscription status
// This can be called from anywhere in the application
func (s *Service) GetUserSubscriptionStatus(ctx context.Context, req *GetUserSubscriptionStatusRequest) (*GetUserSubscriptionStatusResponse, error) {

	var (
		log                   = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	log.Info("getting-subscription-status-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		log.Error("failed-to-access-subscription-status-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get subscriptions for the user
	subscriptionsResp, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    1,
		Page:       1,
		Order:      "created_at_desc",
	})
	if err != nil {
		log.Error("unexpected-error-while-attempting-to-get-user-subscription-status", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		log.Info("no-active-subscription-with-user-id-falling-back-to-user-email", logFields...)
		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: req.UserID})
		if err == nil {
			emailSubsResp, _ := s.BillingService.GetSubscriptionsByEmail(ctx, &billing.GetSubscriptionsByEmailRequest{Email: userResp.User.Email})
			if len(emailSubsResp.Subscriptions) > 0 {
				log.Info("found-email-based-subscription-associating-with-user", append(logFields, zap.String("email", userResp.User.Email), zap.Int("found-subscriptions", len(emailSubsResp.Subscriptions)))...)
				// Associate found subscriptions with user
				_, _ = s.BillingService.AssociateSubscriptionsWithUser(ctx, &billing.AssociateSubscriptionsWithUserRequest{
					UserID: req.UserID,
					Email:  userResp.User.Email,
				})

				// Re-query to get updated results
				subscriptionsResp, err = s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
					ForUserIDs: []string{req.UserID},
					PerPage:    1,
					Page:       1,
					Order:      "created_at_desc",
				})
			}
		}
	}

	if subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		log.Info("no-active-subscription-found", logFields...)
		return &GetUserSubscriptionStatusResponse{
			SubscriptionStatus: &SubscriptionStatus{
				HasSubscription: false,
				Status:          "none",
			},
		}, nil
	}

	subscription := subscriptionsResp.Subscriptions[0]
	log.Info("subscription-status-retrieved", append(logFields, zap.String("subscription-id", subscription.ID))...)

	return &GetUserSubscriptionStatusResponse{
		SubscriptionStatus: &SubscriptionStatus{
			HasSubscription:    true,
			Status:             subscription.Status,
			PlanName:           subscription.PlanName,
			Provider:           subscription.Integrator,
			Amount:             subscription.Amount,
			Currency:           subscription.Currency,
			NextBillingDate:    subscription.NextBillingDate,
			AvailableUntilDate: subscription.AvailableUntilDate,
			CancelURL:          subscription.CancelURL,
			UpdateURL:          subscription.UpdateURL,
			IsActive:           subscription.IsActive(),
			IsInGoodStanding:   subscription.IsInGoodStanding(),
		},
	}, nil
}

// GetUserBillingEvents retrieves billing events for a user
func (s *Service) GetUserBillingEvents(ctx context.Context, req *GetUserBillingEventsRequest) (*GetUserBillingEventsResponse, error) {

	var (
		log                   = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	log.Info("getting-billing-events-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		log.Error("failed-to-access-billing-events-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get billing events for the user
	eventsResp, err := s.BillingService.GetBillingEvents(ctx, &billing.GetBillingEventsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    req.PerPage,
		Page:       req.Page,
		Order:      req.Order,
	})
	if err != nil {
		log.Error("failed-to-retrieve-billing-events-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Convert to summary format
	events := make([]EventSummary, len(eventsResp.BillingEvents))
	for i, e := range eventsResp.BillingEvents {
		events[i] = EventSummary{
			EventID:     e.ID,
			EventType:   e.EventType,
			EventTime:   e.ProviderEventTime,
			Amount:      e.Amount,
			Currency:    e.Currency,
			PlanName:    e.PlanName,
			Status:      e.Status,
			ReceiptURL:  e.ReceiptURL,
			Description: formatEventDescription(e.EventType, e.PlanName, e.Status),
		}
	}

	log.Info("billing-events-retrieved-for-user", append(logFields, zap.Int("total-events", eventsResp.Total), zap.Int("returned-events", len(events)))...)

	return &GetUserBillingEventsResponse{
		Events: events,
		Total:  eventsResp.Total,
	}, nil
}

// GetUserBillingDetail retrieves detailed billing information for a user
func (s *Service) GetUserBillingDetail(ctx context.Context, req *GetUserBillingDetailRequest) (*GetUserBillingDetailResponse, error) {

	var (
		log                   = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	log.Info("getting-billing-detail-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		log.Error("failed-to-access-billing-detail-for-user", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Get subscriptions for the user
	subscriptionsResp, err := s.BillingService.GetSubscriptions(ctx, &billing.GetSubscriptionsRequest{
		ForUserIDs: []string{req.UserID},
		PerPage:    1,
		Page:       1,
		Order:      "created_at_desc",
	})
	if err != nil {
		log.Error("unexpected-error-while-attempting-to-get-user-billing-detail", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		log.Info("no-active-subscription-found", logFields...)
		return &GetUserBillingDetailResponse{
			BillingDetail: &BillingDetail{
				HasSubscription: false,
				Summary:         "No active subscription found",
			},
		}, nil
	}

	subscription := subscriptionsResp.Subscriptions[0]

	detail := &BillingDetail{
		HasSubscription: true,
		Provider:        subscription.Integrator,
		Plan:            subscription.PlanName,
		Status:          subscription.Status,
		CancelURL:       subscription.CancelURL,
		UpdateURL:       subscription.UpdateURL,
	}

	// Generate human-readable summary
	detail.Summary = s.generateSubscriptionSummary(&subscription)

	log.Info("billing-detail-retrieved", logFields...)
	return &GetUserBillingDetailResponse{
		BillingDetail: detail,
	}, nil
}

// isUserAuthorisedToProceedWithUserOperation checks if the requesting user is authorised to perform operations on behalf of the target user.
// Returns an error if not authorised or prerequisites are not met.
func (s *Service) isUserAuthorisedToProceedWithUserOperation(ctx context.Context, targetUserId, requestingUserId string) error {
	var (
		log                   = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(targetUserId, requestingUserId)
	)

	if targetUserId == "" {
		log.Warn("failed-to-get-billing-detail-user-id-is-missing", logFields...)
		return errors.New(ErrKeyBillingManagerRequiresUserIdIsMissing)
	}

	if requestingUserId != "" && requestingUserId != targetUserId {

		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: requestingUserId})
		if err != nil {
			log.Warn("failed-to-get-billing-detail-requesting-user-not-found", append(logFields, zap.Error(err))...)
			return errors.New(ErrKeyBillingManagerRequiresUserIdIsMissing)
		}
		if !userResp.User.IsAdmin() {
			log.Warn("failed-to-get-billing-detail-requesting-user-not-admin", logFields...)
			return errors.New(ErrKeyBillingManagerUserUnauthorisedToCarryOutOperation)
		}

		log.Info("admin-user-requesting-billing-detail-for-another-user", logFields...)
	}
	return nil
}

// resolveUserID attempts to resolve the user ID associated with a payment provider webhook payload.
// note that this may return an empty user ID if only an email is available in the payload but no user
// is found with that email
func (s *Service) resolveUserID(ctx context.Context, payload *paymentprovider.WebhookPayload) (string, error) {

	var (
		log = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		err error
	)

	subResp, err := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           payload.EventType,
		IntegratorSubscriptionID: payload.SubscriptionID,
	})
	if err == nil {
		log.Info("found-existing-subscription-using-event-type-and-subscription-id", zap.String("user-id", subResp.Subscription.UserID), zap.String("subscription-id", subResp.Subscription.ID), zap.String("event-type", payload.EventType))
		return subResp.Subscription.UserID, nil
	}

	log.Info("unable-to-find-existing-subscription-using-event-type-and-subscription-id", zap.String("event-type", payload.EventType))

	if s.UserService != nil && payload.CustomerEmail != "" {
		userResp, err := s.UserService.GetUserByEmail(ctx, &user.GetUserByEmailRequest{Email: payload.CustomerEmail})
		if err == nil {
			log.Info("found-user-id-falling-back-to-payload-email", zap.String("user-id", userResp.User.GetUserId()), zap.String("payload-email", payload.CustomerEmail))
			return userResp.User.GetUserId(), nil
		}
	}

	// if email is missing we need to error out as we have no way to identify the user
	if payload.CustomerEmail == "" {
		log.Warn("unable-to-identify-user-no-email-in-payload", zap.String("subscription-id", payload.SubscriptionID), zap.String("customer-id", payload.CustomerID))
		return "", errors.New(ErrKeyBillingManagerNoUserIdentifyingInformationInPayload)
	}

	log.Info("no-user-found-will-store-subscription-with-email-only", zap.String("email", payload.CustomerEmail))

	return "", nil
}

// findOrCreateSubscription finds an existing subscription or creates a new one
func (s *Service) findOrCreateSubscription(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload, userID string) (*billing.Subscription, error) {

	var (
		log = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		err error
	)

	// Try to find existing subscription by integrator ID
	subResp, err := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           providerName,
		IntegratorSubscriptionID: payload.SubscriptionID,
	})
	if err == nil {
		log.Info("found-existing-subscription-using-integrator-and-subscription-id", zap.String("user-id", subResp.Subscription.UserID), zap.String("subscription-id", subResp.Subscription.ID), zap.String("provider", providerName))
		return subResp.Subscription, nil
	}

	// Create new subscription
	nextBillingDate := parseTimeOrNil(payload.NextBillingDate)
	availableUntilDate := parseTimeOrNil(payload.AvailableUntilDate)

	createReq := &billing.CreateSubscriptionRequest{
		UserID:                   userID,
		Email:                    payload.CustomerEmail,
		Status:                   payload.Status,
		Integrator:               providerName,
		IntegratorSubscriptionID: payload.SubscriptionID,
		IntegratorCustomerID:     payload.CustomerID,
		PlanName:                 payload.PlanName,
		Amount:                   payload.Amount,
		Currency:                 payload.Currency,
		NextBillingDate:          nextBillingDate,
		AvailableUntilDate:       availableUntilDate,
		CancelURL:                payload.CancelURL,
		UpdateURL:                payload.UpdateURL,
	}

	logFields := []zap.Field{
		zap.String("provider", providerName),
		zap.String("subscription-id", payload.SubscriptionID),
		zap.String("email", payload.CustomerEmail),
	}

	// Add user-id to logs if present, otherwise note it's email-only
	if userID != "" {
		logFields = append(logFields, zap.String("user-id", userID))
	} else {
		logFields = append(logFields, zap.String("user-id", "email-only-subscription"))
	}

	if nextBillingDate != nil {
		logFields = append(logFields, zap.String("next-billing-date", nextBillingDate.Format(time.RFC3339)))
	} else {
		logFields = append(logFields, zap.String("next-billing-date", "not-set"))
	}
	if availableUntilDate != nil {
		logFields = append(logFields, zap.String("available-until-date", availableUntilDate.Format(time.RFC3339)))
	} else {
		logFields = append(logFields, zap.String("available-until-date", "not-set"))
	}

	log.Info("attempting-to-create-new-subscription", logFields...)

	createResp, err := s.BillingService.CreateSubscription(ctx, createReq)
	if err != nil {
		return nil, err
	}
	return createResp.Subscription, nil
}

// updateSubscriptionFromPayload updates a subscription based on webhook data
func (s *Service) updateSubscriptionFromPayload(ctx context.Context, subscription *billing.Subscription, payload *paymentprovider.WebhookPayload) error {

	var (
		log       = logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
		logFields = []zap.Field{
			zap.String("subscription-id", subscription.ID),
		}
	)

	updateReq := &billing.UpdateSubscriptionRequest{
		ID:     subscription.ID,
		Status: &payload.Status,
	}

	// Update dates if present
	if payload.NextBillingDate != "" {
		log.Debug("updating-next-billing-date", append(logFields, zap.String("next-billing-date", payload.NextBillingDate))...)
		nextBillingDate := parseTimeOrNil(payload.NextBillingDate)
		updateReq.NextBillingDate = nextBillingDate
	}

	if payload.AvailableUntilDate != "" {
		log.Debug("updating-available-until-date", append(logFields, zap.String("available-until-date", payload.AvailableUntilDate))...)
		availableUntilDate := parseTimeOrNil(payload.AvailableUntilDate)
		updateReq.AvailableUntilDate = availableUntilDate
	}

	// Update plan name if present
	if payload.PlanName != "" {
		log.Debug("updating-plan-name", append(logFields, zap.String("plan-name", payload.PlanName))...)
		updateReq.PlanName = &payload.PlanName
	}

	// Update URLs if present
	if payload.CancelURL != "" {
		log.Debug("updating-cancel-url", append(logFields, zap.String("cancel-url", payload.CancelURL))...)
		updateReq.CancelURL = &payload.CancelURL
	}
	if payload.UpdateURL != "" {
		log.Debug("updating-update-url", append(logFields, zap.String("update-url", payload.UpdateURL))...)
		updateReq.UpdateURL = &payload.UpdateURL
	}

	// Handle cancellation
	if payload.EventType == paymentprovider.EventTypeSubscriptionCancelled {
		log.Info("marking-subscription-as-cancelled", append(logFields, zap.String("user-id", payload.CustomerID))...)
		now := time.Now()
		updateReq.CancelledAt = &now
	}

	_, err := s.BillingService.UpdateSubscription(ctx, updateReq)
	return err
}

// createBillingEvent creates an audit trail event
func (s *Service) createBillingEvent(ctx context.Context, subscriptionID, userID, providerName string, payload *paymentprovider.WebhookPayload) error {

	log := logger.AcquireFrom(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))
	logFields := []zap.Field{
		zap.String("subscription-id", subscriptionID), zap.String("user-id", userID), zap.String("provider", providerName), zap.String("event-type", payload.EventType), zap.String("event-id", payload.EventID),
	}

	log.Info("creating-billing-event", logFields...)
	eventTime := parseTimeOrNil(payload.EventTime)
	if eventTime == nil {
		now := time.Now()
		eventTime = &now
	}

	createReq := &billing.CreateBillingEventRequest{
		SubscriptionID:           subscriptionID,
		UserID:                   userID,
		EventType:                payload.EventType,
		Integrator:               providerName,
		IntegratorEventID:        payload.EventID,
		IntegratorSubscriptionID: payload.SubscriptionID,
		Status:                   payload.Status,
		Amount:                   payload.Amount,
		Currency:                 payload.Currency,
		PlanName:                 payload.PlanName,
		ReceiptURL:               payload.ReceiptURL,
		RawPayload:               payload.RawPayload,
		EventTime:                *eventTime,
	}

	_, err := s.BillingService.CreateBillingEvent(ctx, createReq)

	if err != nil {
		log.Error("failed-to-create-billing-event", append(logFields, zap.Error(err))...)
		return err
	}

	log.Info("billing-event-created", logFields...)
	return nil
}

// generateSubscriptionSummary creates a human-readable summary of the subscription
func (s *Service) generateSubscriptionSummary(sub *billing.Subscription) string {
	switch sub.Status {
	case billing.StatusActive:
		if sub.NextBillingDate != nil {
			return fmt.Sprintf("Your %s plan will automatically renew on %s for %.2f %s",
				sub.PlanName, sub.NextBillingDate.Format("02 January, 2006"),
				float64(sub.Amount)/100, sub.Currency)
		}
		return fmt.Sprintf("Your %s plan is active", sub.PlanName)

	case billing.StatusTrialing:
		if sub.NextBillingDate != nil {
			return fmt.Sprintf("Your trial will end on %s. You'll then be charged %.2f %s for %s",
				sub.NextBillingDate.Format("02 January, 2006"),
				float64(sub.Amount)/100, sub.Currency, sub.PlanName)
		}
		return fmt.Sprintf("You're on a trial of %s", sub.PlanName)

	case billing.StatusPastDue:
		return "Your subscription payment is past due. Please update your payment method."

	case billing.StatusCancelled:
		if sub.AvailableUntilDate != nil {
			return fmt.Sprintf("Your subscription was cancelled and will expire on %s",
				sub.AvailableUntilDate.Format("02 January, 2006"))
		}
		return "Your subscription has been cancelled"

	default:
		return fmt.Sprintf("Subscription status: %s", sub.Status)
	}
}

// initLogFieldsWithUserIdAndRequestingUserId initialises log fields with user ID and requesting user ID
func initLogFieldsWithUserIdAndRequestingUserId(userId, requestingUserId string) []zap.Field {
	var logFields []zap.Field
	if userId != "" {
		logFields = append(logFields, zap.String("user-id", userId))
	}
	if requestingUserId != "" {
		logFields = append(logFields, zap.String("requesting-user-id", requestingUserId))
	}
	return logFields
}
