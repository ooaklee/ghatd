package accesspolicymanager

import (
	"context"
	"github.com/ooaklee/ghatd/external/accesspolicy"
)

// capabilityManagementService keeps optional authority management independent
// of existing token-only handler implementations.
type capabilityManagementService interface {
	ReviewCapabilities(context.Context, string) (*accesspolicy.Grant, error)
	ApplyCapabilities(context.Context, string, int64, accesspolicy.Capabilities) (accesspolicy.Grant, error)
}

// ReviewCapabilities authorizes before resolving the selected stored user and
// reads the owning policy's detached snapshot, including disabled/expired grants.
func (s *Service) ReviewCapabilities(ctx context.Context, userID string) (*accesspolicy.Grant, error) {
	if err := s.capabilityTarget(ctx, userID); err != nil {
		return nil, err
	}
	return s.policy.ReviewGrant(ctx, accesspolicy.Subject{System: s.system, Kind: accesspolicy.UserSubject, ID: userID})
}

// ApplyCapabilities replaces only explicit authority fields under the reviewed
// revision. It neither provisions tokens nor derives permission from a role.
// The lower service owns preservation, current authorization, CAS and audit.
func (s *Service) ApplyCapabilities(ctx context.Context, userID string, expected int64, patch accesspolicy.Capabilities) (accesspolicy.Grant, error) {
	if expected < 0 || expected >= 9007199254740991 || accesspolicy.ValidateCapabilities(patch) != nil {
		return accesspolicy.Grant{}, ErrInvalidRequest
	}
	if err := s.capabilityTarget(ctx, userID); err != nil {
		return accesspolicy.Grant{}, err
	}
	return s.policy.ApplyCapabilities(ctx, accesspolicy.Subject{System: s.system, Kind: accesspolicy.UserSubject, ID: userID}, expected, patch)
}

// capabilityTarget verifies live administrative authority before existence is
// disclosed. Unknown or joined identity failures cannot provision a policy.
func (s *Service) capabilityTarget(ctx context.Context, userID string) error {
	if !identifier(userID) {
		return ErrInvalidRequest
	}
	if err := s.checkManagement(ctx); err != nil {
		return err
	}
	user, err := s.users.GetUserByID(ctx, userID)
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if missingTarget(err) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if user == nil || user.ID != userID {
		return accesspolicy.ErrConfiguration
	}
	return nil
}
