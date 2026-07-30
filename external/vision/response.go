package vision

// VisionResponse wraps a single vision.
type VisionResponse struct {
	Vision *Vision `json:"vision"`
}

// GetVisionsResponse wraps a page of visions.
type GetVisionsResponse struct {
	Visions []Vision `json:"visions"`
	Total   int64    `json:"total"`
}

// DeleteVisionResponse reports deletion state.
type DeleteVisionResponse struct {
	Deleted bool `json:"deleted"`
}
