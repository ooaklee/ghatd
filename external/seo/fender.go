package seo

import (
	"net/http"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
)

// sitemapValidator expected methods of a valid validator.
type sitemapValidator interface {
	Validate(s interface{}) error
}

func validateParsedRequest(request interface{}, validator sitemapValidator) error {
	if validator == nil {
		return nil
	}
	return validator.Validate(request)
}

// MapRequestToCreateSitemapItemRequest maps incoming create request data.
func MapRequestToCreateSitemapItemRequest(request *http.Request, validator sitemapValidator) (*CreateSitemapItemRequest, error) {
	parsedRequest := &CreateSitemapItemRequest{}
	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrSitemapItemError
	}
	if parsedRequest.CreatedByID == "" {
		parsedRequest.CreatedByID = accessmanagerhelpers.AcquireFrom(request.Context())
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToMassSitemapItemCreationByBatchRequest maps incoming batch create request data.
func MapRequestToMassSitemapItemCreationByBatchRequest(request *http.Request, validator sitemapValidator) (*MassSitemapItemCreationByBatchRequest, error) {
	parsedRequest := &MassSitemapItemCreationByBatchRequest{}
	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrSitemapItemError
	}

	requestorID := accessmanagerhelpers.AcquireFrom(request.Context())
	for index := range parsedRequest.Items {
		if parsedRequest.Items[index].CreatedByID == "" {
			parsedRequest.Items[index].CreatedByID = requestorID
		}
	}

	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToGetSitemapItemsRequest maps incoming list request data.
func MapRequestToGetSitemapItemsRequest(request *http.Request, validator sitemapValidator) (*GetSitemapItemsRequest, error) {
	parsedRequest := &GetSitemapItemsRequest{
		Page: 1,
	}
	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		return nil, ErrSitemapItemError
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToUpdateSitemapItemRequest maps incoming update request data.
func MapRequestToUpdateSitemapItemRequest(request *http.Request, validator sitemapValidator) (*UpdateSitemapItemRequest, error) {
	parsedRequest := &UpdateSitemapItemRequest{}
	if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
		return nil, ErrSitemapItemError
	}
	if parsedRequest.UpdatedByID == "" {
		parsedRequest.UpdatedByID = accessmanagerhelpers.AcquireFrom(request.Context())
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToDeleteEntriesWithURIRegexRequest maps incoming regex delete request data.
func MapRequestToDeleteEntriesWithURIRegexRequest(request *http.Request, validator sitemapValidator) (*DeleteEntriesWithURIRegexRequest, error) {
	parsedRequest := &DeleteEntriesWithURIRegexRequest{}
	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		return nil, ErrSitemapItemError
	}
	if parsedRequest.URIRegex == "" {
		parsedRequest.URIRegex = strings.TrimSpace(request.URL.Query().Get("uri_regex"))
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToGenerateSitemapRequest maps incoming generate request data.
func MapRequestToGenerateSitemapRequest(request *http.Request, validator sitemapValidator) (*GenerateSitemapRequest, error) {
	parsedRequest := &GenerateSitemapRequest{}
	if request.Body != nil && request.ContentLength != 0 {
		if err := toolbox.DecodeRequestBody(request, parsedRequest); err != nil {
			return nil, ErrSitemapItemError
		}
	}
	if len(parsedRequest.SaveToPaths) == 0 {
		parsedRequest.SaveToPaths = request.URL.Query()["save_to_paths"]
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapItemError
	}
	return parsedRequest, nil
}

// MapRequestToDownloadSitemapByPathRequest maps incoming download request data.
func MapRequestToDownloadSitemapByPathRequest(request *http.Request, validator sitemapValidator) (*DownloadSitemapByPathRequest, error) {
	parsedRequest := &DownloadSitemapByPathRequest{}
	if err := querydecoder.New(request.URL.Query()).Decode(parsedRequest); err != nil {
		return nil, ErrSitemapPathIsInvalid
	}
	if parsedRequest.Path == "" {
		parsedRequest.Path = request.URL.Query().Get("path")
	}
	if err := validateParsedRequest(parsedRequest, validator); err != nil {
		return nil, ErrSitemapPathIsInvalid
	}
	return parsedRequest, nil
}
