package user

import (
	"context"
	"errors"
	"math"
	"net/mail"
	"reflect"
	"slices"
	"strings"
	"time"
)

var (
	// ErrEmailChangeUnavailable rejects missing capabilities or invalid receipts.
	// It does not establish whether an uncertain write committed.
	ErrEmailChangeUnavailable = errors.New("UserEmailChangeUnavailable")
	// ErrEmailChangeConflict means the selected account snapshot no longer matches.
	ErrEmailChangeConflict = errors.New("UserEmailChangeConflict")
	// ErrEmailChangeRequired prevents ordinary profile updates bypassing revocation.
	ErrEmailChangeRequired = errors.New("UserEmailChangeRequired")
	// ErrEmailIndexesRequired requires the explicit unique email index migration.
	ErrEmailIndexesRequired = errors.New("UserEmailIndexesRequired")
)

// ChangeUserEmailRequest is a trusted, conditional domain command. The caller
// must authorize the operation; these fields are not a public HTTP payload.
type ChangeUserEmailRequest struct {
	// UserID identifies the selected account, never the requesting administrator.
	UserID string
	// Email is the requested new mailbox; ownership is not yet verified.
	Email string
	// ExpectedEmail is the exact previously read persisted address.
	ExpectedEmail string
	// ExpectedRevision is the current revocation revision; legacy absence is zero.
	ExpectedRevision int64
	// ExpectedStatus guards a concurrent suspension or other account transition.
	ExpectedStatus string
	// ExpectedType guards the configuration used to choose the destination status.
	ExpectedType string
}

// EmailChangeRepository is optional for custom adapters, but mandatory for
// changing a mailbox. Implementations must atomically enforce uniqueness and
// the full expected snapshot, clear email verification, increment EmailRevision,
// preserve unrelated fields and return the exact committed post-update account.
// An error may represent an unknown commit outcome; callers must not auto-retry.
type EmailChangeRepository interface {
	// ChangeUserEmail compares the selected snapshot and returns its committed
	// post-image, or an error that may indicate an uncertain write outcome.
	ChangeUserEmail(context.Context, *ChangeUserEmailRequest, string, time.Time) (*UniversalUser, error)
}

// nilUserDependency recognizes typed-nil ports without invoking their methods.
func nilUserDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// normalizeEmailChange snapshots scalar inputs and bounds revision arithmetic.
// ExpectedEmail remains exact for CAS; only the new address is normalized.
func normalizeEmailChange(req *ChangeUserEmailRequest) (ChangeUserEmailRequest, error) {
	if req == nil {
		return ChangeUserEmailRequest{}, ErrInvalidUserBody
	}
	r := *req
	if strings.TrimSpace(r.UserID) == "" || r.ExpectedEmail == "" || r.ExpectedStatus == "" || r.ExpectedRevision < 0 || r.ExpectedRevision == math.MaxInt64 {
		return r, ErrInvalidUserBody
	}
	r.Email = normaliseUserEmail(r.Email)
	parsed, err := mail.ParseAddress(r.Email)
	if err != nil || parsed.Address != r.Email || len(r.Email) > 254 {
		return r, ErrInvalidEmail
	}
	if r.Email == normaliseUserEmail(r.ExpectedEmail) {
		return r, ErrNoChangesDetected
	}
	return r, nil
}

// validEmailChangeReceipt checks the security fields of an acknowledged write.
// This is not a read-after-write: a malformed adapter receipt stays uncertain.
func validEmailChangeReceipt(account *UniversalUser, r ChangeUserEmailRequest, status string) bool {
	return account != nil && account.ID == r.UserID && account.Email == r.Email &&
		account.Type == r.ExpectedType && account.Status == status &&
		account.EmailRevision == r.ExpectedRevision+1 && account.Verification != nil &&
		!account.Verification.EmailVerified && account.Verification.EmailVerifiedAt == ""
}

// ChangeUserEmail uses the stored account type's configured EMAIL_CHANGE
// transition. Even configurations that do not require email verification lose
// the old mailbox's verified flag; they retain their configured default status.
// Session/proof consumers must compare the incremented revision with live state.
// Notification, cache cleanup and audit belong to the authorized manager flow.
func (s *Service) ChangeUserEmail(ctx context.Context, req *ChangeUserEmailRequest) (*UniversalUser, error) {
	if ctx == nil || s == nil || nilUserDependency(s.UserRepository) {
		return nil, ErrEmailChangeUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := normalizeEmailChange(req)
	if err != nil {
		return nil, err
	}
	// Service readers hydrate a default Type on legacy models; repository
	// readers return raw persisted state. Reload through the repository so
	// the CAS can guard missing Type without accepting a
	// concurrent change to a different configured account type.
	stored, err := s.UserRepository.GetUserByID(ctx, r.UserID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stored == nil || stored.ID != r.UserID {
		return nil, ErrEmailChangeUnavailable
	}
	config, err := s.resolveRequestedConfig(stored.Type)
	if err != nil {
		return nil, err
	}
	expectedType := r.ExpectedType
	if expectedType == "" {
		expectedType = s.defaultConfig().GetType(DefaultUserConfig())
	}
	if config.GetType(s.defaultConfig()) != expectedType || stored.Email != r.ExpectedEmail || stored.EmailRevision != r.ExpectedRevision || stored.Status != r.ExpectedStatus {
		return nil, ErrEmailChangeConflict
	}
	r.ExpectedType = stored.Type
	if config.DefaultStatus == "" || !slices.Contains(config.StatusTransitions[AccountStatusValidOriginKeyEmailChange], r.ExpectedStatus) {
		return nil, ErrUserInvalidStatusTransition
	}
	repo, ok := s.UserRepository.(EmailChangeRepository)
	if !ok {
		return nil, ErrEmailChangeUnavailable
	}
	// Keep expected values outside the adapter-owned request pointer.
	command := r
	status := config.DefaultStatus
	account, err := repo.ChangeUserEmail(ctx, &command, status, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if !validEmailChangeReceipt(account, r, status) {
		return nil, ErrEmailChangeUnavailable
	}
	// Do not replace a known committed receipt with a late cancellation error.
	copy := *account
	return s.setUserDependencies(&copy), nil
}
