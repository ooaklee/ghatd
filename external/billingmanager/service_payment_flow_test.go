package billingmanager

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

type payloadRegistry struct {
	payload *paymentprovider.WebhookPayload
	err     error
}

func (r *payloadRegistry) VerifyAndParseWebhookPayload(context.Context, string, *http.Request) (*paymentprovider.WebhookPayload, error) {
	if r.err != nil {
		return nil, r.err
	}
	payload := *r.payload
	return &payload, nil
}

type createEventFailingBillingService struct {
	BillingService
	err error
}

type fixedBillingUserService struct {
	user *user.UniversalUser
}

func (s *fixedBillingUserService) GetUserByEmail(context.Context, *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	return &user.GetUserByEmailResponse{User: s.user}, nil
}

func (s *fixedBillingUserService) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{User: s.user}, nil
}

func (s *createEventFailingBillingService) CreateBillingEvent(context.Context, *billing.CreateBillingEventRequest) (*billing.CreateBillingEventResponse, error) {
	return nil, s.err
}

func TestOneOffPaymentCreatesAccessIsIdempotentAndRefundRevokesIt(t *testing.T) {
	manager, billingService, registry := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_purchase", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypePurchase, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, PaymentStatus: paymentprovider.PaymentStatusSucceeded, TransactionID: "txn_1",
		UserReference: "user_1", CustomerID: "customer_1", Status: billing.StatusActive,
		PlanName: "Lifetime", PlanID: "plan_1", PlanSlug: "lifetime", CostID: "cost_1", ProviderPriceID: "price_1",
		Amount: 1200, Currency: "GBP",
	})

	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("first ProcessBillingProviderWebhooks() error = %v", err)
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("duplicate ProcessBillingProviderWebhooks() error = %v", err)
	}
	registry.payload.EventID = "evt_invoice"
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("correlated invoice ProcessBillingProviderWebhooks() error = %v", err)
	}

	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	got := status.SubscriptionStatus
	if !got.HasAccess || got.HasSubscription || !got.IsOneOff || got.TransactionID != "txn_1" {
		t.Fatalf("one-off status = %#v", got)
	}
	if got.PlanID != "plan_1" || got.CostID != "cost_1" || got.ProviderPriceID != "price_1" {
		t.Fatalf("stable plan identifiers = %#v", got)
	}

	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 2 {
		t.Fatalf("events total after duplicate and correlated invoice = %d, want 2", events.Total)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_partial_refund", EventType: paymentprovider.EventTypePaymentPartiallyRefunded,
		PaymentStatus: paymentprovider.PaymentStatusPartiallyRefunded, TransactionID: "txn_1",
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("partial refund ProcessBillingProviderWebhooks() error = %v", err)
	}
	status, err = manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	got = status.SubscriptionStatus
	if !got.HasAccess || got.PaymentStatus != paymentprovider.PaymentStatusSucceeded || got.Status != billing.StatusActive {
		t.Fatalf("partially refunded status = %#v", got)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_refund", EventType: paymentprovider.EventTypePaymentRefunded,
		PaymentStatus: paymentprovider.PaymentStatusRefunded, TransactionID: "txn_1",
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("refund ProcessBillingProviderWebhooks() error = %v", err)
	}

	status, err = manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	got = status.SubscriptionStatus
	if got.HasAccess || got.HasSubscription || got.PaymentStatus != paymentprovider.PaymentStatusRefunded || got.Status != billing.StatusCancelled {
		t.Fatalf("refunded status = %#v", got)
	}
	subscriptions, err := billingService.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if subscriptions.Total != 1 {
		t.Fatalf("access records after refund = %d, want 1", subscriptions.Total)
	}
}

func TestRecurringPaymentReportsAccessAndSubscription(t *testing.T) {
	manager, _, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_subscription", EventType: paymentprovider.EventTypeSubscriptionCreated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_1", UserReference: "user_1", Status: billing.StatusActive,
		PlanName: "Monthly", PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		PaymentStatus: paymentprovider.PaymentStatusSucceeded,
	})
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}
	response, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !response.SubscriptionStatus.HasAccess || !response.SubscriptionStatus.HasSubscription || response.SubscriptionStatus.IsOneOff {
		t.Fatalf("recurring status = %#v", response.SubscriptionStatus)
	}
}

func TestRecurringTrialGrantsAccessAndAcceptsLaterSubscriptionStatus(t *testing.T) {
	manager, _, registry := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_trial", EventType: paymentprovider.EventTypeSubscriptionCreated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_trial", UserReference: "user_1", Status: billing.StatusTrialing,
		PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		PaymentStatus: paymentprovider.PaymentStatusNoPaymentRequired,
	})
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	response, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !response.SubscriptionStatus.HasAccess || !response.SubscriptionStatus.HasSubscription || response.SubscriptionStatus.Status != billing.StatusTrialing || response.SubscriptionStatus.PaymentStatus != paymentprovider.PaymentStatusNoPaymentRequired {
		t.Fatalf("trial status = %#v", response.SubscriptionStatus)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_trial_active", EventType: paymentprovider.EventTypeSubscriptionUpdated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_trial", UserReference: "user_1", Status: billing.StatusActive,
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	response, err = manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if response.SubscriptionStatus.Status != billing.StatusActive || !response.SubscriptionStatus.HasAccess {
		t.Fatalf("active status after subscription update = %#v", response.SubscriptionStatus)
	}
}

func TestStripeLiveDeliveryOrderKeepsSubscriptionTrialAuthoritative(t *testing.T) {
	manager, billingService, registry := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_invoice_paid", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_trial", TransactionID: "in_initial", UserReference: "user_1", CustomerID: "cus_1",
		PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		PaymentStatus: paymentprovider.PaymentStatusSucceeded,
		// Stripe recurring invoices intentionally carry no access Status.
	})
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("invoice ProcessBillingProviderWebhooks() error = %v", err)
	}
	assertSubscriptionCount(t, billingService, 0)

	trialEnd := time.Date(2026, time.August, 23, 5, 27, 43, 0, time.UTC)
	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_subscription_created", EventType: paymentprovider.EventTypeSubscriptionCreated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_trial", TransactionID: "in_initial", UserReference: "user_1", CustomerID: "cus_1",
		Status: billing.StatusTrialing, PaymentStatus: paymentprovider.PaymentStatusNoPaymentRequired,
		PlanName: "Pro", PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		NextBillingDate: trialEnd.Format(time.RFC3339), AvailableUntilDate: trialEnd.Format(time.RFC3339),
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("subscription ProcessBillingProviderWebhooks() error = %v", err)
	}

	// Stripe can deliver Checkout completion after subscription.created. It is
	// a payment ledger event at this point and must not replace trial state or
	// the provider period with the Checkout/invoice timestamp.
	checkoutTime := trialEnd.Add(-14 * 24 * time.Hour)
	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_checkout_completed", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_trial", TransactionID: "cs_initial", UserReference: "user_1", CustomerID: "cus_1",
		Status: billing.StatusActive, PaymentStatus: paymentprovider.PaymentStatusSucceeded,
		PlanName: "Pro", PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		NextBillingDate: checkoutTime.Format(time.RFC3339), AvailableUntilDate: checkoutTime.Format(time.RFC3339),
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatalf("checkout ProcessBillingProviderWebhooks() error = %v", err)
	}

	response, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	got := response.SubscriptionStatus
	if got.Status != billing.StatusTrialing || !got.HasAccess || !got.HasSubscription {
		t.Fatalf("authoritative trial status = %#v", got)
	}
	if got.NextBillingDate == nil || !got.NextBillingDate.Equal(trialEnd) || got.AvailableUntilDate == nil || !got.AvailableUntilDate.Equal(trialEnd) {
		t.Fatalf("authoritative trial period = %#v", got)
	}
	assertSubscriptionCount(t, billingService, 1)

	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 3 {
		t.Fatalf("ledger events = %d, want 3", events.Total)
	}
}

func TestUnrelatedStripeSubscriptionEmailCannotCreateAccess(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_unrelated_subscription", EventType: paymentprovider.EventTypeSubscriptionCreated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_unrelated", CustomerID: "cus_unrelated", CustomerEmail: "member@example.test",
		Status: billing.StatusActive, PaymentStatus: paymentprovider.PaymentStatusSucceeded,
	})
	manager.WithUserService(&fixedBillingUserService{user: &user.UniversalUser{ID: "user_1", Email: "member@example.test"}})

	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatalf("ProcessBillingProviderWebhooks() error = %v", err)
	}
	assertSubscriptionCount(t, billingService, 0)
	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 || events.BillingEvents[0].SubscriptionID != "" {
		t.Fatalf("unrelated event ledger projection = %#v", events)
	}
}

func TestRecurringRefundIsLedgerOnlyAndMetadataPoorLifecycleStillUpdates(t *testing.T) {
	manager, billingService, registry := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_subscription", EventType: paymentprovider.EventTypeSubscriptionCreated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_1", UserReference: "user_1", CustomerID: "cus_1",
		Status: billing.StatusActive, PaymentStatus: paymentprovider.PaymentStatusSucceeded,
		PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
	})
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_invoice", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_1", TransactionID: "pi_renewal", CustomerID: "cus_1",
		PaymentStatus: paymentprovider.PaymentStatusSucceeded,
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_refund", EventType: paymentprovider.EventTypePaymentRefunded,
		TransactionID: "pi_renewal", CustomerID: "cus_1", PaymentStatus: paymentprovider.PaymentStatusRefunded,
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SubscriptionStatus.Status != billing.StatusActive || !status.SubscriptionStatus.HasAccess || status.SubscriptionStatus.PaymentStatus != paymentprovider.PaymentStatusSucceeded {
		t.Fatalf("recurring access after refund = %#v", status.SubscriptionStatus)
	}

	registry.payload = &paymentprovider.WebhookPayload{
		EventID: "evt_subscription_past_due", EventType: paymentprovider.EventTypeSubscriptionUpdated,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_1", Status: billing.StatusPastDue,
	}
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err = manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SubscriptionStatus.Status != billing.StatusPastDue || status.SubscriptionStatus.HasAccess {
		t.Fatalf("metadata-poor lifecycle update = %#v", status.SubscriptionStatus)
	}
	assertSubscriptionCount(t, billingService, 1)
}

func assertSubscriptionCount(t *testing.T, billingService *billing.Service, want int) {
	t.Helper()
	response, err := billingService.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != want {
		t.Fatalf("subscriptions total = %d, want %d", response.Total, want)
	}
}

func TestLedgerOnlyPaymentDoesNotCreatePlanAccess(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_donation", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypeDonation, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, PaymentStatus: paymentprovider.PaymentStatusSucceeded, TransactionID: "txn_donation",
		UserReference: "user_1", Amount: 500, Currency: "GBP",
	})
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("provider")); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := billingService.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if subscriptions.Total != 0 {
		t.Fatalf("ledger-only access records = %d, want 0", subscriptions.Total)
	}
	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 || events.BillingEvents[0].PaymentType != paymentprovider.PaymentTypeDonation {
		t.Fatalf("ledger events = %#v", events)
	}
}

func TestFailedOneOffPaymentDoesNotCreateAccess(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_failed", EventType: paymentprovider.EventTypePaymentFailed,
		PaymentType: paymentprovider.PaymentTypePurchase, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, PaymentStatus: paymentprovider.PaymentStatusFailed, TransactionID: "txn_failed", UserReference: "user_1",
	})
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := billingService.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if subscriptions.Total != 0 {
		t.Fatalf("failed payment access records = %d, want 0", subscriptions.Total)
	}
}

func TestMetadataFreeOneOffSuccessRemainsLedgerOnly(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_unrelated", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypePurchase, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, PaymentStatus: paymentprovider.PaymentStatusSucceeded, TransactionID: "txn_unrelated", UserReference: "user_1",
	})
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}
	subscriptions, err := billingService.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if subscriptions.Total != 0 {
		t.Fatalf("metadata-free access records = %d, want 0", subscriptions.Total)
	}
	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 || events.BillingEvents[0].IntegratorTransactionID != "txn_unrelated" {
		t.Fatalf("ledger events = %#v", events)
	}
}

func TestWebhookPropagatesRealEventPersistenceFailure(t *testing.T) {
	manager, billingService, registry := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_failure", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypeDonation, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, UserReference: "user_1",
	})
	wantErr := errors.New("event persistence unavailable")
	manager = NewService(registry, &createEventFailingBillingService{BillingService: billingService, err: wantErr})

	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("provider")); !errors.Is(err, wantErr) {
		t.Fatalf("ProcessBillingProviderWebhooks() error = %v, want %v", err, wantErr)
	}
}

func newPaymentFlowService(payload *paymentprovider.WebhookPayload) (*Service, *billing.Service, *payloadRegistry) {
	store := &billing.InMemoryRepositoryStore{
		Subscriptions: make(map[string]*billing.Subscription),
		Events:        make(map[string]*billing.BillingEvent),
	}
	repository := billing.NewInMemoryRepository(store)
	billingService := billing.NewService(repository, repository)
	registry := &payloadRegistry{payload: payload}
	return NewService(registry, billingService), billingService, registry
}

func webhookRequest(provider string) *ProcessBillingProviderWebhooksRequest {
	return &ProcessBillingProviderWebhooksRequest{
		ProviderName: provider,
		Request:      httptest.NewRequest(http.MethodPost, "/webhook", nil),
	}
}
