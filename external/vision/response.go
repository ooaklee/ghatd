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

// GetMetaData returns pagination metadata suitable for reply.WithMeta.
func (r *GetVisionsResponse) GetMetaData() map[string]interface{} {
	return map[string]interface{}{"total_resources": r.Total}
}

// DeleteVisionResponse reports deletion state.
type DeleteVisionResponse struct {
	Deleted bool `json:"deleted"`
}

// VisionConfigCapabilities is the client-safe vision configuration.
type VisionConfigCapabilities struct {
	StatusTransitions map[VisionStatus][]VisionStatus `json:"status_transitions"`
	ValidTypes        []VisionType                    `json:"valid_types"`
	DownvotingEnabled bool                            `json:"downvoting_enabled"`
}

// GetVisionConfigResponse wraps client-safe configuration.
type GetVisionConfigResponse struct {
	Config *VisionConfigCapabilities `json:"config"`
}
