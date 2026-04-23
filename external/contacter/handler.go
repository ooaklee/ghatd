package contacter

import (
	"context"
	"net/http"
)

// ContacterService interface defines expected methods of a valid contacter service
type ContacterService interface {
	CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error)
	GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error)
	UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error)
	GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error)
}

// ContacterValidator interface defines expected methods of a valid validator
type ContacterValidator interface {
	Validate(s interface{}) error
}

// Handler manages contacter requests
type Handler struct {
	Service   ContacterService
	Validator ContacterValidator
}

// NewHandler returns a new contacter handler
func NewHandler(service ContacterService, validator ContacterValidator) *Handler {
	return &Handler{
		Service:   service,
		Validator: validator,
	}
}

// GetCommsStats handles retrieving aggregated stats about platform comms
func (h *Handler) GetCommsStats(w http.ResponseWriter, r *http.Request) {
	request := &GetCommsStatsRequest{
		WithEmailRegex: r.URL.Query().Get("with_email_regex"),
	}

	if err := h.Validator.Validate(request); err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.Service.GetCommsStats(r.Context(), request)
	if err != nil {
		GetBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response)
}

