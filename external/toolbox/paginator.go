package toolbox

import (
	"context"
	"errors"
	"math"

	"github.com/ettle/strcase"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply"
	"go.uber.org/zap"
)

const (
	// ErrKeyPageOutOfRange returned when requested page is out of range
	ErrKeyPageOutOfRange string = "PageOutOfRange"
)

// GetResourcePaginationErrorMap holds Error keys, their corresponding human-friendly message, and response status code
var GetResourcePaginationErrorMap = map[string]reply.ErrorManifestItem{
	ErrKeyPageOutOfRange: {Title: "Bad Request", Detail: "Page out of range", StatusCode: 400},
}

// ResponseMetaKey is a string type used as the keys in the map returned
// by requests
type ResponseMetaKey string

// ResponseMetaKey is a string type used as the keys in the map returned
// by requests
const (

	// ResponseMetaKeyResourcePerPage is the key for the number of resources per page
	ResponseMetaKeyResourcePerPage ResponseMetaKey = "resources_per_page"

	// ResponseMetaKeyTotalResources is the key for the total number of resources
	ResponseMetaKeyTotalResources ResponseMetaKey = "total_resources"

	// ResponseMetaKeyTotalPages is the key for the total number of pages
	ResponseMetaKeyTotalPages ResponseMetaKey = "total_pages"

	// ResponseMetaKeyPage is the key for the current page number
	ResponseMetaKeyPage ResponseMetaKey = "page"
)

// PaginationRequest represents a standard pagination request
type PaginationRequest struct {

	// PerPage is the number of resources per page
	PerPage int `json:"per_page"`

	// Page is the current page number
	Page int `json:"page"`
}

// PaginationResponse represents a standard pagination response
type PaginationResponse[T any] struct {
	Resources       []T `json:"resources"`
	Total           int `json:"total"`
	TotalPages      int `json:"total_pages"`
	ResourcePerPage int `json:"resource_per_page"`
	Page            int `json:"page"`
}

// Paginate handles pagination for any resource type
func Paginate[T any](
	ctx context.Context,
	req *PaginationRequest,
	resources []T,
	totalCount int,
) (*PaginationResponse[T], error) {
	logger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	// Set default values if needed
	perPage := req.PerPage
	if perPage <= 0 {
		perPage = 10 // Default items per page
	}

	page := req.Page
	if page <= 0 {
		page = 1 // Default to first page
	}

	// Calculate pagination parameters
	totalPages := int(math.Ceil(float64(totalCount) / float64(perPage)))

	// If no resources in collection
	if totalCount == 0 && page == 1 {
		return &PaginationResponse[T]{
			Resources:       []T{},
			Total:           totalCount,
			TotalPages:      totalPages,
			ResourcePerPage: perPage,
			Page:            page,
		}, nil
	} else if page > totalPages {
		logger.Warn("pagination-page-exceeds-total-pages",
			zap.Int(string(ResponseMetaKeyPage), page),
			zap.Int(strcase.ToKebab(string(ResponseMetaKeyTotalPages)), totalPages),
		)
		return nil, errors.New(ErrKeyPageOutOfRange)
	}

	return &PaginationResponse[T]{
		Resources:       resources,
		Total:           totalCount,
		TotalPages:      totalPages,
		ResourcePerPage: perPage,
		Page:            page,
	}, nil
}
