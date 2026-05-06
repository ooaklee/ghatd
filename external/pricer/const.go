package pricer

// Error keys
const (
	// ErrKeyInvalidPricePlanPayload is returned when a price plan payload fails validation.
	ErrKeyInvalidPricePlanPayload = "InvalidPricePlanPayload"

	// ErrKeyInvalidPriceFeaturePayload is returned when a price feature payload fails validation.
	ErrKeyInvalidPriceFeaturePayload = "InvalidPriceFeaturePayload"

	// ErrKeyInvalidPricePlanStatus is returned when a plan status is not supported.
	ErrKeyInvalidPricePlanStatus = "InvalidPricePlanStatus"

	// ErrKeyInvalidPriceFeatureType is returned when a feature type is not supported.
	ErrKeyInvalidPriceFeatureType = "InvalidPriceFeatureType"

	// ErrKeyInvalidPriceFeatureUnit is returned when a feature unit is not supported.
	ErrKeyInvalidPriceFeatureUnit = "InvalidPriceFeatureUnit"

	// ErrKeyInvalidPriceBillingCadence is returned when a billing cadence is not supported.
	ErrKeyInvalidPriceBillingCadence = "InvalidPriceBillingCadence"

	// ErrKeyInvalidPriceProvider is returned when a provider is not supported.
	ErrKeyInvalidPriceProvider = "InvalidPriceProvider"

	// ErrKeyInvalidPriceSlug is returned when a slug cannot be normalized to a valid value.
	ErrKeyInvalidPriceSlug = "InvalidPriceSlug"

	// ErrKeyInvalidPriceCost is returned when a cost is invalid.
	ErrKeyInvalidPriceCost = "InvalidPriceCost"

	// ErrKeyInvalidPriceCurrency is returned when a currency is invalid.
	ErrKeyInvalidPriceCurrency = "InvalidPriceCurrency"

	// ErrKeyDuplicatePlanFeatureRef is returned when a plan contains duplicate feature references.
	ErrKeyDuplicatePlanFeatureRef = "DuplicatePlanFeatureRef"

	// ErrKeyMissingPlanFeatureRef is returned when a feature reference has no feature identifier.
	ErrKeyMissingPlanFeatureRef = "MissingPlanFeatureRef"
)

const (
	// PricePlanStatusDraft indicates a plan is not visible for purchase.
	PricePlanStatusDraft PricePlanStatus = "draft"

	// PricePlanStatusPublished indicates a plan is visible for purchase.
	PricePlanStatusPublished PricePlanStatus = "published"

	// PricePlanStatusArchived indicates a plan is no longer offered.
	PricePlanStatusArchived PricePlanStatus = "archived"
)

const (
	// PriceFeatureTypeBoolean represents an on/off entitlement.
	PriceFeatureTypeBoolean PriceFeatureType = "boolean"

	// PriceFeatureTypeQuantity represents a numeric entitlement.
	PriceFeatureTypeQuantity PriceFeatureType = "quantity"

	// PriceFeatureTypeText represents a descriptive entitlement.
	PriceFeatureTypeText PriceFeatureType = "text"
)

const (
	// PriceFeatureUnitFeature is the default unit for non-metered features.
	PriceFeatureUnitFeature PriceFeatureUnit = "feature"

	// PriceFeatureUnitSeat represents user seats.
	PriceFeatureUnitSeat PriceFeatureUnit = "seat"

	// PriceFeatureUnitMember represents members.
	PriceFeatureUnitMember PriceFeatureUnit = "member"

	// PriceFeatureUnitProject represents projects.
	PriceFeatureUnitProject PriceFeatureUnit = "project"

	// PriceFeatureUnitRequest represents requests.
	PriceFeatureUnitRequest PriceFeatureUnit = "request"

	// PriceFeatureUnitGB represents storage in gigabytes.
	PriceFeatureUnitGB PriceFeatureUnit = "gb"
)

const (
	// PriceBillingCadenceOneTime represents a one-off cost.
	PriceBillingCadenceOneTime PriceBillingCadence = "one_time"

	// PriceBillingCadenceWeekly represents a weekly recurring cost.
	PriceBillingCadenceWeekly PriceBillingCadence = "week"

	// PriceBillingCadenceMonthly represents a monthly recurring cost.
	PriceBillingCadenceMonthly PriceBillingCadence = "month"

	// PriceBillingCadenceYearly represents a yearly recurring cost.
	PriceBillingCadenceYearly PriceBillingCadence = "year"
)

const (
	// PriceProviderManual represents internally managed pricing with no payment provider.
	PriceProviderManual PriceProvider = "manual"

	// PriceProviderStripe represents Stripe provider references.
	PriceProviderStripe PriceProvider = "stripe"

	// PriceProviderPaddle represents Paddle provider references.
	PriceProviderPaddle PriceProvider = "paddle"

	// PriceProviderKofi represents kofi provider references.
	PriceProviderKofi PriceProvider = "kofi"
)
