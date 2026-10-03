package user

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"
)

var (
	// ErrRoleUpdateConflict rejects changed security or role snapshots; reload
	// before deciding whether to retry, rather than replaying a stale command.
	ErrRoleUpdateConflict = errors.New("UserRoleUpdateConflict")
	// ErrRoleUpdateUnavailable rejects missing capabilities or uncertain receipts.
	// A write may have committed even when its receipt cannot be confirmed.
	ErrRoleUpdateUnavailable = errors.New("UserRoleUpdateUnavailable")
)

// SetAccountRolesRequest is a trusted model-derived command, never an HTTP body.
// Repositories compare the complete ordered role snapshot and security state.
type SetAccountRolesRequest struct {
	// Account is the raw persisted identity/state before model hydration.
	Account AccountSnapshot
	// PreviousRoles is compared exactly; nil and empty both mean no roles.
	PreviousRoles []string
	// Roles is the validated ordered replacement of only the role field.
	Roles []string
	// At is one canonical UTC instant; updated_at changes only if roles change.
	At string
}

// AccountRolesRepository confirms narrow role writes, including idempotent
// no-ops, with acknowledged post-images. It must not retry, upsert or replace a
// user. A concurrent profile update is unrelated and must survive.
type AccountRolesRepository interface {
	SetAccountRoles(context.Context, *SetAccountRolesRequest) (*UniversalUser, error)
}

// copyRoleCommand isolates both slices from caller or adapter mutation.
func copyRoleCommand(c SetAccountRolesRequest) SetAccountRolesRequest {
	c.PreviousRoles = slices.Clone(c.PreviousRoles)
	c.Roles = slices.Clone(c.Roles)
	return c
}

// validRoleCommand checks selectors and time before any repository operation.
func validRoleCommand(c SetAccountRolesRequest) bool {
	at, err := time.Parse(time.RFC3339Nano, c.At)
	return strings.TrimSpace(c.Account.UserID) != "" && c.Account.Status != "" && c.Account.EmailRevision >= 0 && err == nil && !at.IsZero() && c.At == at.UTC().Format(time.RFC3339Nano)
}

// validRoleReceipt checks all owned fields. A no-op must not require an old
// updated_at value: a concurrent unrelated profile write may have advanced it.
func validRoleReceipt(v *UniversalUser, c SetAccountRolesRequest) bool {
	if !matchesAccountSnapshot(v, c.Account) || !slices.Equal(v.Roles, c.Roles) {
		return false
	}
	return slices.Equal(c.PreviousRoles, c.Roles) || (v.Metadata != nil && v.Metadata.UpdatedAt == c.At)
}

// updateAccountRole is a trusted domain operation, not caller authorization.
// Additions must satisfy the current account configuration. Removal can retire
// an obsolete role and removes all duplicates, so revocation cannot leave a
// second copy behind. Management owns actor-bound audit, only for actual changes.
func (s *Service) updateAccountRole(ctx context.Context, id, role string, remove bool) (*UniversalUser, bool, error) {
	if s == nil || ctx == nil || nilUserDependency(s.UserRepository) || nilUserDependency(s.TimeProvider) || nilUserDependency(s.StringUtils) {
		return nil, false, ErrRoleUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(id) == "" {
		return nil, false, ErrInvalidUserID
	}
	if strings.TrimSpace(role) == "" {
		return nil, false, ErrUserInvalidRole
	}
	repo, ok := s.UserRepository.(AccountRolesRepository)
	if !ok {
		return nil, false, ErrRoleUpdateUnavailable
	}
	stored, err := s.UserRepository.GetUserByID(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if stored == nil || stored.ID != id || stored.EmailRevision < 0 {
		return nil, false, ErrRoleUpdateUnavailable
	}
	config, err := s.resolveRequestedConfig(stored.Type)
	if err != nil {
		return nil, false, err
	}
	at := s.TimeProvider.Now().UTC()
	c := SetAccountRolesRequest{Account: AccountSnapshot{UserID: id, Email: stored.Email, EmailRevision: stored.EmailRevision, Type: stored.Type, Status: stored.Status}, PreviousRoles: slices.Clone(stored.Roles), At: at.Format(time.RFC3339Nano)}
	if !validRoleCommand(c) {
		return nil, false, ErrRoleUpdateUnavailable
	}
	v := copyUserForUpdate(stored)
	v.SetDependencies(config, s.IDGenerator, loginClock{at}, s.StringUtils)
	if remove {
		v.RemoveRole(role)
	} else {
		if !v.isValidRole(role) {
			return nil, false, ErrUserInvalidRole
		}
		v.AddRole(role)
	}
	c.Roles = slices.Clone(v.Roles)
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	expected := copyRoleCommand(c)
	result, err := repo.SetAccountRoles(ctx, &c)
	if err != nil {
		return nil, false, err
	}
	if !validRoleReceipt(result, expected) {
		return nil, false, ErrRoleUpdateUnavailable
	}
	// Late cancellation cannot undo an acknowledged write. Return the confirmed
	// outcome, allowing management to audit it without requesting a duplicate.
	return s.setUserDependencies(copyUserForUpdate(result)), !slices.Equal(expected.PreviousRoles, expected.Roles), nil
}
