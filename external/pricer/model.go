package pricer

import (
	"errors"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// PricePlanStatus represents a price plan lifecycle state.
type PricePlanStatus string

// PriceFeatureType represents how a feature entitlement should be interpreted.
type PriceFeatureType string

// PriceFeatureUnit represents the unit used for a feature entitlement.
type PriceFeatureUnit string

// PriceBillingCadence represents how often a cost is charged.
type PriceBillingCadence string

// PriceProvider represents a supported external price provider.
type PriceProvider string

// PricePlan represents a v1 pricing plan.
type PricePlan struct {
	// ID is the unique identifier for the price plan.
	ID string `json:"id" bson:"_id"`

	// NanoID is the nano ID for the price plan.
	NanoID string `json:"nano_id" bson:"_nano_id"`

	// Slug is the URL-safe identifier for the price plan.
	Slug string `json:"slug" bson:"slug"`

	// Name is the display name for the price plan.
	Name string `json:"name" bson:"name"`

	// Description describes the price plan.
	Description string `json:"description,omitempty" bson:"description,omitempty"`

	// Status is the lifecycle state for the price plan.
	Status PricePlanStatus `json:"status" bson:"status"`

	// Features is the ordered feature catalog references included in this plan.
	Features []PlanFeatureRef `json:"features,omitempty" bson:"features,omitempty"`

	// Costs are the prices attached to this plan.
	Costs []PriceCost `json:"costs,omitempty" bson:"costs,omitempty"`

	// ProviderRefs links this plan to provider-side plan or product identifiers.
	ProviderRefs []PriceProviderRef `json:"provider_refs,omitempty" bson:"provider_refs,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	// PublishedAt is the date and time the price plan was published.
	PublishedAt string `json:"published_at,omitempty" bson:"published_at,omitempty"`

	// PublishedByID is the ID of the user who most recently published the price plan.
	PublishedByID string `json:"published_by_id,omitempty" bson:"published_by_id,omitempty"`

	// CreatedAt is the date and time the price plan was created.
	CreatedAt string `json:"created_at" bson:"created_at"`

	// CreatedByID is the ID of the user who created the price plan.
	CreatedByID string `json:"created_by_id,omitempty" bson:"created_by_id,omitempty"`

	// UpdatedAt is the date and time the price plan was updated.
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`

	// UpdatedByID is the ID of the user who most recently updated the price plan.
	UpdatedByID string `json:"updated_by_id,omitempty" bson:"updated_by_id,omitempty"`

	// DeletedAt is the date and time the price plan was soft deleted.
	DeletedAt string `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`

	// DeletedByID is the ID of the user who soft deleted the price plan.
	DeletedByID string `json:"deleted_by_id,omitempty" bson:"deleted_by_id,omitempty"`
}

// PriceFeature represents a reusable v1 feature catalog item.
type PriceFeature struct {
	// ID is the unique identifier for the feature.
	ID string `json:"id" bson:"_id"`

	// NanoID is the nano ID for the feature.
	NanoID string `json:"nano_id" bson:"_nano_id"`

	// Slug is the URL-safe identifier for the feature.
	Slug string `json:"slug" bson:"slug"`

	// Name is the display name for the feature.
	Name string `json:"name" bson:"name"`

	// Description describes the feature.
	Description string `json:"description,omitempty" bson:"description,omitempty"`

	// Type indicates how the entitlement should be interpreted.
	Type PriceFeatureType `json:"type" bson:"type"`

	// Unit indicates the unit this feature is measured in.
	Unit PriceFeatureUnit `json:"unit,omitempty" bson:"unit,omitempty"`

	// SortOrder can be used to order features in plan comparisons.
	SortOrder int `json:"sort_order,omitempty" bson:"sort_order,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	// PublishedAt is the date and time the feature was published.
	PublishedAt string `json:"published_at,omitempty" bson:"published_at,omitempty"`

	// PublishedByID is the ID of the user who most recently published the feature.
	PublishedByID string `json:"published_by_id,omitempty" bson:"published_by_id,omitempty"`

	// CreatedAt is the date and time the feature was created.
	CreatedAt string `json:"created_at" bson:"created_at"`

	// CreatedByID is the ID of the user who created the feature.
	CreatedByID string `json:"created_by_id,omitempty" bson:"created_by_id,omitempty"`

	// UpdatedAt is the date and time the feature was updated.
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`

	// UpdatedByID is the ID of the user who most recently updated the feature.
	UpdatedByID string `json:"updated_by_id,omitempty" bson:"updated_by_id,omitempty"`

	// DeletedAt is the date and time the feature was soft deleted.
	DeletedAt string `json:"deleted_at,omitempty" bson:"deleted_at,omitempty"`

	// DeletedByID is the ID of the user who soft deleted the feature.
	DeletedByID string `json:"deleted_by_id,omitempty" bson:"deleted_by_id,omitempty"`
}

// PlanFeatureRef describes how a catalog feature is included in a plan.
type PlanFeatureRef struct {
	// FeatureID references PriceFeature.ID.
	FeatureID string `json:"feature_id,omitempty" bson:"feature_id,omitempty"`

	// FeatureSlug references PriceFeature.Slug.
	FeatureSlug string `json:"feature_slug,omitempty" bson:"feature_slug,omitempty"`

	// Label overrides the catalog feature display name for this plan.
	Label string `json:"label,omitempty" bson:"label,omitempty"`

	// Included indicates whether the feature is included in the plan.
	Included bool `json:"included" bson:"included"`

	// Quantity is the included quantity for metered or limited features.
	Quantity int64 `json:"quantity,omitempty" bson:"quantity,omitempty"`

	// Unit indicates the unit this reference is measured in.
	Unit PriceFeatureUnit `json:"unit,omitempty" bson:"unit,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// PriceCost represents a single v1 monetary cost.
type PriceCost struct {
	// ID is the unique identifier for the cost.
	ID string `json:"id,omitempty" bson:"_id,omitempty"`

	// Amount is the cost in the lowest currency unit, e.g. cents.
	Amount int64 `json:"amount" bson:"amount"`

	// Currency is the ISO 4217 currency code.
	Currency string `json:"currency" bson:"currency"`

	// BillingCadence indicates how often the amount is charged.
	BillingCadence PriceBillingCadence `json:"billing_cadence" bson:"billing_cadence"`

	// TrialPeriodDays is the number of trial days before this cost is charged.
	TrialPeriodDays int `json:"trial_period_days,omitempty" bson:"trial_period_days,omitempty"`

	// SetupFeeAmount is an optional one-off setup fee in the lowest currency unit.
	SetupFeeAmount int64 `json:"setup_fee_amount,omitempty" bson:"setup_fee_amount,omitempty"`

	// ProviderRefs links this cost to provider-side price identifiers.
	ProviderRefs []PriceProviderRef `json:"provider_refs,omitempty" bson:"provider_refs,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// PriceProviderRef links internal pricing records to provider-side identifiers.
type PriceProviderRef struct {
	// Provider identifies the payment provider.
	Provider PriceProvider `json:"provider" bson:"provider"`

	// ProviderID is a general provider-side identifier.
	ProviderID string `json:"provider_id,omitempty" bson:"provider_id,omitempty"`

	// ProviderProductID is the provider-side product identifier.
	ProviderProductID string `json:"provider_product_id,omitempty" bson:"provider_product_id,omitempty"`

	// ProviderPriceID is the provider-side price identifier.
	ProviderPriceID string `json:"provider_price_id,omitempty" bson:"provider_price_id,omitempty"`

	// Metadata stores additional provider-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`
}

// NormalisePriceSlug converts text to the package-standard slug format.
func NormalisePriceSlug(value string) string {
	slug, err := toolbox.StringConvertToKebabCase(value)
	if err != nil {
		slug = strings.ReplaceAll(toolbox.StringStandardisedToLower(value), " ", "-")
	}

	return strings.Trim(slug, "-")
}

// NormaliseSlug converts text to the package-standard slug format.
func NormaliseSlug(value string) string {
	return NormalisePriceSlug(value)
}

// NormaliseSlug normalises the plan slug in place.
func (p *PricePlan) NormaliseSlug() *PricePlan {
	p.Slug = NormalisePriceSlug(p.Slug)
	return p
}

// NormaliseSlug normalises the feature slug in place.
func (f *PriceFeature) NormaliseSlug() *PriceFeature {
	f.Slug = NormalisePriceSlug(f.Slug)
	return f
}

// Validate checks whether the plan uses valid v1 enum values and references.
func (p *PricePlan) Validate() error {
	if p == nil {
		return errors.New(ErrKeyInvalidPricePlanPayload)
	}

	if !IsValidPricePlanStatus(string(p.Status)) {
		return errors.New(ErrKeyInvalidPricePlanStatus)
	}

	if strings.TrimSpace(p.Slug) != "" && NormalisePriceSlug(p.Slug) == "" {
		return errors.New(ErrKeyInvalidPriceSlug)
	}

	if err := ValidatePlanFeatureRefs(p.Features); err != nil {
		return err
	}

	if err := ValidatePriceCosts(p.Costs); err != nil {
		return err
	}

	return ValidatePriceProviderRefs(p.ProviderRefs)
}

// Validate checks whether the feature uses valid v1 enum values.
func (f *PriceFeature) Validate() error {
	if f == nil {
		return errors.New(ErrKeyInvalidPriceFeaturePayload)
	}

	if strings.TrimSpace(f.Slug) != "" && NormalisePriceSlug(f.Slug) == "" {
		return errors.New(ErrKeyInvalidPriceSlug)
	}

	if !IsValidPriceFeatureType(string(f.Type)) {
		return errors.New(ErrKeyInvalidPriceFeatureType)
	}

	if f.Unit != "" && !IsValidPriceFeatureUnit(string(f.Unit)) {
		return errors.New(ErrKeyInvalidPriceFeatureUnit)
	}

	return nil
}

// Validate checks whether the cost uses valid v1 values.
func (c *PriceCost) Validate() error {
	if c == nil {
		return errors.New(ErrKeyInvalidPriceCost)
	}

	if c.Amount < 0 || c.SetupFeeAmount < 0 || c.TrialPeriodDays < 0 {
		return errors.New(ErrKeyInvalidPriceCost)
	}

	if !IsValidPriceCurrency(c.Currency) {
		return errors.New(ErrKeyInvalidPriceCurrency)
	}

	if !IsValidPriceBillingCadence(string(c.BillingCadence)) {
		return errors.New(ErrKeyInvalidPriceBillingCadence)
	}

	return ValidatePriceProviderRefs(c.ProviderRefs)
}

// ValidatePriceCosts checks a set of costs.
func ValidatePriceCosts(costs []PriceCost) error {
	for i := range costs {
		if err := costs[i].Validate(); err != nil {
			return err
		}
	}

	return nil
}

// ValidatePriceCost checks a single cost.
func ValidatePriceCost(cost PriceCost) error {
	return cost.Validate()
}

// ValidatePlanFeatureRefs checks references for missing identifiers, duplicate references, and valid units.
func ValidatePlanFeatureRefs(refs []PlanFeatureRef) error {
	if HasDuplicatePlanFeatureRefs(refs) {
		return errors.New(ErrKeyDuplicatePlanFeatureRef)
	}

	for _, ref := range refs {
		if ref.refKey() == "" {
			return errors.New(ErrKeyMissingPlanFeatureRef)
		}

		if ref.Quantity < 0 {
			return errors.New(ErrKeyInvalidPriceFeaturePayload)
		}

		if ref.Unit != "" && !IsValidPriceFeatureUnit(string(ref.Unit)) {
			return errors.New(ErrKeyInvalidPriceFeatureUnit)
		}
	}

	return nil
}

// HasDuplicatePlanFeatureRefs returns true when feature IDs or slugs appear more than once.
func HasDuplicatePlanFeatureRefs(refs []PlanFeatureRef) bool {
	seen := map[string]bool{}
	for _, ref := range refs {
		key := ref.refKey()
		if key == "" {
			continue
		}

		if seen[key] {
			return true
		}
		seen[key] = true
	}

	return false
}

// ValidatePriceProviderRefs checks a set of provider references.
func ValidatePriceProviderRefs(refs []PriceProviderRef) error {
	for _, ref := range refs {
		if !IsValidPriceProvider(string(ref.Provider)) {
			return errors.New(ErrKeyInvalidPriceProvider)
		}
	}

	return nil
}

// NormaliseCurrency uppercases and trims a currency code.
func NormaliseCurrency(currency string) string {
	return strings.ToUpper(strings.TrimSpace(currency))
}

// IsValidPriceCurrency returns true when currency is a three-letter uppercase code.
func IsValidPriceCurrency(currency string) bool {
	currency = NormaliseCurrency(currency)
	if len(currency) != 3 {
		return false
	}

	for _, char := range currency {
		if char < 'A' || char > 'Z' {
			return false
		}
	}

	return true
}

// IsValidPricePlanStatus returns true when status is a supported plan status.
func IsValidPricePlanStatus(status string) bool {
	switch PricePlanStatus(status) {
	case PricePlanStatusDraft, PricePlanStatusPublished, PricePlanStatusArchived:
		return true
	default:
		return false
	}
}

// IsValidPriceFeatureType returns true when featureType is a supported feature type.
func IsValidPriceFeatureType(featureType string) bool {
	switch PriceFeatureType(featureType) {
	case PriceFeatureTypeBoolean, PriceFeatureTypeQuantity, PriceFeatureTypeText:
		return true
	default:
		return false
	}
}

// IsValidPriceFeatureUnit returns true when unit is a supported feature unit.
func IsValidPriceFeatureUnit(unit string) bool {
	switch PriceFeatureUnit(unit) {
	case PriceFeatureUnitFeature, PriceFeatureUnitSeat, PriceFeatureUnitMember, PriceFeatureUnitProject, PriceFeatureUnitRequest, PriceFeatureUnitGB:
		return true
	default:
		return false
	}
}

// IsValidPriceBillingCadence returns true when cadence is a supported billing cadence.
func IsValidPriceBillingCadence(cadence string) bool {
	switch PriceBillingCadence(cadence) {
	case PriceBillingCadenceOneTime, PriceBillingCadenceWeekly, PriceBillingCadenceMonthly, PriceBillingCadenceYearly:
		return true
	default:
		return false
	}
}

// IsValidPriceProvider returns true when provider is supported.
func IsValidPriceProvider(provider string) bool {
	switch PriceProvider(provider) {
	case PriceProviderManual, PriceProviderStripe, PriceProviderPaddle, PriceProviderKofi:
		return true
	default:
		return false
	}
}

func (r PlanFeatureRef) refKey() string {
	if id := strings.TrimSpace(r.FeatureID); id != "" {
		return "id:" + id
	}

	if slug := NormalisePriceSlug(r.FeatureSlug); slug != "" {
		return "slug:" + slug
	}

	return ""
}
