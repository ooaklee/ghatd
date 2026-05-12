package blueprint

// CreateBlueprintRequest holds fields needed to create a blueprint.
type CreateBlueprintRequest struct {
	Name            string                 `json:"name" validate:"required"`
	Kind            string                 `json:"kind" validate:"required"`
	Description     string                 `json:"description,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedByUserID string                 `json:"created_by_user_id,omitempty" query:"-"`
}

// GetBlueprintByIDRequest identifies a blueprint by ID and requestor.
type GetBlueprintByIDRequest struct {
	ID     string `path:"blueprintId" validate:"required"`
	UserID string `validate:"required"`
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
	ID              string                 `json:"id" validate:"required"`
	Name            string                 `json:"name,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	UpdatedByUserID string                 `json:"updated_by_user_id,omitempty"`
}

// DeleteBlueprintRequest identifies a blueprint for deletion.
type DeleteBlueprintRequest struct {
	ID string `json:"id" validate:"required"`
}
