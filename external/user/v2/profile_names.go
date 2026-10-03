package user

import (
	"context"
	"errors"
	"strings"
	"time"
)

var (
	// ErrProfileUpdateUnavailable rejects missing adapters or malformed receipts.
	// A failed write does not establish that no change was committed.
	ErrProfileUpdateUnavailable = errors.New("UserProfileUpdateUnavailable")
	// ErrProfileUpdateConflict means the selected names or account state changed.
	ErrProfileUpdateConflict = errors.New("UserProfileUpdateConflict")
)

// ProfileNames contains only the presentation fields owned by a name update.
type ProfileNames struct {
	// FirstName is the given name, not an authentication identifier.
	FirstName string
	// LastName is the family name, not an authentication identifier.
	LastName string
	// FullName is the derived display name under the configured string policy.
	FullName string
}

// UpdateProfileNamesRequest is a trusted domain command. An authorized manager
// supplies the live account snapshot; these selectors are not an HTTP payload.
type UpdateProfileNamesRequest struct {
	// UserID selects the account authorized by the manager.
	UserID string
	// FirstName replaces the given name when nonempty; empty retains it.
	FirstName string
	// LastName replaces the family name when nonempty; empty retains it.
	LastName string
	// ExpectedEmail protects the exact mailbox observed during authorization.
	ExpectedEmail string
	// ExpectedRevision protects the observed email revocation revision.
	ExpectedRevision int64
	// ExpectedType is the observed configured account type, not a requested type.
	ExpectedType string
	// ExpectedStatus protects the observed account status.
	ExpectedStatus string
}

// SetProfileNamesRequest is a persistence command prepared by the user domain.
// Adapters must compare every expected field and atomically return the post-image.
type SetProfileNamesRequest struct {
	// Account carries the exact raw persisted security snapshot (legacy type may be empty).
	Account UpdateProfileNamesRequest
	// Before is the exact stored name snapshot, with absent/null fields read as empty.
	Before ProfileNames
	// After is the normalized replacement for only these three name fields.
	After ProfileNames
	// UpdatedAt is the UTC audit timestamp for the acknowledged profile write.
	UpdatedAt string
}

// ProfileNamesRepository is deliberately narrower than whole-user persistence.
// Implementations preserve every other field, do not upsert or retry a write,
// and return an acknowledged post-image. Errors may represent uncertain writes.
type ProfileNamesRepository interface {
	// SetProfileNames compares the supplied snapshot and returns its post-image.
	SetProfileNames(context.Context, *SetProfileNamesRequest) (*UniversalUser, error)
}

// profileNames reads an immutable scalar snapshot, including legacy nil objects.
func profileNames(account *UniversalUser) ProfileNames {
	if account.PersonalInfo == nil {
		return ProfileNames{}
	}
	return ProfileNames{account.PersonalInfo.FirstName, account.PersonalInfo.LastName, account.PersonalInfo.FullName}
}

// validProfileAccount matches security fields without accepting a different owner.
func validProfileAccount(account *UniversalUser, r UpdateProfileNamesRequest) bool {
	return account != nil && account.ID == r.UserID && account.Email == r.ExpectedEmail &&
		account.EmailRevision == r.ExpectedRevision && account.Status == r.ExpectedStatus && account.Type == r.ExpectedType
}

// UpdateProfileNames normalizes names using the stored account configuration and
// delegates a conditional, field-level write. It never sends a full user snapshot
// to UpdateUser. Empty names retain existing values; an unchanged request is a
// read-only success at the observed snapshot. No client revision or ABA protection
// is implied. Authorization and auditing belong to the calling manager.
func (s *Service) UpdateProfileNames(ctx context.Context, req *UpdateProfileNamesRequest) (*UniversalUser, error) {
	if s == nil || ctx == nil || nilUserDependency(s.UserRepository) {
		return nil, ErrProfileUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.UserID) == "" || req.ExpectedEmail == "" || req.ExpectedRevision < 0 || req.ExpectedStatus == "" {
		return nil, ErrInvalidUserBody
	}
	input := *req
	repo, ok := s.UserRepository.(ProfileNamesRepository)
	if !ok {
		return nil, ErrProfileUpdateUnavailable
	}
	stored, err := s.UserRepository.GetUserByID(ctx, input.UserID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if stored == nil || stored.ID != input.UserID {
		return nil, ErrProfileUpdateUnavailable
	}
	config, err := s.resolveRequestedConfig(stored.Type)
	if err != nil {
		return nil, err
	}
	expectedType := input.ExpectedType
	if expectedType == "" {
		expectedType = s.defaultConfig().GetType(DefaultUserConfig())
	}
	if config.GetType(s.defaultConfig()) != expectedType {
		return nil, ErrProfileUpdateConflict
	}
	input.ExpectedType = stored.Type
	if !validProfileAccount(stored, input) {
		return nil, ErrProfileUpdateConflict
	}
	before := profileNames(stored)
	if (input.FirstName == "" || input.FirstName == before.FirstName) && (input.LastName == "" || input.LastName == before.LastName) {
		copy := *stored
		copy.Type = config.GetType(s.defaultConfig())
		return &copy, nil
	}
	// A changing name needs a real normalization policy; missing wiring must not
	// silently leave full_name describing the old names.
	if nilUserDependency(s.StringUtils) {
		return nil, ErrProfileUpdateUnavailable
	}
	// Dependency injection and validation may initialize mutable model fields.
	// Build a detached model; the adapter-owned stored account remains untouched.
	value := *stored
	personal := PersonalInfo{}
	if stored.PersonalInfo != nil {
		personal = *stored.PersonalInfo
	}
	value.PersonalInfo = &personal
	value.Metadata = nil
	value.SetDependencies(config, s.IDGenerator, s.TimeProvider, s.StringUtils)
	if input.FirstName != "" {
		personal.FirstName = input.FirstName
	}
	if input.LastName != "" {
		personal.LastName = input.LastName
	}
	// Match the existing model's configured name normalization without changing
	// unrelated email, roles, verification or extension data.
	personal.FirstName = s.StringUtils.ToTitleCase(personal.FirstName)
	personal.LastName = s.StringUtils.ToTitleCase(personal.LastName)
	value.SetFullName()
	if err := value.Validate(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	after := profileNames(&value)
	if after == before {
		copy := *stored
		copy.Type = config.GetType(s.defaultConfig())
		return &copy, nil
	}
	at := time.Now()
	if !nilUserDependency(s.TimeProvider) {
		// The legacy NowUTC formatter intentionally omits a zone. Use the typed
		// clock value to produce this command's unambiguous UTC timestamp.
		at = s.TimeProvider.Now()
	}
	if at.IsZero() {
		return nil, ErrProfileUpdateUnavailable
	}
	stamp := at.UTC().Format(time.RFC3339Nano)
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		return nil, ErrProfileUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := SetProfileNamesRequest{Account: input, Before: before, After: after, UpdatedAt: stamp}
	// Keep expected receipt values outside the pointer exposed to the adapter.
	expected := command
	result, err := repo.SetProfileNames(ctx, &command)
	if err != nil {
		return nil, err
	}
	if !validProfileAccount(result, expected.Account) || profileNames(result) != expected.After || result.Metadata == nil || result.Metadata.UpdatedAt != expected.UpdatedAt {
		return nil, ErrProfileUpdateUnavailable
	}
	// A late cancellation cannot turn an acknowledged, valid post-image into an
	// invented rollback. Native errors from uncertain writes are returned above.
	copy := *result
	copy.Type = config.GetType(s.defaultConfig())
	return &copy, nil
}
