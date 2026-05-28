package contentmanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/logger"
	"net/http"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// contentManagerService manages business logic for
// handling content based requests
type contentManagerService interface {
	CreatePost(ctx context.Context, req *CreatePostRequest) (*CreatePostResponse, error)
	UpdatePostById(ctx context.Context, req *UpdatePostByIdRequest) (*UpdatePostByIdResponse, error)
	DeletePostById(ctx context.Context, req *DeletePostByIdRequest) (*DeletePostByIdResponse, error)
	RestorePostById(ctx context.Context, req *RestorePostByIdRequest) (*RestorePostByIdResponse, error)

	GetChangelogItems(ctx context.Context, req *GetChangelogItemsRequest) (*GetChangelogItemsResponse, error)
	GetChangelogItemByUrlFriendlyId(ctx context.Context, req *GetChangelogItemByUrlFriendlyIdRequest) (*post.Post, error)

	GetGlossaryItems(ctx context.Context, req *GetGlossaryItemsRequest) (*GetGlossaryItemsResponse, error)
	GetFaqItems(ctx context.Context, req *GetFaqItemsRequest) (*GetFaqItemsResponse, error)
	GetArticles(ctx context.Context, req *GetArticlesRequest) (*GetArticlesResponse, error)
	GetArticleItemByUrlFriendlyId(ctx context.Context, req *GetArticleItemByUrlFriendlyIdRequest) (*post.Post, error)

	// GetArticleSitemapItems returns sitemap-ready article URL entries.
	GetArticleSitemapItems(ctx context.Context, req *GetArticleSitemapItemsRequest) (*GetArticleSitemapItemsResponse, error)

	GetLatestPostsByType(ctx context.Context, req *GetLatestPostsByTypeRequest) (*GetLatestPostsByTypeResponse, error)
	GetLatestNotificationOverviews(ctx context.Context, req *GetLatestNotificationOverviewsRequest) (*GetLatestNotificationOverviewsResponse, error)
}

// contentManagerValidator expected methods of a valid
// validator
type contentManagerValidator interface {
	Validate(s interface{}) error
}

// Handler manages content manager requests
type Handler struct {
	// service represents the content manager service
	service contentManagerService

	// validator represents the content manager validator
	validator contentManagerValidator

	// errorMaps holds the error maps for the handler
	errorMaps []reply.ErrorManifest
}

// NewHandler returns a new instance of the content manager handler
func NewHandler(service contentManagerService, validator contentManagerValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
		errorMaps: errorMaps,
	}
}

// CreatePost handles the request for creating a post
func (h *Handler) CreatePost(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-create-post")
	request, err := mapRequestToCreatePostRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	newlyCreated, err := h.service.CreatePost(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, newlyCreated.Post)
}

// UpdatePostById handles the request for updating a post by its ID
func (h *Handler) UpdatePostById(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-update-post-by-id")
	request, err := mapRequestToUpdatePostByIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	updatedPost, err := h.service.UpdatePostById(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, updatedPost.Post)
}

// DeletePostById handles the request for deleting a post by its ID
func (h *Handler) DeletePostById(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-delete-post-by-id")
	request, err := mapRequestToDeletePostByIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_, err = h.service.DeletePostById(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusNoContent, nil)
}

// RestorePostById handles the request for restoring a post by its ID
func (h *Handler) RestorePostById(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-restore-post-by-id")
	request, err := mapRequestToRestorePostByIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	restoredPost, err := h.service.RestorePostById(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, restoredPost.Post)
}

// GetChangelogItems handles the request for getting changelog posts
func (h *Handler) GetChangelogItems(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-changelog-items")
	request, err := mapRequestToGetChangelogItemsRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	posts, err := h.service.GetChangelogItems(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts, reply.WithMeta(posts.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts)
}

// GetChangelogItemByUrlFriendlyId handles the request for getting changelog item by its
// url friendly id
func (h *Handler) GetChangelogItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-changelog-item-by-url-friendly-id")
	request, err := mapRequestToGetChangelogItemByUrlFriendlyIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	post, err := h.service.GetChangelogItemByUrlFriendlyId(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, post)
}

// GetGlossaryItems handles the request for getting glossary posts
func (h *Handler) GetGlossaryItems(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-glossary-items")
	request, err := mapRequestToGetGlossaryItemsRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	posts, err := h.service.GetGlossaryItems(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts, reply.WithMeta(posts.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts)
}

// GetFaqItems handles the request for getting faq posts
func (h *Handler) GetFaqItems(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-faq-items")
	request, err := mapRequestToGetFaqItemsRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	posts, err := h.service.GetFaqItems(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts, reply.WithMeta(posts.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts)
}

// GetArticles handles the request for getting articles posts
func (h *Handler) GetArticles(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-articles")
	request, err := mapRequestToGetArticlesRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	posts, err := h.service.GetArticles(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if request.Meta {
		//nolint will set up default fallback later
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts, reply.WithMeta(posts.GetMetaData()))
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Posts)
}

// GetArticleItemByUrlFriendlyId handles the request for getting article item by its
// url friendly id
func (h *Handler) GetArticleItemByUrlFriendlyId(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-article-item-by-url-friendly-id")
	request, err := mapRequestToGetArticleItemByUrlFriendlyIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	post, err := h.service.GetArticleItemByUrlFriendlyId(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, post)
}

// GetArticleSitemapItems handles the request for getting article sitemap entries.
func (h *Handler) GetArticleSitemapItems(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-article-sitemap-items")
	request, err := mapRequestToGetArticleSitemapItemsRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetArticleSitemapItems(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GetLatestPostsByType handles the request for getting the latest posts by type
func (h *Handler) GetLatestPostsByType(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-latest-posts-by-type")
	request, err := mapRequestToGetLatestPostsByTypeRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	posts, err := h.service.GetLatestPostsByType(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, posts.Overviews)
}

// GetLatestNotificationOverviews handles the request for getting the latest notification overviews for the user
func (h *Handler) GetLatestNotificationOverviews(w http.ResponseWriter, r *http.Request) {
	logger := logger.AcquireOperationFrom(r.Context(), "external/contentmanager", "handle-get-latest-notification-overviews")
	request, err := mapRequestToGetLatestNotificationOverviewsRequest(r, h.validator)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	overviews, err := h.service.GetLatestNotificationOverviews(r.Context(), request)
	if err != nil {
		logger.Warn("handler-returning-error-response", zap.Error(err))
		h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	if overviews == nil || overviews.GetLatestNotificationOverviewsResponse == nil {
		h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, []common.NotificationOverview{})
		return
	}

	h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, overviews.Overviews)
}

// getBaseResponseHandler returns response handler with ContentManagerErrorMap
// as the base layer and caller-supplied maps as overrides.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(ContentManagerErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
