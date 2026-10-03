package user

import (
	"context"
	"github.com/ooaklee/reply/v2"
)

// RoleManager owns HTTP-facing administrator authority and attributable audit.
// Implementations must use a live verifier; trusted domain methods alone do not
// satisfy this contract. Its native manifests join the handler response chain.
type RoleManager interface {
	AddUserRole(context.Context, *AddUserRoleRequest) (*AddUserRoleResponse, error)
	RemoveUserRole(context.Context, *RemoveUserRoleRequest) (*RemoveUserRoleResponse, error)
	RoleManagerErrorMaps() []reply.ErrorManifest
}

// WithRoleManager installs administrative role management during composition.
// The handler fails closed if omitted; Service is never an authority fallback.
func (h *Handler) WithRoleManager(manager RoleManager) *Handler {
	h.RoleManager = manager
	return h
}
