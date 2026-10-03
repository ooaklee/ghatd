package accesspolicy

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// migrationStore models only the management surface; enforcement methods are
// deliberately not implemented, so a migration cannot silently use admission.
type migrationStore struct {
	Store
	current       *Grant
	readErr       error
	replaceErr    error
	commitThenErr error
	writes        int
	actors        []string
}

// absenceAlias exercises errors.Is compatibility without proving stored absence.
type absenceAlias struct{}

func (absenceAlias) Error() string     { return "lookup unavailable" }
func (absenceAlias) Is(err error) bool { return err == ErrDenied }

// absenceCycle models a malformed custom store error without an unwrap terminal.
type absenceCycle struct{}

func (e *absenceCycle) Error() string { return "cyclic lookup failure" }
func (e *absenceCycle) Unwrap() error { return e }

func TestMigrationAbsenceClassification(t *testing.T) {
	deep := error(ErrDenied)
	for i := 0; i < 63; i++ {
		deep = fmt.Errorf("lookup: %w", deep)
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"success is not absence", nil, false},
		{"direct absence", ErrDenied, true},
		{"wrapped absence", fmt.Errorf("lookup: %w", ErrDenied), true},
		{"single joined cause", errors.Join(ErrDenied), false},
		{"mixed failure", errors.Join(ErrDenied, context.DeadlineExceeded), false},
		{"Is-only alias", absenceAlias{}, false},
		{"maximum depth", deep, true},
		{"excess depth", fmt.Errorf("lookup: %w", deep), false},
		{"cyclic error", &absenceCycle{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, isAbsentGrant(tc.err))
		})
	}
}

// Read returns a detached fixture; wrong identity fixtures deliberately exercise
// the service's stored-subject validation before a plan can be produced.
func (s *migrationStore) Read(context.Context, Subject) (Grant, error) {
	if s.readErr != nil {
		return Grant{}, s.readErr
	}
	if s.current == nil {
		return Grant{}, ErrDenied
	}
	return copyMigrationGrant(*s.current), nil
}

// Replace models revision CAS and can lose its response after a committed write.
func (s *migrationStore) Replace(_ context.Context, grant Grant, expected int64, actor string, _ time.Time) (Grant, error) {
	s.writes++
	if s.replaceErr != nil {
		return Grant{}, s.replaceErr
	}
	if s.current == nil && expected != 0 || s.current != nil && s.current.Revision != expected {
		return Grant{}, ErrConflict
	}
	s.current = copyMigrationBefore(&grant)
	s.actors = append(s.actors, actor)
	if s.commitThenErr != nil {
		return Grant{}, s.commitThenErr
	}
	return copyMigrationGrant(grant), nil
}

func TestTokenLimitMigrationPlanning(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		want          error
		changed       bool
	}{
		{"create missing", "missing", nil, true},
		{"wrapped absence", "wrapped-missing", nil, true},
		{"joined absence is not authoritative", "joined-missing", ErrDenied, false},
		{"mixed read failure is not absence", "mixed-read", context.DeadlineExceeded, false},
		{"wrapped mixed read failure is not absence", "wrapped-mixed-read", context.DeadlineExceeded, false},
		{"update current", "", nil, true},
		{"keep disabled", "disabled", nil, true},
		{"keep expired", "expired", nil, true},
		{"no-op", "same", nil, false},
		{"maximum revision no-op", "max-same", nil, false},
		{"maximum revision cannot update", "max", ErrConfiguration, false},
		{"invalid existing limits", "invalid-stored", ErrConfiguration, false},
		{"invalid existing quota window", "bad-window", ErrConfiguration, false},
		{"invalid existing quota maximum", "bad-maximum", ErrConfiguration, false},
		{"wrong stored identity", "wrong-stored", ErrConfiguration, false},
		{"invalid stored revision", "bad-revision", ErrConfiguration, false},
		{"read error is not absence", "read-error", context.DeadlineExceeded, false},
		{"no authorizer", "no-authorizer", ErrDenied, false},
		{"denied authorizer", "denied", ErrDenied, false},
		{"invalid audit actor", "bad-actor", ErrDenied, false},
		{"API credentials excluded", "api", ErrConfiguration, false},
		{"invalid system", "bad-system", ErrConfiguration, false},
		{"invalid user", "bad-user", ErrConfiguration, false},
		{"negative limit", "negative", ErrConfiguration, false},
		{"ephemeral needs TTL", "bad-ttl", ErrConfiguration, false},
		{"nil context", "nil-context", ErrConfiguration, false},
		{"canceled context", "canceled", context.Canceled, false},
		{"nil service", "nil-service", ErrConfiguration, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := policyFixture()
			subject := original.Subject
			limits := TokenLimits{Permanent: 7}
			store := &migrationStore{current: &original}
			ctx := context.Background()
			manage := ManagementAuthorizer(func(_ context.Context, system string) (string, error) {
				require.Equal(t, subject.System, system)
				return "reviewer", nil
			})
			switch tc.variant {
			case "missing":
				store.current = nil
			case "wrapped-missing":
				store.current = nil
				store.readErr = fmt.Errorf("lookup: %w", ErrDenied)
			case "joined-missing":
				store.readErr = errors.Join(ErrDenied)
			case "mixed-read", "wrapped-mixed-read":
				store.readErr = errors.Join(ErrDenied, context.DeadlineExceeded)
				if tc.variant == "wrapped-mixed-read" {
					store.readErr = fmt.Errorf("lookup: %w", store.readErr)
				}
			case "disabled":
				original.Enabled = false
			case "expired":
				original.ExpiresAt = time.Unix(1, 0)
			case "same":
				limits = original.Tokens
			case "max-same", "max":
				original.Revision = 9007199254740991
				if tc.variant == "max-same" {
					limits = original.Tokens
				}
			case "invalid-stored":
				original.Scopes = []string{"*"}
			case "bad-window":
				original.Limits["api.requests"] = Limit{Maximum: 1, WindowSeconds: 0}
			case "bad-maximum":
				original.Limits["api.requests"] = Limit{Maximum: -1, WindowSeconds: 60}
			case "wrong-stored":
				original.Subject.ID = "different"
			case "bad-revision":
				original.Revision = 0
			case "read-error":
				store.readErr = context.DeadlineExceeded
			case "no-authorizer":
				manage = nil
			case "denied":
				manage = func(context.Context, string) (string, error) { return "", ErrDenied }
			case "bad-actor":
				manage = func(context.Context, string) (string, error) { return "bad actor", nil }
			case "api":
				subject.Kind = APITokenSubject
			case "bad-system":
				subject.System = ""
			case "bad-user":
				subject.ID = strings.Repeat("x", 257)
			case "negative":
				limits.Permanent = -1
			case "bad-ttl":
				limits.Ephemeral = 1
			case "nil-context":
				ctx = nil
			case "canceled":
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = canceled
			}
			service, err := NewService(store, manage)
			require.NoError(t, err)
			if tc.variant == "nil-service" {
				service = nil
			}
			plan, err := service.PlanTokenLimits(ctx, subject, limits)
			require.Zero(t, store.writes, "planning must never change policy")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, plan)
				return
			}
			require.NoError(t, err)
			preview := plan.Preview()
			require.Equal(t, tc.changed, preview.Changed)
			require.Equal(t, limits, preview.After.Tokens)
			if store.current == nil {
				require.Nil(t, preview.Before)
				require.Equal(t, Grant{Subject: subject, Revision: 1, Enabled: true, Tokens: limits}, preview.After)
			} else {
				require.Equal(t, original, *preview.Before)
				expected := copyMigrationGrant(original)
				expected.Tokens = limits
				if tc.changed {
					expected.Revision++
				}
				require.Equal(t, expected, preview.After)
			}
		})
	}
}

func TestTokenLimitMigrationApplyAndRollback(t *testing.T) {
	for _, tc := range []struct {
		name, variant string
		want          error
		rollback      bool
	}{
		{"apply and restore tokens", "", nil, false},
		{"create and disable rollback", "missing", nil, false},
		{"no-op and no-op rollback", "same", nil, false},
		{"preview mutations cannot alter plan", "mutate-preview", nil, false},
		{"receipt snapshot mutations cannot alter rollback", "mutate-receipt", nil, false},
		{"apply denied after review", "denied", ErrDenied, false},
		{"stale apply", "stale", ErrConflict, false},
		{"stale no-op apply", "stale-no-op", ErrConflict, false},
		{"changed grant without new revision", "corrupt", ErrConflict, false},
		{"missing grant after review", "deleted", ErrConflict, false},
		{"reapply conflicts", "replay", ErrConflict, false},
		{"store write failure", "write-error", context.DeadlineExceeded, false},
		{"unknown commit has no receipt", "unknown", context.DeadlineExceeded, false},
		{"nil plan", "nil-plan", ErrConfiguration, false},
		{"plan from another service", "other-service", ErrConfiguration, false},
		{"zero plan", "zero-plan", ErrConfiguration, false},
		{"stale rollback", "stale", ErrConflict, true},
		{"stale no-op rollback", "stale-no-op", ErrConflict, true},
		{"rollback denied after apply", "denied", ErrDenied, true},
		{"rollback receipt from another service", "other-service", ErrConfiguration, true},
		{"zero receipt", "zero-receipt", ErrConfiguration, true},
		{"nil receipt", "nil-receipt", ErrConfiguration, true},
		{"repeated rollback", "replay", ErrConflict, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := policyFixture()
			store := &migrationStore{current: copyMigrationBefore(&original)}
			allowed := true
			manage := ManagementAuthorizer(func(context.Context, string) (string, error) {
				if !allowed {
					return "", ErrDenied
				}
				return "migration-admin", nil
			})
			service, err := NewService(store, manage)
			require.NoError(t, err)
			limits := TokenLimits{Permanent: 8}
			if tc.variant == "missing" {
				store.current = nil
			}
			if tc.variant == "same" || tc.variant == "stale-no-op" {
				limits = original.Tokens
			}
			ctx := context.Background()
			plan, err := service.PlanTokenLimits(ctx, original.Subject, limits)
			require.NoError(t, err)
			var receipt *TokenLimitReceipt
			if tc.rollback {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
				require.NoError(t, err)
			}
			switch tc.variant {
			case "mutate-preview":
				preview := plan.Preview()
				preview.After.Tokens.Permanent = 999
				preview.After.Permissions[0] = "admin:all"
				preview.After.Limits["api.requests"] = Limit{}
				preview.Before.Tokens.Permanent = 999
				preview.Before.Scopes[0] = "admin:all"
			case "denied":
				allowed = false
			case "stale", "stale-no-op":
				store.current.Revision++
			case "corrupt":
				store.current.Permissions[0] = "admin:all"
			case "deleted":
				store.current = nil
			case "replay":
				if tc.rollback {
					_, err = service.RollbackTokenLimits(ctx, receipt)
				} else {
					_, err = service.ApplyTokenLimits(ctx, plan)
				}
				require.NoError(t, err)
			case "write-error":
				store.replaceErr = context.DeadlineExceeded
			case "unknown":
				store.commitThenErr = context.DeadlineExceeded
			case "nil-plan":
				plan = nil
			case "zero-plan":
				plan = &TokenLimitPlan{}
			case "nil-receipt":
				receipt = nil
			case "zero-receipt":
				receipt = &TokenLimitReceipt{}
			case "other-service":
				service, err = NewService(store, manage)
				require.NoError(t, err)
			}
			writesBefore := store.writes
			if tc.rollback {
				_, err = service.RollbackTokenLimits(ctx, receipt)
			} else {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				if !tc.rollback {
					require.Nil(t, receipt, "no rollback receipt on a failed/uncertain apply")
				}
				if tc.variant != "write-error" && tc.variant != "unknown" {
					require.Equal(t, writesBefore, store.writes)
				}
				if tc.variant == "unknown" {
					require.Equal(t, limits, store.current.Tokens, "an error does not prove rollback")
					replanned, err := service.PlanTokenLimits(ctx, original.Subject, limits)
					require.NoError(t, err)
					require.False(t, replanned.Preview().Changed)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, limits, receipt.Snapshot().Tokens)
			if tc.variant == "mutate-receipt" {
				snapshot := receipt.Snapshot()
				snapshot.Permissions[0] = "admin:all"
				snapshot.Limits["api.requests"] = Limit{}
			}
			restored, err := service.RollbackTokenLimits(ctx, receipt)
			require.NoError(t, err)
			if tc.variant == "missing" {
				require.False(t, restored.Enabled)
				require.Equal(t, TokenLimits{}, restored.Tokens)
				require.Empty(t, restored.Scopes)
				require.Empty(t, restored.Permissions)
				require.Empty(t, restored.Limits)
			} else {
				original.Revision = restored.Revision
				require.Equal(t, original, restored)
			}
			if tc.variant == "same" {
				require.Zero(t, store.writes)
			} else {
				require.Equal(t, 2, store.writes)
				require.Equal(t, []string{"migration-admin", "migration-admin"}, store.actors)
			}
		})
	}
}

func TestTokenMigrationRechecksAuthorityBeforeWrite(t *testing.T) {
	for _, operation := range []string{"apply", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			original := policyFixture()
			store := &migrationStore{current: &original}
			calls, denyAt := 0, 3 // Plan read, apply read, then audited write.
			if operation == "rollback" {
				denyAt = 5
			}
			service, err := NewService(store, func(context.Context, string) (string, error) {
				calls++
				if calls >= denyAt {
					return "", ErrDenied
				}
				return "reviewer", nil
			})
			require.NoError(t, err)
			plan, err := service.PlanTokenLimits(context.Background(), original.Subject, TokenLimits{Permanent: 7})
			require.NoError(t, err)
			receipt, err := service.ApplyTokenLimits(context.Background(), plan)
			if operation == "rollback" {
				require.NoError(t, err)
				_, err = service.RollbackTokenLimits(context.Background(), receipt)
				require.Equal(t, 1, store.writes)
			} else {
				require.Nil(t, receipt)
				require.Zero(t, store.writes)
			}
			require.ErrorIs(t, err, ErrDenied)
		})
	}
}

func TestTokenMigrationRevisionBoundary(t *testing.T) {
	for _, starting := range []int64{9007199254740989, 9007199254740990} {
		t.Run(strconv.FormatInt(starting, 10), func(t *testing.T) {
			original := policyFixture()
			original.Revision = starting
			store := &migrationStore{current: &original}
			service := migrationService(t, store)
			ctx := context.Background()
			limits := TokenLimits{Permanent: 8}
			plan, err := service.PlanTokenLimits(ctx, original.Subject, limits)
			require.NoError(t, err)
			receipt, err := service.ApplyTokenLimits(ctx, plan)
			require.NoError(t, err)
			require.Equal(t, starting+1, receipt.Snapshot().Revision)
			noop, err := service.PlanTokenLimits(ctx, original.Subject, limits)
			require.NoError(t, err)
			require.False(t, noop.Preview().Changed)
			_, err = service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 9})
			if starting == 9007199254740990 {
				require.ErrorIs(t, err, ErrConfiguration)
				_, err = service.RollbackTokenLimits(ctx, receipt)
				require.ErrorIs(t, err, ErrConfiguration)
				require.Equal(t, 1, store.writes)
			} else {
				require.NoError(t, err)
				_, err = service.RollbackTokenLimits(ctx, receipt)
				require.NoError(t, err)
			}
		})
	}
}

func TestPolicyManagementFailureCauses(t *testing.T) {
	for _, operation := range []string{"plan", "apply", "rollback", "replace"} {
		for _, failure := range []struct {
			name string
			err  error
		}{{"denied", ErrDenied}, {"timeout", context.DeadlineExceeded}, {"canceled", context.Canceled}} {
			t.Run(operation+"/"+failure.name, func(t *testing.T) {
				original := policyFixture()
				store := &migrationStore{current: &original}
				var authErr error
				service, err := NewService(store, func(context.Context, string) (string, error) { return "operator", authErr })
				require.NoError(t, err)
				ctx := context.Background()
				plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
				require.NoError(t, err)
				var receipt *TokenLimitReceipt
				if operation == "rollback" {
					receipt, err = service.ApplyTokenLimits(ctx, plan)
					require.NoError(t, err)
				}
				authErr = failure.err
				before := store.writes
				switch operation {
				case "plan":
					_, err = service.PlanTokenLimits(ctx, original.Subject, TokenLimits{})
				case "apply":
					_, err = service.ApplyTokenLimits(ctx, plan)
				case "rollback":
					_, err = service.RollbackTokenLimits(ctx, receipt)
				case "replace":
					_, err = service.ReplaceGrant(ctx, original, original.Revision)
				}
				require.ErrorIs(t, err, failure.err)
				require.Equal(t, before, store.writes)
			})
		}
	}
}
