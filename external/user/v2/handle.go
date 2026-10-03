package user

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/toolbox"
)

var (
	// ErrInvalidHandle rejects noncanonical or unsupported display names.
	ErrInvalidHandle = errors.New("UserInvalidHandle")
	// ErrHandleTaken means the exact requested name belongs to another account.
	ErrHandleTaken = errors.New("UserHandleTaken")
	// ErrHandleConflict requires rereading the current handle revision/state.
	ErrHandleConflict = errors.New("UserHandleConflict")
	// ErrHandleUnsupported requires the optional handle persistence capability.
	ErrHandleUnsupported = errors.New("UserHandleUnsupported")
	// ErrHandleIndexesRequired requires the explicit unique handle index migration.
	ErrHandleIndexesRequired = errors.New("UserHandleIndexesRequired")
	// ErrHandleExhausted bounds candidate searches; it does not imply an outage.
	ErrHandleExhausted = errors.New("UserHandleExhausted")
)

// MaxHandleRevision keeps revision/count values exact in JSON number clients.
const MaxHandleRevision int64 = 9007199254740991
const handleAttempts = 100

// handleSuggestionAttempts caps interactive validation at ten indexed lookups:
// the desired handle and at most nine alternatives. Exhaustion is not an error.
const handleSuggestionAttempts = 10

// WithHandleGenerator configures an optional display-name source before serving.
// Names are normalized/truncated and still require atomic unique insertion.
// The function must be concurrency-safe and is never used for authentication.
// Fewer than three usable characters or a leading digit fails creation with
// ErrInvalidHandle; this hook must be configured before concurrent requests.
func (s *Service) WithHandleGenerator(generate func() string) *Service {
	s.handleGenerator = generate
	return s
}

// generatedHandle reduces codename words to a bounded ASCII display candidate.
func generatedHandle(name string) (string, error) {
	var result strings.Builder
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			result.WriteRune(c)
		} else {
			result.WriteByte('-')
		}
	}
	parts := strings.FieldsFunc(result.String(), func(c rune) bool { return c == '-' })
	value := strings.Join(parts, "-")
	if len(value) > 30 {
		value = value[:30]
	}
	return NormalizeHandle(strings.TrimRight(value, "-"))
}

// HandleMetadata records handle changes independently of email/profile updates.
// Timestamps are UTC RFC3339Nano. There are no aliases or retained previous names.
type HandleMetadata struct {
	// CreatedAt is set on the first assignment, including a legacy user's first one.
	CreatedAt string `json:"created_at,omitempty" bson:"created_at"`
	// UpdatedAt records the last assignment, not a read or same-handle no-op.
	UpdatedAt string `json:"updated_at,omitempty" bson:"updated_at"`
	// LastUserChangeAt excludes automatic generation during account creation.
	LastUserChangeAt string `json:"last_user_change_at,omitempty" bson:"last_user_change_at,omitempty"`
	// Revision is zero before assignment and increases on each actual change.
	Revision int64 `json:"revision" bson:"revision"`
	// ChangeCount counts manual assignments, including a legacy user's first one.
	ChangeCount int64 `json:"change_count" bson:"change_count"`
}

// UserHandle is a self-service projection, not a complete user or credential.
type UserHandle struct {
	// Handle is the bare canonical name; clients may display it with an @ prefix.
	Handle string `json:"handle"`
	// Metadata is a value copy so callers cannot mutate the stored model snapshot.
	Metadata HandleMetadata `json:"metadata"`
}

// ValidateUserHandleRequest checks availability for one trusted target account.
type ValidateUserHandleRequest struct {
	// UserID is server-resolved; it is not accepted from a self-service HTTP body.
	UserID string
	// Handle is normalized before checking syntax and availability.
	Handle string
}

// ValidateUserHandleResponse is advisory; suggestions do not reserve any name.
type ValidateUserHandleResponse struct {
	// Handle is the normalized requested name.
	Handle string `json:"handle"`
	// Available includes the target account already owning this exact handle.
	Available bool `json:"available"`
	// Suggestion is one bounded numeric-suffix candidate when the name is taken.
	Suggestion string `json:"suggestion,omitempty"`
}

// UpdateUserHandleRequest is a trusted domain command with an explicit revision.
type UpdateUserHandleRequest struct {
	// UserID is the persistent target identity; handle text cannot select a user.
	UserID string
	// Handle is the exact desired candidate; updates never silently add a suffix.
	Handle string
	// ExpectedRevision is the previously read self-handle revision (zero if unset).
	ExpectedRevision int64
}

// HandleRepository is optional, leaving the legacy UserRepository unchanged.
// Implementations must enforce uniqueness and revision/state checks atomically.
type HandleRepository interface {
	// RequireHandleStorage verifies the configured uniqueness constraint exists.
	RequireHandleStorage(context.Context) error
	// HandleAvailable excludes only the given persistent owner, never another ID.
	HandleAvailable(context.Context, string, string) (bool, error)
	// SetUserHandle requires ACTIVE status and returns the exact write/no-op image.
	// Only a proven handle-index collision returns the exact ErrHandleTaken sentinel.
	SetUserHandle(context.Context, *UpdateUserHandleRequest, time.Time) (*UserHandle, error)
}

// NormalizeHandle accepts one optional @ and outer whitespace, then validates
// 3..30 ASCII characters: leading letter, alphanumerics and single -/_ separators.
// Unicode lookalikes, adjacent separators and trailing separators are rejected.
func NormalizeHandle(input string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(input), "@")
	if len(value) < 3 || len(value) > 30 {
		return "", ErrInvalidHandle
	}
	bytes := []byte(value)
	separator := false
	for i, c := range bytes {
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
			bytes[i] = c
		}
		letter := c >= 'a' && c <= 'z'
		digit := c >= '0' && c <= '9'
		sep := c == '-' || c == '_'
		if (!letter && !digit && !sep) || (i == 0 && !letter) || (sep && (separator || i == len(bytes)-1)) {
			return "", ErrInvalidHandle
		}
		separator = sep
	}
	return string(bytes), nil
}

// handleCandidate bounds a numeric suffix without producing a terminal separator.
func handleCandidate(base string, attempt int) string {
	if attempt == 0 {
		return base
	}
	suffix := "-" + strconv.Itoa(attempt)
	if len(base)+len(suffix) > 30 {
		base = base[:30-len(suffix)]
	}
	return strings.TrimRight(base, "-_") + suffix
}

// handleView copies valid lifecycle state. Malformed persisted metadata fails
// closed instead of presenting a misleading revision that could overwrite it.
func handleView(user *UniversalUser) (*UserHandle, error) {
	if user == nil {
		return nil, ErrUserNotFound
	}
	if user.Handle == "" && user.HandleMetadata == nil {
		return &UserHandle{}, nil
	}
	canonical, err := NormalizeHandle(user.Handle)
	m := user.HandleMetadata
	if err != nil || canonical != user.Handle || m == nil || m.Revision < 1 || m.Revision > MaxHandleRevision || m.ChangeCount < 0 || m.ChangeCount > m.Revision {
		return nil, ErrHandleConflict
	}
	for _, stamp := range []string{m.CreatedAt, m.UpdatedAt} {
		if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
			return nil, ErrHandleConflict
		}
	}
	if m.LastUserChangeAt != "" {
		if _, err := time.Parse(time.RFC3339Nano, m.LastUserChangeAt); err != nil {
			return nil, ErrHandleConflict
		}
	}
	return &UserHandle{Handle: user.Handle, Metadata: *m}, nil
}

// handleTarget loads live state for self-service operations; restricted users
// cannot probe or mutate handles even through a trusted in-process caller.
func (s *Service) handleTarget(ctx context.Context, id string) (*UserHandle, HandleRepository, error) {
	response, err := s.GetUserByID(ctx, &GetUserByIDRequest{ID: id})
	if err != nil {
		return nil, nil, err
	}
	if response.User.ID != id || response.User.Status != "ACTIVE" {
		return nil, nil, ErrUnauthorisedAccess
	}
	repo, ok := s.UserRepository.(HandleRepository)
	if !ok {
		return nil, nil, ErrHandleUnsupported
	}
	if err := repo.RequireHandleStorage(ctx); err != nil {
		return nil, nil, err
	}
	view, err := handleView(response.User)
	return view, repo, err
}

// GetUserHandle reads current self-handle metadata without generating or writing.
func (s *Service) GetUserHandle(ctx context.Context, id string) (*UserHandle, error) {
	view, _, err := s.handleTarget(ctx, id)
	return view, err
}

// ValidateUserHandle validates syntax and checks current availability. A taken
// name receives at most one suggestion; no availability result grants a lease.
func (s *Service) ValidateUserHandle(ctx context.Context, req *ValidateUserHandleRequest) (*ValidateUserHandleResponse, error) {
	if req == nil {
		return nil, ErrInvalidHandle
	}
	handle, err := NormalizeHandle(req.Handle)
	if err != nil {
		return nil, err
	}
	_, repo, err := s.handleTarget(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	for attempt := 0; attempt < handleSuggestionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		candidate := handleCandidate(handle, attempt)
		available, err := repo.HandleAvailable(ctx, candidate, req.UserID)
		if err != nil {
			return nil, err
		}
		if available {
			result := &ValidateUserHandleResponse{Handle: handle, Available: attempt == 0}
			if attempt > 0 {
				result.Suggestion = candidate
			}
			return result, nil
		}
	}
	return &ValidateUserHandleResponse{Handle: handle, Available: false}, nil
}

// UpdateUserHandle changes only the handle lifecycle fields using a live ACTIVE
// check and revision CAS. Same-handle writes are no-ops; old names are released.
// Audit is best-effort after success, without names or diagnostic payloads.
func (s *Service) UpdateUserHandle(ctx context.Context, req *UpdateUserHandleRequest) (*UserHandle, error) {
	if req == nil || req.ExpectedRevision < 0 || req.ExpectedRevision >= MaxHandleRevision {
		return nil, ErrHandleConflict
	}
	handle, err := NormalizeHandle(req.Handle)
	if err != nil {
		return nil, err
	}
	_, repo, err := s.handleTarget(ctx, req.UserID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	command := *req
	command.Handle = handle
	at := time.Now().UTC()
	if s.TimeProvider != nil {
		at = s.TimeProvider.Now().UTC()
	}
	result, err := repo.SetUserHandle(ctx, &command, at)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrDatabaseError
	}
	if s.AuditService != nil && result.Metadata.Revision != req.ExpectedRevision {
		_ = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: req.UserID, TargetId: req.UserID, TargetType: audit.TargetTypeUser, Domain: "user", Action: "user.handle_updated", Details: map[string]any{"revision": result.Metadata.Revision}})
	}
	return result, nil
}

// createWithHandle prepares the entire account before its single insert. Only
// a proven handle collision is retried, never outages, email conflicts or an
// ambiguous write. Configuration and dependencies must be immutable while serving.
func createWithHandle[T any](ctx context.Context, s *Service, config *UserConfig, user *UniversalUser, insert func() (T, error)) (T, error) {
	var zero T
	if !config.GenerateHandle {
		return insert()
	}
	repo, ok := s.UserRepository.(HandleRepository)
	if !ok {
		return zero, ErrHandleUnsupported
	}
	if err := repo.RequireHandleStorage(ctx); err != nil {
		return zero, err
	}
	generate := s.handleGenerator
	if generate == nil {
		generate = toolbox.GenerateAnimalCodedName
	}
	base, err := generatedHandle(generate())
	if err != nil {
		return zero, err
	}
	at := time.Now().UTC()
	if s.TimeProvider != nil {
		at = s.TimeProvider.Now().UTC()
	}
	stamp := at.Format(time.RFC3339Nano)
	for attempt := 0; attempt < handleAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		user.Handle = handleCandidate(base, attempt)
		user.HandleMetadata = &HandleMetadata{CreatedAt: stamp, UpdatedAt: stamp, Revision: 1}
		result, err := insert()
		if err != ErrHandleTaken {
			return result, err
		}
	}
	return zero, ErrHandleExhausted
}
