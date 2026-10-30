package indexnow

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// receiptStore isolates acknowledgement semantics from driver behavior. An
// unexpected call through the embedded port fails the test instead of inventing
// a database receipt.
type receiptStore struct {
	MongoDbStore
	insert                        *mongo.InsertOneResult
	update                        *mongo.UpdateResult
	stored                        *Attempt
	insertErr, updateErr, findErr error
	finds                         int
	filter, change                any
}

func (s *receiptStore) ExecuteInsertOneCommand(context.Context, *mongo.Collection, interface{}, string) (*mongo.InsertOneResult, error) {
	return s.insert, s.insertErr
}
func (s *receiptStore) ExecuteUpdateOneCommandResult(_ context.Context, _ *mongo.Collection, filter, update any, _ ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	s.filter = filter
	s.change = update
	return s.update, s.updateErr
}
func (s *receiptStore) ExecuteFindOneCommandDecodeResult(_ context.Context, _ *mongo.Collection, _ interface{}, result interface{}, _ string, _ bool, _ error) error {
	s.finds++
	if s.findErr != nil {
		return s.findErr
	}
	if s.stored == nil {
		return mongo.ErrNoDocuments
	}
	*result.(*Attempt) = *cloneSubmissionAttempt(s.stored)
	return nil
}

func receiptRepository(t *testing.T, store *receiptStore) *Repository {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1").SetServerSelectionTimeout(time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		require.NoError(t, client.Disconnect(ctx))
	})
	r := NewRepository(store)
	r.collection = client.Database("receipt_contract").Collection(AttemptCollection)
	return r
}

func TestRepositoryInsertReceipts(t *testing.T) {
	native := errors.New("native storage failure")
	type testCase struct {
		name        string
		receipt     *mongo.InsertOneResult
		cause, want error
	}
	cases := []testCase{
		{name: "acknowledged identity", receipt: &mongo.InsertOneResult{Acknowledged: true, InsertedID: "one"}},
		{name: "missing receipt", want: ErrAttemptUnavailable},
		{name: "unacknowledged", receipt: &mongo.InsertOneResult{InsertedID: "one"}, want: ErrAttemptUnavailable},
		{name: "different identity", receipt: &mongo.InsertOneResult{Acknowledged: true, InsertedID: "other"}, want: ErrAttemptUnavailable},
		{name: "wrong identity type", receipt: &mongo.InsertOneResult{Acknowledged: true, InsertedID: 123}, want: ErrAttemptUnavailable},
		{name: "native error retained", cause: native, want: native},
		{name: "duplicate code", cause: mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}}}, want: ErrAttemptDuplicateID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &receiptStore{insert: tc.receipt, insertErr: tc.cause}
			repo := receiptRepository(t, store)
			require.ErrorIs(t, repo.RecordAttempt(context.Background(), memoryFixture("one", time.Now())), tc.want)
		})
	}
}

func TestRepositoryCompletionReceipts(t *testing.T) {
	native := errors.New("native update failure")
	type testCase struct {
		name                        string
		receipt                     *mongo.UpdateResult
		cause, want                 error
		storedFinal, storedConflict bool
		wantFinds                   int
	}
	cases := []testCase{
		{name: "acknowledged transition", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}},
		{name: "missing receipt", want: ErrAttemptUnavailable},
		{name: "unacknowledged", receipt: &mongo.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, want: ErrAttemptUnavailable},
		{name: "upsert identity", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1, UpsertedID: "one"}, want: ErrAttemptUnavailable},
		{name: "upsert count", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1, UpsertedCount: 1}, want: ErrAttemptUnavailable},
		{name: "negative matches", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: -1}, want: ErrAttemptUnavailable},
		{name: "negative modified", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: -1}, want: ErrAttemptUnavailable},
		{name: "too many matches", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 2, ModifiedCount: 1}, want: ErrAttemptUnavailable},
		{name: "more modifications than matches", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 2}, want: ErrAttemptUnavailable},
		{name: "zero match positive modified cannot replay", receipt: &mongo.UpdateResult{Acknowledged: true, ModifiedCount: 1}, storedFinal: true, want: ErrAttemptUnavailable},
		{name: "zero match negative modified cannot replay", receipt: &mongo.UpdateResult{Acknowledged: true, ModifiedCount: -1}, storedFinal: true, want: ErrAttemptUnavailable},
		{name: "guarded transition cannot be no-op", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}, want: ErrAttemptUnavailable},
		{name: "confirmed absence", receipt: &mongo.UpdateResult{Acknowledged: true}, want: ErrAttemptNotFound, wantFinds: 1},
		{name: "identical final replay", receipt: &mongo.UpdateResult{Acknowledged: true}, storedFinal: true, wantFinds: 1},
		{name: "conflicting final replay", receipt: &mongo.UpdateResult{Acknowledged: true}, storedConflict: true, want: ErrAttemptAlreadyFinished, wantFinds: 1},
		{name: "native error retained", cause: native, want: native},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Now().UTC().Truncate(time.Millisecond)
			final := finishedMemoryFixture("one", at)
			store := &receiptStore{update: tc.receipt, updateErr: tc.cause}
			if tc.storedFinal || tc.storedConflict {
				store.stored = cloneSubmissionAttempt(final)
			}
			if tc.storedConflict {
				store.stored.StatusCode = 202
				store.stored.Outcome = OutcomePending
			}
			repo := receiptRepository(t, store)
			require.ErrorIs(t, repo.FinishAttempt(context.Background(), final), tc.want)
			require.Equal(t, tc.wantFinds, store.finds, "invalid receipts must fail before a replay read")
			if tc.receipt != nil && tc.receipt.Acknowledged {
				change := store.change.(bson.M)["$set"].(bson.M)
				for _, immutable := range []string{"_id", "urls", "method", "started_at"} {
					require.NotContains(t, change, immutable, "completion must preserve original fields")
				}
			}
		})
	}
}

func TestRepositoryCachedCollectionAdmission(t *testing.T) {
	type testCase struct {
		name                           string
		nilContext, canceled, typedNil bool
		want                           error
	}
	cases := []testCase{{name: "cached valid"}, {name: "cached nil context", nilContext: true, want: ErrAttemptInvalidPayload}, {name: "cached canceled", canceled: true, want: context.Canceled}, {name: "typed nil store", typedNil: true, want: ErrAttemptUnavailable}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &receiptStore{}
			repo := receiptRepository(t, store)
			if tc.typedNil {
				repo.Store = (*receiptStore)(nil)
			}
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				ctx = canceledContext()
			}
			got, err := repo.GetAttemptCollection(ctx)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.NotNil(t, got)
			} else {
				require.Nil(t, got)
			}
		})
	}
}
