package accesspolicy

import (
	"context"
	"errors"
	"maps"
	"slices"
)

// TokenLimitPreview is a detached review snapshot, not an authorization or an
// executable request. A missing Before means create-only; otherwise After
// preserves every policy field except Tokens and the next Revision.
type TokenLimitPreview struct {
	// Before is the stored policy at planning time, including disabled/expired grants.
	Before *Grant `json:"before,omitempty"`
	// After is the proposed policy. New grants have no scopes, permissions or usage budgets.
	After Grant `json:"after"`
	// Changed is false when the stored token limits already equal the reviewed limits.
	Changed bool `json:"changed"`
}

// TokenLimitPlan captures an explicit, user-only token allowance change. Its
// immutable state is private; edits to Preview cannot change what gets applied.
// Plans are bound to the Service that created them, are not bearer authority,
// and are not serializable approvals. Re-plan and review after a process restart.
type TokenLimitPlan struct {
	// service binds the reviewed snapshot to the same policy store and authorizer.
	service *Service
	// before is nil only when the exact subject had no stored policy.
	before *Grant
	// after contains only the proposed token-limit patch and its revision.
	after Grant
	// changed distinguishes a fresh no-op observation from a policy mutation.
	changed bool
}

// Preview returns independent slices/maps so callers may render or serialize
// the report without gaining a way to alter the approved in-process plan.
func (p *TokenLimitPlan) Preview() TokenLimitPreview {
	if p == nil {
		return TokenLimitPreview{}
	}
	return TokenLimitPreview{Before: copyMigrationBefore(p.before), After: copyMigrationGrant(p.after), Changed: p.changed}
}

// TokenLimitReceipt exists only after a known successful apply (or fresh no-op
// observation). It is an in-process rollback capability, still requiring live
// management authorization. An uncertain commit never produces a receipt.
type TokenLimitReceipt struct {
	// service prevents replaying a receipt against another store or authorizer.
	service *Service
	// before is retained for a revision-checked rollback; nil means grant creation.
	before *Grant
	// applied is the exact committed policy returned by the store.
	applied Grant
	// changed is false for a no-op, which must not produce an audit mutation on rollback.
	changed bool
}

// Snapshot returns the resulting stored policy without exposing receipt state.
// A successful no-op is a read-time observation, not a lease on future policy.
func (r *TokenLimitReceipt) Snapshot() Grant {
	if r == nil {
		return Grant{}
	}
	return copyMigrationGrant(r.applied)
}

// PlanTokenLimits performs a read-only, live-authorized review for an explicitly
// selected user and reviewed allowances. It does not read role rankings, scan
// users, prepare inventory locks, enable existing policies or copy permissions.
// Missing grants propose an enabled user policy with only these token limits;
// existing disabled/expired grants remain disabled/expired. The host must verify
// the target is a stored user and review source allowances before calling this.
func (s *Service) PlanTokenLimits(ctx context.Context, subject Subject, limits TokenLimits) (*TokenLimitPlan, error) {
	proposed := Grant{Subject: subject, Enabled: true, Tokens: limits}
	if subject.Kind != UserSubject || validateGrant(proposed) != nil {
		return nil, ErrConfiguration
	}
	if _, err := s.authorizeManagement(ctx, subject.System); err != nil {
		return nil, err
	}
	before, err := s.readMigrationGrant(ctx, subject)
	if err != nil {
		return nil, err
	}
	changed := before == nil || before.Tokens != limits
	if before != nil {
		proposed = copyMigrationGrant(*before)
		proposed.Tokens = limits
	}
	if changed {
		// Match ReplaceGrant's maximum exactly; never overflow or wrap revisions.
		if proposed.Revision >= 9007199254740991 {
			return nil, ErrConfiguration
		}
		proposed.Revision++
	}
	if err := validateGrant(proposed); err != nil {
		return nil, err
	}
	return &TokenLimitPlan{service: s, before: before, after: proposed, changed: changed}, nil
}

// ApplyTokenLimits reauthorizes and compares the complete reviewed snapshot,
// then uses ReplaceGrant's audited compare-and-swap. A stale plan is a conflict,
// even if another writer happened to set the same limits. A no-op rechecks the
// snapshot but makes no write/audit entry. Errors never imply no commit: after
// an uncertain outcome inspect/re-plan; do not retry issuance or assume success.
func (s *Service) ApplyTokenLimits(ctx context.Context, plan *TokenLimitPlan) (*TokenLimitReceipt, error) {
	if s == nil || plan == nil || plan.service != s {
		return nil, ErrConfiguration
	}
	if _, err := s.authorizeManagement(ctx, plan.after.Subject.System); err != nil {
		return nil, err
	}
	if err := s.checkMigrationSnapshot(ctx, plan.after.Subject, plan.before); err != nil {
		return nil, err
	}
	applied := copyMigrationGrant(plan.after)
	if plan.changed {
		expected := int64(0)
		if plan.before != nil {
			expected = plan.before.Revision
		}
		var err error
		applied, err = s.ReplaceGrant(ctx, applied, expected)
		if err != nil {
			return nil, err
		}
	}
	return &TokenLimitReceipt{service: s, before: copyMigrationBefore(plan.before), applied: copyMigrationGrant(applied), changed: plan.changed}, nil
}

// RollbackTokenLimits reauthorizes and requires the exact applied policy and
// revision, never overwriting later administrative edits. Existing policies
// regain only their previous Tokens; new policies are disabled with zero token
// limits rather than deleted, preserving audit history. It cannot undo issued
// credentials or usage and does not remove owner inventory locks. A repeated
// mutating rollback conflicts; a failed/uncertain apply has no rollback receipt.
func (s *Service) RollbackTokenLimits(ctx context.Context, receipt *TokenLimitReceipt) (Grant, error) {
	if s == nil || receipt == nil || receipt.service != s {
		return Grant{}, ErrConfiguration
	}
	if _, err := s.authorizeManagement(ctx, receipt.applied.Subject.System); err != nil {
		return Grant{}, err
	}
	if err := s.checkMigrationSnapshot(ctx, receipt.applied.Subject, &receipt.applied); err != nil {
		return Grant{}, err
	}
	if !receipt.changed {
		return copyMigrationGrant(receipt.applied), nil
	}
	restored := copyMigrationGrant(receipt.applied)
	if receipt.before == nil {
		restored.Enabled = false
		restored.Tokens = TokenLimits{}
	} else {
		restored.Tokens = receipt.before.Tokens
	}
	return s.ReplaceGrant(ctx, restored, receipt.applied.Revision)
}

// readMigrationGrant distinguishes absence from stored ineligible policies.
// Store.Read must reserve ErrDenied for absence; other failures propagate.
func (s *Service) readMigrationGrant(ctx context.Context, subject Subject) (*Grant, error) {
	if err := s.checkContext(ctx); err != nil {
		return nil, err
	}
	grant, err := s.store.Read(ctx, subject)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if isAbsentGrant(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if grant.Subject != subject || grant.Revision < 1 || grant.Revision > 9007199254740991 || validateGrant(grant) != nil {
		return nil, ErrConfiguration
	}
	grant = copyMigrationGrant(grant)
	return &grant, nil
}

// isAbsentGrant accepts only a bounded single-cause chain ending in ErrDenied.
// A joined error or custom Is alias cannot establish authoritative absence;
// preserve it as a failure instead of planning a create against unknown state.
func isAbsentGrant(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == ErrDenied {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// checkMigrationSnapshot detects changes since review, including a store that
// violates the revision contract. The subsequent write still needs atomic CAS.
func (s *Service) checkMigrationSnapshot(ctx context.Context, subject Subject, expected *Grant) error {
	current, err := s.readMigrationGrant(ctx, subject)
	if err != nil {
		return err
	}
	if current == nil || expected == nil {
		if current != nil || expected != nil {
			return ErrConflict
		}
		return nil
	}
	if !sameMigrationGrant(*current, *expected) {
		return ErrConflict
	}
	return nil
}

// sameMigrationGrant compares policy meaning without treating nil/empty
// containers or equivalent time locations as changes. Ordered names stay exact.
func sameMigrationGrant(a, b Grant) bool {
	return a.Subject == b.Subject && a.Revision == b.Revision && a.Enabled == b.Enabled && a.ExpiresAt.Equal(b.ExpiresAt) &&
		a.Tokens == b.Tokens && slices.Equal(a.Scopes, b.Scopes) && slices.Equal(a.Permissions, b.Permissions) && maps.Equal(a.Limits, b.Limits)
}

// copyMigrationGrant separates all mutable policy containers at API boundaries.
func copyMigrationGrant(grant Grant) Grant {
	grant.Scopes = slices.Clone(grant.Scopes)
	grant.Permissions = slices.Clone(grant.Permissions)
	grant.Limits = maps.Clone(grant.Limits)
	return grant
}

// copyMigrationBefore preserves the distinction between absent and empty policy.
func copyMigrationBefore(grant *Grant) *Grant {
	if grant == nil {
		return nil
	}
	copy := copyMigrationGrant(*grant)
	return &copy
}
