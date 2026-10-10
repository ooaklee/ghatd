package user

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrLoginStateConflict means the account changed after proof authorization.
	// A consumed proof is not reusable; the manager owns recovery instructions.
	ErrLoginStateConflict = errors.New("UserLoginStateConflict")
	// ErrLoginStateUnavailable rejects incomplete wiring or unconfirmed receipts.
	// An uncertain write may have committed; never automatically replay it.
	ErrLoginStateUnavailable = errors.New("UserLoginStateUnavailable")
)

// AccountSnapshot is a trusted security snapshot, not an HTTP payload or proof.
// A manager establishes authority before supplying it to a narrow domain command.
type AccountSnapshot struct {
	// UserID selects the independently authorized account.
	UserID string
	// Email is the exact mailbox observed during authorization.
	Email string
	// EmailRevision is the observed email-bound credential revocation generation.
	EmailRevision int64
	// Type is the observed account configuration type, not a requested new type.
	Type string
	// Status is the observed state, not a requested transition destination.
	Status string
}

// SetLoginStateRequest contains only selectors and one UTC transition instant.
// Repository adapters compare the raw persisted type, including legacy empties.
type SetLoginStateRequest struct {
	// Account is the exact security state to compare atomically with the write.
	Account AccountSnapshot
	// At stamps both login fields and, for activation, its verification/state fields.
	At string
}

// LoginStateRepository persists only fields owned by the selected transition.
// Implementations return acknowledged post-images and never upsert or retry.
type LoginStateRepository interface {
	// SetFreshLogin requires ACTIVE and changes only last_login_at/last_fresh_login_at.
	SetFreshLogin(context.Context, *SetLoginStateRequest) (*UniversalUser, error)
	// SetVerifiedEmailActivation requires PROVISIONED and activates that mailbox.
	SetVerifiedEmailActivation(context.Context, *SetLoginStateRequest) (*UniversalUser, error)
}

// matchesAccountSnapshot compares exact persisted security values, not roles.
func matchesAccountSnapshot(v *UniversalUser, a AccountSnapshot) bool {
	return v != nil && v.ID == a.UserID && v.Email == a.Email && v.EmailRevision == a.EmailRevision && v.Type == a.Type && v.Status == a.Status
}

// loginSource selects the only allowed source state for each narrow command.
func loginSource(activate bool) string {
	if activate {
		return AccountStatusKeyProvisioned
	}
	return AccountStatusKeyActive
}

// validLoginSnapshot rejects malformed commands before any storage operation.
func validLoginSnapshot(a AccountSnapshot, activate bool) bool {
	return strings.TrimSpace(a.UserID) != "" && a.Email != "" && a.EmailRevision >= 0 && a.Status == loginSource(activate)
}

// validLoginReceipt checks the acknowledged owned fields, not every user field.
// Concurrent unrelated updates must survive and may legitimately change roles.
func validLoginReceipt(v *UniversalUser, command SetLoginStateRequest, activate bool) bool {
	a := command.Account
	a.Status = AccountStatusKeyActive
	if !matchesAccountSnapshot(v, a) || v.Metadata == nil || v.Metadata.LastLoginAt != command.At || v.Metadata.LastFreshLoginAt != command.At {
		return false
	}
	return !activate || (v.Verification != nil && v.Verification.EmailVerified && v.Verification.EmailVerifiedAt == command.At && v.Metadata.ActivatedAt == command.At && v.Metadata.StatusChangedAt == command.At && v.Metadata.UpdatedAt == command.At)
}

// loginClock makes model validation/transitions use one typed UTC instant.
// Unlike the legacy formatter, NowUTC retains its explicit timezone.
type loginClock struct{ at time.Time }

// Now returns the single instant captured by this login clock, making model
// transitions use one consistent time.
func (c loginClock) Now() time.Time { return c.at }

// NowUTC formats the captured instant as RFC3339Nano, retaining its explicit
// timezone rather than forcing UTC formatting.
func (c loginClock) NowUTC() string { return c.at.Format(time.RFC3339Nano) }

// RecordFreshLogin confirms a live ACTIVE account without replacing its profile.
// It records a consumed proof's fresh login, but does not authenticate, consume
// proofs, mint tokens or audit. Those operations belong to the calling manager.
func (s *Service) RecordFreshLogin(ctx context.Context, req *AccountSnapshot) (*UniversalUser, error) {
	return s.updateLoginState(ctx, req, false)
}

// ActivateVerifiedEmail conditionally moves PROVISIONED to ACTIVE after a manager
// has verified and consumed a mailbox proof. It cannot reactivate other states.
func (s *Service) ActivateVerifiedEmail(ctx context.Context, req *AccountSnapshot) (*UniversalUser, error) {
	return s.updateLoginState(ctx, req, true)
}

// updateLoginState rechecks authority-relevant state and validates configured
// model rules on a detached copy. Its storage command never contains that model.
func (s *Service) updateLoginState(ctx context.Context, req *AccountSnapshot, activate bool) (*UniversalUser, error) {
	if s == nil || ctx == nil || nilUserDependency(s.UserRepository) || nilUserDependency(s.TimeProvider) || nilUserDependency(s.StringUtils) {
		return nil, ErrLoginStateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || !validLoginSnapshot(*req, activate) {
		return nil, ErrInvalidUserBody
	}
	a := *req
	repo, ok := s.UserRepository.(LoginStateRepository)
	if !ok {
		return nil, ErrLoginStateUnavailable
	}
	stored, err := s.UserRepository.GetUserByID(ctx, a.UserID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stored == nil || stored.ID != a.UserID {
		return nil, ErrLoginStateUnavailable
	}
	config, err := s.resolveRequestedConfig(stored.Type)
	if err != nil {
		return nil, err
	}
	expectedType := a.Type
	if expectedType == "" {
		expectedType = s.defaultConfig().GetType(DefaultUserConfig())
	}
	if config.GetType(s.defaultConfig()) != expectedType {
		return nil, ErrLoginStateConflict
	}
	a.Type = stored.Type
	if !matchesAccountSnapshot(stored, a) {
		return nil, ErrLoginStateConflict
	}
	at := s.TimeProvider.Now().UTC()
	if at.IsZero() {
		return nil, ErrLoginStateUnavailable
	}
	stamp := at.Format(time.RFC3339Nano)
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		return nil, ErrLoginStateUnavailable
	}
	value := copyUserForUpdate(stored)
	value.SetDependencies(config, s.IDGenerator, loginClock{at}, s.StringUtils)
	if activate {
		value.VerifyEmail()
		if _, err := value.UpdateStatus(AccountStatusKeyActive); err != nil {
			return nil, err
		}
	}
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := SetLoginStateRequest{Account: a, At: stamp}
	expected := command
	var result *UniversalUser
	if activate {
		result, err = repo.SetVerifiedEmailActivation(ctx, &command)
	} else {
		result, err = repo.SetFreshLogin(ctx, &command)
	}
	if err != nil {
		return nil, err
	}
	if !validLoginReceipt(result, expected, activate) {
		return nil, ErrLoginStateUnavailable
	}
	// Preserve native failures above. An acknowledged receipt is not a rollback
	// merely because context was canceled after storage completed.
	return s.setUserDependencies(copyUserForUpdate(result)), nil
}
