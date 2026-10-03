package accesspolicy

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// migrationService uses an explicit test actor, not a role-derived authorizer.
func migrationService(t *testing.T, store Store) *Service {
	t.Helper()
	service, err := NewService(store, func(context.Context, string) (string, error) { return "migration-operator", nil })
	require.NoError(t, err)
	return service
}

func TestMongoTokenLimitMigrationLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		noOp        bool
	}{
		{"create and retire", "missing", false},
		{"enabled policy", "enabled", false},
		{"disabled policy stays disabled", "disabled", false},
		{"expired policy stays expired", "expired", false},
		{"no-op does not create audit", "enabled", true},
		{"disabled no-op stays denied", "disabled", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			service := migrationService(t, store)
			original := policyFixture()
			if tc.state == "disabled" {
				original.Enabled = false
			}
			if tc.state == "expired" {
				original.ExpiresAt = time.Unix(100, 987654321)
			}
			var err error
			if tc.state != "missing" {
				original, err = store.Replace(ctx, original, 0, "original-operator", time.Now())
				require.NoError(t, err)
			}
			limits := TokenLimits{Permanent: 8, Ephemeral: 2, MinimumTTL: 60, MaximumTTL: 7200, TTLIncrement: 60}
			if tc.noOp {
				limits = original.Tokens
			}
			initialAudits := collectionCount(t, store, ctx, auditCollection)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, limits)
			require.NoError(t, err)
			require.Equal(t, initialAudits, collectionCount(t, store, ctx, auditCollection))
			receipt, err := service.ApplyTokenLimits(ctx, plan)
			require.NoError(t, err)
			stored, err := store.Read(ctx, original.Subject)
			require.NoError(t, err)
			require.True(t, sameMigrationGrant(stored, receipt.Snapshot()))
			expected := plan.Preview().After
			require.True(t, sameMigrationGrant(expected, stored))
			if tc.state == "disabled" || tc.state == "expired" {
				_, err = service.Resolve(ctx, original.Subject)
				require.ErrorIs(t, err, ErrDenied)
			}
			if tc.noOp {
				require.Equal(t, initialAudits, collectionCount(t, store, ctx, auditCollection))
			} else {
				require.Equal(t, initialAudits+1, collectionCount(t, store, ctx, auditCollection))
				var audit mongoAudit
				require.NoError(t, store.collection(auditCollection).FindOne(ctx, bson.M{"grant.revision": stored.Revision}).Decode(&audit))
				require.Equal(t, "migration-operator", audit.Actor)
				require.Equal(t, stored.Revision-1, audit.Expected)
			}
			restored, err := service.RollbackTokenLimits(ctx, receipt)
			require.NoError(t, err)
			if tc.state == "missing" {
				require.False(t, restored.Enabled)
				require.Equal(t, TokenLimits{}, restored.Tokens)
				require.Empty(t, restored.Scopes)
				require.Empty(t, restored.Permissions)
				require.Empty(t, restored.Limits)
				require.Equal(t, int64(1), collectionCount(t, store, ctx, grantCollection), "never delete grant history")
			} else {
				original.Revision = restored.Revision
				require.True(t, sameMigrationGrant(original, restored), "only token limits/revision may change")
			}
			if tc.noOp {
				require.Equal(t, initialAudits, collectionCount(t, store, ctx, auditCollection))
			} else {
				require.Equal(t, initialAudits+2, collectionCount(t, store, ctx, auditCollection))
				_, err = service.RollbackTokenLimits(ctx, receipt)
				require.ErrorIs(t, err, ErrConflict)
			}
		})
	}
}

func TestMongoTokenMigrationConcurrentPlans(t *testing.T) {
	for _, state := range []string{"missing", "existing"} {
		t.Run(state, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			original := policyFixture()
			if state == "existing" {
				original = seedPolicy(t, store, ctx)
			}
			service := migrationService(t, store)
			const attempts = 16
			plans := make([]*TokenLimitPlan, attempts)
			for i := range plans {
				var err error
				plans[i], err = service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
				require.NoError(t, err)
			}
			start := make(chan struct{})
			errorsOut := make(chan error, attempts)
			receipts := make(chan *TokenLimitReceipt, attempts)
			var workers sync.WaitGroup
			for _, plan := range plans {
				workers.Add(1)
				go func(plan *TokenLimitPlan) {
					defer workers.Done()
					<-start
					receipt, err := service.ApplyTokenLimits(ctx, plan)
					errorsOut <- err
					receipts <- receipt
				}(plan)
			}
			close(start)
			workers.Wait()
			close(errorsOut)
			close(receipts)
			success, receiptCount := 0, 0
			for err := range errorsOut {
				if err == nil {
					success++
				} else {
					require.ErrorIs(t, err, ErrConflict)
				}
			}
			for receipt := range receipts {
				if receipt != nil {
					receiptCount++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, receiptCount)
			wantAudits := int64(1)
			if state == "existing" {
				wantAudits++
			}
			require.Equal(t, wantAudits, collectionCount(t, store, ctx, auditCollection))
		})
	}
}

// interposedMigrationStore injects a real concurrent edit or a lost success
// response at the write boundary without replacing Mongo's CAS/audit behavior.
type interposedMigrationStore struct {
	Store
	beforeReplace func()
	afterCommit   error
}

func (s *interposedMigrationStore) Replace(ctx context.Context, grant Grant, expected int64, actor string, at time.Time) (Grant, error) {
	if s.beforeReplace != nil {
		s.beforeReplace()
	}
	result, err := s.Store.Replace(ctx, grant, expected, actor, at)
	if err == nil && s.afterCommit != nil {
		return Grant{}, s.afterCommit
	}
	return result, err
}

func TestMongoTokenMigrationWriteBoundaries(t *testing.T) {
	for _, mode := range []string{"edit before apply CAS", "edit before rollback CAS", "audit collision", "commit then error"} {
		t.Run(mode, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			original := seedPolicy(t, store, ctx)
			adapter := &interposedMigrationStore{Store: store}
			service := migrationService(t, adapter)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
			require.NoError(t, err)
			var receipt *TokenLimitReceipt
			if mode == "edit before rollback CAS" {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
				require.NoError(t, err)
			}
			switch mode {
			case "edit before apply CAS", "edit before rollback CAS":
				adapter.beforeReplace = func() {
					current, err := store.Read(ctx, original.Subject)
					require.NoError(t, err)
					current.Permissions = append(current.Permissions, "items:review")
					current.Revision++
					_, err = store.Replace(ctx, current, current.Revision-1, "concurrent-operator", time.Now())
					require.NoError(t, err)
				}
			case "audit collision":
				_, err = store.collection(auditCollection).InsertOne(ctx, bson.M{"_id": tupleID(grantID(original.Subject), "2")})
				require.NoError(t, err)
			case "commit then error":
				adapter.afterCommit = context.DeadlineExceeded
			}
			if mode == "edit before rollback CAS" {
				_, err = service.RollbackTokenLimits(ctx, receipt)
			} else {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
				require.Nil(t, receipt)
			}
			stored, readErr := store.Read(ctx, original.Subject)
			require.NoError(t, readErr)
			if mode == "commit then error" {
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, int64(8), stored.Tokens.Permanent, "lost response does not mean lost commit")
				_, replayErr := service.ApplyTokenLimits(ctx, plan)
				require.ErrorIs(t, replayErr, ErrConflict, "matching content cannot prove which writer committed")
			} else {
				require.ErrorIs(t, err, ErrConflict)
				if mode == "audit collision" {
					require.True(t, sameMigrationGrant(original, stored), "audit failure must roll back grant")
				} else {
					require.Contains(t, stored.Permissions, "items:review", "migration must never overwrite concurrent permission edit")
				}
			}
		})
	}
}

func TestMongoTokenMigrationRetainsUsageAndFences(t *testing.T) {
	for _, operation := range []string{"usage receipt", "grant fence only"} {
		t.Run(operation, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			original := seedPolicy(t, store, ctx)
			service := migrationService(t, store)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
			require.NoError(t, err)
			var initial Usage
			if operation == "usage receipt" {
				initial, err = service.ConsumeAuthorized(ctx, consumptionFixture())
			} else {
				err = store.WithGrant(ctx, original.Subject, time.Now(), func(context.Context, Grant) error { return nil })
			}
			require.NoError(t, err)
			receipt, err := service.ApplyTokenLimits(ctx, plan)
			require.NoError(t, err, "internal fences do not stale an administrative revision")
			_, err = service.RollbackTokenLimits(ctx, receipt)
			require.NoError(t, err)
			if operation == "usage receipt" {
				replay, err := service.ConsumeAuthorized(ctx, consumptionFixture())
				require.NoError(t, err)
				require.True(t, replay.Replayed)
				require.Equal(t, initial.Count, replay.Count)
				require.Equal(t, initial.Maximum, replay.Maximum)
				require.True(t, initial.ResetsAt.Equal(replay.ResetsAt))
				require.Equal(t, int64(1), collectionCount(t, store, ctx, receiptCollection))
				require.Equal(t, int64(1), collectionCount(t, store, ctx, counterCollection))
			}
			_, err = service.Resolve(ctx, original.Subject)
			require.False(t, errors.Is(err, ErrDenied))
			require.NoError(t, err)
		})
	}
}

func TestMongoTokenMigrationCancelledBeforeCAS(t *testing.T) {
	for _, operation := range []string{"apply", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			store, base := isolatedPolicyStore(t)
			original := seedPolicy(t, store, base)
			ctx, cancel := context.WithCancel(base)
			t.Cleanup(cancel)
			adapter := &interposedMigrationStore{Store: store}
			service := migrationService(t, adapter)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
			require.NoError(t, err)
			var receipt *TokenLimitReceipt
			if operation == "rollback" {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
				require.NoError(t, err)
			}
			before, err := store.Read(base, original.Subject)
			require.NoError(t, err)
			audits := collectionCount(t, store, base, auditCollection)
			adapter.beforeReplace = cancel // Snapshot passed; actual CAS has not started.
			if operation == "rollback" {
				_, err = service.RollbackTokenLimits(ctx, receipt)
			} else {
				receipt, err = service.ApplyTokenLimits(ctx, plan)
				require.Nil(t, receipt)
			}
			require.ErrorIs(t, err, context.Canceled)
			after, err := store.Read(base, original.Subject)
			require.NoError(t, err)
			require.True(t, sameMigrationGrant(before, after))
			require.Equal(t, audits, collectionCount(t, store, base, auditCollection))
		})
	}
}

func TestMongoTokenMigrationUsesWriteTimeActor(t *testing.T) {
	for _, operation := range []string{"apply", "rollback"} {
		t.Run(operation, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			original := seedPolicy(t, store, ctx)
			calls := 0
			service, err := NewService(store, func(context.Context, string) (string, error) {
				calls++
				return fmt.Sprintf("actor-%d", calls), nil
			})
			require.NoError(t, err)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
			require.NoError(t, err)
			receipt, err := service.ApplyTokenLimits(ctx, plan)
			require.NoError(t, err)
			result := receipt.Snapshot()
			wantActor := "actor-3"
			if operation == "rollback" {
				result, err = service.RollbackTokenLimits(ctx, receipt)
				require.NoError(t, err)
				wantActor = "actor-5"
			}
			var audit mongoAudit
			require.NoError(t, store.collection(auditCollection).FindOne(ctx, bson.M{"grant.revision": result.Revision}).Decode(&audit))
			require.Equal(t, wantActor, audit.Actor)
		})
	}
}

func TestMongoTokenMigrationConcurrentRollback(t *testing.T) {
	for _, challenger := range []string{"token migration", "permission edit"} {
		t.Run(challenger, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			original := seedPolicy(t, store, ctx)
			service := migrationService(t, store)
			plan, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 8})
			require.NoError(t, err)
			receipt, err := service.ApplyTokenLimits(ctx, plan)
			require.NoError(t, err)
			followup, err := service.PlanTokenLimits(ctx, original.Subject, TokenLimits{Permanent: 9})
			require.NoError(t, err)
			start := make(chan struct{})
			results := make([]error, 2)
			var workers sync.WaitGroup
			workers.Add(2)
			go func() {
				defer workers.Done()
				<-start
				_, results[0] = service.RollbackTokenLimits(ctx, receipt)
			}()
			go func() {
				defer workers.Done()
				<-start
				if challenger == "token migration" {
					_, results[1] = service.ApplyTokenLimits(ctx, followup)
				} else {
					updated := receipt.Snapshot()
					updated.Permissions = append(updated.Permissions, "items:review")
					_, results[1] = service.ReplaceGrant(ctx, updated, updated.Revision)
				}
			}()
			close(start)
			workers.Wait()
			winners := 0
			for _, err := range results {
				if err == nil {
					winners++
				} else {
					require.ErrorIs(t, err, ErrConflict)
				}
			}
			require.Equal(t, 1, winners)
			stored, err := store.Read(ctx, original.Subject)
			require.NoError(t, err)
			require.Equal(t, int64(3), stored.Revision)
			require.Equal(t, int64(3), collectionCount(t, store, ctx, auditCollection))
			if results[0] == nil {
				require.Equal(t, original.Tokens, stored.Tokens)
				require.Equal(t, original.Permissions, stored.Permissions)
			} else if challenger == "token migration" {
				require.Equal(t, int64(9), stored.Tokens.Permanent)
			} else {
				require.Equal(t, int64(8), stored.Tokens.Permanent)
				require.Contains(t, stored.Permissions, "items:review")
			}
		})
	}
}
