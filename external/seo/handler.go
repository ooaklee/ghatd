package seo

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// sitemapService manages business logic around sitemap requests.
type sitemapService interface {
	CreateSitemapItemIfDoesNotAlreadyExist(ctx context.Context, r *CreateSitemapItemRequest) (*SitemapItemResponse, error)
	DeleteEntriesWithUriRegex(ctx context.Context, r *DeleteEntriesWithURIRegexRequest) (*DeleteEntriesWithURIRegexResponse, error)
	DownloadSitemapByPath(ctx context.Context, r *DownloadSitemapByPathRequest) (*DownloadSitemapByPathResponse, error)
	GenerateSitemap(ctx context.Context, r *GenerateSitemapRequest) (*GenerateSitemapResponse, error)
	GetSitemapItems(ctx context.Context, r *GetSitemapItemsRequest) (*GetSitemapItemsResponse, error)
	MassSitemapItemCreationByBatch(ctx context.Context, r *MassSitemapItemCreationByBatchRequest) (*MassSitemapItemCreationByBatchResponse, error)
	UpdateSitemapItemByUri(ctx context.Context, r *UpdateSitemapItemRequest) (*SitemapItemResponse, error)
}

// Handler manages sitemap requests.
type Handler struct {
	service   sitemapService
	validator sitemapValidator
	errorMaps []reply.ErrorManifest
}

// NewHandler returns a sitemap handler.
func NewHandler(service sitemapService, validator sitemapValidator, errorMaps ...reply.ErrorManifest) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
		errorMaps: errorMaps,
	}
}

// CreateSitemapItem handles sitemap item creation.
func (h *Handler) CreateSitemapItem(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToCreateSitemapItemRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateSitemapItemIfDoesNotAlreadyExist(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// MassSitemapItemCreationByBatch handles batch sitemap item creation.
func (h *Handler) MassSitemapItemCreationByBatch(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToMassSitemapItemCreationByBatchRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.MassSitemapItemCreationByBatch(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// GetSitemapItems handles listing sitemap items.
func (h *Handler) GetSitemapItems(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetSitemapItemsRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetSitemapItems(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateSitemapItemByUri handles updating sitemap items by URI.
func (h *Handler) UpdateSitemapItemByUri(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateSitemapItemRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.UpdateSitemapItemByUri(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DeleteEntriesWithUriRegex handles deleting sitemap entries whose URI matches a regex.
func (h *Handler) DeleteEntriesWithUriRegex(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteEntriesWithURIRegexRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.DeleteEntriesWithUriRegex(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GenerateSitemap handles XML generation and optional file saves.
func (h *Handler) GenerateSitemap(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGenerateSitemapRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GenerateSitemap(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DownloadSitemapByPath handles admin sitemap downloads by path.
func (h *Handler) DownloadSitemapByPath(w http.ResponseWriter, r *http.Request) {
	h.writeSitemapFileResponse(w, r, true)
}

// GetSitemap handles the public sitemap endpoint.
func (h *Handler) GetSitemap(w http.ResponseWriter, r *http.Request) {
	h.writeSitemapFileResponse(w, r, false)
}

func (h *Handler) writeSitemapFileResponse(w http.ResponseWriter, r *http.Request, attachment bool) {
	request, err := MapRequestToDownloadSitemapByPathRequest(r, h.validator)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.DownloadSitemapByPath(r.Context(), request)
	if err != nil {
		_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	w.Header().Set("Content-Type", response.ContentType)
	if attachment {
		w.Header().Set("Content-Disposition", `attachment; filename="`+response.FileName+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response.Content)
}

func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(
		errormanifest.NewComposer().
			Add(SitemapErrorMap).
			AddOverrides(h.errorMaps...).
			Build(),
	)
}
