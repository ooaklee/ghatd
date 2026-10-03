package blueprint

// CreateBlueprintRequest holds fields needed to create a blueprint.
type CreateBlueprintRequest struct {
	Name        string                 `json:"name" validate:"required"`
	Kind        string                 `json:"kind" validate:"required"`
	Description string                 `json:"description,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	// ActorID is the verified caller, bound after transport decoding, not stored ownership.
	ActorID string `json:"-"`
}

// GetBlueprintByIDRequest separates the selected record from the verified caller.
type GetBlueprintByIDRequest struct {
	ID string `path:"blueprintId" validate:"required"`
	// ActorID is supplied by authenticated middleware or an authorized in-process caller.
	ActorID string `json:"-" validate:"required"`
}

// GetBlueprintByNameRequest identifies a blueprint by its natural key.
type GetBlueprintByNameRequest struct {
	Name string `json:"name" validate:"required"`
	Kind string `json:"kind" validate:"required"`
}

// GetBlueprintsRequest holds optional query filters and pagination.
type GetBlueprintsRequest struct {
	Query    string `query:"query"`
	Kind     string `query:"kind"`
	Status   string `query:"status"`
	Page     int64  `query:"page"`
	PageSize int64  `query:"page_size"`
}

// UpdateBlueprintRequest holds mutable blueprint fields.
type UpdateBlueprintRequest struct {
	ID          string                 `json:"id" validate:"required"`
	Name        string                 `json:"name,omitempty"`
	Kind        string                 `json:"kind,omitempty"`
	Description string                 `json:"description,omitempty"`
	Status      string                 `json:"status,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	// ActorID becomes update attribution; it does not grant permission to edit.
	ActorID string `json:"-"`
}

// DeleteBlueprintRequest identifies a blueprint for deletion.
type DeleteBlueprintRequest struct {
	ID string `json:"id" validate:"required"`
	// ActorID identifies the caller; the integrating manager must authorize deletion.
	ActorID string `json:"-"`
}
