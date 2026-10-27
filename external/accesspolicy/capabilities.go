package accesspolicy

import (
	"context"
	"slices"
	"strings"
	"time"
)

// Capabilities is an explicit replacement of current access admission and named
// authority. Token allowances and usage budgets are deliberately excluded.
type Capabilities struct {
	// Enabled makes activation or revocation an explicit administrative decision.
	Enabled bool `json:"enabled"`
	// ExpiresAt is an explicit policy expiry; zero means no expiry.
	ExpiresAt time.Time `json:"expires_at"`
	// Scopes are exact resource scopes, never inferred from roles or names.
	Scopes []string `json:"scopes"`
	// Permissions are exact actions authorized within those scopes.
	Permissions []string `json:"permissions"`
}

// ValidateCapabilities bounds exact named authority without I/O or entitlement.
func ValidateCapabilities(patch Capabilities) error {
	for _, names := range [][]string{patch.Scopes, patch.Permissions} {
		if len(names) > 128 {
			return ErrConfiguration
		}
		seen := make(map[string]bool, len(names))
		for _, name := range names {
			if !validName(name) || strings.Contains(name, "*") || seen[name] {
				return ErrConfiguration
			}
			seen[name] = true
		}
	}
	return nil
}

// CapabilityPreview is a detached before/after review, never write authority.
// After preserves token allowances and usage budgets; Changed is semantic.
type CapabilityPreview struct {
	// Before is nil only for an absent stored grant, never an implicit grant.
	Before *Grant `json:"before,omitempty"`
	// After is the detached complete proposed policy, including preserved accounting fields.
	After Grant `json:"after"`
	// Changed reports semantic authority changes after canonical set/expiry normalization.
	Changed bool `json:"changed"`
}

// CanonicalCapabilities validates and detaches authority fields. Named sets are
// sorted and expiry uses Mongo's millisecond UTC precision for review equality.
func CanonicalCapabilities(patch Capabilities) (Capabilities, error) {
	if ValidateCapabilities(patch) != nil {
		return Capabilities{}, ErrConfiguration
	}
	patch.Scopes = slices.Clone(patch.Scopes)
	patch.Permissions = slices.Clone(patch.Permissions)
	slices.Sort(patch.Scopes)
	slices.Sort(patch.Permissions)
	patch.ExpiresAt = patch.ExpiresAt.UTC().Truncate(time.Millisecond)
	return patch, nil
}

// PreviewCapabilities performs current authorized reads only. It proposes one
// complete authority replacement without initializing storage or inventory.
func (s *Service) PreviewCapabilities(ctx context.Context, subject Subject, patch Capabilities) (CapabilityPreview, error) {
	patch, err := CanonicalCapabilities(patch)
	if err != nil || subject.Kind != UserSubject {
		return CapabilityPreview{}, ErrConfiguration
	}
	proposed := Grant{Subject: subject, Enabled: patch.Enabled, ExpiresAt: patch.ExpiresAt, Scopes: patch.Scopes, Permissions: patch.Permissions}
	if validateGrant(proposed) != nil {
		return CapabilityPreview{}, ErrConfiguration
	}
	before, err := s.ReviewGrant(ctx, subject)
	if err != nil {
		return CapabilityPreview{}, err
	}
	changed := true
	if before != nil {
		current, e := CanonicalCapabilities(Capabilities{Enabled: before.Enabled, ExpiresAt: before.ExpiresAt, Scopes: before.Scopes, Permissions: before.Permissions})
		if e != nil {
			return CapabilityPreview{}, e
		}
		changed = current.Enabled != patch.Enabled || !current.ExpiresAt.Equal(patch.ExpiresAt) || !slices.Equal(current.Scopes, patch.Scopes) || !slices.Equal(current.Permissions, patch.Permissions)
		proposed = copyMigrationGrant(*before)
		proposed.Enabled, proposed.ExpiresAt, proposed.Scopes, proposed.Permissions = patch.Enabled, patch.ExpiresAt, patch.Scopes, patch.Permissions
	}
	if changed {
		if proposed.Revision >= 9007199254740991 {
			return CapabilityPreview{}, ErrConfiguration
		}
		proposed.Revision++
	}
	if validateGrant(proposed) != nil {
		return CapabilityPreview{}, ErrConfiguration
	}
	return CapabilityPreview{Before: copyMigrationBefore(before), After: copyMigrationGrant(proposed), Changed: changed}, nil
}

// ReviewGrant reads an administratively authorized snapshot, including disabled
// or expired policies. Nil means known absence, not an enabled empty grant.
// This is a review operation, not enforcement or reusable write authority.
func (s *Service) ReviewGrant(ctx context.Context, subject Subject) (*Grant, error) {
	if !validSubject(subject) || subject.Kind != UserSubject {
		return nil, ErrConfiguration
	}
	if _, err := s.authorizeManagement(ctx, subject.System); err != nil {
		return nil, err
	}
	grant, err := s.readMigrationGrant(ctx, subject)
	if err != nil {
		return nil, err
	}
	if _, err := s.authorizeManagement(ctx, subject.System); err != nil {
		return nil, err
	}
	return grant, nil
}

// ApplyCapabilities replaces only named authority, activation and expiry under
// the reviewed revision. It preserves token allowances and usage budgets. Zero
// creates only a missing user grant, with no token or budget entitlement. The
// caller must verify the selected stored user through its owning identity API.
// Writes use the existing audited grant CAS; errors do not prove rollback, and
// a known successful receipt is retained if cancellation races its return.
func (s *Service) ApplyCapabilities(ctx context.Context, subject Subject, expected int64, patch Capabilities) (Grant, error) {
	proposed := Grant{Subject: subject, Enabled: patch.Enabled, ExpiresAt: patch.ExpiresAt, Scopes: slices.Clone(patch.Scopes), Permissions: slices.Clone(patch.Permissions)}
	if subject.Kind != UserSubject || expected < 0 || expected >= 9007199254740991 || ValidateCapabilities(patch) != nil || validateGrant(proposed) != nil {
		return Grant{}, ErrConfiguration
	}
	before, err := s.ReviewGrant(ctx, subject)
	if err != nil {
		return Grant{}, err
	}
	revision := int64(0)
	if before != nil {
		revision = before.Revision
		proposed.Tokens = before.Tokens
		proposed.Limits = before.Limits
	}
	if revision != expected {
		return Grant{}, ErrConflict
	}
	// Recheck the complete snapshot as well as the final transactional revision.
	if err := s.checkMigrationSnapshot(ctx, subject, before); err != nil {
		return Grant{}, err
	}
	return s.ReplaceGrant(ctx, proposed, expected)
}
