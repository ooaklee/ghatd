package billingmanager

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/paymentprovider"
)

func TestSubscriptionLifecycleProjectsCommercialTermsWithoutChangingLedgerAmount(t *testing.T) {
	amount := int64(1800)
	currency := "USD"
	interval := "month"
	intervalCount := int64(1)
	quantity := int64(1)
	eventTime := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	trialEnd := eventTime.Add(14 * 24 * time.Hour)
	payload := recurringLifecyclePayload("evt_created", eventTime, billing.StatusTrialing, "price_1")
	payload.PaymentStatus = paymentprovider.PaymentStatusNoPaymentRequired
	payload.Amount = 0
	payload.Currency = currency
	payload.NextBillingDate = trialEnd.Format(time.RFC3339)
	payload.TrialEndsAt = trialEnd.Format(time.RFC3339)
	payload.SubscriptionTerms = paymentprovider.SubscriptionTerms{
		Observed: true, Amount: &amount, Currency: &currency, BillingInterval: &interval,
		BillingIntervalCount: &intervalCount, Quantity: &quantity, ProviderPriceID: stringPointer("price_1"),
	}
	manager, billingService, _ := newPaymentFlowService(payload)
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}

	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	got := status.SubscriptionStatus
	if !got.AmountKnown || got.Amount != 1800 || got.Currency != "USD" || got.BillingInterval != "month" || got.BillingIntervalCount != 1 || got.Quantity != 1 {
		t.Fatalf("structured commercial terms = %#v", got)
	}
	if got.TrialEndsAt == nil || !got.TrialEndsAt.Equal(trialEnd) {
		t.Fatalf("trial end = %#v", got.TrialEndsAt)
	}
	detail, err := manager.GetUserBillingDetail(context.Background(), &GetUserBillingDetailRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(detail.BillingDetail.Summary, "0.00") || strings.Contains(detail.BillingDetail.Summary, "charged") {
		t.Fatalf("summary promises an incorrect amount: %q", detail.BillingDetail.Summary)
	}

	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 || events.BillingEvents[0].Amount != 0 || events.BillingEvents[0].Currency != "USD" {
		t.Fatalf("ledger event = %#v", events)
	}
}

func TestDuplicateLifecycleRepairsLegacyProjectionWithoutDuplicatingLedger(t *testing.T) {
	amount := int64(1800)
	eventTime := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	payload := recurringLifecyclePayload("evt_created", eventTime, billing.StatusTrialing, "price_1")
	payload.SubscriptionTerms = recurringTerms(&amount, "price_1")
	manager, billingService, registry := newPaymentFlowService(payload)
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	subscription := getOnlySubscription(t, billingService)
	zero := int64(0)
	unknown := false
	zeroTime := time.Time{}
	if _, err := billingService.UpdateSubscription(context.Background(), &billing.UpdateSubscriptionRequest{
		ID: subscription.ID, Amount: &zero, AmountKnown: &unknown, ProviderUpdatedAt: &zeroTime,
	}); err != nil {
		t.Fatal(err)
	}

	registry.payload = payload
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	repaired := getOnlySubscription(t, billingService)
	if !repaired.AmountKnown || repaired.Amount != 1800 {
		t.Fatalf("repaired subscription = %#v", repaired)
	}
	events, err := billingService.GetBillingEvents(context.Background(), &billing.GetBillingEventsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if events.Total != 1 {
		t.Fatalf("ledger events = %d, want 1", events.Total)
	}
}

func TestOlderLifecycleCannotRollbackNewerTermsOrStatus(t *testing.T) {
	oldAmount := int64(1800)
	newAmount := int64(2400)
	oldTime := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	oldPayload := recurringLifecyclePayload("evt_old", oldTime, billing.StatusTrialing, "price_old")
	oldPayload.SubscriptionTerms = recurringTerms(&oldAmount, "price_old")
	manager, billingService, registry := newPaymentFlowService(oldPayload)
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	newPayload := recurringLifecyclePayload("evt_new", newTime, billing.StatusActive, "price_new")
	newPayload.EventType = paymentprovider.EventTypeSubscriptionUpdated
	newPayload.SubscriptionTerms = recurringTerms(&newAmount, "price_new")
	registry.payload = newPayload
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	registry.payload = oldPayload
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	got := getOnlySubscription(t, billingService)
	if got.Status != billing.StatusActive || got.Amount != 2400 || !got.AmountKnown || got.ProviderPriceID != "price_new" || !got.ProviderUpdatedAt.Equal(newTime) {
		t.Fatalf("subscription rolled back = %#v", got)
	}
}

func TestObservedUnknownTermsClearStaleAmountWhileAbsentTermsPreserveIt(t *testing.T) {
	amount := int64(1800)
	createdAt := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	payload := recurringLifecyclePayload("evt_created", createdAt, billing.StatusActive, "price_1")
	payload.SubscriptionTerms = recurringTerms(&amount, "price_1")
	manager, billingService, registry := newPaymentFlowService(payload)
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	absent := recurringLifecyclePayload("evt_absent", createdAt.Add(time.Minute), billing.StatusActive, "price_1")
	absent.EventType = paymentprovider.EventTypeSubscriptionUpdated
	registry.payload = absent
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if got := getOnlySubscription(t, billingService); got.Amount != 1800 || !got.AmountKnown {
		t.Fatalf("absent terms overwrote known amount = %#v", got)
	}

	unknown := recurringLifecyclePayload("evt_unknown", createdAt.Add(2*time.Minute), billing.StatusActive, "price_1")
	unknown.EventType = paymentprovider.EventTypeSubscriptionUpdated
	unknown.SubscriptionTerms = paymentprovider.SubscriptionTerms{Observed: true}
	registry.payload = unknown
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	got := getOnlySubscription(t, billingService)
	if got.AmountKnown || got.Amount != 0 {
		t.Fatalf("observed unknown terms left a stale amount = %#v", got)
	}
}

func TestKnownFreeAndLegacyRecurringAmountsRemainPresenceAware(t *testing.T) {
	zero := int64(0)
	freePayload := recurringLifecyclePayload("evt_free", time.Now().UTC(), billing.StatusTrialing, "price_free")
	freePayload.SubscriptionTerms = recurringTerms(&zero, "price_free")
	manager, _, _ := newPaymentFlowService(freePayload)
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}
	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.SubscriptionStatus.AmountKnown || status.SubscriptionStatus.Amount != 0 {
		t.Fatalf("known-free status = %#v", status.SubscriptionStatus)
	}
	encoded, err := json.Marshal(status.SubscriptionStatus)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"amount":0`) || !strings.Contains(string(encoded), `"amount_known":true`) {
		t.Fatalf("known-free JSON = %s", encoded)
	}
	detail, err := manager.GetUserBillingDetail(context.Background(), &GetUserBillingDetailRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	detailJSON, err := json.Marshal(detail.BillingDetail)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(detailJSON), `"amount":0`) || !strings.Contains(string(detailJSON), `"amount_known":true`) {
		t.Fatalf("known-free billing-detail JSON = %s", detailJSON)
	}

	legacyPayload := recurringLifecyclePayload("evt_legacy", time.Time{}, billing.StatusActive, "price_legacy")
	legacyPayload.SubscriptionStateAuthoritative = false
	legacyPayload.Amount = 1500
	legacyPayload.Currency = "GBP"
	legacyManager, _, _ := newPaymentFlowService(legacyPayload)
	if err := legacyManager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("legacy-provider")); err != nil {
		t.Fatal(err)
	}
	legacyStatus, err := legacyManager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !legacyStatus.SubscriptionStatus.AmountKnown || legacyStatus.SubscriptionStatus.Amount != 1500 || legacyStatus.SubscriptionStatus.Currency != "GBP" {
		t.Fatalf("legacy recurring terms = %#v", legacyStatus.SubscriptionStatus)
	}
}

func TestPaidZeroTrialCheckoutBeforeLifecycleKeepsTrialAndLearnsRecurringTerms(t *testing.T) {
	checkoutTime := time.Date(2026, time.August, 9, 12, 0, 0, 0, time.UTC)
	checkout := recurringLifecyclePayload("evt_checkout", checkoutTime, billing.StatusTrialing, "price_1")
	checkout.EventType = paymentprovider.EventTypeSubscriptionCreated
	checkout.SubscriptionStateAuthoritative = false
	checkout.PaymentStatus = paymentprovider.PaymentStatusSucceeded
	checkout.Amount = 0
	checkout.Currency = "USD"
	manager, _, registry := newPaymentFlowService(checkout)
	request := webhookRequest("stripe")
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SubscriptionStatus.Status != billing.StatusTrialing || status.SubscriptionStatus.AmountKnown {
		t.Fatalf("checkout bootstrap = %#v", status.SubscriptionStatus)
	}

	amount := int64(1800)
	lifecycle := recurringLifecyclePayload("evt_lifecycle", checkoutTime.Add(time.Minute), billing.StatusTrialing, "price_1")
	lifecycle.SubscriptionTerms = recurringTerms(&amount, "price_1")
	registry.payload = lifecycle
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	status, err = manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if status.SubscriptionStatus.Status != billing.StatusTrialing || !status.SubscriptionStatus.AmountKnown || status.SubscriptionStatus.Amount != 1800 {
		t.Fatalf("authoritative lifecycle = %#v", status.SubscriptionStatus)
	}
}

func TestOneOffAccessKeepsTransactionAmountAndCurrency(t *testing.T) {
	manager, _, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{
		EventID: "evt_purchase_terms", EventType: paymentprovider.EventTypePaymentSucceeded,
		PaymentType: paymentprovider.PaymentTypePurchase, BillingKind: paymentprovider.BillingKindOneTime,
		IsOneOff: true, PaymentStatus: paymentprovider.PaymentStatusSucceeded, Status: billing.StatusActive,
		TransactionID: "pi_1", UserReference: "user_1", PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: "price_1",
		Amount: 799, Currency: "GBP",
	})
	if err := manager.ProcessBillingProviderWebhooks(context.Background(), webhookRequest("stripe")); err != nil {
		t.Fatal(err)
	}
	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.SubscriptionStatus.AmountKnown || status.SubscriptionStatus.Amount != 799 || status.SubscriptionStatus.Currency != "GBP" {
		t.Fatalf("one-off terms = %#v", status.SubscriptionStatus)
	}
}

func TestLegacyStoredNonzeroAmountIsExposedAsKnown(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{})
	_, err := billingService.CreateSubscription(context.Background(), &billing.CreateSubscriptionRequest{
		UserID: "user_1", Status: billing.StatusActive, Integrator: "provider", IntegratorSubscriptionID: "sub_legacy",
		BillingKind: string(paymentprovider.BillingKindRecurring), PaymentType: paymentprovider.PaymentTypeSubscription,
		PlanName: "Legacy", Amount: 1200, Currency: "USD", AmountKnown: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	status, err := manager.GetUserSubscriptionStatus(context.Background(), &GetUserSubscriptionStatusRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	if !status.SubscriptionStatus.AmountKnown || status.SubscriptionStatus.Amount != 1200 {
		t.Fatalf("legacy stored amount = %#v", status.SubscriptionStatus)
	}
}

type invoicePreviewProviderStub struct {
	preview *paymentprovider.UpcomingInvoicePreview
	err     error
	wait    bool
	request *paymentprovider.UpcomingInvoicePreviewRequest
}

func (p *invoicePreviewProviderStub) CreateUpcomingInvoicePreview(ctx context.Context, request *paymentprovider.UpcomingInvoicePreviewRequest) (*paymentprovider.UpcomingInvoicePreview, error) {
	p.request = request
	if p.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.preview, p.err
}

type invoicePreviewRegistryStub struct {
	provider paymentprovider.UpcomingInvoicePreviewProvider
	err      error
}

func (r *invoicePreviewRegistryStub) GetUpcomingInvoicePreviewProvider(string) (paymentprovider.UpcomingInvoicePreviewProvider, error) {
	return r.provider, r.err
}

func TestBillingDetailBestEffortUpcomingInvoiceEstimate(t *testing.T) {
	manager, billingService, _ := newPaymentFlowService(&paymentprovider.WebhookPayload{})
	trialEnd := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	_, err := billingService.CreateSubscription(context.Background(), &billing.CreateSubscriptionRequest{
		UserID: "user_1", Status: billing.StatusTrialing, Integrator: "stripe", IntegratorSubscriptionID: "sub_server_owned",
		BillingKind: string(paymentprovider.BillingKindRecurring), PaymentType: paymentprovider.PaymentTypeSubscription,
		PlanName: "Pro", NextBillingDate: &trialEnd,
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &invoicePreviewProviderStub{preview: &paymentprovider.UpcomingInvoicePreview{
		Subtotal: 1800, TaxAmount: 360, Total: 2160, AmountDue: 0, Currency: "USD",
	}}
	manager.UpcomingInvoicePreviewProviderRegistry = &invoicePreviewRegistryStub{provider: provider}
	response, err := manager.GetUserBillingDetail(context.Background(), &GetUserBillingDetailRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil {
		t.Fatal(err)
	}
	estimate := response.BillingDetail.UpcomingInvoiceEstimate
	if estimate == nil || !estimate.Estimated || estimate.TaxAmount != 360 || estimate.AmountDue != 0 || estimate.Currency != "USD" {
		t.Fatalf("estimate = %#v", estimate)
	}
	if provider.request == nil || provider.request.SubscriptionID != "sub_server_owned" {
		t.Fatalf("preview request = %#v", provider.request)
	}

	provider.err = errors.New("provider unavailable")
	provider.preview = nil
	response, err = manager.GetUserBillingDetail(context.Background(), &GetUserBillingDetailRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil || response.BillingDetail.UpcomingInvoiceEstimate != nil {
		t.Fatalf("provider failure response = %#v, %v", response, err)
	}

	provider.err = nil
	provider.wait = true
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	response, err = manager.GetUserBillingDetail(ctx, &GetUserBillingDetailRequest{UserID: "user_1", RequestingUserID: "user_1"})
	if err != nil || response.BillingDetail.UpcomingInvoiceEstimate != nil {
		t.Fatalf("preview timeout response = %#v, %v", response, err)
	}
}

func recurringLifecyclePayload(eventID string, eventTime time.Time, status, priceID string) *paymentprovider.WebhookPayload {
	eventTimeValue := ""
	if !eventTime.IsZero() {
		eventTimeValue = eventTime.UTC().Format(time.RFC3339)
	}
	return &paymentprovider.WebhookPayload{
		EventID: eventID, EventType: paymentprovider.EventTypeSubscriptionCreated, EventTime: eventTimeValue,
		PaymentType: paymentprovider.PaymentTypeSubscription, BillingKind: paymentprovider.BillingKindRecurring,
		SubscriptionID: "sub_1", UserReference: "user_1", CustomerID: "cus_1", Status: status,
		PlanName: "Pro", PlanID: "plan_1", CostID: "cost_1", ProviderPriceID: priceID,
		SubscriptionStateAuthoritative: true,
	}
}

func recurringTerms(amount *int64, priceID string) paymentprovider.SubscriptionTerms {
	currency := "USD"
	interval := "month"
	intervalCount := int64(1)
	quantity := int64(1)
	return paymentprovider.SubscriptionTerms{
		Observed: true, Amount: amount, Currency: &currency, BillingInterval: &interval,
		BillingIntervalCount: &intervalCount, Quantity: &quantity, ProviderPriceID: &priceID,
	}
}

func getOnlySubscription(t *testing.T, service *billing.Service) *billing.Subscription {
	t.Helper()
	response, err := service.GetSubscriptions(context.Background(), &billing.GetSubscriptionsRequest{ForUserIDs: []string{"user_1"}, PerPage: 25, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Subscriptions) != 1 {
		t.Fatalf("subscriptions = %#v", response)
	}
	return &response.Subscriptions[0]
}

func stringPointer(value string) *string { return &value }
