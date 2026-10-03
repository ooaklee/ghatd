package accessmanager

import (
	"context"
	"unicode"
	"unicode/utf8"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// PolicyManagementAuthorizer binds administrative policy operations to one
// configured system and an already authenticated session context. Every call
// rechecks the live session, identity/email/type revision and current ACTIVE
// administrator account. API credentials, anonymous contexts and signed admin
// flags alone never authorize management. No bearer is copied into context.
// This uses the existing live account administrator authority; it does not seed
// an administrator grant or infer credential permissions from account roles.
func (s *Service) PolicyManagementAuthorizer(system string) (accesspolicy.ManagementAuthorizer, error) {
	if s == nil || s.UserService == nil || s.EphemeralStore == nil || system == "" || len(system) > 256 || !utf8.ValidString(system) {
		return nil, accesspolicy.ErrConfiguration
	}
	for _, r := range system {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return nil, accesspolicy.ErrConfiguration
		}
	}
	return func(ctx context.Context, requestedSystem string) (string, error) {
		if ctx == nil {
			return "", accesspolicy.ErrConfiguration
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if requestedSystem != system {
			return "", accesspolicy.ErrDenied
		}
		session := helpers.AcquireSessionFrom(ctx)
		if session == nil {
			return "", accesspolicy.ErrDenied
		}
		current, err := s.authenticateTokenDetails(ctx, session)
		if err != nil {
			return "", err
		}
		if current.User.Status != userv2.AccountStatusKeyActive || !current.User.IsAdmin() {
			return "", accesspolicy.ErrDenied
		}
		return current.UserID, nil
	}, nil
}
