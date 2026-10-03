package accessmanager

import (
	"context"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/reply/v2"
)

// AdministratorErrorMaps exposes the canonical native failures of this port.
// Consumers compose these maps before their own host overrides.
func (s *Service) AdministratorErrorMaps() []reply.ErrorManifest {
	return errormanifest.NewComposer().Add(AccessmanagerErrorMap).Add(DependencyErrorMaps()...).Build()
}

// AuthorizeAdministrator rechecks trusted middleware context against the live
// session and current account, including email revision/type, ACTIVE status and
// administrator role. Signed role flags, API credentials and identity alone are
// insufficient. It returns the verified actor, never a transport-selected target.
// This is a point-in-time check, not a transaction with downstream mutations.
func (s *Service) AuthorizeAdministrator(ctx context.Context) (string, error) {
	if s == nil || ctx == nil || nilAccessDependency(s.UserService) || nilAccessDependency(s.EphemeralStore) {
		return "", ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	session := helpers.AcquireSessionFrom(ctx)
	if session == nil {
		return "", auth.ErrUnauthorized
	}
	current, err := s.authenticateTokenDetails(ctx, session)
	if err != nil {
		return "", err
	}
	current, err = requireAdministrator(current)
	if err != nil {
		return "", err
	}
	return current.UserID, nil
}

// requireAdministrator is shared by HTTP middleware and manager authorization;
// callers must first verify live session ownership and account identity.
func requireAdministrator(current *MiddlewareAuthedUserResponse) (*MiddlewareAuthedUserResponse, error) {
	current, err := requireActiveSession(current)
	if err != nil {
		return nil, err
	}
	if !current.User.IsAdmin() {
		return nil, ErrUnauthorizedAdminAccessAttempted
	}
	return current, nil
}
