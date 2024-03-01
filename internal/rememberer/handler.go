package rememberer

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"text/template"
	"time"

	"github.com/ooaklee/reply"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/common"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"
	"go.uber.org/zap"
)

// remembererService manages business logic around rememberer request
type remembererService interface {
	DeleteWordById(ctx context.Context, r *DeleteWordRequest) error
	CreateWord(ctx context.Context, r *CreateWordRequest) (*CreateWordResponse, error)
	GetWords(ctx context.Context, r *GetWordsRequest) (*GetWordsResponse, error)
	GetWordById(ctx context.Context, r *GetWordByIdRequest) (*GetWordByIdResponse, error)
}

// remembererValidator expected methods of a valid
type remembererValidator interface {
	Validate(s interface{}) error
}

// Handler manages rememberer requests
type Handler struct {
	service         remembererService
	validator       remembererValidator
	embeddedContent fs.FS
}

// NewHandler returns rememberer handler
func NewHandler(service remembererService, validator remembererValidator, embeddedContent fs.FS) *Handler {
	return &Handler{
		service:         service,
		validator:       validator,
		embeddedContent: embeddedContent,
	}
}

// DeleteWord returns reponse after handling word delete request
func (h *Handler) DeleteWord(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	request, err := mapRequestToDeleteWordRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	err = h.service.DeleteWordById(r.Context(), request)
	if err != nil {
		logger.Warn(fmt.Sprintf("failed-to-deletes-word-with-id: %s", request.Id))
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Info(fmt.Sprintf("successfully-deleted-word-with-id: %s", request.Id))
	//nolint will set up default fallback later
	getBaseResponseHandler().NewHTTPBlankResponse(w, http.StatusOK)
}

// CreateWord returns reponse from word creation request
func (h *Handler) CreateWord(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	request, err := mapRequestToCreateWordRequest(r, h.validator)
	if err != nil {
		logger.Error(fmt.Sprintf("failed-to-create-word-with-name: %s", request.Name))
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.CreateWord(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Info(fmt.Sprintf("successfully-created-word-with-name-and-id: %s (%s)", response.Word.Name, response.Word.Id))
	//nolint will set up default fallback later
	getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusCreated, response.Word)
}

// GetWords returns response for request querying all the words
func (h *Handler) GetWords(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	request, err := mapRequestToGetWordsRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	words, err := h.service.GetWords(r.Context(), request)
	if err != nil {
		logger.Error("failed-to-retrieve-all-words-on-platform")
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	logger.Info("successfully-retrieve-all-words-on-platform")

	if strings.Contains(r.Header.Get(common.HtmxHttpRequestHeader), "true") {

		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedContent, "internal/webapp/ui/html/responses/get-words.tmpl.html")
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Added so that I can test the loader when using
		// hx-indicator
		time.Sleep(time.Second * 3)

		// Write template to response
		err = parsedTemplates.Execute(w, words)
		if err != nil {
			logger.Error("Unable to execute parsed template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, words.Words)

}

// GetWordById returns response for request looking for a specific word
func (h *Handler) GetWordById(w http.ResponseWriter, r *http.Request) {

	request, err := mapRequestToGetWordByIdRequest(r, h.validator)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	response, err := h.service.GetWordById(r.Context(), request)
	if err != nil {
		//nolint will set up default fallback later
		getBaseResponseHandler().NewHTTPErrorResponse(w, err)
		return
	}

	//nolint will set up default fallback later
	getBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response.Word)

}

// getBaseResponseHandler returns response handler configured with auth error map
// TODO: remove nolint
// nolint will be used later
func getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(append([]reply.ErrorManifest{}, remembererErrorMap))
}
