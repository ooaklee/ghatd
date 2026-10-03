package usermanager

// groupUpdatePayload is the editable HTTP projection of group.UpdateGroupRequest.
// Full records can redirect a lower-domain update through Group.ID and must not
// cross this boundary. Internal callers retain the lower-domain request type.
type groupUpdatePayload struct {
	// Name is an optional replacement name, normalized by the group service.
	Name *string `json:"name,omitempty"`
	// Description replaces the group's display description when present.
	Description *string `json:"description,omitempty"`
	// Email replaces the group's contact address when present.
	Email *string `json:"email,omitempty"`
	// Icon replaces the group's display icon when present.
	Icon *string `json:"icon,omitempty"`
	// Visibility requests a visibility change subject to domain validation.
	Visibility *string `json:"visibility,omitempty"`
	// Status requests a status change subject to domain validation.
	Status *string `json:"status,omitempty"`
	// Extensions carries application-defined data, not group identity or ownership.
	Extensions map[string]interface{} `json:"extensions,omitempty"`
}
