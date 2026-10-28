package contentmanager

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/ooaklee/ghatd/external/post"
)

// ChangelogSitemapService is an optional handler capability so existing custom
// content manager services need not implement the newly introduced operation.
type ChangelogSitemapService interface {
	GetChangelogSitemapItems(context.Context, *GetChangelogSitemapItemsRequest) (*GetChangelogSitemapItemsResponse, error)
}

// ChangelogSitemapHandler enables the optional administrative changelog source
// route. Custom handlers that omit this capability retain their existing routes.
type ChangelogSitemapHandler interface {
	GetChangelogSitemapItems(http.ResponseWriter, *http.Request)
}

// GetChangelogSitemapItems returns candidates in the existing ingestion data
// envelope. The route's administrator adapter enforces HTTP access. A custom
// service without this optional capability fails closed with unavailable.
func (h *Handler) GetChangelogSitemapItems(w http.ResponseWriter, r *http.Request) {
	req, err := mapRequestToGetChangelogSitemapItemsRequest(r, h.validator)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.service.(ChangelogSitemapService)
	if !ok || nilContentDependency(service) {
		_ = h.NewHTTPErrorResponse(w, ErrContentManagerUnavailable)
		return
	}
	result, err := service.GetChangelogSitemapItems(r.Context(), req)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	_ = h.getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, result)
}

func mapRequestToGetChangelogSitemapItemsRequest(r *http.Request, validator contentManagerValidator) (*GetChangelogSitemapItemsRequest, error) {
	if r == nil || r.URL == nil {
		return nil, post.ErrPostBadRequest
	}
	if r.Body != nil && r.ContentLength != 0 {
		body, err := io.ReadAll(io.LimitReader(r.Body, 8193))
		if err != nil || len(body) > 8192 || !json.Valid(body) {
			return nil, post.ErrInvalidPostQueryParam
		}
		r = r.Clone(r.Context())
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	// Reuse query/body precedence, validator behaviour and verified actor binding.
	return mapRequestToGetArticleSitemapItemsRequest(r, validator)
}
