package seo

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// sitemapService manages business logic around sitemap requests.
type sitemapService interface {
	// CreateSitemapItemIfDoesNotAlreadyExist returns the sitemap item for the
	// request's URI, creating one only when absent, as a SitemapItemResponse
	// indicating whether creation occurred. The implementation validates the item
	// before repository access.
	CreateSitemapItemIfDoesNotAlreadyExist(ctx context.Context, r *CreateSitemapItemRequest) (*SitemapItemResponse, error)
	// DeleteEntriesWithUriRegex deletes sitemap entries whose URI matches the
	// request's regular expression, returning the deleted URI list and count. The
	// implementation compiles and bounds the regex before matching stored items.
	DeleteEntriesWithUriRegex(ctx context.Context, r *DeleteEntriesWithURIRegexRequest) (*DeleteEntriesWithURIRegexResponse, error)
	// DownloadSitemapByPath returns sitemap file content from the request's safe
	// local path, or the default path when unset, with file name and XML content
	// type in the response.
	DownloadSitemapByPath(ctx context.Context, r *DownloadSitemapByPathRequest) (*DownloadSitemapByPathResponse, error)
	// GenerateSitemap builds sitemap XML from stored items and optionally writes it
	// to each requested save path, returning the XML, URL count, and saved paths.
	// Requires a configured frontend domain.
	GenerateSitemap(ctx context.Context, r *GenerateSitemapRequest) (*GenerateSitemapResponse, error)
	// GetSitemapItems returns sitemap items matching the request filter alongside
	// the total matching count, per the sitemapService contract. The implementation
	// delegates listing and counting to the repository.
	GetSitemapItems(ctx context.Context, r *GetSitemapItemsRequest) (*GetSitemapItemsResponse, error)
	// MassSitemapItemCreationByBatch processes each requested item, upserting when
	// override is requested or skipping existing URIs otherwise, and reports
	// created, updated, and skipped counts with the resulting items.
	MassSitemapItemCreationByBatch(ctx context.Context, r *MassSitemapItemCreationByBatchRequest) (*MassSitemapItemCreationByBatchResponse, error)
	// UpdateSitemapItemByUri applies the request's mutable field changes to the
	// existing item identified by URI and returns the updated item. The
	// implementation validates the merged item before persisting.
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
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateSitemapItemIfDoesNotAlreadyExist(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// MassSitemapItemCreationByBatch handles batch sitemap item creation.
func (h *Handler) MassSitemapItemCreationByBatch(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToMassSitemapItemCreationByBatchRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.MassSitemapItemCreationByBatch(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response)
}

// GetSitemapItems handles listing sitemap items.
func (h *Handler) GetSitemapItems(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGetSitemapItemsRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetSitemapItems(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// UpdateSitemapItemByUri handles updating sitemap items by URI.
func (h *Handler) UpdateSitemapItemByUri(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToUpdateSitemapItemRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.UpdateSitemapItemByUri(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// DeleteEntriesWithUriRegex handles deleting sitemap entries whose URI matches a regex.
func (h *Handler) DeleteEntriesWithUriRegex(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToDeleteEntriesWithURIRegexRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.DeleteEntriesWithUriRegex(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

// GenerateSitemap handles XML generation and optional file saves.
func (h *Handler) GenerateSitemap(w http.ResponseWriter, r *http.Request) {
	request, err := MapRequestToGenerateSitemapRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GenerateSitemap(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
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

// writeSitemapFileResponse maps the download request, fetches sitemap content
// via the service and writes it with the response content type, optionally as
// an attachment. Mapping or service failures are written as HTTP error
// responses.
func (h *Handler) writeSitemapFileResponse(w http.ResponseWriter, r *http.Request, attachment bool) {
	request, err := MapRequestToDownloadSitemapByPathRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.DownloadSitemapByPath(r.Context(), request)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}

	w.Header().Set("Content-Type", response.ContentType)
	if attachment {
		w.Header().Set("Content-Disposition", `attachment; filename="`+response.FileName+`"`)
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(response.Content)
}

// getBaseResponseHandler builds a reply.Replier from the handler's layered
// response manifests.
func (h *Handler) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.responseManifests())
}

// responseManifests keeps success factories and error writers on the same
// domain base and last-wins caller override layers.
func (h *Handler) responseManifests() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(SitemapErrorMap).AddOverrides(h.errorMaps...).Build()
}

// NewHTTPErrorResponse preserves mapped wrappers and validation collections.
// It returns writer failures and never passes raw diagnostic causes to reply.
func (h *Handler) NewHTTPErrorResponse(w http.ResponseWriter, err error, attributes ...reply.ResponseAttributes) error {
	return errormanifest.WriteHTTPError(w, err, h.responseManifests(), attributes...)
}
