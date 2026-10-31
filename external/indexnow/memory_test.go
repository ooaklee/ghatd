package indexnow

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func memoryFixture(id string, started time.Time) *Attempt {
	return &Attempt{ID: id, URLs: []string{"https://example.com/a"}, Method: "POST", StartedAt: started, Outcome: OutcomeInFlight}
}
func finishedMemoryFixture(id string, started time.Time) *Attempt {
	a := memoryFixture(id, started)
	a.FinishedAt = started.Add(time.Second)
	a.StatusCode = 200
	a.Outcome = OutcomeAcknowledged
	a.Success = true
	return a
}

func TestMemoryRepositoryRecordValidation(t *testing.T) {
	type testCase struct {
		name                                                                            string
		nilContext, canceled, nilRecord, missingID, missingTime, finished, wrongOutcome bool
		want                                                                            error
	}
	cases := []testCase{{name: "valid"}, {name: "nil context", nilContext: true, want: ErrAttemptInvalidPayload}, {name: "canceled", canceled: true, want: context.Canceled}, {name: "nil record", nilRecord: true, want: ErrAttemptInvalidPayload}, {name: "missing ID", missingID: true, want: ErrAttemptInvalidPayload}, {name: "missing start", missingTime: true, want: ErrAttemptInvalidPayload}, {name: "already complete", finished: true, want: ErrAttemptInvalidPayload}, {name: "not in flight", wrongOutcome: true, want: ErrAttemptInvalidPayload}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			a := memoryFixture("one", time.Now())
			if tc.missingID {
				a.ID = ""
			}
			if tc.missingTime {
				a.StartedAt = time.Time{}
			}
			if tc.finished {
				a.FinishedAt = time.Now()
			}
			if tc.wrongOutcome {
				a.Outcome = OutcomePending
			}
			if tc.nilRecord {
				a = nil
			}
			require.ErrorIs(t, r.RecordAttempt(ctx, a), tc.want)
		})
	}
}

func TestMemoryRepositoryDuplicateIDs(t *testing.T) {
	type testCase struct {
		name, newID string
		finishFirst bool
		want        error
	}
	cases := []testCase{{name: "duplicate in flight", newID: "one", want: ErrAttemptDuplicateID}, {name: "duplicate completed", newID: "one", finishFirst: true, want: ErrAttemptDuplicateID}, {name: "distinct identity", newID: "two"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			ctx := context.Background()
			at := time.Now()
			require.NoError(t, r.RecordAttempt(ctx, memoryFixture("one", at)))
			if tc.finishFirst {
				require.NoError(t, r.FinishAttempt(ctx, finishedMemoryFixture("one", at)))
			}
			require.ErrorIs(t, r.RecordAttempt(ctx, memoryFixture(tc.newID, at)), tc.want)
		})
	}
}

func TestMemoryRepositoryCompletion(t *testing.T) {
	type testCase struct {
		name                                                                                          string
		missing, replay, conflict, zeroFinish, changedURLs, changedMethod, changedStart, changedDelay bool
		want                                                                                          error
	}
	cases := []testCase{{name: "first completion"}, {name: "unknown ID", missing: true, want: ErrAttemptNotFound}, {name: "identical replay", replay: true}, {name: "conflicting replay", replay: true, conflict: true, want: ErrAttemptAlreadyFinished}, {name: "replay with changed delay", replay: true, changedDelay: true, want: ErrAttemptAlreadyFinished}, {name: "finish missing", zeroFinish: true, want: ErrAttemptInvalidPayload}, {name: "original URLs immutable", changedURLs: true, want: ErrAttemptInvalidPayload}, {name: "original method immutable", changedMethod: true, want: ErrAttemptInvalidPayload}, {name: "original start immutable", changedStart: true, want: ErrAttemptInvalidPayload}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			ctx := context.Background()
			at := time.Now()
			if !tc.missing {
				require.NoError(t, r.RecordAttempt(ctx, memoryFixture("one", at)))
			}
			a := finishedMemoryFixture("one", at)
			if tc.replay {
				require.NoError(t, r.FinishAttempt(ctx, a))
			}
			if tc.conflict {
				a.Outcome = OutcomePending
				a.StatusCode = 202
			}
			if tc.changedDelay {
				a.RetryAfter = time.Minute
			}
			if tc.zeroFinish {
				a.FinishedAt = time.Time{}
			}
			if tc.changedURLs {
				a.URLs[0] = "https://example.com/changed"
			}
			if tc.changedMethod {
				a.Method = "GET"
			}
			if tc.changedStart {
				a.StartedAt = at.Add(-time.Second)
			}
			require.ErrorIs(t, r.FinishAttempt(ctx, a), tc.want)
			stored, err := r.GetAttempt(ctx, "one")
			if tc.missing {
				require.ErrorIs(t, err, ErrAttemptNotFound)
				return
			}
			require.NoError(t, err)
			require.Equal(t, []string{"https://example.com/a"}, stored.URLs)
			if tc.want == nil {
				require.True(t, stored.Success)
			}
		})
	}
}

func TestMemoryRepositoryListOrdering(t *testing.T) {
	type testCase struct {
		name  string
		limit int
		want  []string
	}
	cases := []testCase{{"default", 0, []string{"b", "c", "a"}}, {"bounded page", 2, []string{"b", "c"}}, {"negative default", -1, []string{"b", "c", "a"}}, {"large capped", 1001, []string{"b", "c", "a"}}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			ctx := context.Background()
			base := time.Now()
			require.NoError(t, r.RecordAttempt(ctx, memoryFixture("a", base)))
			require.NoError(t, r.RecordAttempt(ctx, memoryFixture("c", base.Add(time.Hour))))
			require.NoError(t, r.RecordAttempt(ctx, memoryFixture("b", base.Add(time.Hour))))
			got, err := r.ListAttempts(ctx, tc.limit)
			require.NoError(t, err)
			ids := []string{}
			for _, a := range got {
				ids = append(ids, a.ID)
			}
			require.Equal(t, tc.want, ids)
		})
	}
}

func TestMemoryRepositoryReadAdmission(t *testing.T) {
	type testCase struct {
		name                              string
		nilContext, canceled, nilReceiver bool
		want                              error
	}
	cases := []testCase{{name: "nil context", nilContext: true, want: ErrAttemptInvalidPayload}, {name: "canceled", canceled: true, want: context.Canceled}, {name: "nil receiver", nilReceiver: true, want: ErrAttemptInvalidPayload}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			if tc.nilReceiver {
				r = nil
			}
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.canceled {
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			_, err := r.GetAttempt(ctx, "one")
			require.ErrorIs(t, err, tc.want)
			_, err = r.ListAttempts(ctx, 10)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestMemoryRepositoryCopies(t *testing.T) {
	type testCase struct{ name, source string }
	cases := []testCase{{"record input", "input"}, {"single read", "get"}, {"listed read", "list"}, {"completion input", "finish"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := NewMemoryRepository()
			ctx := context.Background()
			at := time.Now()
			a := memoryFixture("one", at)
			require.NoError(t, r.RecordAttempt(ctx, a))
			switch tc.source {
			case "input":
				a.URLs[0] = "changed"
			case "get":
				got, err := r.GetAttempt(ctx, "one")
				require.NoError(t, err)
				got.URLs[0] = "changed"
			case "list":
				got, err := r.ListAttempts(ctx, 10)
				require.NoError(t, err)
				got[0].URLs[0] = "changed"
			case "finish":
				a = finishedMemoryFixture("one", at)
				require.NoError(t, r.FinishAttempt(ctx, a))
				a.URLs[0] = "changed"
			}
			got, err := r.GetAttempt(ctx, "one")
			require.NoError(t, err)
			require.Equal(t, []string{"https://example.com/a"}, got.URLs)
		})
	}
}

// This standalone test exercises one shared state interleaving of concurrent
// writers, completion replays and history readers; isolation would remove the
// contention that the scenario verifies.
func TestMemoryRepositoryConcurrentAccess(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()
	at := time.Now()
	var wg sync.WaitGroup
	for w := range 8 {
		wg.Go(func() {
			for i := range 50 {
				id := fmt.Sprintf("%d-%d", w, i)
				a := memoryFixture(id, at)
				if err := r.RecordAttempt(ctx, a); err != nil {
					t.Error(err)
					return
				}
				a = finishedMemoryFixture(id, at)
				if err := r.FinishAttempt(ctx, a); err != nil {
					t.Error(err)
				}
				if err := r.FinishAttempt(ctx, a); err != nil {
					t.Error(err)
				}
				got, err := r.GetAttempt(ctx, id)
				if err != nil {
					t.Error(err)
				} else if !slices.Equal(got.URLs, a.URLs) {
					t.Error("inconsistent record")
				}
			}
		})
	}
	for range 4 {
		wg.Go(func() {
			for range 25 {
				if _, err := r.ListAttempts(ctx, 1000); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	got, err := r.ListAttempts(ctx, 1000)
	require.NoError(t, err)
	require.Len(t, got, 400)
}
