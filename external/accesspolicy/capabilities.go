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
