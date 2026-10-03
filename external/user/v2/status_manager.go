package user

import (
	"context"

	"github.com/ooaklee/reply/v2"
)

// StatusManager is the administrative orchestration port behind the existing
// status HTTP routes. Implementations bind actors from verified request context,
// recheck live authority and audit each acknowledged domain transition.
type StatusManager interface {
	UpdateUserStatus(context.Context, *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error)
	BulkUpdateUsersStatus(context.Context, *BulkUpdateUsersStatusRequest) (*BulkUpdateUsersStatusResponse, error)
	// StatusManagerErrorMaps supplies native dependency mappings, not diagnostics.
	StatusManagerErrorMaps() []reply.ErrorManifest
}

// WithStatusManager installs administrative orchestration at startup. Missing or
// typed-nil wiring fails closed; handlers never fall back to trusted domain calls.
func (h *Handler) WithStatusManager(manager StatusManager) *Handler {
	h.StatusManager = manager
	return h
}
