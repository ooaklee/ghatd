package usermanager

import (
	"context"
	"slices"
	"strings"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// AccountRoleService is the configured domain's narrow role-management port.
// Its receipts report actual changes, so management does not audit no-ops.
type AccountRoleService interface {
	AddUserRole(context.Context, *user.AddUserRoleRequest) (*user.AddUserRoleResponse, error)
	RemoveUserRole(context.Context, *user.RemoveUserRoleRequest) (*user.RemoveUserRoleResponse, error)
}

// ChangeAccountRoleRequest separates a verified caller from the selected user.
// These are trusted manager fields; HTTP adapters cannot decode the actor.
type ChangeAccountRoleRequest struct {
	// ActorID must match both trusted context and the live administrator verifier.
	ActorID string `json:"-" query:"-" form:"-"`
	// TargetUserID is independent and may equal ActorID under existing policy.
	TargetUserID string
	// Role uses exact configured spelling; removal can retire obsolete roles.
	Role string
	// Remove selects revocation of all occurrences rather than addition.
	Remove bool
}

// RoleManagerErrorMaps composes the same native manager/domain/verifier chain
// as status management. User-handler overrides remain the final layer.
func (s *Service) RoleManagerErrorMaps() []reply.ErrorManifest { return s.StatusManagerErrorMaps() }

// ChangeAccountRole rechecks live administrator authority before a narrow domain
// command, validates its receipt and audits only a confirmed change. Audit is
// best effort; failure does not turn an acknowledged mutation into a replay.
// The authorization check and write are not a revocation transaction.
func (s *Service) ChangeAccountRole(ctx context.Context, req *ChangeAccountRoleRequest) (*user.UniversalUser, bool, error) {
	if req == nil {
		return nil, false, ErrInvalidUserBody
	}
	r := *req
	if err := s.administratorActor(ctx, r.ActorID, user.ErrRoleUpdateUnavailable); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(r.TargetUserID) == "" {
		return nil, false, user.ErrInvalidUserID
	}
	if strings.TrimSpace(r.Role) == "" {
		return nil, false, user.ErrUserInvalidRole
	}
	domain, ok := s.UserService.(AccountRoleService)
	if !ok || nilProfilePort(domain) {
		return nil, false, user.ErrRoleUpdateUnavailable
	}
	var result *user.UniversalUser
	var changed bool
	if r.Remove {
		response, err := domain.RemoveUserRole(ctx, &user.RemoveUserRoleRequest{ID: r.TargetUserID, Role: r.Role})
		if err != nil {
			return nil, false, err
		}
		if response != nil {
			result, changed = response.User, response.Changed
		}
	} else {
		response, err := domain.AddUserRole(ctx, &user.AddUserRoleRequest{ID: r.TargetUserID, Role: r.Role})
		if err != nil {
			return nil, false, err
		}
		if response != nil {
			result, changed = response.User, response.Changed
		}
	}
	if result == nil || result.ID != r.TargetUserID || slices.Contains(result.Roles, r.Role) == r.Remove {
		return nil, false, user.ErrRoleUpdateUnavailable
	}
	if changed && !nilProfilePort(s.AuditService) {
		action := audit.AuditAction("user.role_added")
		if r.Remove {
			action = "user.role_removed"
		}
		if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: r.ActorID, Action: action, TargetId: r.TargetUserID, TargetType: audit.TargetTypeUser, Details: map[string]interface{}{"role": r.Role}}); err != nil {
			logger.AcquireOperationFrom(ctx, "external/usermanager", "change-account-role").Warn("role-audit-delivery-failed")
		}
	}
	return result, changed, nil
}

// AddUserRole binds only a trusted-context actor to the existing HTTP payload.
func (s *Service) AddUserRole(ctx context.Context, req *user.AddUserRoleRequest) (*user.AddUserRoleResponse, error) {
	if ctx == nil {
		return nil, user.ErrRoleUpdateUnavailable
	}
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	v, changed, err := s.ChangeAccountRole(ctx, &ChangeAccountRoleRequest{ActorID: helpers.AcquireAuthenticatedUserIDFrom(ctx), TargetUserID: req.ID, Role: req.Role})
	if err != nil {
		return nil, err
	}
	return &user.AddUserRoleResponse{User: v, Changed: changed}, nil
}

// RemoveUserRole preserves the HTTP shape without letting a body select actor.
func (s *Service) RemoveUserRole(ctx context.Context, req *user.RemoveUserRoleRequest) (*user.RemoveUserRoleResponse, error) {
	if ctx == nil {
		return nil, user.ErrRoleUpdateUnavailable
	}
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	v, changed, err := s.ChangeAccountRole(ctx, &ChangeAccountRoleRequest{ActorID: helpers.AcquireAuthenticatedUserIDFrom(ctx), TargetUserID: req.ID, Role: req.Role, Remove: true})
	if err != nil {
		return nil, err
	}
	return &user.RemoveUserRoleResponse{User: v, Changed: changed}, nil
}

var _ user.RoleManager = (*Service)(nil)
