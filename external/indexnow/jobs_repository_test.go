package indexnow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type jobReceiptStore struct {
	MongoDbStore
	updates   []*mongo.UpdateResult
	updateErr error
	stored    *JobLease
	calls     int
}

func (s *jobReceiptStore) ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	s.calls++
	if s.updateErr != nil {
		return nil, s.updateErr
	}
	if len(s.updates) == 0 {
		return nil, nil
	}
	result := s.updates[0]
	s.updates = s.updates[1:]
	return result, nil
}
func (s *jobReceiptStore) ExecuteFindOneCommandDecodeResult(_ context.Context, _ *mongo.Collection, _ interface{}, result interface{}, _ string, _ bool, _ error) error {
	if s.stored == nil {
		return ErrJobLeaseLost
	}
	*result.(*JobLease) = *s.stored
	return nil
}

func jobReceiptRepository(t *testing.T, store *jobReceiptStore) *MongoJobRepository {
	t.Helper()
	base := receiptRepository(t, &receiptStore{})
	base.Store = store
	return &MongoJobRepository{attempts: base}
}

func receiptJobLease() *JobLease {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	return &JobLease{Scope: testJobScope, Token: strings.Repeat("b", 32), Checkpoint: now, Initialized: true, ExpiresAt: now.Add(time.Minute)}
}

func TestJobRepositoryStrictMutationReceipts(t *testing.T) {
	type testCase struct {
		name        string
		receipt     *mongo.UpdateResult
		cause, want error
	}
	native := errors.New("native helper error")
	cases := []testCase{
		{name: "acknowledged transition", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}},
		{name: "nil receipt", want: ErrJobUnavailable},
		{name: "unacknowledged", receipt: &mongo.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, want: ErrJobUnavailable},
		{name: "no matching owner", receipt: &mongo.UpdateResult{Acknowledged: true}, want: ErrJobLeaseLost},
		{name: "matched no-op is uncertain", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}, want: ErrJobUnavailable},
		{name: "upsert forbidden", receipt: &mongo.UpdateResult{Acknowledged: true, UpsertedCount: 1, UpsertedID: testJobScope}, want: ErrJobUnavailable},
		{name: "impossible negative match", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: -1}, want: ErrJobUnavailable},
		{name: "impossible multiple match", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 2, ModifiedCount: 1}, want: ErrJobUnavailable},
		{name: "impossible modification count", receipt: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 2}, want: ErrJobUnavailable},
		{name: "native helper cause retained", cause: native, want: native},
	}
	for _, operation := range []string{"complete", "release", "defer"} {
		t.Run(operation, func(t *testing.T) {
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					store := &jobReceiptStore{updates: []*mongo.UpdateResult{tc.receipt}, updateErr: tc.cause}
					repo := jobReceiptRepository(t, store)
					lease := receiptJobLease()
					var err error
					if operation == "defer" {
						err = repo.Defer(context.Background(), lease, time.Hour)
					} else if operation == "complete" {
						err = repo.Complete(context.Background(), lease, lease.Checkpoint.Add(time.Minute))
					} else {
						err = repo.Release(context.Background(), lease)
					}
					require.ErrorIs(t, err, tc.want)
					require.Equal(t, 1, store.calls)
				})
			}
		})
	}
}

func TestJobRepositorySeedAndClaimReceipts(t *testing.T) {
	type testCase struct {
		name        string
		seed, claim *mongo.UpdateResult
		want        error
	}
	validSeed := &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}
	validClaim := &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}
	cases := []testCase{
		{name: "existing seed and acquired lease", seed: validSeed, claim: validClaim},
		{name: "new seed identity", seed: &mongo.UpdateResult{Acknowledged: true, UpsertedCount: 1, UpsertedID: testJobScope}, claim: validClaim},
		{name: "seed nil", claim: validClaim, want: ErrJobUnavailable},
		{name: "seed wrong id", seed: &mongo.UpdateResult{Acknowledged: true, UpsertedCount: 1, UpsertedID: "wrong"}, claim: validClaim, want: ErrJobUnavailable},
		{name: "seed malformed matched modification", seed: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}, claim: validClaim, want: ErrJobUnavailable},
		{name: "seed impossible upsert match", seed: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, UpsertedCount: 1, UpsertedID: testJobScope}, claim: validClaim, want: ErrJobUnavailable},
		{name: "claim busy", seed: validSeed, claim: &mongo.UpdateResult{Acknowledged: true}, want: ErrJobBusy},
		{name: "claim nil", seed: validSeed, want: ErrJobUnavailable},
		{name: "claim matched no-op", seed: validSeed, claim: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}, want: ErrJobUnavailable},
		{name: "claim unacknowledged", seed: validSeed, claim: &mongo.UpdateResult{MatchedCount: 1, ModifiedCount: 1}, want: ErrJobUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lease := receiptJobLease()
			store := &jobReceiptStore{updates: []*mongo.UpdateResult{tc.seed, tc.claim}, stored: lease}
			repo := jobReceiptRepository(t, store)
			got, err := repo.Claim(context.Background(), lease.Scope, lease.Token, lease.Checkpoint, time.Minute)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, lease, got)
			}
		})
	}
}

func TestJobRepositoryInputAdmission(t *testing.T) {
	type testCase struct {
		name                                                           string
		badScope, badToken, nilContext, canceled, badLease, badThrough bool
		activationAdvance                                              bool
		want                                                           error
	}
	cases := []testCase{
		{name: "raw scope", badScope: true, want: ErrJobInvalid},
		{name: "token not hex", badToken: true, want: ErrJobInvalid},
		{name: "nil context", nilContext: true, want: ErrJobInvalid},
		{name: "canceled context", canceled: true, want: context.Canceled},
		{name: "invalid duration", badLease: true, want: ErrJobInvalid},
		{name: "overbound complete", badThrough: true, want: ErrJobInvalid},
		{name: "activation cannot advance", activationAdvance: true, want: ErrJobInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &jobReceiptStore{}
			repo := jobReceiptRepository(t, store)
			lease := receiptJobLease()
			duration := time.Minute
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if tc.badScope {
				lease.Scope = "https://example.com"
			}
			if tc.badToken {
				lease.Token = strings.Repeat("x", 32)
			}
			if tc.badLease {
				duration = 0
			}
			var err error
			if tc.activationAdvance {
				lease.Initialized = false
				err = repo.Complete(ctx, lease, lease.Checkpoint.Add(time.Minute))
			} else if tc.badThrough {
				err = repo.Complete(ctx, lease, lease.Checkpoint.Add(25*time.Hour))
			} else {
				_, err = repo.Claim(ctx, lease.Scope, lease.Token, lease.Checkpoint, duration)
			}
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, store.calls)
		})
	}
}
