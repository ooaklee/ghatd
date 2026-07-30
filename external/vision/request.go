package vision

// CreateVisionRequest holds fields needed to create a vision.
type CreateVisionRequest struct {
	Name            string                 `json:"name" validate:"required"`
	Kind            string                 `json:"kind" validate:"required"`
	Description     string                 `json:"description,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	CreatedByUserID string                 `json:"created_by_user_id,omitempty" query:"-"`
}

// GetVisionByIDRequest identifies a vision by ID and requestor.
type GetVisionByIDRequest struct {
	ID     string `path:"visionID" validate:"required"`
	UserID string `validate:"required"`
}

// GetVisionByNameRequest identifies a vision by its natural key.
type GetVisionByNameRequest struct {
	Name string `json:"name" validate:"required"`
	Kind string `json:"kind" validate:"required"`
}

// GetVisionsRequest holds optional query filters and pagination.
type GetVisionsRequest struct {
	Query    string `query:"query"`
	Kind     string `query:"kind"`
	Status   string `query:"status"`
	Page     int64  `query:"page"`
	PageSize int64  `query:"page_size"`
}

// UpdateVisionRequest holds mutable vision fields.
type UpdateVisionRequest struct {
	ID              string                 `json:"id" validate:"required"`
	Name            string                 `json:"name,omitempty"`
	Kind            string                 `json:"kind,omitempty"`
	Description     string                 `json:"description,omitempty"`
	Status          string                 `json:"status,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	UpdatedByUserID string                 `json:"updated_by_user_id,omitempty"`
}

// DeleteVisionRequest identifies a vision for deletion.
type DeleteVisionRequest struct {
	ID string `json:"id" validate:"required"`
}
