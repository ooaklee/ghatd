package contacter

import "github.com/ooaklee/ghatd/external/toolbox"

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
