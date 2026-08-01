package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
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

// userByEmailFinder is an optional capability implemented by user/v2 for
// association flows where no matching user is an expected outcome.
type userByEmailFinder interface {
	FindUserByEmail(ctx context.Context, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error)
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

// PricerService defines the pricing operations exposed through billing manager.
type PricerService interface {
	GetPricePlans(ctx context.Context, req *pricer.GetPricePlansRequest) (*pricer.GetPricePlansResponse, error)
	GetPricePlanBySlug(ctx context.Context, req *pricer.GetPricePlanBySlugRequest) (*pricer.GetPricePlanBySlugResponse, error)
	GetFeatures(ctx context.Context, req *pricer.GetFeaturesRequest) (*pricer.GetFeaturesResponse, error)
}

// Service orchestrates webhook processing and billing operations
// It uses paymentprovider for webhook verification and billingstore for persistence
type Service struct {
	ProviderRegistry ProviderRegistry
	BillingService   BillingService
	AuditService     AuditService // Optional audit logging
	UserService      UserService  // Optional user service integration
	PricerService    PricerService
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

// WithPricerService adds pricing catalog read capability.
func (s *Service) WithPricerService(pricerSvc PricerService) *Service {
	s.PricerService = pricerSvc
	return s
}

// ProcessBillingProviderWebhooks handles incoming webhooks from payment providers
// This is the main entry point for webhook processing
func (s *Service) ProcessBillingProviderWebhooks(ctx context.Context, req *ProcessBillingProviderWebhooksRequest) error {

	logger := logger.AcquirePackageFrom(ctx, "external/billingmanager")
	var (
		subscriptionId string
	)

	payload, err := s.ProviderRegistry.VerifyAndParseWebhookPayload(ctx, req.ProviderName, req.Request)
	if err != nil {
		logger.Error("failed-to-verify-and-parse-webhook-payload", zap.String("provider", req.ProviderName), zap.Error(err))
		return err
	}

	userID, err := s.resolveUserID(ctx, payload)
	if err != nil {
		logger.Error("failed-to-resolve-user-id", zap.String("provider", req.ProviderName), zap.Error(err))
		return err
	}

	if payload.IsSubscription() {

		subscription, err := s.findOrCreateSubscription(ctx, req.ProviderName, payload, userID)
		if err != nil {
			logger.Error("failed-to-find-or-create-subscription", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.Error(err))...)
			return err
		}

		if err := s.updateSubscriptionFromPayload(ctx, subscription, payload); err != nil {
			logger.Error("failed-to-update-subscription-from-payload", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.String("subscription-id", subscription.ID), zap.Error(err))...)
			return err
		}

		subscriptionId = subscription.ID
	}

	billingEventSuccessfullyCreated := true
	if err := s.createBillingEvent(ctx, subscriptionId, userID, req.ProviderName, payload); err != nil {
		billingEventSuccessfullyCreated = false
		logger.Warn("failed-to-create-billing-event", append(webhookPayloadFieldsForLog(req.ProviderName, userID, payload), zap.String("subscription-id", subscriptionId), zap.Error(err))...)
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

// GetPricingPlans retrieves pricing plans for external BMS clients.
func (s *Service) GetPricingPlans(ctx context.Context, req *GetPricingPlansRequest) (*GetPricingPlansResponse, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access pricing in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active pricing plans
		if req.GetPricePlansRequest == nil {
			req.GetPricePlansRequest = &pricer.GetPricePlansRequest{}
		}
		req.GetPricePlansRequest.IsNotDeleted = true
		req.GetPricePlansRequest.IsPublished = true
		logger.Debug("non-admin-user-requesting-pricing-plans-only-returning-plans-in-valid-state", zap.String("user-id", req.UserID))
	}

	response, err := s.PricerService.GetPricePlans(ctx, req.GetPricePlansRequest)
	if err != nil {
		logger.Error("failed-to-get-pricing-plans", zap.String("user-id", req.UserID), zap.Error(err))
		return nil, err
	}

	return &GetPricingPlansResponse{GetPricePlansResponse: response}, nil
}

// GetPricePlanBySlug retrieves a pricing plan by slug for external BMS clients.
func (s *Service) GetPricePlanBySlug(ctx context.Context, req *GetPricePlanBySlugRequest) (*GetPricePlanBySlugResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	response, err := s.PricerService.GetPricePlanBySlug(ctx, req.GetPricePlanBySlugRequest)
	if err != nil {
		return nil, err
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access pricing in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active pricing plans
		if response.PricePlan.DeletedAt != "" || !isPricePlanPubliclyVisible(response.PricePlan.PublishedAt) {
			logger.Debug("non-admin-user-requesting-pricing-plans-only-returning-plans-in-valid-state", zap.String("user-id", req.UserID))
			return nil, pricer.ErrPricePlanNotFound
		}
	}

	return &GetPricePlanBySlugResponse{GetPricePlanBySlugResponse: response}, nil
}

// GetPricingFeatures retrieves pricing feature catalog items for external BMS clients.
func (s *Service) GetPricingFeatures(ctx context.Context, req *GetPriceFeaturesRequest) (*GetPriceFeaturesResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")

	if s.PricerService == nil {
		logger.Error("pricer-service-not-enabled", zap.String("user-id", req.UserID))
		return nil, ErrBillingManagerPricerServiceNotSet
	}

	isAdmin := s.isRequesterAdmin(ctx, req.UserID, logger)
	if !isAdmin {
		// Non-admin users are not allowed to access price features in certain states, i.e draft, archieved, etc
		// we should override any queries to ensure they can only see active features
		if req.GetFeaturesRequest == nil {
			req.GetFeaturesRequest = &pricer.GetFeaturesRequest{}
		}

		req.GetFeaturesRequest.IsNotDeleted = true
		req.GetFeaturesRequest.IsPublished = true
		logger.Debug("non-admin-user-requesting-pricing-features-only-returning-features-in-valid-state", zap.String("user-id", req.UserID))
	}

	response, err := s.PricerService.GetFeatures(ctx, req.GetFeaturesRequest)
	if err != nil {
		return nil, err
	}

	return &GetPriceFeaturesResponse{GetFeaturesResponse: response}, nil
}

// isPricePlanPubliclyVisible checks if a price plan is publicly visible based on its published_at timestamp.
func isPricePlanPubliclyVisible(publishedAt string) bool {
	publishedAt = strings.TrimSpace(publishedAt)
	if publishedAt == "" {
		return false
	}

	publishedAtTime, err := time.Parse(time.RFC3339, publishedAt)
	if err != nil {
		return false
	}

	return !publishedAtTime.After(time.Now().UTC())
}

// GetUserSubscriptionStatus retrieves a user's subscription status
// This can be called from anywhere in the application
func (s *Service) GetUserSubscriptionStatus(ctx context.Context, req *GetUserSubscriptionStatusRequest) (*GetUserSubscriptionStatusResponse, error) {

	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-subscription-status-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-subscription-status-for-user", append(logFields, zap.Error(err))...)
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
		logger.Error("unexpected-error-while-attempting-to-get-user-subscription-status", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		logger.Info("no-active-subscription-with-user-id-falling-back-to-user-email", logFields...)
		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: req.UserID})
		if err == nil {
			emailSubsResp, _ := s.BillingService.GetSubscriptionsByEmail(ctx, &billing.GetSubscriptionsByEmailRequest{Email: userResp.User.Email})
			if len(emailSubsResp.Subscriptions) > 0 {
				logger.Info("found-email-based-subscription-associating-with-user", append(logFields,
					zap.Bool("email-present", emailPresentForLog(userResp.User.Email)),
					zap.String("email-domain", emailDomainForLog(userResp.User.Email)),
					zap.Int("found-subscriptions", len(emailSubsResp.Subscriptions)),
				)...)
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
		logger.Info("no-active-subscription-found", logFields...)
		return &GetUserSubscriptionStatusResponse{
			SubscriptionStatus: &SubscriptionStatus{
				HasSubscription: false,
				Status:          "none",
			},
		}, nil
	}

	subscription := subscriptionsResp.Subscriptions[0]
	logger.Info("subscription-status-retrieved", append(logFields, zap.String("subscription-id", subscription.ID))...)

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
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-billing-events-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-billing-events-for-user", append(logFields, zap.Error(err))...)
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
		logger.Error("failed-to-retrieve-billing-events-for-user", append(logFields, zap.Error(err))...)
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

	logger.Info("billing-events-retrieved-for-user", append(logFields, zap.Int("total-events", eventsResp.Total), zap.Int("returned-events", len(events)))...)

	return &GetUserBillingEventsResponse{
		Events: events,
		Total:  eventsResp.Total,
	}, nil
}

// GetUserBillingDetail retrieves detailed billing information for a user
func (s *Service) GetUserBillingDetail(ctx context.Context, req *GetUserBillingDetailRequest) (*GetUserBillingDetailResponse, error) {

	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(req.UserID, req.RequestingUserID)
	)

	logger.Info("getting-billing-detail-for-user")

	err := s.isUserAuthorisedToProceedWithUserOperation(ctx, req.UserID, req.RequestingUserID)
	if err != nil {
		logger.Error("failed-to-access-billing-detail-for-user", append(logFields, zap.Error(err))...)
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
		logger.Error("unexpected-error-while-attempting-to-get-user-billing-detail", append(logFields, zap.Error(err))...)
		return nil, err
	}

	// Check if user has any subscriptions
	if subscriptionsResp.Total == 0 || len(subscriptionsResp.Subscriptions) == 0 {
		logger.Info("no-active-subscription-found", logFields...)
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

	logger.Info("billing-detail-retrieved", logFields...)
	return &GetUserBillingDetailResponse{
		BillingDetail: detail,
	}, nil
}

// isRequesterAdmin safely checks if the requester has admin privileges
func (s *Service) isRequesterAdmin(ctx context.Context, userID string, logger *zap.Logger) bool {
	if s.UserService == nil {
		return false
	}

	userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: userID})
	if err != nil || userResp == nil || userResp.User == nil {
		logger.Warn("unable-to-resolve-requester-for-admin-check", zap.String("user-id", userID), zap.Error(err))
		return false
	}

	return userResp.User.IsAdmin()
}

// isUserAuthorisedToProceedWithUserOperation checks if the requesting user is authorised to perform operations on behalf of the target user.
// Returns an error if not authorised or prerequisites are not met.
func (s *Service) isUserAuthorisedToProceedWithUserOperation(ctx context.Context, targetUserId, requestingUserId string) error {
	var (
		logger                = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		logFields []zap.Field = initLogFieldsWithUserIdAndRequestingUserId(targetUserId, requestingUserId)
	)

	if targetUserId == "" {
		logger.Warn("failed-to-get-billing-detail-user-id-is-missing", logFields...)
		return ErrBillingManagerRequiresUserIdIsMissing
	}

	if requestingUserId != "" && requestingUserId != targetUserId {

		userResp, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: requestingUserId})
		if err != nil {
			logger.Warn("failed-to-get-billing-detail-requesting-user-not-found", append(logFields, zap.Error(err))...)
			return ErrBillingManagerRequiresUserIdIsMissing
		}
		if !userResp.User.IsAdmin() {
			logger.Warn("failed-to-get-billing-detail-requesting-user-not-admin", logFields...)
			return ErrBillingManagerUserUnauthorisedToCarryOutOperation
		}

		logger.Info("admin-user-requesting-billing-detail-for-another-user", logFields...)
	}
	return nil
}

// resolveUserID attempts to resolve the user ID associated with a payment provider webhook payload.
// note that this may return an empty user ID if only an email is available in the payload but no user
// is found with that email
func (s *Service) resolveUserID(ctx context.Context, payload *paymentprovider.WebhookPayload) (string, error) {

	var (
		logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		err    error
	)

	subResp, err := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           payload.EventType,
		IntegratorSubscriptionID: payload.SubscriptionID,
	})
	if err == nil {
		logger.Info("found-existing-subscription-using-event-type-and-subscription-id", zap.String("user-id", subResp.Subscription.UserID), zap.String("subscription-id", subResp.Subscription.ID), zap.String("event-type", payload.EventType))
		return subResp.Subscription.UserID, nil
	}

	logger.Info("unable-to-find-existing-subscription-using-event-type-and-subscription-id", zap.String("event-type", payload.EventType))

	if s.UserService != nil && payload.CustomerEmail != "" {
		userResp, userErr := findUserByEmail(ctx, s.UserService, &user.GetUserByEmailRequest{Email: payload.CustomerEmail})
		if userErr == nil {
			logger.Info("found-user-id-falling-back-to-payload-email",
				zap.String("user-id", userResp.User.GetUserId()),
				zap.Bool("payload-email-present", emailPresentForLog(payload.CustomerEmail)),
				zap.String("payload-email-domain", emailDomainForLog(payload.CustomerEmail)),
			)
			return userResp.User.GetUserId(), nil
		}
		if !errors.Is(userErr, user.ErrUserNotFound) {
			return "", userErr
		}
	}

	// if email is missing we need to error out as we have no way to identify the user
	if payload.CustomerEmail == "" {
		logger.Warn("unable-to-identify-user-no-email-in-payload", zap.String("subscription-id", payload.SubscriptionID), zap.String("customer-id", payload.CustomerID))
		return "", ErrBillingManagerNoUserIdentifyingInformationInPayload
	}

	logger.Info("no-user-found-will-store-subscription-with-email-only",
		zap.Bool("email-present", emailPresentForLog(payload.CustomerEmail)),
		zap.String("email-domain", emailDomainForLog(payload.CustomerEmail)),
	)

	return "", nil
}

// findUserByEmail prefers the optional userByEmailFinder capability when the
// underlying user service implements it, so callers receive expected-absence
// semantics. Otherwise it falls back to the strict GetUserByEmail lookup for
// backward compatibility.
func findUserByEmail(ctx context.Context, userService UserService, req *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	if finder, ok := userService.(userByEmailFinder); ok {
		return finder.FindUserByEmail(ctx, req)
	}
	return userService.GetUserByEmail(ctx, req)
}

// findOrCreateSubscription finds an existing subscription or creates a new one
func (s *Service) findOrCreateSubscription(ctx context.Context, providerName string, payload *paymentprovider.WebhookPayload, userID string) (*billing.Subscription, error) {

	var (
		logger = logger.AcquirePackageFrom(ctx, "external/billingmanager")
		err    error
	)

	// Try to find existing subscription by integrator ID
	subResp, err := s.BillingService.GetSubscriptionByIntegratorID(ctx, &billing.GetSubscriptionByIntegratorIDRequest{
		IntegratorName:           providerName,
		IntegratorSubscriptionID: payload.SubscriptionID,
	})
	if err == nil {
		logger.Info("found-existing-subscription-using-integrator-and-subscription-id", zap.String("user-id", subResp.Subscription.UserID), zap.String("subscription-id", subResp.Subscription.ID), zap.String("provider", providerName))
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
		zap.Bool("email-present", emailPresentForLog(payload.CustomerEmail)),
		zap.String("email-domain", emailDomainForLog(payload.CustomerEmail)),
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

	logger.Info("attempting-to-create-new-subscription", logFields...)

	createResp, err := s.BillingService.CreateSubscription(ctx, createReq)
	if err != nil {
		return nil, err
	}
	return createResp.Subscription, nil
}

// updateSubscriptionFromPayload updates a subscription based on webhook data
func (s *Service) updateSubscriptionFromPayload(ctx context.Context, subscription *billing.Subscription, payload *paymentprovider.WebhookPayload) error {

	var (
		logger    = logger.AcquirePackageFrom(ctx, "external/billingmanager")
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
		logger.Debug("updating-next-billing-date", append(logFields, zap.String("next-billing-date", payload.NextBillingDate))...)
		nextBillingDate := parseTimeOrNil(payload.NextBillingDate)
		updateReq.NextBillingDate = nextBillingDate
	}

	if payload.AvailableUntilDate != "" {
		logger.Debug("updating-available-until-date", append(logFields, zap.String("available-until-date", payload.AvailableUntilDate))...)
		availableUntilDate := parseTimeOrNil(payload.AvailableUntilDate)
		updateReq.AvailableUntilDate = availableUntilDate
	}

	// Update plan name if present
	if payload.PlanName != "" {
		logger.Debug("updating-plan-name", append(logFields, zap.String("plan-name", payload.PlanName))...)
		updateReq.PlanName = &payload.PlanName
	}

	// Update URLs if present
	if payload.CancelURL != "" {
		logger.Debug("updating-cancel-url", append(logFields, zap.String("cancel-url", payload.CancelURL))...)
		updateReq.CancelURL = &payload.CancelURL
	}
	if payload.UpdateURL != "" {
		logger.Debug("updating-update-url", append(logFields, zap.String("update-url", payload.UpdateURL))...)
		updateReq.UpdateURL = &payload.UpdateURL
	}

	// Handle cancellation
	if payload.EventType == paymentprovider.EventTypeSubscriptionCancelled {
		logger.Info("marking-subscription-as-cancelled", append(logFields, zap.String("user-id", payload.CustomerID))...)
		now := time.Now()
		updateReq.CancelledAt = &now
	}

	_, err := s.BillingService.UpdateSubscription(ctx, updateReq)
	return err
}

// createBillingEvent creates an audit trail event
func (s *Service) createBillingEvent(ctx context.Context, subscriptionID, userID, providerName string, payload *paymentprovider.WebhookPayload) error {

	logger := logger.AcquirePackageFrom(ctx, "external/billingmanager")
	logFields := []zap.Field{
		zap.String("subscription-id", subscriptionID), zap.String("user-id", userID), zap.String("provider", providerName), zap.String("event-type", payload.EventType), zap.String("event-id", payload.EventID),
	}

	logger.Info("creating-billing-event", logFields...)
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
		Email:                    payload.CustomerEmail,
		ReceiptURL:               payload.ReceiptURL,
		RawPayload:               payload.RawPayload,
		EventTime:                *eventTime,
	}

	_, err := s.BillingService.CreateBillingEvent(ctx, createReq)

	if err != nil {
		logger.Error("failed-to-create-billing-event", append(logFields, zap.Error(err))...)
		return err
	}

	logger.Info("billing-event-created", logFields...)
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

func webhookPayloadFieldsForLog(providerName string, userID string, payload *paymentprovider.WebhookPayload) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", providerName),
		zap.String("user-id", userID),
	}
	if payload == nil {
		return fields
	}

	return append(fields,
		zap.String("event-type", payload.EventType),
		zap.String("event-id", payload.EventID),
		zap.String("subscription-id", payload.SubscriptionID),
		zap.String("customer-id", payload.CustomerID),
		zap.Bool("is-subscription", payload.IsSubscription()),
	)
}
