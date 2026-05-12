package blueprint

// BlueprintResponse wraps a single blueprint.
type BlueprintResponse struct {
	Blueprint *Blueprint `json:"blueprint"`
}

// GetBlueprintsResponse wraps a page of blueprints.
type GetBlueprintsResponse struct {
	Blueprints []Blueprint `json:"blueprints"`
	Total      int64       `json:"total"`
}

// DeleteBlueprintResponse reports deletion state.
type DeleteBlueprintResponse struct {
	Deleted bool `json:"deleted"`
}
