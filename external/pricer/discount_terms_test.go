package pricer_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/pricer"
)

func TestPriceDiscount_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		discount    pricer.PriceDiscount
		expectError string
	}{
		{
			name: "Success - amount discount",
			discount: pricer.PriceDiscount{
				Type:     pricer.PriceDiscountTypeAmount,
				Amount:   500,
				Currency: "USD",
			},
		},
		{
			name: "Success - percent discount",
			discount: pricer.PriceDiscount{
				Type:       pricer.PriceDiscountTypePercent,
				PercentBps: 2500,
			},
		},
		{
			name: "Failure - amount discount requires valid currency",
			discount: pricer.PriceDiscount{
				Type:     pricer.PriceDiscountTypeAmount,
				Amount:   500,
				Currency: "USDX",
			},
			expectError: pricer.ErrKeyInvalidPriceCurrency,
		},
		{
			name: "Failure - percent out of range",
			discount: pricer.PriceDiscount{
				Type:       pricer.PriceDiscountTypePercent,
				PercentBps: 10001,
			},
			expectError: pricer.ErrKeyInvalidPriceDiscount,
		},
		{
			name: "Failure - end before start",
			discount: pricer.PriceDiscount{
				Type:     pricer.PriceDiscountTypeAmount,
				Amount:   500,
				Currency: "USD",
				StartsAt: "2026-05-08",
				EndsAt:   "2026-05-07",
			},
			expectError: pricer.ErrKeyInvalidPriceDiscount,
		},
		{
			name: "Failure - invalid provider",
			discount: pricer.PriceDiscount{
				Type:     pricer.PriceDiscountTypeAmount,
				Amount:   500,
				Currency: "USD",
				ProviderRefs: []pricer.PriceProviderRef{
					{Provider: "unknown"},
				},
			},
			expectError: pricer.ErrKeyInvalidPriceProvider,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.discount.Validate()
			if tt.expectError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectError)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestPricePaymentTerms_Validate(t *testing.T) {
	t.Parallel()

	assert.NoError(t, (&pricer.PricePaymentTerms{
		Label:            "Net 30",
		DueDays:          30,
		CollectionMethod: pricer.PricePaymentCollectionMethodInvoice,
	}).Validate())

	err := (&pricer.PricePaymentTerms{
		DueDays: -1,
	}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPricePaymentTerms)

	err = (&pricer.PricePaymentTerms{
		CollectionMethod: "wire_transfer",
	}).Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPricePaymentTerms)
}

func TestService_PricePlanDiscountsAndPaymentTerms(t *testing.T) {
	t.Parallel()

	t.Run("Create preserves typed pricing controls", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(&mockPricerRepository{
			createPricePlanFunc: func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
				return pp, nil
			},
		})

		response, err := svc.CreatePricePlan(context.Background(), &pricer.CreatePricePlanRequest{
			UserID: testUserID,
			Name:   testPlanName,
			Costs: []pricer.PriceCost{
				{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
			},
			Discounts: []pricer.PriceDiscount{
				{Type: pricer.PriceDiscountTypePercent, PercentBps: 1500},
			},
			PaymentTerms: &pricer.PricePaymentTerms{
				Label:            "Net 14",
				DueDays:          14,
				CollectionMethod: pricer.PricePaymentCollectionMethodInvoice,
			},
			ProviderRefs: []pricer.PriceProviderRef{
				{Provider: pricer.PriceProviderManual},
			},
		})

		require.NoError(t, err)
		require.NotNil(t, response.PricePlan)
		require.Len(t, response.PricePlan.Discounts, 1)
		assert.Equal(t, int64(1500), response.PricePlan.Discounts[0].PercentBps)
		require.NotNil(t, response.PricePlan.PaymentTerms)
		assert.Equal(t, 14, response.PricePlan.PaymentTerms.DueDays)
	})

	t.Run("Update preserves typed pricing controls", func(t *testing.T) {
		t.Parallel()

		existingPlan := makeValidPlan()
		svc := newTestService(&mockPricerRepository{
			getPricePlanByIDFunc: func(ctx context.Context, id string, req *pricer.GetPricePlanByIDRequest) (*pricer.PricePlan, error) {
				return existingPlan, nil
			},
			updatePricePlanFunc: func(ctx context.Context, pp *pricer.PricePlan) (*pricer.PricePlan, error) {
				return pp, nil
			},
		})

		response, err := svc.UpdatePricePlan(context.Background(), &pricer.UpdatePricePlanRequest{
			ID:     testPlanID,
			UserID: testUserID,
			Discounts: []pricer.PriceDiscount{
				{Type: pricer.PriceDiscountTypeAmount, Amount: 250, Currency: "USD"},
			},
			PaymentTerms: &pricer.PricePaymentTerms{
				Label:            "Due on receipt",
				DueDays:          0,
				CollectionMethod: pricer.PricePaymentCollectionMethodManual,
			},
		})

		require.NoError(t, err)
		require.NotNil(t, response.PricePlan)
		require.Len(t, response.PricePlan.Discounts, 1)
		assert.Equal(t, int64(250), response.PricePlan.Discounts[0].Amount)
		require.NotNil(t, response.PricePlan.PaymentTerms)
		assert.Equal(t, pricer.PricePaymentCollectionMethodManual, response.PricePlan.PaymentTerms.CollectionMethod)
	})
}

func TestService_ValidatePriceSlug(t *testing.T) {
	t.Parallel()

	t.Run("Success - plan slug is available and normalized from name", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(&mockPricerRepository{})
		response, err := svc.ValidatePriceSlug(context.Background(), &pricer.ValidatePriceSlugRequest{
			Name:         "Pro Plan",
			ResourceType: "plan",
		})

		require.NoError(t, err)
		require.NotNil(t, response)
		assert.Equal(t, "pro-plan", response.Slug)
		assert.True(t, response.Available)
		assert.True(t, response.Adjusted)
	})

	t.Run("Success - plan slug conflict", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(&mockPricerRepository{
			getPricePlanBySlugFunc: func(ctx context.Context, slug string, req *pricer.GetPricePlanBySlugRequest) (*pricer.PricePlan, error) {
				return &pricer.PricePlan{ID: testPlanID, Slug: slug}, nil
			},
		})
		response, err := svc.ValidatePriceSlug(context.Background(), &pricer.ValidatePriceSlugRequest{
			Slug:         testPlanSlug,
			ResourceType: "plan",
		})

		require.NoError(t, err)
		require.NotNil(t, response)
		assert.False(t, response.Available)
		assert.Equal(t, testPlanID, response.ExistingID)
	})

	t.Run("Success - feature edit excludes current feature", func(t *testing.T) {
		t.Parallel()

		svc := newTestService(&mockPricerRepository{
			getFeaturesFunc: func(ctx context.Context, req *pricer.GetFeaturesRequest) ([]pricer.PriceFeature, error) {
				return []pricer.PriceFeature{{ID: testFeatureID, Slug: testFeatureSlug}}, nil
			},
		})
		response, err := svc.ValidatePriceSlug(context.Background(), &pricer.ValidatePriceSlugRequest{
			Slug:         testFeatureSlug,
			ResourceType: "feature",
			ExcludeID:    testFeatureID,
		})

		require.NoError(t, err)
		require.NotNil(t, response)
		assert.True(t, response.Available)
		assert.Equal(t, "feature", response.ResourceType)
	})
}
