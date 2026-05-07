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

	// ErrKeyInvalidPriceDiscount is returned when a discount is invalid.
	ErrKeyInvalidPriceDiscount = "InvalidPriceDiscount"

	// ErrKeyInvalidPricePaymentTerms is returned when payment terms are invalid.
	ErrKeyInvalidPricePaymentTerms = "InvalidPricePaymentTerms"

	// ErrKeyInvalidPriceCurrency is returned when a currency is invalid.
	ErrKeyInvalidPriceCurrency = "InvalidPriceCurrency"

	// ErrKeyDuplicatePlanFeatureRef is returned when a plan contains duplicate feature references.
	ErrKeyDuplicatePlanFeatureRef = "DuplicatePlanFeatureRef"

	// ErrKeyMissingPlanFeatureRef is returned when a feature reference has no feature identifier.
	ErrKeyMissingPlanFeatureRef = "MissingPlanFeatureRef"

	// ErrKeyPricePlanNotFound is returned when a price plan cannot be found.
	ErrKeyPricePlanNotFound = "PricePlanNotFound"

	// ErrKeyPriceFeatureNotFound is returned when a feature cannot be found.
	ErrKeyPriceFeatureNotFound = "PriceFeatureNotFound"

	// ErrKeyPriceUserIDRequired is returned when an operation requires a user ID.
	ErrKeyPriceUserIDRequired = "PriceUserIDRequired"

	// ErrKeyPricePlanIDRequired is returned when an operation requires a price plan ID.
	ErrKeyPricePlanIDRequired = "PricePlanIDRequired"

	// ErrKeyPriceFeatureIDRequired is returned when an operation requires a feature ID.
	ErrKeyPriceFeatureIDRequired = "PriceFeatureIDRequired"

	// ErrKeyPricePlanPublishRequiresCost is returned when publishing a plan without a valid cost.
	ErrKeyPricePlanPublishRequiresCost = "PricePlanPublishRequiresCost"

	// ErrKeyPricePlanPublishRequiresProvider is returned when publishing a plan without provider references.
	ErrKeyPricePlanPublishRequiresProvider = "PricePlanPublishRequiresProvider"

	// ErrKeyInvalidPriceQueryParam is returned when a price query parameter is invalid.
	ErrKeyInvalidPriceQueryParam = "InvalidPriceQueryParam"

	// ErrKeyInvalidPriceDate is returned when a price date value cannot be parsed.
	ErrKeyInvalidPriceDate = "InvalidPriceDate"
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

const (
	// PriceSlugResourcePlan validates slugs in the price plan namespace.
	PriceSlugResourcePlan = "plan"

	// PriceSlugResourceFeature validates slugs in the price feature namespace.
	PriceSlugResourceFeature = "feature"

	// PriceSlugResourcePlanFeatureRef validates plan feature reference slugs.
	PriceSlugResourcePlanFeatureRef = "plan_feature_ref"
)

// PriceDiscountType represents how a discount should be interpreted.
type PriceDiscountType string

const (
	// PriceDiscountTypeAmount represents a fixed monetary discount.
	PriceDiscountTypeAmount PriceDiscountType = "amount"

	// PriceDiscountTypePercent represents a percentage discount in basis points.
	PriceDiscountTypePercent PriceDiscountType = "percent"
)

// PricePaymentCollectionMethod represents how payment should be collected.
type PricePaymentCollectionMethod string

const (
	// PricePaymentCollectionMethodManual represents manually collected payment.
	PricePaymentCollectionMethodManual PricePaymentCollectionMethod = "manual"

	// PricePaymentCollectionMethodAutomatic represents automatically collected payment.
	PricePaymentCollectionMethodAutomatic PricePaymentCollectionMethod = "automatic"

	// PricePaymentCollectionMethodInvoice represents invoice-based collection.
	PricePaymentCollectionMethodInvoice PricePaymentCollectionMethod = "invoice"
)
