package pricer_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/pricer"
)

func TestPricePlan_Validate(t *testing.T) {
	t.Parallel()

	validPlan := func() *pricer.PricePlan {
		return &pricer.PricePlan{
			ID:     "plan-1",
			Slug:   "pro-plan",
			Name:   "Pro Plan",
			Status: pricer.PricePlanStatusDraft,
			Costs: []pricer.PriceCost{
				{Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly},
			},
			Features: []pricer.PlanFeatureRef{
				{FeatureID: "feat-1", FeatureSlug: "projects", Included: true},
			},
			ProviderRefs: []pricer.PriceProviderRef{
				{Provider: pricer.PriceProviderManual},
			},
		}
	}

	tests := []struct {
		name        string
		modifyPlan  func(p *pricer.PricePlan)
		expectError bool
		errorKey    string
	}{
		{
			name:        "Success - valid plan",
			modifyPlan:  func(p *pricer.PricePlan) {},
			expectError: false,
		},
		{
			name:        "Failure - nil plan",
			modifyPlan:  func(p *pricer.PricePlan) {},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPricePlanPayload,
		},
		{
			name: "Failure - invalid status",
			modifyPlan: func(p *pricer.PricePlan) {
				p.Status = "invalid"
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPricePlanStatus,
		},
		{
			name: "Failure - invalid cost currency",
			modifyPlan: func(p *pricer.PricePlan) {
				p.Costs = []pricer.PriceCost{
					{Amount: 1000, Currency: "INVALID", BillingCadence: pricer.PriceBillingCadenceMonthly},
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCurrency,
		},
		{
			name: "Failure - invalid billing cadence",
			modifyPlan: func(p *pricer.PricePlan) {
				p.Costs = []pricer.PriceCost{
					{Amount: 1000, Currency: "USD", BillingCadence: "quarterly"},
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceBillingCadence,
		},
		{
			name: "Failure - duplicate feature refs",
			modifyPlan: func(p *pricer.PricePlan) {
				p.Features = []pricer.PlanFeatureRef{
					{FeatureID: "feat-1", Included: true},
					{FeatureID: "feat-1", Included: true},
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyDuplicatePlanFeatureRef,
		},
		{
			name: "Failure - invalid provider ref",
			modifyPlan: func(p *pricer.PricePlan) {
				p.ProviderRefs = []pricer.PriceProviderRef{
					{Provider: "nonexistent"},
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceProvider,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var plan *pricer.PricePlan
			if tt.name != "Failure - nil plan" {
				p := validPlan()
				if tt.modifyPlan != nil {
					tt.modifyPlan(p)
				}
				plan = p
			}

			err := plan.Validate()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorKey != "" {
					assert.Contains(t, err.Error(), tt.errorKey)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestPriceFeature_Validate(t *testing.T) {
	t.Parallel()

	validFeature := func() *pricer.PriceFeature {
		return &pricer.PriceFeature{
			ID:   "feat-1",
			Slug: "api-access",
			Name: "API Access",
			Type: pricer.PriceFeatureTypeBoolean,
		}
	}

	tests := []struct {
		name          string
		modifyFeature func(f *pricer.PriceFeature)
		expectError   bool
		errorKey      string
	}{
		{
			name:          "Success - valid feature",
			modifyFeature: func(f *pricer.PriceFeature) {},
			expectError:   false,
		},
		{
			name:          "Failure - nil feature",
			modifyFeature: func(f *pricer.PriceFeature) {},
			expectError:   true,
			errorKey:      pricer.ErrKeyInvalidPriceFeaturePayload,
		},
		{
			name: "Failure - invalid type",
			modifyFeature: func(f *pricer.PriceFeature) {
				f.Type = "invalid_type"
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceFeatureType,
		},
		{
			name: "Failure - invalid unit",
			modifyFeature: func(f *pricer.PriceFeature) {
				f.Type = pricer.PriceFeatureTypeQuantity
				f.Unit = "invalid_unit"
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceFeatureUnit,
		},
		{
			name: "Success - valid unit",
			modifyFeature: func(f *pricer.PriceFeature) {
				f.Type = pricer.PriceFeatureTypeQuantity
				f.Unit = pricer.PriceFeatureUnitSeat
			},
			expectError: false,
		},
		{
			name: "Success - empty unit on non-quantity type",
			modifyFeature: func(f *pricer.PriceFeature) {
				f.Unit = ""
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var feature *pricer.PriceFeature
			if tt.name != "Failure - nil feature" {
				f := validFeature()
				if tt.modifyFeature != nil {
					tt.modifyFeature(f)
				}
				feature = f
			}

			err := feature.Validate()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorKey != "" {
					assert.Contains(t, err.Error(), tt.errorKey)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestPriceCost_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cost        func() *pricer.PriceCost
		expectError bool
		errorKey    string
	}{
		{
			name: "Success - valid cost",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:         1000,
					Currency:       "USD",
					BillingCadence: pricer.PriceBillingCadenceMonthly,
				}
			},
			expectError: false,
		},
		{
			name: "Failure - nil cost",
			cost: func() *pricer.PriceCost {
				return nil
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCost,
		},
		{
			name: "Failure - negative amount",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:         -1,
					Currency:       "USD",
					BillingCadence: pricer.PriceBillingCadenceMonthly,
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCost,
		},
		{
			name: "Failure - negative setup fee",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:         1000,
					SetupFeeAmount: -1,
					Currency:       "USD",
					BillingCadence: pricer.PriceBillingCadenceMonthly,
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCost,
		},
		{
			name: "Failure - negative trial days",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:          1000,
					TrialPeriodDays: -1,
					Currency:        "USD",
					BillingCadence:  pricer.PriceBillingCadenceMonthly,
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCost,
		},
		{
			name: "Failure - invalid currency",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:         1000,
					Currency:       "US",
					BillingCadence: pricer.PriceBillingCadenceMonthly,
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceCurrency,
		},
		{
			name: "Failure - invalid cadence",
			cost: func() *pricer.PriceCost {
				return &pricer.PriceCost{
					Amount:         1000,
					Currency:       "USD",
					BillingCadence: "daily",
				}
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceBillingCadence,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cost := tt.cost()
			var err error
			if cost == nil {
				var nilCost *pricer.PriceCost
				err = nilCost.Validate()
			} else {
				err = pricer.ValidatePriceCost(*cost)
			}

			if tt.expectError {
				require.Error(t, err)
				if tt.errorKey != "" {
					assert.Contains(t, err.Error(), tt.errorKey)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestNormaliseSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"Success - kebab case", "hello-world", "hello-world"},
		{"Success - spaces to hyphens", "Hello World", "hello-world"},
		{"Success - mixed case", "My Pro Plan", "my-pro-plan"},
		{"Success - trailing spaces", "  Plan Name  ", "plan-name"},
		{"Success - special chars", "Plan & Features!", "plan-features"},
		{"Success - already normalised", "starter", "starter"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := pricer.NormalisePriceSlug(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPlan_NormaliseSlug(t *testing.T) {
	t.Parallel()

	plan := &pricer.PricePlan{Slug: "My Pro Plan"}
	plan.NormaliseSlug()
	assert.Equal(t, "my-pro-plan", plan.Slug)
}

func TestFeature_NormaliseSlug(t *testing.T) {
	t.Parallel()

	feature := &pricer.PriceFeature{Slug: "API Access"}
	feature.NormaliseSlug()
	assert.Equal(t, "api-access", feature.Slug)
}

func TestHasDuplicatePlanFeatureRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		refs     []pricer.PlanFeatureRef
		expected bool
	}{
		{
			name:     "Success - no duplicates",
			refs:     []pricer.PlanFeatureRef{{FeatureID: "a"}, {FeatureID: "b"}},
			expected: false,
		},
		{
			name:     "Success - empty list",
			refs:     []pricer.PlanFeatureRef{},
			expected: false,
		},
		{
			name:     "Success - detects duplicate IDs",
			refs:     []pricer.PlanFeatureRef{{FeatureID: "a"}, {FeatureID: "a"}},
			expected: true,
		},
		{
			name: "Success - detects duplicate slugs",
			refs: []pricer.PlanFeatureRef{
				{FeatureSlug: "projects"},
				{FeatureSlug: "projects"},
			},
			expected: true,
		},
		{
			name: "Success - empty refs skipped",
			refs: []pricer.PlanFeatureRef{
				{},
				{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, pricer.HasDuplicatePlanFeatureRefs(tt.refs))
		})
	}
}

func TestValidatePlanFeatureRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		refs        []pricer.PlanFeatureRef
		expectError bool
		errorKey    string
	}{
		{
			name:        "Success - valid refs",
			refs:        []pricer.PlanFeatureRef{{FeatureID: "feat-1", Included: true}},
			expectError: false,
		},
		{
			name:        "Success - empty refs",
			refs:        []pricer.PlanFeatureRef{},
			expectError: false,
		},
		{
			name: "Failure - missing identifier",
			refs: []pricer.PlanFeatureRef{
				{Included: true},
			},
			expectError: true,
			errorKey:    pricer.ErrKeyMissingPlanFeatureRef,
		},
		{
			name: "Failure - negative quantity",
			refs: []pricer.PlanFeatureRef{
				{FeatureID: "feat-1", Quantity: -1, Included: true},
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceFeaturePayload,
		},
		{
			name: "Failure - invalid unit",
			refs: []pricer.PlanFeatureRef{
				{FeatureID: "feat-1", Unit: "invalid_unit", Included: true},
			},
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceFeatureUnit,
		},
		{
			name: "Success - valid unit",
			refs: []pricer.PlanFeatureRef{
				{FeatureID: "feat-1", Unit: pricer.PriceFeatureUnitGB, Included: true},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := pricer.ValidatePlanFeatureRefs(tt.refs)

			if tt.expectError {
				require.Error(t, err)
				if tt.errorKey != "" {
					assert.Contains(t, err.Error(), tt.errorKey)
				}
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestValidatePriceCosts(t *testing.T) {
	t.Parallel()

	validCost := pricer.PriceCost{
		Amount: 1000, Currency: "USD", BillingCadence: pricer.PriceBillingCadenceMonthly,
	}

	require.NoError(t, pricer.ValidatePriceCosts([]pricer.PriceCost{}))
	require.NoError(t, pricer.ValidatePriceCosts([]pricer.PriceCost{validCost}))

	invalidCost := pricer.PriceCost{
		Amount: 1000, Currency: "XX", BillingCadence: pricer.PriceBillingCadenceMonthly,
	}
	err := pricer.ValidatePriceCosts([]pricer.PriceCost{invalidCost})
	require.Error(t, err)
	assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPriceCurrency)
}

func TestValidatePriceProviderRefs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		refs        []pricer.PriceProviderRef
		expectError bool
	}{
		{
			name: "Success - valid providers",
			refs: []pricer.PriceProviderRef{
				{Provider: pricer.PriceProviderStripe},
				{Provider: pricer.PriceProviderManual},
			},
			expectError: false,
		},
		{
			name: "Failure - invalid provider",
			refs: []pricer.PriceProviderRef{
				{Provider: "paypal"},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := pricer.ValidatePriceProviderRefs(tt.refs)

			if tt.expectError {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestIsValidPricePlanStatus(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPricePlanStatus(string(pricer.PricePlanStatusDraft)))
	assert.True(t, pricer.IsValidPricePlanStatus(string(pricer.PricePlanStatusPublished)))
	assert.True(t, pricer.IsValidPricePlanStatus(string(pricer.PricePlanStatusArchived)))
	assert.False(t, pricer.IsValidPricePlanStatus("active"))
	assert.False(t, pricer.IsValidPricePlanStatus("deleted"))
}

func TestIsValidPriceFeatureType(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPriceFeatureType(string(pricer.PriceFeatureTypeBoolean)))
	assert.True(t, pricer.IsValidPriceFeatureType(string(pricer.PriceFeatureTypeQuantity)))
	assert.True(t, pricer.IsValidPriceFeatureType(string(pricer.PriceFeatureTypeText)))
	assert.False(t, pricer.IsValidPriceFeatureType("numeric"))
}

func TestIsValidPriceFeatureUnit(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitFeature)))
	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitSeat)))
	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitMember)))
	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitProject)))
	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitRequest)))
	assert.True(t, pricer.IsValidPriceFeatureUnit(string(pricer.PriceFeatureUnitGB)))
	assert.False(t, pricer.IsValidPriceFeatureUnit("invalid"))
}

func TestIsValidPriceBillingCadence(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPriceBillingCadence(string(pricer.PriceBillingCadenceOneTime)))
	assert.True(t, pricer.IsValidPriceBillingCadence(string(pricer.PriceBillingCadenceWeekly)))
	assert.True(t, pricer.IsValidPriceBillingCadence(string(pricer.PriceBillingCadenceMonthly)))
	assert.True(t, pricer.IsValidPriceBillingCadence(string(pricer.PriceBillingCadenceYearly)))
	assert.False(t, pricer.IsValidPriceBillingCadence("daily"))
}

func TestIsValidPriceProvider(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPriceProvider(string(pricer.PriceProviderManual)))
	assert.True(t, pricer.IsValidPriceProvider(string(pricer.PriceProviderStripe)))
	assert.True(t, pricer.IsValidPriceProvider(string(pricer.PriceProviderPaddle)))
	assert.True(t, pricer.IsValidPriceProvider(string(pricer.PriceProviderKofi)))
	assert.False(t, pricer.IsValidPriceProvider("paypal"))
}

func TestIsValidPriceCurrency(t *testing.T) {
	t.Parallel()

	assert.True(t, pricer.IsValidPriceCurrency("USD"))
	assert.True(t, pricer.IsValidPriceCurrency("GBP"))
	assert.True(t, pricer.IsValidPriceCurrency("eur"), "lowercase should be accepted")
	assert.False(t, pricer.IsValidPriceCurrency("US"))
	assert.False(t, pricer.IsValidPriceCurrency("USDD"))
	assert.False(t, pricer.IsValidPriceCurrency("U5D"))
}

func TestNormaliseCurrency(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "USD", pricer.NormaliseCurrency("usd"))
	assert.Equal(t, "GBP", pricer.NormaliseCurrency("gbp"))
	assert.Equal(t, "EUR", pricer.NormaliseCurrency("eur"))
	assert.Equal(t, "USD", pricer.NormaliseCurrency("  usd  "))
}

func TestPricePlan_Validate_SlugEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		slug        string
		expectError bool
		errorKey    string
	}{
		{
			name:        "Success - valid kebab slug",
			slug:        "pro-plan",
			expectError: false,
		},
		{
			name:        "Success - empty slug",
			slug:        "",
			expectError: false,
		},
		{
			name:        "Failure - slug with special chars only",
			slug:        "!!!!",
			expectError: true,
			errorKey:    pricer.ErrKeyInvalidPriceSlug,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan := &pricer.PricePlan{
				ID:     "plan-1",
				Slug:   tt.slug,
				Name:   "Test Plan",
				Status: pricer.PricePlanStatusDraft,
			}

			err := plan.Validate()

			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorKey)
				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestPricePlan_Validate_WithoutSlugValidation(t *testing.T) {
	t.Parallel()

	plan := &pricer.PricePlan{
		ID:     "plan-1",
		Name:   "Valid Plan",
		Status: pricer.PricePlanStatusDraft,
	}

	err := plan.Validate()
	assert.NoError(t, err)
}

func TestPriceFeature_Validate_SlugEdgeCases(t *testing.T) {
	t.Parallel()

	// A slug that has non-whitespace content but normalises to empty (e.g. only special chars)
	feature := &pricer.PriceFeature{
		ID:   "feat-1",
		Name: "Test",
		Slug: "!!!!",
		Type: pricer.PriceFeatureTypeBoolean,
	}

	err := feature.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), pricer.ErrKeyInvalidPriceSlug)
}
