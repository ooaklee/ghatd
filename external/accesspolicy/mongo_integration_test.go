package accesspolicy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// isolatedPolicyStore uses only an explicitly configured test server and a new
// case-owned database. Cleanup never targets a database supplied in the URI.
func isolatedPolicyStore(t *testing.T) (*MongoStore, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated Mongo replica set")
	}
	name := "ghatd_policy_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	manager, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	store, err := NewMongoStore(ctx, repository.NewMongoDbRepositoryWithDefaults(manager, name))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, store.database.Drop(cleanup))
		require.NoError(t, manager.Close(cleanup))
	})
	require.NoError(t, store.Initialize(ctx))
	return store, ctx
}

// policyTime avoids tests racing a real wall-clock quota-window boundary.
func policyTime() time.Time { return time.Date(2026, 10, 2, 12, 0, 10, 0, time.UTC) }

// seedPolicy creates a grant through the same atomic CAS/audit path used by hosts.
func seedPolicy(t *testing.T, store *MongoStore, ctx context.Context) Grant {
	t.Helper()
	grant, err := store.Replace(ctx, policyFixture(), 0, "policy-admin", policyTime())
	require.NoError(t, err)
	return grant
}

// consumptionFixture binds an admission to exact scopes and permissions.
func consumptionFixture() Consumption {
	return Consumption{Subject: policyFixture().Subject, Metric: "api.requests", Key: "operation-1", Fingerprint: "payload-1", Scopes: []string{"items:write"}, Permissions: []string{"items:create"}}
}

// collectionCount checks persisted side effects after transaction completion.
func collectionCount(t *testing.T, store *MongoStore, ctx context.Context, name string) int64 {
	t.Helper()
	count, err := store.collection(name).CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	return count
}

func TestMongoPolicyRevisionAndAudit(t *testing.T) {
	for _, tc := range []struct {
		name           string
		seed           bool
		expected       int64
		auditCollision bool
		want           error
		revisions      int64
	}{
		{"create", false, 0, false, nil, 1},
		{"replace", true, 1, false, nil, 2},
		{"create cannot overwrite", true, 0, false, ErrConflict, 1},
		{"stale revision", true, 2, false, ErrConflict, 1},
		{"update cannot insert", false, 1, false, ErrConflict, 0},
		{"audit failure rolls back policy", false, 0, true, ErrConflict, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			if tc.seed {
				seedPolicy(t, store, ctx)
			}
			grant := policyFixture()
			grant.Revision = tc.expected + 1
			if tc.auditCollision {
				_, err := store.collection(auditCollection).InsertOne(ctx, bson.M{"_id": tupleID(grantID(grant.Subject), "1"), "sentinel": true})
				require.NoError(t, err)
			}
			got, err := store.Replace(ctx, grant, tc.expected, "second-admin", policyTime())
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
				require.Equal(t, grant.Revision, got.Revision)
			}
			stored, err := store.Read(ctx, grant.Subject)
			if tc.revisions == 0 {
				require.ErrorIs(t, err, ErrDenied)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.revisions, stored.Revision)
			}
			audits := tc.revisions
			if tc.auditCollision {
				audits++
			}
			require.Equal(t, audits, collectionCount(t, store, ctx, auditCollection))
			if tc.want == nil {
				var audit mongoAudit
				require.NoError(t, store.collection(auditCollection).FindOne(ctx, bson.M{"_id": tupleID(grantID(grant.Subject), fmt.Sprint(grant.Revision))}).Decode(&audit))
				require.Equal(t, "second-admin", audit.Actor)
				require.Equal(t, tc.expected, audit.Expected)
				require.Equal(t, policyTime(), audit.At)
			}
		})
	}
}

func TestMongoPolicyReadIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, field string
		want        error
	}{
		{"stored snapshot", "", nil}, {"different system", "system", ErrDenied}, {"different identity kind", "kind", ErrDenied}, {"different user", "id", ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := seedPolicy(t, store, ctx)
			subject := grant.Subject
			switch tc.field {
			case "system":
				subject.System = "other"
			case "kind":
				subject.Kind = APITokenSubject
			case "id":
				subject.ID = "other"
			}
			got, err := store.Read(ctx, subject)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			got.Scopes[0] = "mutated"
			got.Limits["api.requests"] = Limit{}
			again, err := store.Read(ctx, subject)
			require.NoError(t, err)
			require.Equal(t, grant, again)
		})
	}
}

func TestMongoPolicyConsumptionAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name, change    string
		want            error
		replay          bool
		count, receipts int64
	}{
		{"same operation", "", nil, true, 1, 1},
		{"new operation", "new", nil, false, 2, 2},
		{"changed payload", "fingerprint", ErrConflict, false, 0, 1},
		{"revoked", "disabled", ErrDenied, false, 0, 1},
		{"expired", "expired", ErrDenied, false, 0, 1},
		{"scope removed", "scope", ErrDenied, false, 0, 1},
		{"permission removed", "permission", ErrDenied, false, 0, 1},
		{"metric removed", "metric", ErrDenied, false, 0, 1},
		{"zero quota", "zero", ErrLimitReached, false, 0, 1},
		{"positive reduction preserves receipt", "lower-replay", nil, true, 1, 1},
		{"positive reduction rejects new admission", "lower-new", ErrLimitReached, false, 0, 1},
		{"window rollover preserves receipt", "later-replay", nil, true, 1, 1},
		{"window rollover new admission", "later-new", nil, false, 1, 2},
		{"new revision does not reset quota", "revision", nil, false, 2, 2},
		{"duration change creates new budget", "duration", nil, false, 1, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := seedPolicy(t, store, ctx)
			request := consumptionFixture()
			original, err := store.Consume(ctx, request, policyTime())
			require.NoError(t, err)
			require.Equal(t, int64(1), original.Count)
			at := policyTime()
			switch tc.change {
			case "new", "revision":
				request.Key = "new-operation"
			case "fingerprint":
				request.Fingerprint = "different"
			case "disabled":
				grant.Enabled = false
			case "expired":
				grant.ExpiresAt = at
			case "scope":
				grant.Scopes = nil
			case "permission":
				grant.Permissions = nil
			case "metric":
				grant.Limits = nil
			case "zero":
				grant.Limits[request.Metric] = Limit{Maximum: 0, WindowSeconds: 60}
			case "lower-replay", "lower-new":
				grant.Limits[request.Metric] = Limit{Maximum: 1, WindowSeconds: 60}
				if tc.change == "lower-new" {
					request.Key = "new-operation"
				}
			case "later-replay", "later-new":
				at = at.Add(time.Minute)
				if tc.change == "later-new" {
					request.Key = "new-operation"
				}
			case "duration":
				grant.Limits[request.Metric] = Limit{Maximum: 3, WindowSeconds: 3600}
				request.Key = "new-operation"
			}
			grant.Revision++
			_, err = store.Replace(ctx, grant, 1, "policy-admin", at)
			require.NoError(t, err)
			got, err := store.Consume(ctx, request, at)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.count, got.Count)
				require.Equal(t, tc.replay, got.Replayed)
				if tc.replay {
					original.Replayed = true
					require.Equal(t, original, got)
				}
			}
			require.Equal(t, tc.receipts, collectionCount(t, store, ctx, receiptCollection))
			stored, err := store.Read(ctx, grant.Subject)
			require.NoError(t, err)
			require.Equal(t, int64(2), stored.Revision, "usage must not increment policy revision")
		})
	}
}

func TestMongoPolicyConcurrentAdmissions(t *testing.T) {
	for _, tc := range []struct {
		name                       string
		sameKey                    bool
		admitted, replayed, denied int
	}{
		{"distinct operations obey limit", false, 3, 0, 9},
		{"same operation charged once", true, 1, 11, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			seedPolicy(t, store, ctx)
			type result struct {
				usage Usage
				err   error
			}
			results := make(chan result, 12)
			start := make(chan struct{})
			for i := 0; i < 12; i++ {
				go func(i int) {
					<-start
					request := consumptionFixture()
					if !tc.sameKey {
						request.Key = fmt.Sprint("operation-", i)
					}
					usage, err := store.Consume(ctx, request, policyTime())
					results <- result{usage, err}
				}(i)
			}
			close(start)
			admitted, replayed, denied := 0, 0, 0
			for i := 0; i < 12; i++ {
				got := <-results
				if errors.Is(got.err, ErrLimitReached) {
					denied++
					continue
				}
				require.NoError(t, got.err)
				if got.usage.Replayed {
					replayed++
				} else {
					admitted++
				}
			}
			require.Equal(t, tc.admitted, admitted)
			require.Equal(t, tc.replayed, replayed)
			require.Equal(t, tc.denied, denied)
			require.Equal(t, int64(admitted), collectionCount(t, store, ctx, receiptCollection))
			var counter mongoCounter
			require.NoError(t, store.collection(counterCollection).FindOne(ctx, bson.M{}).Decode(&counter))
			require.Equal(t, int64(admitted), counter.Count)
		})
	}
}

func TestMongoPolicyAtomicBusinessConsumption(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fail, retry bool
		want        error
		calls       int
	}{
		{"commit and replay", false, false, nil, 1},
		{"callback failure rolls back charge and effects", true, false, ErrDenied, 1},
		{"transient retry rolls back first attempt", false, true, nil, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			seedPolicy(t, store, ctx)
			require.NoError(t, store.database.CreateCollection(ctx, "business_operations"))
			calls := 0
			apply := func(tx context.Context, usage Usage) error {
				calls++
				if _, err := store.collection("business_operations").InsertOne(tx, bson.M{"_id": "business-operation", "charge": usage.Count}); err != nil {
					return err
				}
				if tc.fail {
					return ErrDenied
				}
				if tc.retry && calls == 1 {
					return mongo.CommandError{Code: 112, Labels: []string{"TransientTransactionError"}, Message: "injected transient callback error"}
				}
				return nil
			}
			checks := 0
			action := ConsumptionAction{Check: func(context.Context) error { checks++; return nil }, Apply: apply}
			got, err := store.WithConsumption(ctx, consumptionFixture(), policyTime(), action)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, got)
				for _, name := range []string{counterCollection, receiptCollection, "business_operations"} {
					require.Zero(t, collectionCount(t, store, ctx, name))
				}
			} else {
				require.NoError(t, err)
				require.False(t, got.Replayed)
				replayed, err := store.WithConsumption(ctx, consumptionFixture(), policyTime(), action)
				require.NoError(t, err)
				require.True(t, replayed.Replayed)
				for _, name := range []string{counterCollection, receiptCollection, "business_operations"} {
					require.Equal(t, int64(1), collectionCount(t, store, ctx, name))
				}
			}
			require.Equal(t, tc.calls, calls)
			wantChecks := tc.calls
			if tc.want == nil {
				wantChecks++
			}
			require.Equal(t, wantChecks, checks, "resource authorization runs on every attempt and replay")
		})
	}
}

func TestMongoPolicyConcurrentTokenInventory(t *testing.T) {
	for _, maximum := range []int64{0, 1, 3} {
		t.Run(fmt.Sprintf("limit_%d", maximum), func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := policyFixture()
			grant.Tokens.Permanent = maximum
			_, err := store.Replace(ctx, grant, 0, "policy-admin", policyTime())
			require.NoError(t, err)
			require.NoError(t, store.database.CreateCollection(ctx, "inventory"))
			results := make(chan error, 8)
			start := make(chan struct{})
			for i := 0; i < 8; i++ {
				go func(i int) {
					<-start
					results <- store.WithGrant(ctx, grant.Subject, policyTime(), func(tx context.Context, current Grant) error {
						count, err := store.collection("inventory").CountDocuments(tx, bson.M{"owner": current.Subject.ID})
						if err != nil {
							return err
						}
						if count >= current.Tokens.Permanent {
							return ErrLimitReached
						}
						_, err = store.collection("inventory").InsertOne(tx, bson.M{"_id": fmt.Sprint(i), "owner": current.Subject.ID})
						return err
					})
				}(i)
			}
			close(start)
			var admitted int64
			for i := 0; i < 8; i++ {
				if err := <-results; err == nil {
					admitted++
				} else {
					require.ErrorIs(t, err, ErrLimitReached)
				}
			}
			require.Equal(t, maximum, admitted)
			require.Equal(t, maximum, collectionCount(t, store, ctx, "inventory"))
		})
	}
}

func TestMongoPolicyTransactionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"callback error", "error", ErrDenied},
		{"expiry during callback", "expiry", ErrDenied},
		{"callback abort is not success", "abort", ErrConfiguration},
		{"foreign client rejects supplied session", "foreign", mongo.ErrWrongClient},
		{"nested operation is rejected", "nested", ErrConfiguration},
		{"cancelled transaction rolls back", "cancel", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := policyFixture()
			if tc.mode == "expiry" {
				grant.ExpiresAt = policyTime().Add(200 * time.Millisecond)
			}
			_, err := store.Replace(ctx, grant, 0, "policy-admin", policyTime())
			require.NoError(t, err)
			require.NoError(t, store.database.CreateCollection(ctx, "effects"))
			foreign, err := mongo.Connect(options.Client().ApplyURI(os.Getenv("GHATD_TEST_MONGO_URI")))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, foreign.Disconnect(context.Background())) })
			run, cancel := context.WithCancel(ctx)
			defer cancel()
			err = store.WithGrant(run, grant.Subject, policyTime(), func(tx context.Context, current Grant) error {
				if tc.mode == "foreign" {
					_, err := foreign.Database(store.database.Name()).Collection("effects").InsertOne(tx, bson.M{"_id": "foreign"})
					return err
				}
				if _, err := store.collection("effects").InsertOne(tx, bson.M{"_id": "effect"}); err != nil {
					return err
				}
				switch tc.mode {
				case "error":
					return ErrDenied
				case "expiry":
					current.ExpiresAt = time.Time{} // callback cannot change eligibility
					time.Sleep(220 * time.Millisecond)
				case "abort":
					return mongo.SessionFromContext(tx).AbortTransaction(tx)
				case "nested":
					_, err := store.Consume(tx, consumptionFixture(), policyTime())
					return err
				case "cancel":
					cancel()
				}
				return nil
			})
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, collectionCount(t, store, ctx, "effects"))
		})
	}
}

func TestMongoPolicySnapshotFence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fenced bool
		want   error
		writes int64
	}{
		{"unfenced snapshot can commit stale authority", false, nil, 1},
		{"write fence forces retry and observes revocation", true, ErrDenied, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := seedPolicy(t, store, ctx)
			require.NoError(t, store.database.CreateCollection(ctx, "effects"))
			read, resume := make(chan struct{}), make(chan struct{})
			var attempts atomic.Int32
			var once sync.Once
			result := make(chan error, 1)
			go func() {
				result <- store.transaction(ctx, func(tx context.Context) error {
					attempts.Add(1)
					current, err := store.readGrant(tx, grant.Subject)
					if err != nil {
						return err
					}
					if err := usableGrant(current, grant.Subject, policyTime()); err != nil {
						return err
					}
					once.Do(func() {
						close(read)
						select {
						case <-resume:
						case <-ctx.Done():
						}
					})
					if tc.fenced {
						if _, err := store.fenceGrant(tx, grant.Subject, policyTime()); err != nil {
							return err
						}
					}
					_, err = store.collection("effects").InsertOne(tx, bson.M{"_id": "effect"})
					return err
				})
			}()
			select {
			case <-read:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			grant.Enabled = false
			grant.Revision++
			_, err := store.Replace(ctx, grant, 1, "revoking-admin", policyTime())
			close(resume)
			require.NoError(t, err)
			err = <-result
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.GreaterOrEqual(t, attempts.Load(), int32(2))
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.writes, collectionCount(t, store, ctx, "effects"))
		})
	}
}

func TestMongoPolicyConcurrentRevisionCAS(t *testing.T) {
	for _, create := range []bool{true, false} {
		t.Run(fmt.Sprintf("create_%t", create), func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			expected := int64(0)
			if !create {
				seedPolicy(t, store, ctx)
				expected = 1
			}
			start, results := make(chan struct{}), make(chan error, 8)
			for i := 0; i < 8; i++ {
				go func() {
					<-start
					grant := policyFixture()
					grant.Revision = expected + 1
					_, err := store.Replace(ctx, grant, expected, "policy-admin", policyTime())
					results <- err
				}()
			}
			close(start)
			winners := 0
			for i := 0; i < 8; i++ {
				if err := <-results; err == nil {
					winners++
				} else {
					require.ErrorIs(t, err, ErrConflict)
				}
			}
			require.Equal(t, 1, winners)
			require.Equal(t, expected+1, collectionCount(t, store, ctx, auditCollection))
		})
	}
}

func TestMongoPolicyServiceManagement(t *testing.T) {
	for _, tc := range []struct {
		name, actor     string
		enabled, reject bool
		expected        int64
		want            error
	}{
		{"management disabled", "", false, false, 0, ErrDenied},
		{"authorizer rejects", "admin", true, true, 0, ErrDenied},
		{"missing actor", "", true, false, 0, ErrDenied},
		{"invalid actor", "spoofed actor", true, false, 0, ErrDenied},
		{"bad expected revision", "admin", true, false, -1, ErrConfiguration},
		{"authorized creation", "admin", true, false, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			var authorize ManagementAuthorizer
			if tc.enabled {
				authorize = func(_ context.Context, system string) (string, error) {
					require.Equal(t, policyFixture().Subject.System, system)
					if tc.reject {
						return "", ErrDenied
					}
					return tc.actor, nil
				}
			}
			service, err := NewService(store, authorize)
			require.NoError(t, err)
			_, err = service.Resolve(ctx, policyFixture().Subject)
			require.ErrorIs(t, err, ErrDenied, "no implicit role/default grant")
			submitted := policyFixture()
			submitted.Revision = 77 // caller input must not be rewritten on any path
			grant, err := service.ReplaceGrant(ctx, submitted, tc.expected)
			require.Equal(t, int64(77), submitted.Revision)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, collectionCount(t, store, ctx, grantCollection))
				require.Zero(t, collectionCount(t, store, ctx, auditCollection))
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(1), grant.Revision)
			require.NoError(t, service.Authorize(ctx, grant.Subject, []string{"items:write"}, []string{"items:create"}))
			require.ErrorIs(t, service.Authorize(ctx, grant.Subject, []string{"items:write", "items:delete"}, nil), ErrDenied)
			credential := grant.Subject
			credential.Kind = APITokenSubject
			require.ErrorIs(t, service.Authorize(ctx, credential, nil, nil), ErrDenied)
			policy := TokenPolicy{Service: service, System: grant.Subject.System}
			limits, err := policy.TokenLimits(ctx, grant.Subject.ID)
			require.NoError(t, err)
			require.Equal(t, grant.Tokens, limits)
			require.NoError(t, policy.WithTokenCreation(ctx, grant.Subject.ID, func(tx context.Context, limits TokenLimits) error {
				require.NotNil(t, mongo.SessionFromContext(tx))
				require.Equal(t, grant.Tokens, limits)
				return nil
			}))
		})
	}
}

func TestMongoPolicyCorruptUsageFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, collection, field string
		value                   any
	}{
		{"receipt identity", receiptCollection, "subject.id", "other"},
		{"receipt count", receiptCollection, "usage.count", int64(-1)},
		{"counter identity", counterCollection, "subject.id", "other"},
		{"counter count", counterCollection, "count", int64(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			seedPolicy(t, store, ctx)
			request := consumptionFixture()
			_, err := store.Consume(ctx, request, policyTime())
			require.NoError(t, err)
			_, err = store.collection(tc.collection).UpdateOne(ctx, bson.M{}, bson.M{"$set": bson.M{tc.field: tc.value}})
			require.NoError(t, err)
			if tc.collection == counterCollection {
				request.Key = "new-operation"
			}
			_, err = store.Consume(ctx, request, policyTime())
			require.ErrorIs(t, err, ErrConfiguration)
			require.Equal(t, int64(1), collectionCount(t, store, ctx, receiptCollection))
		})
	}
}

func TestMongoPolicySessionRejection(t *testing.T) {
	for _, operation := range []string{"initialize", "read", "replace", "grant", "consume", "business"} {
		t.Run(operation, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			seedPolicy(t, store, ctx)
			session, err := store.database.Client().StartSession()
			require.NoError(t, err)
			defer session.EndSession(ctx)
			run := mongo.NewSessionContext(ctx, session)
			called := false
			switch operation {
			case "initialize":
				err = store.Initialize(run)
			case "read":
				_, err = store.Read(run, policyFixture().Subject)
			case "replace":
				_, err = store.Replace(run, policyFixture(), 0, "admin", policyTime())
			case "grant":
				err = store.WithGrant(run, policyFixture().Subject, policyTime(), func(context.Context, Grant) error { called = true; return nil })
			case "consume":
				_, err = store.Consume(run, consumptionFixture(), policyTime())
			case "business":
				_, err = store.WithConsumption(run, consumptionFixture(), policyTime(), ConsumptionAction{Check: func(context.Context) error { called = true; return nil }, Apply: func(context.Context, Usage) error { called = true; return nil }})
			}
			require.ErrorIs(t, err, ErrConfiguration)
			require.False(t, called)
			require.Zero(t, collectionCount(t, store, ctx, counterCollection))
		})
	}
}

func TestMongoPolicyResourceAuthorityOnReplay(t *testing.T) {
	for _, denyAt := range []string{"first admission", "replay"} {
		t.Run(denyAt, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			seedPolicy(t, store, ctx)
			denied, applications := denyAt == "first admission", 0
			action := ConsumptionAction{
				Check: func(context.Context) error {
					if denied {
						return ErrDenied
					}
					return nil
				},
				Apply: func(context.Context, Usage) error { applications++; return nil },
			}
			wantReceipts := int64(0)
			if !denied {
				_, err := store.WithConsumption(ctx, consumptionFixture(), policyTime(), action)
				require.NoError(t, err)
				wantReceipts = 1
				denied = true
			}
			_, err := store.WithConsumption(ctx, consumptionFixture(), policyTime(), action)
			require.ErrorIs(t, err, ErrDenied)
			require.Equal(t, int(wantReceipts), applications)
			require.Equal(t, wantReceipts, collectionCount(t, store, ctx, receiptCollection))
		})
	}
}

func TestMongoPolicyInitializeIdempotency(t *testing.T) {
	for _, seeded := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing_grant_%t", seeded), func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			if seeded {
				seedPolicy(t, store, ctx)
			}
			require.NoError(t, store.Initialize(ctx))
			wantGrants := int64(0)
			if seeded {
				wantGrants = 1
			}
			require.Equal(t, wantGrants, collectionCount(t, store, ctx, grantCollection), "probe must leave no persisted policy document")
			for _, name := range []string{grantCollection, auditCollection, counterCollection, receiptCollection} {
				indexes, err := store.collection(name).Indexes().List(ctx)
				require.NoError(t, err)
				var specs []bson.M
				require.NoError(t, indexes.All(ctx, &specs))
				require.Len(t, specs, 1, "initialization must not install destructive retention indexes")
				require.Equal(t, "_id_", specs[0]["name"])
			}
			grant, err := store.Read(ctx, policyFixture().Subject)
			if seeded {
				require.NoError(t, err)
				require.Equal(t, int64(1), grant.Revision)
			} else {
				require.ErrorIs(t, err, ErrDenied)
			}
		})
	}
}

func TestMongoPolicyRevocationAgainstPublicOperations(t *testing.T) {
	for _, operation := range []string{"consume", "business", "grant"} {
		t.Run(operation, func(t *testing.T) {
			store, ctx := isolatedPolicyStore(t)
			grant := seedPolicy(t, store, ctx)
			require.NoError(t, store.database.CreateCollection(ctx, "effects"))
			callback := func(tx context.Context) error {
				_, err := store.collection("effects").InsertOne(tx, bson.M{"_id": "operation"})
				return err
			}
			run := func() error {
				switch operation {
				case "consume":
					_, err := store.Consume(ctx, consumptionFixture(), policyTime())
					return err
				case "business":
					_, err := store.WithConsumption(ctx, consumptionFixture(), policyTime(), ConsumptionAction{Check: func(context.Context) error { return nil }, Apply: func(tx context.Context, _ Usage) error { return callback(tx) }})
					return err
				default:
					return store.WithGrant(ctx, grant.Subject, policyTime(), func(tx context.Context, _ Grant) error { return callback(tx) })
				}
			}
			start, admitted, revoked := make(chan struct{}), make(chan error, 1), make(chan error, 1)
			go func() { <-start; admitted <- run() }()
			go func() {
				<-start
				replacement := grant
				replacement.Revision++
				replacement.Enabled = false
				_, err := store.Replace(ctx, replacement, 1, "revoking-admin", policyTime())
				revoked <- err
			}()
			close(start)
			admissionErr, revocationErr := <-admitted, <-revoked
			require.NoError(t, revocationErr)
			if admissionErr != nil {
				require.ErrorIs(t, admissionErr, ErrDenied)
			}
			// Either operation may linearize first. Once revocation commits, no
			// public entry point may admit or replay through the old authority.
			require.ErrorIs(t, run(), ErrDenied)
			if operation != "consume" {
				want := int64(0)
				if admissionErr == nil {
					want = 1
				}
				require.Equal(t, want, collectionCount(t, store, ctx, "effects"))
			}
		})
	}
}
