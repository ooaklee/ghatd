package user

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrStatusUpdateConflict rejects a security snapshot changed before the write.
	// Callers must reload the account; the command is never automatically replayed.
	ErrStatusUpdateConflict = errors.New("UserStatusUpdateConflict")
	// ErrStatusUpdateUnavailable rejects incomplete wiring or unconfirmed receipts.
	// An uncertain write may have committed; this error does not establish rollback.
	ErrStatusUpdateUnavailable = errors.New("UserStatusUpdateUnavailable")
)

// SetAccountStatusRequest is a trusted, model-derived command, not an HTTP body.
// Repository adapters must compare Account and change only the owned fields.
type SetAccountStatusRequest struct {
	// Account contains the raw persisted security values observed before validation.
	Account AccountSnapshot
	// Status is the resolved destination, never the EMAIL_CHANGE pseudo-transition.
	Status string
	// At is the single UTC RFC3339Nano instant for all transition timestamps.
	At string
	// ClearEmailVerification carries the configured EMAIL_CHANGE model side effect.
	// False leaves all verification fields unchanged, including concurrent writes.
	ClearEmailVerification bool
	// PreviousEmailVerified and PreviousEmailVerifiedAt guard the verification
	// fields when clearing them, so a concurrent verification cannot be erased.
	PreviousEmailVerified   bool
	PreviousEmailVerifiedAt string
}

// AccountStatusRepository persists configured status transitions without broad
// user replacement, upsert or retry. It returns an acknowledged post-image.
type AccountStatusRepository interface {
	SetAccountStatus(context.Context, *SetAccountStatusRequest) (*UniversalUser, error)
}

// validStatusCommand rejects malformed selectors and uncanonical clock values.
func validStatusCommand(c SetAccountStatusRequest) bool {
	at, err := time.Parse(time.RFC3339Nano, c.At)
	return strings.TrimSpace(c.Account.UserID) != "" && c.Account.Status != "" && c.Account.EmailRevision >= 0 &&
		c.Status != "" && c.Status != "EMAIL_CHANGE" && err == nil && !at.IsZero() && c.At == at.UTC().Format(time.RFC3339Nano)
}

// validStatusReceipt checks every owned field and the unchanged security identity.
// Unrelated fields may legitimately differ because the write preserves them.
func validStatusReceipt(v *UniversalUser, c SetAccountStatusRequest) bool {
	a := c.Account
	a.Status = c.Status
	if !matchesAccountSnapshot(v, a) || v.Metadata == nil || v.Metadata.UpdatedAt != c.At || v.Metadata.StatusChangedAt != c.At {
		return false
	}
	if c.Status == AccountStatusKeyActive && v.Metadata.ActivatedAt != c.At {
		return false
	}
	return !c.ClearEmailVerification || (v.Verification != nil && !v.Verification.EmailVerified && v.Verification.EmailVerifiedAt == "")
}

// updateAccountStatus validates configured model rules on a detached snapshot.
// Authorization and attributable audit belong to the manager; trusted internal
// callers must establish their own authority before calling UpdateUserStatus.
func (s *Service) updateAccountStatus(ctx context.Context, req *UpdateUserStatusRequest) (*UpdateUserStatusResponse, error) {
	if s == nil || ctx == nil || nilUserDependency(s.UserRepository) || nilUserDependency(s.TimeProvider) || nilUserDependency(s.StringUtils) {
		return nil, ErrStatusUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.ID) == "" || strings.TrimSpace(req.DesiredStatus) == "" {
		return nil, ErrInvalidUserBody
	}
	id, desired := req.ID, req.DesiredStatus
	repo, ok := s.UserRepository.(AccountStatusRepository)
	if !ok {
		return nil, ErrStatusUpdateUnavailable
	}
	stored, err := s.UserRepository.GetUserByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stored == nil || stored.ID != id || stored.EmailRevision < 0 {
		return nil, ErrStatusUpdateUnavailable
	}
	config, err := s.resolveRequestedConfig(stored.Type)
	if err != nil {
		return nil, err
	}
	at := s.TimeProvider.Now().UTC()
	c := SetAccountStatusRequest{Account: AccountSnapshot{UserID: id, Email: stored.Email, EmailRevision: stored.EmailRevision, Type: stored.Type, Status: stored.Status}, At: at.Format(time.RFC3339Nano)}
	if at.IsZero() {
		return nil, ErrStatusUpdateUnavailable
	}
	v := copyUserForUpdate(stored)
	v.SetDependencies(config, s.IDGenerator, loginClock{at}, s.StringUtils)
	if _, err := v.UpdateStatus(desired); err != nil {
		return nil, err
	}
	c.Status = v.Status
	c.ClearEmailVerification = desired == "EMAIL_CHANGE" && config.EmailVerificationRequired
	if stored.Verification != nil {
		c.PreviousEmailVerified, c.PreviousEmailVerifiedAt = stored.Verification.EmailVerified, stored.Verification.EmailVerifiedAt
	}
	if !validStatusCommand(c) {
		return nil, ErrStatusUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	expected := c // adapters cannot change the expected receipt by mutating input
	result, err := repo.SetAccountStatus(ctx, &c)
	if err != nil {
		return nil, err
	}
	if !validStatusReceipt(result, expected) {
		return nil, ErrStatusUpdateUnavailable
	}
	// An acknowledged write remains successful after late cancellation; the caller
	// must not infer that returning cancellation would roll it back.
	return &UpdateUserStatusResponse{User: s.setUserDependencies(copyUserForUpdate(result))}, nil
}
