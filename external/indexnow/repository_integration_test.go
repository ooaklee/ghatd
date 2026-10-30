package indexnow

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isolatedIndexnowMongo resolves the mandatory test Mongo URI (explicit
// GHATD_TEST_MONGO_URI makes the test mandatory even in short mode) and
// creates a unique database dropped on cleanup. It is called inside each
// t.Run so no mutable fixture survives between cases.
func isolatedIndexnowMongo(t *testing.T) (*Repository, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if testing.Short() && uri == "" {
		t.Skip("skipping integration test in short mode")
	}
	if uri == "" {
		t.Skip("skipping: GHATD_TEST_MONGO_URI not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)

	dbName := "ghatd_indexnow_test_" + toolbox.GenerateUuidV4()
	mongoHandler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, dbName))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, mongoHandler.Close(cleanup))
	})

	store := repository.NewMongoDbRepositoryWithDefaults(mongoHandler, dbName)
	// Compile-time proof that the shared production store satisfies the
	// package's persistence port.
	var _ MongoDbStore = store
	repo := NewRepository(store)

	collection, err := repo.GetAttemptCollection(ctx)
	require.NoError(t, err, "an explicit Mongo URI must be reachable")
	assert.Equal(t, AttemptCollection, collection.Name())
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, collection.Database().Drop(cleanup))
	})
	return repo, ctx
}

// inflightAttempt builds a pre-dispatch record shaped exactly as the service
// persists it: OutcomeInFlight, zero FinishedAt (omitted by BSON omitempty so
// the repository completion guard can later match its absence).
func inflightAttempt(started time.Time, urls ...string) *Attempt {
	return &Attempt{
		ID:        toolbox.GenerateUuidV4(),
		URLs:      urls,
		Method:    "POST",
		StartedAt: started,
		Outcome:   OutcomeInFlight,
	}
}

// completedFrom returns the final-state record for a given in-flight attempt.
func completedFrom(a *Attempt, outcome Outcome, statusCode int, finished time.Time) *Attempt {
	result := *a
	result.FinishedAt = finished
	result.StatusCode = statusCode
	result.Outcome = outcome
	result.Success = outcome == OutcomeAcknowledged || outcome == OutcomePending
	return &result
}

// seedInflight records one in-flight attempt and returns it.
func seedInflight(t *testing.T, repo *Repository, ctx context.Context, started time.Time, urls ...string) *Attempt {
	t.Helper()
	if len(urls) == 0 {
		urls = []string{"https://example.com/a"}
	}
	a := inflightAttempt(started, urls...)
	require.NoError(t, repo.RecordAttempt(ctx, a))
	return a
}

// canceledContext returns an already-canceled context for table cases.
func canceledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

// TestIntegration_IndexnowRepositoryLifecycle follows one record through the
// full audit contract (record in-flight -> read -> finish -> read -> replay ->
// conflict). It is a standalone exception to the table-driven convention: the
// behaviour under test is exactly this sequence over the same record, and
// splitting the steps into isolated cases would lose the state-machine
// contract.
func TestIntegration_IndexnowRepositoryLifecycle(t *testing.T) {
	repo, ctx := isolatedIndexnowMongo(t)

	started := time.Now().UTC().Truncate(time.Millisecond)
	inflight := seedInflight(t, repo, ctx, started, "https://example.com/a", "https://example.com/b")

	// Record: pre-send shape with zero FinishedAt.
	stored, err := repo.GetAttempt(ctx, inflight.ID)
	require.NoError(t, err)
	assert.Equal(t, OutcomeInFlight, stored.Outcome)
	assert.True(t, stored.FinishedAt.IsZero(), "initial record must omit finished_at")
	assert.Equal(t, inflight.URLs, stored.URLs)
	assert.Equal(t, inflight.Method, stored.Method)
	assert.True(t, stored.StartedAt.Equal(inflight.StartedAt))

	// Finish: final state must not mutate identity or pre-dispatch fields.
	finished := started.Add(3 * time.Second)
	final := completedFrom(inflight, OutcomeAcknowledged, 200, finished)
	final.RetryAfter = 90 * time.Second
	require.NoError(t, repo.FinishAttempt(ctx, final))

	completed, err := repo.GetAttempt(ctx, inflight.ID)
	require.NoError(t, err)
	assert.False(t, completed.FinishedAt.IsZero(), "finished record must pin a nonzero finish time")
	assert.Equal(t, OutcomeAcknowledged, completed.Outcome)
	assert.Equal(t, 200, completed.StatusCode)
	assert.True(t, completed.Success)
	assert.Equal(t, 90*time.Second, completed.RetryAfter)
	assert.True(t, completed.FinishedAt.Equal(finished))
	assert.Equal(t, inflight.ID, completed.ID, "_id must not change")
	assert.Equal(t, inflight.URLs, completed.URLs, "original URLs must not change")
	assert.Equal(t, inflight.Method, completed.Method, "original method must not change")
	assert.True(t, completed.StartedAt.Equal(inflight.StartedAt), "original start must not change")

	// Identical replay stays idempotent; the record is untouched.
	before, err := repo.GetAttempt(ctx, inflight.ID)
	require.NoError(t, err)
	require.NoError(t, repo.FinishAttempt(ctx, final))
	after, err := repo.GetAttempt(ctx, inflight.ID)
	require.NoError(t, err)
	assert.Equal(t, before, after, "idempotent replay must not alter the record")

	// Conflicting replay is an explicit conflict, never a silent rewrite.
	conflict := completedFrom(inflight, OutcomeServerError, 503, finished.Add(time.Minute))
	err = repo.FinishAttempt(ctx, conflict)
	assert.ErrorIs(t, err, ErrAttemptAlreadyFinished)
	unchanged, err := repo.GetAttempt(ctx, inflight.ID)
	require.NoError(t, err)
	assert.Equal(t, OutcomeAcknowledged, unchanged.Outcome, "conflicting replay must not rewrite")
}

func TestIntegration_IndexnowRepositoryRecordAttemptRules(t *testing.T) {
	const startedAgo = time.Duration(0)

	tests := []struct {
		name string
		// ctxCanceled selects an already-canceled context; ctxNil selects a
		// nil context; otherwise a fresh context is used.
		ctxNil      bool
		ctxCanceled bool
		// recordNil supplies a nil record; blankID zeroes the ID; zeroStart
		// zeroes StartedAt; duplicateSeed records the same ID first.
		recordNil   bool
		blankID     bool
		zeroStart   bool
		duplicateID bool
		wantError   error
	}{
		{name: "nil context is invalid", ctxNil: true, wantError: ErrAttemptInvalidPayload},
		{name: "canceled context aborts before I/O", ctxCanceled: true, wantError: context.Canceled},
		{name: "nil record is invalid", recordNil: true, wantError: ErrAttemptInvalidPayload},
		{name: "missing id is invalid", blankID: true, wantError: ErrAttemptInvalidPayload},
		{name: "missing started timestamp is invalid", zeroStart: true, wantError: ErrAttemptInvalidPayload},
		{name: "duplicate id is rejected", duplicateID: true, wantError: ErrAttemptDuplicateID},
		{name: "valid record is accepted", wantError: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			started := time.Now().UTC().Truncate(time.Millisecond).Add(startedAgo)

			if tt.ctxNil {
				ctx = nil
			} else if tt.ctxCanceled {
				ctx = canceledContext()
			}

			record := inflightAttempt(started)
			if tt.recordNil {
				record = nil
			}
			if tt.blankID {
				record.ID = ""
			}
			if tt.zeroStart {
				record.StartedAt = time.Time{}
			}
			if tt.duplicateID {
				require.NoError(t, repo.RecordAttempt(context.Background(), record))
			}

			err := repo.RecordAttempt(ctx, record)
			if tt.wantError == nil {
				assert.NoError(t, err)
				return
			}
			assert.ErrorIs(t, err, tt.wantError)
		})
	}
}

func TestIntegration_IndexnowRepositoryFinishAttemptRules(t *testing.T) {
	tests := []struct {
		name string
		// seed records an in-flight attempt first so finishes can target it.
		seed bool
		// blankID zeroes the ID; zeroFinish omits the finish timestamp;
		// inFlightOutcome keeps OutcomeInFlight; finishBeforeStart backdates
		// the finish timestamp; unknownID replaces the ID with a random one.
		blankID           bool
		zeroFinish        bool
		inFlightOutcome   bool
		finishBeforeStart bool
		unknownID         bool
		wantError         error
	}{
		{name: "missing id is invalid", seed: true, blankID: true, zeroFinish: false, wantError: ErrAttemptInvalidPayload},
		{name: "finish without finished timestamp is invalid", seed: true, zeroFinish: true, wantError: ErrAttemptInvalidPayload},
		{name: "in-flight outcome cannot finalize", seed: true, inFlightOutcome: true, wantError: ErrAttemptInvalidPayload},
		{name: "finish before start is invalid", seed: true, finishBeforeStart: true, wantError: ErrAttemptInvalidPayload},
		{name: "unknown id is not upserted", seed: false, unknownID: true, wantError: ErrAttemptNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			started := time.Now().UTC().Truncate(time.Millisecond)
			finished := started.Add(2 * time.Second)

			var seeded *Attempt
			if tt.seed {
				seeded = seedInflight(t, repo, ctx, started)
			}

			finish := &Attempt{
				ID:         seededID(seeded, tt.unknownID, tt.blankID),
				URLs:       []string{"https://example.com/a"},
				Method:     "POST",
				StartedAt:  started,
				FinishedAt: finished,
				StatusCode: 200,
				Outcome:    OutcomeAcknowledged,
				Success:    true,
			}
			if tt.zeroFinish {
				finish.FinishedAt = time.Time{}
			}
			if tt.inFlightOutcome {
				finish.Outcome = OutcomeInFlight
				finish.Success = false
			}
			if tt.finishBeforeStart {
				finish.FinishedAt = started.Add(-time.Second)
			}

			err := repo.FinishAttempt(ctx, finish)
			assert.ErrorIs(t, err, tt.wantError)

			if tt.unknownID {
				_, gerr := repo.GetAttempt(ctx, finish.ID)
				assert.ErrorIs(t, gerr, ErrAttemptNotFound, "finish must never fabricate a record")
			}
		})
	}
}

// seededID resolves the finish target ID from the row flags: a blank finish,
// a seeded record's ID, or a random unknown ID.
func seededID(seeded *Attempt, unknown, blank bool) string {
	if blank {
		return ""
	}
	if unknown || seeded == nil {
		return toolbox.GenerateUuidV4()
	}
	return seeded.ID
}

func TestIntegration_IndexnowRepositoryGetAttemptRules(t *testing.T) {
	tests := []struct {
		name string
		// ctxNil / ctxCanceled replace the context; blankID queries with
		// whitespace; missingID queries a random unknown ID.
		ctxNil      bool
		ctxCanceled bool
		blankID     bool
		missingID   bool
		wantError   error
	}{
		{name: "stored id resolves", wantError: nil},
		{name: "missing id reports not found", missingID: true, wantError: ErrAttemptNotFound},
		{name: "blank id is invalid", blankID: true, wantError: ErrAttemptInvalidPayload},
		{name: "nil context is invalid", ctxNil: true, wantError: ErrAttemptInvalidPayload},
		{name: "canceled context aborts", ctxCanceled: true, wantError: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			started := time.Now().UTC().Truncate(time.Millisecond)
			seed := seedInflight(t, repo, ctx, started)

			id := seed.ID
			if tt.missingID {
				id = toolbox.GenerateUuidV4()
			}
			if tt.blankID {
				id = " "
			}
			if tt.ctxNil {
				ctx = nil
			} else if tt.ctxCanceled {
				ctx = canceledContext()
			}

			got, err := repo.GetAttempt(ctx, id)
			if tt.wantError == nil {
				require.NoError(t, err)
				assert.Equal(t, seed.ID, got.ID)
				return
			}
			assert.ErrorIs(t, err, tt.wantError)
		})
	}
}

func TestIntegration_IndexnowRepositoryListAttemptsDeterminismAndLimits(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int // expected result count; ordering asserted against seed IDs
	}{
		{name: "default limit newest first with stable id tiebreak", limit: 0, want: 4},
		{name: "explicit limit truncates deterministically", limit: 2, want: 2},
		{name: "over-bound limit is capped not rejected", limit: repositoryListLimit + 7, want: 4},
		{name: "negative limit uses the bounded default", limit: -5, want: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

			// Four records: offsets 0h, 2h, 1h, 2h — two share a timestamp to
			// force the stable ID tiebreak.
			offsets := []time.Duration{0, 2 * time.Hour, time.Hour, 2 * time.Hour}
			ids := make([]string, 0, len(offsets))
			for _, offset := range offsets {
				a := seedInflight(t, repo, ctx, base.Add(offset))
				ids = append(ids, a.ID)
			}
			tieLow, tieHigh := ids[1], ids[3]
			if tieLow > tieHigh {
				tieLow, tieHigh = tieHigh, tieLow
			}
			wantOrder := []string{tieLow, tieHigh, ids[2], ids[0]}
			if tt.want < len(wantOrder) {
				wantOrder = wantOrder[:tt.want]
			}

			got, err := repo.ListAttempts(ctx, tt.limit)
			require.NoError(t, err)
			require.Len(t, got, tt.want)
			gotIDs := make([]string, 0, len(got))
			for _, a := range got {
				gotIDs = append(gotIDs, a.ID)
			}
			assert.Equal(t, wantOrder, gotIDs)
		})
	}

	t.Run("nil context is invalid", func(t *testing.T) {
		repo, _ := isolatedIndexnowMongo(t)
		_, err := repo.ListAttempts(nil, 10)
		assert.ErrorIs(t, err, ErrAttemptInvalidPayload)
	})
}

func TestIntegration_IndexnowRepositoryReadsReturnCopies(t *testing.T) {
	tests := []struct {
		name string
		// viaList mutates a ListAttempts result instead of a GetAttempt one.
		viaList bool
	}{
		{name: "point read returns copies"},
		{name: "list read returns copies", viaList: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			started := time.Now().UTC().Truncate(time.Millisecond)
			seed := seedInflight(t, repo, ctx, started, "https://example.com/a")

			if tt.viaList {
				listed, err := repo.ListAttempts(ctx, 10)
				require.NoError(t, err)
				require.Len(t, listed, 1)
				listed[0].URLs[0] = "https://example.com/mutated"
			} else {
				got, err := repo.GetAttempt(ctx, seed.ID)
				require.NoError(t, err)
				got.URLs[0] = "https://example.com/mutated"
			}

			still, err := repo.GetAttempt(ctx, seed.ID)
			require.NoError(t, err)
			assert.Equal(t, []string{"https://example.com/a"}, still.URLs, "reads must return copies")
		})
	}
}

// Each table case is a live
// concurrency scenario whose goroutine interleaving over one shared record is
// the behaviour under test. Goroutines report errors on a channel; assertions
// run only on the test goroutine.
func TestIntegration_IndexnowRepositoryConcurrentFinish(t *testing.T) {
	tests := []struct {
		name string
		// conflicting supplies a second, different final state; otherwise all
		// goroutines send the identical completion.
		conflicting bool
		// wantConflicts is the exact number of goroutines that must report
		// ErrAttemptAlreadyFinished.
		wantConflicts int
	}{
		{name: "identical simultaneous finishes all succeed", conflicting: false, wantConflicts: 0},
		{name: "conflicting final states retain the winning state", conflicting: true, wantConflicts: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, ctx := isolatedIndexnowMongo(t)
			started := time.Now().UTC().Truncate(time.Millisecond)
			seed := seedInflight(t, repo, ctx, started)
			finished := started.Add(time.Second)

			primary := completedFrom(seed, OutcomeAcknowledged, 200, finished)
			var other *Attempt
			if tt.conflicting {
				other = completedFrom(seed, OutcomeServerError, 503, finished)
			}

			const goroutines = 4
			errCh := make(chan error, goroutines)
			start := make(chan struct{})
			for i := 0; i < goroutines; i++ {
				go func(i int) {
					record := primary
					if tt.conflicting && i%2 == 1 {
						record = other
					}
					<-start
					errCh <- repo.FinishAttempt(ctx, record)
				}(i)
			}
			close(start)

			conflicts := 0
			for i := 0; i < goroutines; i++ {
				err := <-errCh
				if err == nil {
					continue
				}
				if assert.ErrorIs(t, err, ErrAttemptAlreadyFinished) {
					conflicts++
				}
			}
			assert.Equal(t, tt.wantConflicts, conflicts)

			// The record ends in exactly one final state.
			final, err := repo.GetAttempt(ctx, seed.ID)
			require.NoError(t, err)
			assert.False(t, final.FinishedAt.IsZero())
			if tt.conflicting {
				oneOf := assert.ObjectsAreEqual(final.Outcome, OutcomeAcknowledged) ||
					assert.ObjectsAreEqual(final.Outcome, OutcomeServerError)
				assert.True(t, oneOf, "record must hold exactly one of the racing final states")
			} else {
				assert.Equal(t, OutcomeAcknowledged, final.Outcome)
			}
		})
	}
}
