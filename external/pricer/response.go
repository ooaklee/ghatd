package pricer

import "github.com/ooaklee/ghatd/external/toolbox"

// DeletePricePlanResponse represents the response after soft-deleting a price plan.
type DeletePricePlanResponse struct {
	// PricePlan is the soft-deleted price plan.
	PricePlan *PricePlan `json:"price_plan,omitempty"`
}

// DeleteFeatureResponse represents the response after soft-deleting a feature.
type DeleteFeatureResponse struct {
	// Feature is the soft-deleted feature.
	Feature *PriceFeature `json:"feature,omitempty"`
}

// CreatePricePlanResponse holds everything needed to return the response to creating a price plan.
type CreatePricePlanResponse struct {
	// PricePlan is the price plan that was created.
	PricePlan *PricePlan `json:"price_plan"`
}

// UpdatePricePlanResponse holds everything needed to return the response to updating a price plan.
type UpdatePricePlanResponse struct {
	// PricePlan is the price plan that was updated.
	PricePlan *PricePlan `json:"price_plan"`
}

// GetPricePlanResponse holds everything needed to return the response to getting a price plan.
type GetPricePlanResponse struct {
	// PricePlan is the requested price plan.
	PricePlan *PricePlan `json:"price_plan"`
}

// GetPricePlanByIDResponse holds everything needed to return the response to getting a price plan by ID.
type GetPricePlanByIDResponse struct {
	*GetPricePlanResponse
}

// GetEmbeddedPricePlanResponse returns the embedded price plan response.
func (g *GetPricePlanByIDResponse) GetEmbeddedPricePlanResponse() *GetPricePlanResponse {
	return g.GetPricePlanResponse
}

// GetPricePlanBySlugResponse holds everything needed to return the response to getting a price plan by slug.
type GetPricePlanBySlugResponse struct {
	*GetPricePlanResponse
}

// GetEmbeddedPricePlanResponse returns the embedded price plan response.
func (g *GetPricePlanBySlugResponse) GetEmbeddedPricePlanResponse() *GetPricePlanResponse {
	return g.GetPricePlanResponse
}

// GetPricePlansResponse holds everything needed to return the response to getting price plans.
type GetPricePlansResponse struct {
	// PricePlans is the list of price plans found.
	PricePlans []PricePlan `json:"price_plans"`

	// Total is the number of price plans found that matched provided filters.
	Total int

	// TotalPages is the total pages available, based on the provided filters and resources per page.
	TotalPages int

	// PerPage is the number of price plans set to be returned per page.
	PerPage int

	// Page specifies the page results were taken from.
	Page int
}

// ValidatePriceSlugResponse describes the outcome of a pricing slug validation check.
type ValidatePriceSlugResponse struct {
	// RawName is the provided display name after leading/trailing whitespace is removed.
	RawName string `json:"raw_name,omitempty"`

	// RawSlug is the provided explicit slug after leading/trailing whitespace is removed.
	RawSlug string `json:"raw_slug,omitempty"`

	// Slug is the normalized slug candidate.
	Slug string `json:"slug"`

	// ResourceType is the pricing resource collection checked.
	ResourceType string `json:"resource_type"`

	// Adjusted is true when the normalized slug differs from the provided slug/name.
	Adjusted bool `json:"adjusted"`

	// Available is true when no other resource of this type uses the slug.
	Available bool `json:"available"`

	// ExistingID is the conflicting resource ID when the slug is unavailable.
	ExistingID string `json:"existing_id,omitempty"`

	// Hint is a human-readable message suitable for display below a slug input field.
	Hint string `json:"hint,omitempty"`
}

// GetMetaData returns a map containing metadata about the GetPricePlansResponse.
func (g *GetPricePlansResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}

// PublishPricePlanResponse holds everything needed to return the response to publishing a price plan.
type PublishPricePlanResponse struct {
	// PricePlan is the price plan that was published.
	PricePlan *PricePlan `json:"price_plan"`
}

// ArchivePricePlanResponse holds everything needed to return the response to archiving a price plan.
type ArchivePricePlanResponse struct {
	// PricePlan is the price plan that was archived.
	PricePlan *PricePlan `json:"price_plan"`
}

// CreateFeatureResponse holds everything needed to return the response to creating a feature.
type CreateFeatureResponse struct {
	// Feature is the feature that was created.
	Feature *PriceFeature `json:"feature"`
}

// UpdateFeatureResponse holds everything needed to return the response to updating a feature.
type UpdateFeatureResponse struct {
	// Feature is the feature that was updated.
	Feature *PriceFeature `json:"feature"`
}

// GetFeaturesResponse holds everything needed to return the response to getting features.
type GetFeaturesResponse struct {
	// Features is the list of features found.
	Features []PriceFeature `json:"features"`

	// Total is the number of features found that matched provided filters.
	Total int

	// TotalPages is the total pages available, based on the provided filters and resources per page.
	TotalPages int

	// PerPage is the number of features set to be returned per page.
	PerPage int

	// Page specifies the page results were taken from.
	Page int
}

// GetMetaData returns a map containing metadata about the GetFeaturesResponse.
func (g *GetFeaturesResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}
