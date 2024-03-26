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
func GetResourcePagination(ctx context.Context, r *GetResourcePaginationRequest, resources []interface{}) (*GetResourcePaginationResponse, error) {
	var responseSlice []interface{}
	var err error

	numberOfResources := len(resources)

	totalPage := int(math.Ceil(float64(numberOfResources) / float64(r.PerPage)))

	// If no resources in collection return empty list
	if numberOfResources == 0 && r.Page == 1 {
		return &GetResourcePaginationResponse{
			Total:           numberOfResources,
			TotalPages:      1,
			Resources:       responseSlice,
			Page:            r.Page,
			ResourcePerPage: r.PerPage,
		}, nil
	} else if r.Page > totalPage {
		return nil, errors.New(ErrKeyPageOutOfRange)
	}

	lowerLimit := getLowerLimit(r)
	upperLimit := getUpperLimit(r, numberOfResources)

	if upperLimit == lowerLimit {
		responseSlice = resources[lowerLimit:]
	} else {
		responseSlice = resources[lowerLimit:upperLimit]
	}

	return &GetResourcePaginationResponse{
		Total:           numberOfResources,
		TotalPages:      totalPage,
		Resources:       responseSlice,
		Page:            r.Page,
		ResourcePerPage: r.PerPage,
	}, err
}

// getLowerLimit returns the point to start of resource array
func getLowerLimit(r *GetResourcePaginationRequest) int {
	if r.Page == 1 {
		return 0
	}

	return ((r.PerPage * r.Page) - r.PerPage)
}

// getUpperLimit returns the point to end array from for resource
func getUpperLimit(r *GetResourcePaginationRequest, lengthOfArray int) int {
	l := getLowerLimit(r)

	if l == 0 {
		// Limit to length of array if there are not
		// enough elements
		if r.PerPage > lengthOfArray {
			return (lengthOfArray)
		}
		return r.PerPage
	}

	if (l + r.PerPage) <= (lengthOfArray - 1) {
		return (l + r.PerPage)
	}

	return (lengthOfArray)

}
