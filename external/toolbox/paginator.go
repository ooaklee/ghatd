package toolbox

import (
	"context"
	"errors"
	"math"

	"github.com/ooaklee/reply"
)

const (
	// ErrKeyPageOutOfRange returned when requested page is out of range
	ErrKeyPageOutOfRange string = "PageOutOfRange"
)

// GetResourcePaginationErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var GetResourcePaginationErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyPageOutOfRange: {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400},
}

// GetResourcePaginationResponse is the data shaped on the requested
// item
type GetResourcePaginationResponse struct {

	// Resources is the collection of the resource to paginate
	Resources []interface{}

	// Total - number of resources found
	Total int

	// TotalPages pages available
	TotalPages int

	// ResourcePerPage is how many many resources
	// are in the page
	ResourcePerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

// GetResourcePaginationRequest is parameters to shape the data
type GetResourcePaginationRequest struct {

	// Total number of resources to return per page
	PerPage int

	// Page specifies the page results should be taken from. Default 1.
	Page int
}

// GetResourcePagination returns appropiate pagination response based on request and
// passed resource slice
func GetResourcePagination(ctx context.Context, r *GetResourcePaginationRequest, passedResources []interface{}, totalResources int) (*GetResourcePaginationResponse, error) {
	var err error

	totalPage := int(math.Ceil(float64(totalResources) / float64(r.PerPage)))

	// If no resources in collection return empty list
	if totalResources == 0 && r.Page == 1 {
		return &GetResourcePaginationResponse{
			Total:           totalResources,
			TotalPages:      1,
			Resources:       passedResources,
			Page:            r.Page,
			ResourcePerPage: r.PerPage,
		}, nil
	} else if r.Page > totalPage {
		return nil, errors.New(ErrKeyPageOutOfRange)
	}

	return &GetResourcePaginationResponse{
		Total:           totalResources,
		TotalPages:      totalPage,
		Resources:       passedResources,
		Page:            r.Page,
		ResourcePerPage: r.PerPage,
	}, err
}
