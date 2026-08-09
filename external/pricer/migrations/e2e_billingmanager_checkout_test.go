package migrations_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/pricer"
	pricermigrations "github.com/ooaklee/ghatd/external/pricer/migrations"
	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

const checkoutMatrixReturnURL = "https://app.example.test/app/plan?stripe=success&session_id={CHECKOUT_SESSION_ID}"

type checkoutMatrixProvider struct {
	requests []*paymentprovider.CheckoutSessionRequest
}

func (p *checkoutMatrixProvider) CreateCheckoutSession(_ context.Context, request *paymentprovider.CheckoutSessionRequest) (*paymentprovider.CheckoutSession, error) {
	p.requests = append(p.requests, request)
	return &paymentprovider.CheckoutSession{
		ID:           fmt.Sprintf("cs_test_matrix_%d", len(p.requests)),
		ClientSecret: fmt.Sprintf("cs_test_matrix_%d_secret", len(p.requests)),
	}, nil
}

func (*checkoutMatrixProvider) GetCheckoutReturnURL() string {
	return checkoutMatrixReturnURL
}

type checkoutMatrixProviderRegistry struct {
	provider paymentprovider.CheckoutProvider
}

func (r *checkoutMatrixProviderRegistry) GetCheckoutProvider(name string) (paymentprovider.CheckoutProvider, error) {
	if name != string(pricer.PriceProviderStripe) {
		return nil, paymentprovider.ErrPaymentProviderNotFound
	}
	return r.provider, nil
}

type checkoutMatrixUserService struct{}

func (*checkoutMatrixUserService) GetUserByEmail(context.Context, *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error) {
	return nil, user.ErrUserNotFound
}

func (*checkoutMatrixUserService) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	return &user.GetUserByIDResponse{User: &user.UniversalUser{
		ID:    "checkout-matrix-user",
		Email: "checkout-matrix@example.test",
	}}, nil
}

// TestE2E_BillingManagerCheckoutFromStripeSeed proves that the local/test
// Stripe catalogue fixtures survive persistence and pricer decoding before
// Billing Manager maps them to a provider-neutral checkout request. The fake
// provider deliberately makes no network calls.
func TestE2E_BillingManagerCheckoutFromStripeSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping E2E billing-manager checkout matrix test in short mode")
	}

	ctx, mongoHandler, db, dbName := setupE2EMongo(t)
	require.NoError(t, pricermigrations.InitPricingIndexesUp(db))
	require.NoError(t, pricermigrations.InitTestPlansSeedUp(db))

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	pricingService := pricer.NewService(pricer.NewRepository(store))
	provider := &checkoutMatrixProvider{}
	service := billingmanager.NewService(nil, nil).
		WithCheckoutProviderRegistry(&checkoutMatrixProviderRegistry{provider: provider}).
		WithPricerService(pricingService).
		WithUserService(&checkoutMatrixUserService{})

	tests := []struct {
		name      string
		planID    string
		planSlug  string
		planName  string
		costID    string
		priceID   string
		amount    int64
		currency  string
		cadence   pricer.PriceBillingCadence
		mode      string
		trialDays int
	}{
		{
			name:     "one-time payment",
			planID:   pricermigrations.TestSeedPlanStripeOneTimeID,
			planSlug: "stripe-one-time-test",
			planName: "Stripe One-Time Test",
			costID:   pricermigrations.TestSeedCostStripeOneTimeID,
			priceID:  pricermigrations.TestSeedStripeOneTimePriceID,
			amount:   4900,
			currency: "USD",
			cadence:  pricer.PriceBillingCadenceOneTime,
			mode:     paymentprovider.CheckoutModePayment,
		},
		{
			name:     "weekly subscription",
			planID:   pricermigrations.TestSeedPlanStripeRecurringID,
			planSlug: "stripe-recurring-test",
			planName: "Stripe Recurring Test",
			costID:   pricermigrations.TestSeedCostStripeRecurringWeekID,
			priceID:  pricermigrations.TestSeedStripeRecurringWeekPriceID,
			amount:   500,
			currency: "USD",
			cadence:  pricer.PriceBillingCadenceWeekly,
			mode:     paymentprovider.CheckoutModeSubscription,
		},
		{
			name:     "monthly subscription",
			planID:   pricermigrations.TestSeedPlanStripeRecurringID,
			planSlug: "stripe-recurring-test",
			planName: "Stripe Recurring Test",
			costID:   pricermigrations.TestSeedCostStripeRecurringMonthID,
			priceID:  pricermigrations.TestSeedStripeRecurringMonthPriceID,
			amount:   1800,
			currency: "USD",
			cadence:  pricer.PriceBillingCadenceMonthly,
			mode:     paymentprovider.CheckoutModeSubscription,
		},
		{
			name:     "yearly subscription",
			planID:   pricermigrations.TestSeedPlanStripeRecurringID,
			planSlug: "stripe-recurring-test",
			planName: "Stripe Recurring Test",
			costID:   pricermigrations.TestSeedCostStripeRecurringYearID,
			priceID:  pricermigrations.TestSeedStripeRecurringYearPriceID,
			amount:   18000,
			currency: "USD",
			cadence:  pricer.PriceBillingCadenceYearly,
			mode:     paymentprovider.CheckoutModeSubscription,
		},
		{
			name:      "weekly subscription with trial",
			planID:    pricermigrations.TestSeedPlanStripeTrialID,
			planSlug:  "stripe-recurring-trial-test",
			planName:  "Stripe Recurring Trial Test",
			costID:    pricermigrations.TestSeedCostStripeTrialWeekID,
			priceID:   pricermigrations.TestSeedStripeTrialWeekPriceID,
			amount:    500,
			currency:  "USD",
			cadence:   pricer.PriceBillingCadenceWeekly,
			mode:      paymentprovider.CheckoutModeSubscription,
			trialDays: 7,
		},
		{
			name:      "monthly subscription with trial",
			planID:    pricermigrations.TestSeedPlanStripeTrialID,
			planSlug:  "stripe-recurring-trial-test",
			planName:  "Stripe Recurring Trial Test",
			costID:    pricermigrations.TestSeedCostStripeTrialMonthID,
			priceID:   pricermigrations.TestSeedStripeTrialMonthPriceID,
			amount:    1800,
			currency:  "USD",
			cadence:   pricer.PriceBillingCadenceMonthly,
			mode:      paymentprovider.CheckoutModeSubscription,
			trialDays: 14,
		},
		{
			name:      "yearly subscription with trial",
			planID:    pricermigrations.TestSeedPlanStripeTrialID,
			planSlug:  "stripe-recurring-trial-test",
			planName:  "Stripe Recurring Trial Test",
			costID:    pricermigrations.TestSeedCostStripeTrialYearID,
			priceID:   pricermigrations.TestSeedStripeTrialYearPriceID,
			amount:    18000,
			currency:  "USD",
			cadence:   pricer.PriceBillingCadenceYearly,
			mode:      paymentprovider.CheckoutModeSubscription,
			trialDays: 30,
		},
	}
	require.Len(t, tests, 7, "the seed must expose one one-time and six recurring checkout variants")

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			callsBefore := len(provider.requests)
			response, err := service.ProcessBillingProviderCheckout(ctx, &billingmanager.ProcessBillingProviderCheckoutRequest{
				UserID:         "checkout-matrix-user",
				ProviderName:   string(pricer.PriceProviderStripe),
				PriceID:        test.priceID,
				IdempotencyKey: "checkout-matrix-attempt",
				Origin:         "https://app.example.test",
				SecFetchSite:   "same-origin",
			})
			require.NoError(t, err)
			require.NotNil(t, response)
			require.NotNil(t, response.Session)
			require.Len(t, provider.requests, callsBefore+1, "each catalogue selection must make exactly one provider call")

			got := provider.requests[callsBefore]
			assert.Equal(t, test.priceID, got.PriceID)
			assert.Equal(t, test.planID, got.PlanID)
			assert.Equal(t, test.planSlug, got.PlanSlug)
			assert.Equal(t, test.planName, got.PlanName)
			assert.Equal(t, test.costID, got.CostID)
			assert.Equal(t, test.amount, got.ExpectedAmount)
			assert.Equal(t, test.currency, got.ExpectedCurrency)
			assert.Equal(t, string(test.cadence), got.ExpectedBillingCadence)
			assert.Equal(t, test.mode, got.Mode)
			assert.Equal(t, test.trialDays, got.TrialPeriodDays)
			assert.Equal(t, test.planID, got.Metadata["plan_id"])
			assert.Equal(t, test.costID, got.Metadata["cost_id"])
			assert.Equal(t, test.priceID, got.Metadata["provider_price_id"])
		})
	}

	assert.Len(t, provider.requests, 7, "the fake provider must receive exactly the seven seeded checkout variants")
}
