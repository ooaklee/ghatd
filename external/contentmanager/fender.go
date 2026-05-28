package contentmanager

import (
	"net/http"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/toolbox"
	tctcToolbox "github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ritwickdey/querydecoder"
	"go.uber.org/zap"
)

// mapRequestToCreatePostRequest maps the http request to the create post request
func mapRequestToCreatePostRequest(r *http.Request, validator contentManagerValidator) (*CreatePostRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = &CreatePostRequest{
			CreatePostRequest: &post.CreatePostRequest{},
		}
	)

	baseRequest := post.CreatePostRequest{}

	err = toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, post.ErrInvalidPostPayload
	}
	baseRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	parsedRequest.CreatePostRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-create-post-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return parsedRequest, nil
}

// mapRequestToUpdatePostByIdRequest maps the http request to the update post by id request
func mapRequestToUpdatePostByIdRequest(r *http.Request, validator contentManagerValidator) (*UpdatePostByIdRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = &UpdatePostByIdRequest{
			UpdatePostRequest: &post.UpdatePostRequest{},
		}
	)

	baseRequest := post.UpdatePostRequest{}

	err = toolbox.DecodeRequestBody(r, &baseRequest)
	if err != nil {
		return nil, post.ErrInvalidPostPayload
	}
	baseRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// Extract postId from URI
	baseRequest.PostId, err = tctcToolbox.GetVariableValueFromUri(r, "postId")
	if err != nil {
		logger.Warn("failed-to-extract-post-id-from-uri", zap.Error(err))
		return nil, post.ErrIdIsRequired
	}

	parsedRequest.UpdatePostRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-update-post-by-id-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return parsedRequest, nil
}

// mapRequestToRestorePostByIdRequest maps the http request to the restore post by id request
func mapRequestToRestorePostByIdRequest(r *http.Request, validator contentManagerValidator) (*RestorePostByIdRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = &RestorePostByIdRequest{
			RestorePostByIdRequest: &post.RestorePostByIdRequest{},
		}
	)

	baseRequest := post.RestorePostByIdRequest{}
	baseRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// Extract postId from URI
	baseRequest.Id, err = tctcToolbox.GetVariableValueFromUri(r, "postId")
	if err != nil {
		logger.Warn("failed-to-extract-post-id-from-uri", zap.Error(err))
		return nil, post.ErrIdIsRequired
	}

	parsedRequest.RestorePostByIdRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-restore-post-by-id-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return parsedRequest, nil
}

// mapRequestToDeletePostByIdRequest maps the http request to the delete post by id request
func mapRequestToDeletePostByIdRequest(r *http.Request, validator contentManagerValidator) (*DeletePostByIdRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = &DeletePostByIdRequest{
			DeletePostByIdRequest: &post.DeletePostByIdRequest{},
		}
	)

	baseRequest := post.DeletePostByIdRequest{}
	baseRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// Decode query parameters
	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-glossary-items-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	// Extract postId from URI
	baseRequest.Id, err = tctcToolbox.GetVariableValueFromUri(r, "postId")
	if err != nil {
		logger.Warn("failed-to-extract-post-id-from-uri", zap.Error(err))
		return nil, post.ErrIdIsRequired
	}

	parsedRequest.DeletePostByIdRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-delete-post-by-id-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return parsedRequest, nil
}

// mapRequestToGetGlossaryItemsRequest maps the http request to the get glossary items request
func mapRequestToGetGlossaryItemsRequest(r *http.Request, validator contentManagerValidator) (*GetGlossaryItemsRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetGlossaryItemsRequest{
			GetGlossaryItemsRequest: &post.GetGlossaryItemsRequest{
				GetPostsRequest: &post.GetPostsRequest{},
			},
		}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	baseRequest := post.GetPostsRequest{}

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-glossary-items-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.GetGlossaryItemsRequest.GetPostsRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-glossary-items-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// mapRequestToGetFaqItemsRequest maps the http request to the get faq items request
func mapRequestToGetFaqItemsRequest(r *http.Request, validator contentManagerValidator) (*GetFaqItemsRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetFaqItemsRequest{
			GetFaqItemsRequest: &post.GetFaqItemsRequest{
				GetPostsRequest: &post.GetPostsRequest{},
			},
		}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	baseRequest := post.GetPostsRequest{}

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-faq-items-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.GetFaqItemsRequest.GetPostsRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-faq-items-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// mapRequestToGetArticlesRequest maps the http request to the get articles request
func mapRequestToGetArticlesRequest(r *http.Request, validator contentManagerValidator) (*GetArticlesRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetArticlesRequest{
			GetArticlesRequest: &post.GetArticlesRequest{
				GetPostsRequest: &post.GetPostsRequest{},
			},
		}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	baseRequest := post.GetPostsRequest{}

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-articles-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.GetArticlesRequest.GetPostsRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-articles-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// mapRequestToGetArticleSitemapItemsRequest maps the http request to the article sitemap items request.
func mapRequestToGetArticleSitemapItemsRequest(r *http.Request, validator contentManagerValidator) (*GetArticleSitemapItemsRequest, error) {
	var (
		err           error
		logger        = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")
		parsedRequest = &GetArticleSitemapItemsRequest{}
	)

	if r.Body != nil && r.ContentLength != 0 {
		err = toolbox.DecodeRequestBody(r, parsedRequest)
		if err != nil {
			logger.Warn("failed-to-decode-body-for-get-article-sitemap-items-request", zap.Error(err))
			return nil, post.ErrInvalidPostQueryParam
		}
	}

	err = querydecoder.New(r.URL.Query()).Decode(parsedRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-article-sitemap-items-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-article-sitemap-items-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return parsedRequest, nil
}

// mapRequestToGetChangelogItemsRequest maps the http request to the get changelog items request
func mapRequestToGetChangelogItemsRequest(r *http.Request, validator contentManagerValidator) (*GetChangelogItemsRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetChangelogItemsRequest{
			GetChangelogItemsRequest: &post.GetChangelogItemsRequest{
				GetPostsRequest: &post.GetPostsRequest{},
			},
		}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	baseRequest := post.GetPostsRequest{}

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-changelog-items-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.GetChangelogItemsRequest.GetPostsRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-changelog-items-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// mapRequestToGetChangelogItemByUrlFriendlyIdRequest maps the http request to the get changelog item by url friendly id request
func mapRequestToGetChangelogItemByUrlFriendlyIdRequest(r *http.Request, validator contentManagerValidator) (*GetChangelogItemByUrlFriendlyIdRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetChangelogItemByUrlFriendlyIdRequest{}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// Add urlFriendlyId from uri
	parsedRequest.UrlFriendlyId, err = tctcToolbox.GetVariableValueFromUri(r, "urlFriendlyId")
	if err != nil {
		return nil, err
	}

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-changelog-item-by-url-friendly-id-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil

}

// mapRequestToGetArticleItemByUrlFriendlyIdRequest maps the http request to the get article item by url friendly id request
func mapRequestToGetArticleItemByUrlFriendlyIdRequest(r *http.Request, validator contentManagerValidator) (*GetArticleItemByUrlFriendlyIdRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetArticleItemByUrlFriendlyIdRequest{}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	// Add urlFriendlyId from uri
	parsedRequest.UrlFriendlyId, err = tctcToolbox.GetVariableValueFromUri(r, "urlFriendlyId")
	if err != nil {
		return nil, err
	}

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-article-item-by-url-friendly-id-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil

}

// mapRequestToGetLatestPostsByTypeRequest maps the http request to the get latest posts by type request
func mapRequestToGetLatestPostsByTypeRequest(r *http.Request, validator contentManagerValidator) (*GetLatestPostsByTypeRequest, error) {

	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetLatestPostsByTypeRequest{
			GetLatestPostsByTypeRequest: &post.GetLatestPostsByTypeRequest{},
		}
	)

	parsedRequest.UserId = accessmanagerhelpers.AcquireFrom(r.Context())

	baseRequest := post.GetLatestPostsByTypeRequest{}

	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-latest-posts-by-type-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	parsedRequest.GetLatestPostsByTypeRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-latest-posts-by-type-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// mapRequestToGetLatestNotificationOverviewsRequest maps the http request to the get latest notification overviews request
func mapRequestToGetLatestNotificationOverviewsRequest(r *http.Request, validator contentManagerValidator) (*GetLatestNotificationOverviewsRequest, error) {
	var (
		err    error
		logger *zap.Logger = logger.AcquirePackageFrom(r.Context(), "external/contentmanager")

		parsedRequest = GetLatestNotificationOverviewsRequest{
			GetLatestNotificationOverviewsRequest: &common.GetLatestNotificationOverviewsRequest{},
		}
	)

	baseRequest := common.GetLatestNotificationOverviewsRequest{}
	query := r.URL.Query()
	err = querydecoder.New(query).Decode(&baseRequest)
	if err != nil {
		logger.Warn("failed-to-decode-query-for-get-latest-notification-overviews-request", zap.Error(err))
		return nil, post.ErrInvalidPostQueryParam
	}

	baseRequest.UserID = accessmanagerhelpers.AcquireFrom(r.Context())
	parsedRequest.GetLatestNotificationOverviewsRequest = &baseRequest

	err = validateParsedRequest(parsedRequest, validator)
	if err != nil {
		logger.Warn("validation-failed-for-get-latest-notification-overviews-request", zap.Error(err))
		return nil, post.ErrPostBadRequest
	}

	return &parsedRequest, nil
}

// validateParsedRequest validates the request
func validateParsedRequest(request interface{}, validator contentManagerValidator) error {
	return validator.Validate(request)
}
