package pricer

// DeletePricePlanRequest represents the request payload for soft-deleting a price plan by its ID.
type DeletePricePlanRequest struct {
	// ID is the ID of the price plan to delete.
	ID string `validate:"required,uuid"`

	// UserID is the ID of the user performing the deletion.
	UserID string `validate:"required,uuid"`
}

// DeleteFeatureRequest represents the request payload for soft-deleting a feature by its ID.
type DeleteFeatureRequest struct {
	// ID is the ID of the feature to delete.
	ID string `validate:"required,uuid"`

	// UserID is the ID of the user performing the deletion.
	UserID string `validate:"required,uuid"`
}

// GetPricePlanByIDRequest holds everything needed to get a price plan by ID.
type GetPricePlanByIDRequest struct {
	// ID is the ID of the price plan to retrieve.
	ID string `validate:"required,uuid"`

	// IncludeFeatures includes plan feature references in the response.
	IncludeFeatures bool `query:"include_features"`

	// IncludeCosts includes plan costs in the response.
	IncludeCosts bool `query:"include_costs"`

	// IncludeProviders includes provider references in the response.
	IncludeProviders bool `query:"include_providers"`
}

// GetPricePlanBySlugRequest holds everything needed to get a price plan by slug.
type GetPricePlanBySlugRequest struct {
	// Slug is the slug of the price plan to retrieve.
	Slug string `validate:"required"`

	// IncludeFeatures includes plan feature references in the response.
	IncludeFeatures bool `query:"include_features"`

	// IncludeCosts includes plan costs in the response.
	IncludeCosts bool `query:"include_costs"`

	// IncludeProviders includes provider references in the response.
	IncludeProviders bool `query:"include_providers"`
}

// GetPricePlansRequest holds everything needed to get price plans.
type GetPricePlansRequest struct {
	// Order defines how the response should be sorted.
	Order string `query:"order"`

	// PerPage is the total number of price plans to return per page.
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from.
	Page int `query:"page"`

	// TotalCount specifies the total count of all price plans.
	TotalCount int

	// TotalPages specifies the total pages of results.
	TotalPages int

	// Meta determines whether the response should contain meta information.
	Meta bool `query:"meta"`

	// IncludeFeatures includes plan feature references in the response.
	IncludeFeatures bool `query:"include_features"`

	// IncludeCosts includes plan costs in the response.
	IncludeCosts bool `query:"include_costs"`

	// IncludeProviders includes provider references in the response.
	IncludeProviders bool `query:"include_providers"`

	// WithStatus filters plans by status.
	// comma-separated list of statuses
	WithStatus string `query:"with_status"`

	// WithBillingCadence filters plans by billing cadence.
	// comma-separated list of billing cadences
	WithBillingCadence string `query:"with_billing_cadence"`

	// WithCurrency filters plans by currency.
	// comma-separated list of ISO 4217 currency codes
	WithCurrency string `query:"with_currency"`

	// Slugs filters plans by slug.
	// comma-separated list of slugs
	Slugs string `query:"slugs"`

	// CreatedByIDs filters plans by creator IDs.
	// comma-separated list of IDs
	CreatedByIDs string `query:"created_by_ids"`

	// PublishedByIDs filters plans by publisher IDs.
	// comma-separated list of IDs
	PublishedByIDs string `query:"published_by_ids"`

	// CreatedAtFrom filters plans created at from the provided date.
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters plans created at up to the provided date.
	CreatedAtTo string `query:"created_at_to"`

	// PublishedAtFrom filters plans published at from the provided date.
	PublishedAtFrom string `query:"published_at_from"`

	// PublishedAtTo filters plans published at up to the provided date.
	PublishedAtTo string `query:"published_at_to"`

	// DeletedAtFrom filters plans deleted at from the provided date.
	DeletedAtFrom string `query:"deleted_at_from"`

	// DeletedAtTo filters plans deleted at up to the provided date.
	DeletedAtTo string `query:"deleted_at_to"`

	// IsDeleted filters for plans that are deleted.
	IsDeleted bool `query:"is_deleted"`

	// IsNotDeleted filters for plans that are not deleted.
	IsNotDeleted bool `query:"is_not_deleted"`

	// IsPublished filters for plans that are published.
	IsPublished bool `query:"is_published"`

	// IsNotPublished filters for plans that are not published.
	IsNotPublished bool `query:"is_not_published"`
}

// GetMetaData returns a map of metadata about the GetPricePlansRequest.
func (g *GetPricePlansRequest) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap["resources_per_page"] = g.PerPage
	responseMap["total_resources"] = g.TotalCount
	responseMap["total_pages"] = g.TotalPages
	responseMap["page"] = g.Page

	return responseMap
}

// CreatePricePlanRequest holds everything needed to create a price plan.
type CreatePricePlanRequest struct {
	// UserID is the ID of the user making the request.
	UserID string

	// Slug is the URL-safe identifier for the price plan.
	Slug string `json:"slug,omitempty"`

	// Name is the display name for the price plan.
	Name string `json:"name" validate:"required"`

	// Description describes the price plan.
	Description string `json:"description,omitempty"`

	// Status is the lifecycle state for the price plan.
	Status PricePlanStatus `json:"status,omitempty"`

	// Features are the feature references included in the price plan.
	Features []PlanFeatureRef `json:"features,omitempty"`

	// Costs are the costs attached to the price plan.
	Costs []PriceCost `json:"costs,omitempty"`

	// ProviderRefs are provider-side plan or product references.
	ProviderRefs []PriceProviderRef `json:"provider_refs,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// PublishNow is whether the price plan should be published immediately.
	PublishNow bool `json:"publish_now,omitempty"`

	// PublishAtUtc is the target publish at date for the price plan.
	PublishAtUtc string `json:"publish_at_utc,omitempty"`
}

// UpdatePricePlanRequest holds everything needed to update a price plan.
type UpdatePricePlanRequest struct {
	// UserID is the ID of the user making the request.
	UserID string `validate:"required,uuid"`

	// ID is the ID of the price plan to update.
	ID string `validate:"required,uuid"`

	// PricePlan is the complete price plan object to update.
	PricePlan *PricePlan `json:"price_plan,omitempty"`

	// Slug is the updated URL-safe identifier for the price plan.
	Slug *string `json:"slug,omitempty"`

	// Name is the updated display name for the price plan.
	Name *string `json:"name,omitempty"`

	// Description is the updated description for the price plan.
	Description *string `json:"description,omitempty"`

	// Status is the updated lifecycle state for the price plan.
	Status *PricePlanStatus `json:"status,omitempty"`

	// Features are the updated feature references included in the price plan.
	Features []PlanFeatureRef `json:"features,omitempty"`

	// Costs are the updated costs attached to the price plan.
	Costs []PriceCost `json:"costs,omitempty"`

	// ProviderRefs are the updated provider-side plan or product references.
	ProviderRefs []PriceProviderRef `json:"provider_refs,omitempty"`

	// Metadata stores updated project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// PublishPricePlanRequest holds everything needed to publish a price plan.
type PublishPricePlanRequest struct {
	// ID is the ID of the price plan to publish.
	ID string `validate:"required,uuid"`

	// UserID is the ID of the user publishing the price plan.
	UserID string `validate:"required,uuid"`

	// PublishAtUtc is the target publish at date for the price plan.
	PublishAtUtc string `json:"publish_at_utc,omitempty"`
}

// ArchivePricePlanRequest holds everything needed to archive a price plan.
type ArchivePricePlanRequest struct {
	// ID is the ID of the price plan to archive.
	ID string `validate:"required,uuid"`

	// UserID is the ID of the user archiving the price plan.
	UserID string `validate:"required,uuid"`
}

// CreateFeatureRequest holds everything needed to create a feature catalog item.
type CreateFeatureRequest struct {
	// UserID is the ID of the user making the request.
	UserID string

	// Slug is the URL-safe identifier for the feature.
	Slug string `json:"slug,omitempty"`

	// Name is the display name for the feature.
	Name string `json:"name" validate:"required"`

	// Description describes the feature.
	Description string `json:"description,omitempty"`

	// Type indicates how the entitlement should be interpreted.
	Type PriceFeatureType `json:"type" validate:"required"`

	// Unit indicates the unit this feature is measured in.
	Unit PriceFeatureUnit `json:"unit,omitempty"`

	// SortOrder can be used to order features in plan comparisons.
	SortOrder int `json:"sort_order,omitempty"`

	// Metadata stores additional project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// PublishNow is whether the feature should be published immediately.
	PublishNow bool `json:"publish_now,omitempty"`

	// PublishAtUtc is the target publish at date for the feature.
	PublishAtUtc string `json:"publish_at_utc,omitempty"`
}

// UpdateFeatureRequest holds everything needed to update a feature catalog item.
type UpdateFeatureRequest struct {
	// UserID is the ID of the user making the request.
	UserID string `validate:"required,uuid"`

	// ID is the ID of the feature to update.
	ID string `validate:"required,uuid"`

	// Feature is the complete feature object to update.
	Feature *PriceFeature `json:"feature,omitempty"`

	// Slug is the updated URL-safe identifier for the feature.
	Slug *string `json:"slug,omitempty"`

	// Name is the updated display name for the feature.
	Name *string `json:"name,omitempty"`

	// Description is the updated description for the feature.
	Description *string `json:"description,omitempty"`

	// Type indicates the updated feature type.
	Type *PriceFeatureType `json:"type,omitempty"`

	// Unit indicates the updated unit this feature is measured in.
	Unit *PriceFeatureUnit `json:"unit,omitempty"`

	// SortOrder is the updated display ordering value.
	SortOrder *int `json:"sort_order,omitempty"`

	// Metadata stores updated project-specific data.
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// GetFeaturesRequest holds everything needed to get feature catalog items.
type GetFeaturesRequest struct {
	// Order defines how the response should be sorted.
	Order string `query:"order"`

	// PerPage is the total number of features to return per page.
	PerPage int `query:"per_page"`

	// Page specifies the page results should be taken from.
	Page int `query:"page"`

	// TotalCount specifies the total count of all features.
	TotalCount int

	// TotalPages specifies the total pages of results.
	TotalPages int

	// Meta determines whether the response should contain meta information.
	Meta bool `query:"meta"`

	// WithTypes filters features by type.
	// comma-separated list of feature types
	WithTypes string `query:"with_types"`

	// WithUnits filters features by unit.
	// comma-separated list of feature units
	WithUnits string `query:"with_units"`

	// Slugs filters features by slug.
	// comma-separated list of slugs
	Slugs string `query:"slugs"`

	// CreatedByIDs filters features by creator IDs.
	// comma-separated list of IDs
	CreatedByIDs string `query:"created_by_ids"`

	// PublishedByIDs filters features by publisher IDs.
	// comma-separated list of IDs
	PublishedByIDs string `query:"published_by_ids"`

	// CreatedAtFrom filters features created at from the provided date.
	CreatedAtFrom string `query:"created_at_from"`

	// CreatedAtTo filters features created at up to the provided date.
	CreatedAtTo string `query:"created_at_to"`

	// PublishedAtFrom filters features published at from the provided date.
	PublishedAtFrom string `query:"published_at_from"`

	// PublishedAtTo filters features published at up to the provided date.
	PublishedAtTo string `query:"published_at_to"`

	// DeletedAtFrom filters features deleted at from the provided date.
	DeletedAtFrom string `query:"deleted_at_from"`

	// DeletedAtTo filters features deleted at up to the provided date.
	DeletedAtTo string `query:"deleted_at_to"`

	// IsDeleted filters for features that are deleted.
	IsDeleted bool `query:"is_deleted"`

	// IsNotDeleted filters for features that are not deleted.
	IsNotDeleted bool `query:"is_not_deleted"`

	// IsPublished filters for features that are published.
	IsPublished bool `query:"is_published"`

	// IsNotPublished filters for features that are not published.
	IsNotPublished bool `query:"is_not_published"`
}

// GetMetaData returns a map of metadata about the GetFeaturesRequest.
func (g *GetFeaturesRequest) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap["resources_per_page"] = g.PerPage
	responseMap["total_resources"] = g.TotalCount
	responseMap["total_pages"] = g.TotalPages
	responseMap["page"] = g.Page

	return responseMap
}
