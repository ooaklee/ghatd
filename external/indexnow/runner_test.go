package indexnow

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testJobScope = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// memoryJobs models the durable ownership contract for runner tests. Every case
// owns a fresh instance; production always uses the Mongo implementation.
type memoryJobs struct {
	mu                                sync.Mutex
	job                               *JobLease
	claimed                           bool
	completes, releases               int
	defers                            int
	deferred                          time.Duration
	claimErr, completeErr, releaseErr error
	deferErr                          error
}

func (s *memoryJobs) Defer(ctx context.Context, lease *JobLease, delay time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.deferErr != nil {
		return s.deferErr
	}
	if !s.claimed || s.job.Token != lease.Token {
		return ErrJobLeaseLost
	}
	s.defers++
	s.deferred = delay
	return nil
}

type scanDelayFailure struct{ delay time.Duration }

func (e scanDelayFailure) Error() string             { return "private protocol diagnostic must never escape" }
func (e scanDelayFailure) RetryDelay() time.Duration { return e.delay }

func TestRunnerProtocolRetryDeferral(t *testing.T) {
	type testCase struct {
		name                  string
		delay                 time.Duration
		canceled, deferFailed bool
		wantState             RunState
		wantErr               error
		wantDefers            int
	}
	cases := []testCase{
		{name: "positive delay is durably requested", delay: time.Hour, wantState: RunDeferred, wantErr: ErrJobScanFailed, wantDefers: 1},
		{name: "largest duration never shortened", delay: time.Duration(1<<63 - 1), wantState: RunDeferred, wantErr: ErrJobScanFailed, wantDefers: 1},
		{name: "zero delay does not defer", wantState: RunFailed, wantErr: ErrJobScanFailed},
		{name: "negative delay does not defer", delay: -time.Second, wantState: RunFailed, wantErr: ErrJobScanFailed},
		{name: "cancelled pass still persists delay with bounded cleanup", delay: time.Hour, canceled: true, wantState: RunCanceled, wantErr: context.Canceled, wantDefers: 1},
		{name: "uncertain deferral reports unavailable", delay: time.Hour, deferFailed: true, wantState: RunFailed, wantErr: ErrJobUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			checkpoint := now.Add(-time.Hour)
			jobs := &memoryJobs{job: &JobLease{Scope: testJobScope, Checkpoint: checkpoint, Initialized: true}}
			if tc.deferFailed {
				jobs.deferErr = errors.New("private storage diagnostic")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, jobs, func(context.Context, time.Time, time.Time, int) (bool, error) {
				if tc.canceled {
					cancel()
				}
				return false, errors.Join(scanDelayFailure{delay: tc.delay}, errors.New("raw diagnostics"))
			})
			require.NoError(t, err)
			runner.now = func() time.Time { return now }
			report, err := runner.RunOnce(ctx)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantState, report.State)
			require.Equal(t, tc.wantDefers, jobs.defers)
			require.Zero(t, jobs.completes)
			require.True(t, jobs.job.Checkpoint.Equal(checkpoint))
			require.Equal(t, 1, jobs.releases)
			if tc.wantDefers > 0 {
				require.Equal(t, tc.delay, jobs.deferred)
			}
			require.NotContains(t, err.Error(), "private")
			require.NotContains(t, err.Error(), "raw")
		})
	}
}

func (s *memoryJobs) Claim(ctx context.Context, scope, token string, now time.Time, lease time.Duration) (*JobLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimErr != nil {
		return nil, s.claimErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.claimed {
		return nil, ErrJobBusy
	}
	if s.job == nil {
		s.job = &JobLease{Scope: scope, Checkpoint: now}
	}
	s.job.Token, s.job.ExpiresAt = token, now.Add(lease)
	s.claimed = true
	result := *s.job
	return &result, nil
}

func (s *memoryJobs) Complete(ctx context.Context, lease *JobLease, through time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completes++
	if s.completeErr != nil {
		return s.completeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.claimed || s.job.Token != lease.Token {
		return ErrJobLeaseLost
	}
	s.job.Checkpoint, s.job.Initialized = through, true
	return nil
}

func (s *memoryJobs) Release(_ context.Context, lease *JobLease) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releases++
	if s.releaseErr != nil {
		return s.releaseErr
	}
	if !s.claimed || s.job.Token != lease.Token {
		return ErrJobLeaseLost
	}
	s.claimed = false
	return nil
}

func TestRunnerConfiguration(t *testing.T) {
	type testCase struct {
		name                                   string
		config                                 RunnerConfig
		missingJobs, typedNilJobs, missingScan bool
		invalid                                bool
	}
	cases := []testCase{
		{name: "defaults", config: RunnerConfig{Scope: testJobScope}},
		{name: "explicit bounds", config: RunnerConfig{Scope: testJobScope, Interval: time.Second, ScanLimit: 1, Lease: 3 * time.Second, PassTimeout: time.Second}},
		{name: "raw scope rejected", config: RunnerConfig{Scope: "https://example.com"}, invalid: true},
		{name: "uppercase scope rejected", config: RunnerConfig{Scope: strings.ToUpper(testJobScope)}, invalid: true},
		{name: "negative interval", config: RunnerConfig{Scope: testJobScope, Interval: -time.Second}, invalid: true},
		{name: "overbound interval", config: RunnerConfig{Scope: testJobScope, Interval: 25 * time.Hour}, invalid: true},
		{name: "negative limit", config: RunnerConfig{Scope: testJobScope, ScanLimit: -1}, invalid: true},
		{name: "overbound limit", config: RunnerConfig{Scope: testJobScope, ScanLimit: 10001}, invalid: true},
		{name: "negative lease", config: RunnerConfig{Scope: testJobScope, Lease: -time.Second}, invalid: true},
		{name: "overbound lease", config: RunnerConfig{Scope: testJobScope, Lease: 25 * time.Hour}, invalid: true},
		{name: "timeout equals lease", config: RunnerConfig{Scope: testJobScope, Lease: time.Minute, PassTimeout: time.Minute}, invalid: true},
		{name: "negative timeout", config: RunnerConfig{Scope: testJobScope, PassTimeout: -time.Second}, invalid: true},
		{name: "nil repository", config: RunnerConfig{Scope: testJobScope}, missingJobs: true, invalid: true},
		{name: "typed nil repository", config: RunnerConfig{Scope: testJobScope}, typedNilJobs: true, invalid: true},
		{name: "nil scanner", config: RunnerConfig{Scope: testJobScope}, missingScan: true, invalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var jobs JobRepository = &memoryJobs{}
			if tc.missingJobs {
				jobs = nil
			}
			if tc.typedNilJobs {
				var missing *memoryJobs
				jobs = missing
			}
			var scan DueScan = func(context.Context, time.Time, time.Time, int) (bool, error) { return true, nil }
			if tc.missingScan {
				scan = nil
			}
			runner, err := NewRunner(tc.config, jobs, scan)
			if tc.invalid {
				require.ErrorIs(t, err, ErrJobInvalid)
				require.Nil(t, runner)
				return
			}
			require.NoError(t, err)
			require.Positive(t, runner.config.Interval)
			require.Less(t, runner.config.PassTimeout, runner.config.Lease)
		})
	}
}

func TestRunnerPassOutcomes(t *testing.T) {
	type testCase struct {
		name                                                                                          string
		initial, busy, partial, scanFailure, cancelScan, completeFailure, releaseFailure, ctxCanceled bool
		wantState                                                                                     RunState
		wantErr                                                                                       error
		wantScans, wantCompletes                                                                      int
	}
	cases := []testCase{
		{name: "first activation never scans history", initial: true, wantState: RunActivated, wantCompletes: 1},
		{name: "persisted restart resumes", wantState: RunCompleted, wantScans: 1, wantCompletes: 1},
		{name: "busy replica does not scan", busy: true, wantState: RunBusy, wantErr: ErrJobBusy},
		{name: "partial scan does not advance", partial: true, wantState: RunIncomplete, wantErr: ErrJobIncomplete, wantScans: 1},
		{name: "scan diagnostics scrubbed", scanFailure: true, wantState: RunFailed, wantErr: ErrJobScanFailed, wantScans: 1},
		{name: "cancellation after complete callback does not advance", cancelScan: true, wantState: RunCanceled, wantErr: context.Canceled, wantScans: 1},
		{name: "uncertain write never confirms progress", completeFailure: true, wantState: RunFailed, wantErr: ErrJobUnavailable, wantScans: 1, wantCompletes: 1},
		{name: "release failure is visible", releaseFailure: true, wantState: RunFailed, wantErr: ErrJobUnavailable, wantScans: 1, wantCompletes: 1},
		{name: "cancelled admission does no work", ctxCanceled: true, wantState: RunCanceled, wantErr: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
			jobs := &memoryJobs{}
			if !tc.initial {
				jobs.job = &JobLease{Scope: testJobScope, Checkpoint: now.Add(-time.Hour), Initialized: true}
			}
			if tc.busy {
				jobs.claimErr = ErrJobBusy
			}
			if tc.completeFailure {
				jobs.completeErr = errors.New("private database diagnostics")
			}
			if tc.releaseFailure {
				jobs.releaseErr = errors.New("private release diagnostics")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.ctxCanceled {
				cancel()
			}
			scans := 0
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, jobs, func(ctx context.Context, after, through time.Time, limit int) (bool, error) {
				scans++
				require.Equal(t, 10000, limit)
				require.True(t, through.Equal(now))
				require.True(t, after.Equal(now.Add(-time.Hour)))
				if tc.cancelScan {
					cancel()
				}
				if tc.scanFailure {
					return false, errors.New("secret key and response body")
				}
				return !tc.partial, nil
			})
			require.NoError(t, err)
			runner.now = func() time.Time { return now }
			report, err := runner.RunOnce(ctx)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.wantState, report.State)
			require.Equal(t, tc.wantScans, scans)
			require.Equal(t, tc.wantCompletes, jobs.completes)
			if err != nil {
				require.NotContains(t, err.Error(), "secret")
				require.NotContains(t, err.Error(), "private")
			}
			if tc.initial {
				require.True(t, jobs.job.Checkpoint.Equal(now))
			}
			if tc.partial || tc.scanFailure || tc.cancelScan || tc.completeFailure {
				require.True(t, jobs.job.Checkpoint.Equal(now.Add(-time.Hour)))
			}
			if scans > 0 {
				require.Equal(t, 1, jobs.releases, "cancelled scans still release with a separate cleanup context")
			}
		})
	}
}

func TestRunnerCatchupWindows(t *testing.T) {
	type testCase struct {
		name        string
		lag         time.Duration
		wantWindows int
		wantLast    time.Duration
	}
	cases := []testCase{{name: "two full windows then remainder", lag: 49 * time.Hour, wantWindows: 3, wantLast: time.Hour}, {name: "exact boundary", lag: 48 * time.Hour, wantWindows: 2, wantLast: 24 * time.Hour}, {name: "clock behind progress", lag: -time.Hour, wantWindows: 0}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
			start := now.Add(-tc.lag)
			jobs := &memoryJobs{job: &JobLease{Scope: testJobScope, Checkpoint: start, Initialized: true}}
			previous := start
			windows := 0
			var last time.Duration
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, jobs, func(_ context.Context, after, through time.Time, _ int) (bool, error) {
				require.True(t, after.Equal(previous))
				require.Greater(t, through.Sub(after), time.Duration(0))
				require.LessOrEqual(t, through.Sub(after), 24*time.Hour)
				windows++
				last = through.Sub(after)
				previous = through
				return true, nil
			})
			require.NoError(t, err)
			runner.now = func() time.Time { return now }
			for i := 0; i < tc.wantWindows+1; i++ {
				_, err := runner.RunOnce(context.Background())
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantWindows, windows)
			require.Equal(t, tc.wantLast, last)
		})
	}
}

func TestRunnerCooperativeTimeoutAndLoopDelay(t *testing.T) {
	type testCase struct {
		name    string
		timeout bool
	}
	for _, tc := range []testCase{{name: "callback deadline preserves checkpoint", timeout: true}, {name: "loop waits interval after failure"}} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Millisecond)
			jobs := &memoryJobs{job: &JobLease{Scope: testJobScope, Checkpoint: now.Add(-time.Minute), Initialized: true}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			first := time.Time{}
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope, Interval: 30 * time.Millisecond, Lease: time.Second, PassTimeout: 10 * time.Millisecond}, jobs, func(pass context.Context, _, _ time.Time, _ int) (bool, error) {
				calls++
				if tc.timeout {
					<-pass.Done()
					return true, nil
				}
				if calls == 1 {
					first = time.Now()
				} else {
					require.GreaterOrEqual(t, time.Since(first), 25*time.Millisecond)
					cancel()
				}
				return false, errors.New("not externally reportable")
			})
			require.NoError(t, err)
			runner.now = func() time.Time { return now }
			if tc.timeout {
				report, err := runner.RunOnce(ctx)
				require.ErrorIs(t, err, context.DeadlineExceeded)
				require.Equal(t, RunCanceled, report.State)
				require.Zero(t, jobs.completes)
				require.Equal(t, 1, jobs.releases)
				return
			}
			require.ErrorIs(t, runner.Run(ctx), context.Canceled)
			require.Equal(t, 2, calls)
		})
	}
}

type mismatchedJobs struct {
	*memoryJobs
	wrongScope   bool
	invalidToken bool
}

func (s *mismatchedJobs) Claim(ctx context.Context, scope, token string, now time.Time, duration time.Duration) (*JobLease, error) {
	lease, err := s.memoryJobs.Claim(ctx, scope, token, now, duration)
	if err != nil {
		return nil, err
	}
	if s.wrongScope {
		lease.Scope = strings.Repeat("b", 64)
	} else if s.invalidToken {
		lease.Token = strings.Repeat("x", 32)
	} else {
		lease.Token = strings.Repeat("c", 32)
	}
	return lease, nil
}

func TestRunnerRejectsUnownedClaimReceipts(t *testing.T) {
	type testCase struct {
		name                     string
		wrongScope, invalidToken bool
	}
	for _, tc := range []testCase{{name: "different scope", wrongScope: true}, {name: "different valid token"}, {name: "invalid token", invalidToken: true}} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := &mismatchedJobs{memoryJobs: &memoryJobs{}, wrongScope: tc.wrongScope, invalidToken: tc.invalidToken}
			scans := 0
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, jobs, func(context.Context, time.Time, time.Time, int) (bool, error) { scans++; return true, nil })
			require.NoError(t, err)
			_, err = runner.RunOnce(context.Background())
			require.ErrorIs(t, err, ErrJobUnavailable)
			require.Zero(t, scans)
			require.Zero(t, jobs.completes)
			require.Zero(t, jobs.releases, "an unowned receipt cannot authorize release")
		})
	}
}

func TestRunnerStorageCancellationScrubsJoinedDiagnostics(t *testing.T) {
	type testCase struct {
		name  string
		cause error
	}
	for _, tc := range []testCase{{name: "canceled", cause: context.Canceled}, {name: "deadline", cause: context.DeadlineExceeded}} {
		t.Run(tc.name, func(t *testing.T) {
			jobs := &memoryJobs{claimErr: errors.Join(tc.cause, errors.New("private connection URI"))}
			runner, err := NewRunner(RunnerConfig{Scope: testJobScope}, jobs, func(context.Context, time.Time, time.Time, int) (bool, error) { return true, nil })
			require.NoError(t, err)
			report, err := runner.RunOnce(context.Background())
			require.ErrorIs(t, err, tc.cause)
			require.NotContains(t, err.Error(), "private")
			require.Equal(t, RunCanceled, report.State)
		})
	}
}
