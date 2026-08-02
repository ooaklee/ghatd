package contacter

import (
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/reply/v2"
)

// CreateCommsResponse holds everything needed to return
// the response to creating a comms
type CreateCommsResponse struct {

	// Comms is the comms that was created
	Comms *Comms `json:"comms"`
}

// GetCommsResponse holds everything needed to return
// the response to get comms
type GetCommsResponse struct {
	Comms []Comms `json:"comms"`

	// Total number of comms found that matched provided
	// filters
	Total int

	// TotalPages total pages available, based on the provided
	// filters and resources per page
	TotalPages int

	// PerPage number of comms set to be returned per page
	PerPage int

	// Page specifies the page results were taken from. Default 1.
	Page int
}

// GetMetaData returns a map containing metadata about the GetCommsResponse,
// including the number of resources per page, total resources, total pages,
// and the current page.
func (g *GetCommsResponse) GetMetaData() map[string]interface{} {
	var responseMap = make(map[string]interface{})

	responseMap[string(toolbox.ResponseMetaKeyResourcePerPage)] = g.PerPage
	responseMap[string(toolbox.ResponseMetaKeyTotalResources)] = g.Total
	responseMap[string(toolbox.ResponseMetaKeyTotalPages)] = g.TotalPages
	responseMap[string(toolbox.ResponseMetaKeyPage)] = g.Page

	return responseMap
}

// UpdateCommsResponse holds everything needed to return
// the response to updating a comms
type UpdateCommsResponse struct {

	// Comms is the comms that was updated
	Comms *Comms `json:"comms"`
}

// GetCommsStatsResponse holds the response for comms stats
type GetCommsStatsResponse struct {
	*CommsStats
}

// GetAvailableCommsTypesResponse holds the communication types accepted by
// this contacter service. The map key is submitted as the comms type and the
// value is its user-facing label.
type GetAvailableCommsTypesResponse struct {
	CommsTypes CommsTypeMap `json:"comms_types"`
}

// GetBaseResponseHandler returns response handler with ContacterErrorMap as base
// and caller-supplied maps as overrides.
func (h *Handler) GetBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(ContacterErrorMap).
			AddOverrides(h.ErrorMaps...).
			Build(),
	)
}
