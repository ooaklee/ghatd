package blueprint

import (
	"github.com/ooaklee/reply"
)

// blueprintService manages business logic around blueprint request
type blueprintService interface {
}

// blueprintValidator expected methods of a valid
type blueprintValidator interface {
	Validate(s interface{}) error
}

// Handler manages blueprint requests
type Handler struct {
	service   blueprintService
	validator blueprintValidator
}

// NewHandler returns blueprint handler
func NewHandler(service blueprintService, validator blueprintValidator) *Handler {
	return &Handler{
		service:   service,
		validator: validator,
	}
}

// getBaseResponseHandler returns response handler configured with auth error map
// nolint will be used later
func getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(append([]reply.ErrorManifest{}, blueprintErrorMap))
}
