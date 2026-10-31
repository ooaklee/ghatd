package indexnow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

const maxReconcileWindow = 24 * time.Hour

// DueScan scans the half-open eligibility interval (after, through], using the
// supplied total candidate limit. It must honor ctx, return complete=false for
// truncation, and return an error for any unconfirmed notification/audit result.
// Hosts combine CMS and SEO eligibility before acknowledging the whole window.
// A successful notification receipt is not an indexing guarantee.
type DueScan func(ctx context.Context, after, through time.Time, limit int) (complete bool, err error)

// RetryDelayError is the safe protocol-delay contract for a failed scan. Its
// positive delay is persisted before relinquishing the lease, without advancing
// progress. The callback must preserve the greatest delay across combined work.
// All positive time.Duration values are supported; no shorter cap is applied.
type RetryDelayError interface {
	error
	RetryDelay() time.Duration
}

// RunnerConfig bounds one opt-in, host-operated reconciliation loop.
type RunnerConfig struct {
	// Scope is a stable lowercase SHA-256 hex hash of origin and policy identity.
	// Never include the key itself, raw URLs or user data in the persisted scope.
	Scope string
	// Interval is the delay after each pass; zero selects five minutes.
	Interval time.Duration
	// ScanLimit bounds total candidates; zero selects 10000, the maximum.
	ScanLimit int
	// Lease bounds durable ownership; zero selects two minutes.
	Lease time.Duration
	// PassTimeout bounds all pass operations and must be shorter than Lease.
	// Zero selects 90 seconds. Callbacks must cooperate with cancellation.
	PassTimeout time.Duration
	// Report receives fixed safe outcomes after each pass, including failures.
	// It must return promptly and must not initiate a second concurrent pass.
	Report func(RunReport)
}

// RunState is a fixed outcome suitable for logs/metrics without raw errors.
type RunState string

const (
	RunActivated  RunState = "activated"
	RunCompleted  RunState = "completed"
	RunIdle       RunState = "idle"
	RunBusy       RunState = "busy"
	RunIncomplete RunState = "incomplete"
	RunDeferred   RunState = "deferred"
	RunFailed     RunState = "failed"
	RunCanceled   RunState = "canceled"
)

// RunReport excludes scope, tokens, URL data, request bodies and diagnostics.
type RunReport struct {
	State RunState
	// Windows is one only after an acknowledged complete eligibility window.
	Windows int
}

// Runner resumes durable progress across restarts and replicas. A pass scans at
// most one 24-hour window; subsequent passes catch up consecutively. It does not
// promise exactly-once delivery: crashes, process pauses and uncertain outbound
// or audit outcomes can require operator reconciliation and can cause replay.
type Runner struct {
	config RunnerConfig
	jobs   JobRepository
	scan   DueScan
	// now is replaceable only by package tests; production always uses UTC now.
	now func() time.Time
}

// NewRunner validates all bounds before any database or notification activity.
// Construction does not activate the job; the first RunOnce durably seeds its
// checkpoint at that activation time and performs no historical scan.
func NewRunner(config RunnerConfig, jobs JobRepository, scan DueScan) (*Runner, error) {
	if config.Interval == 0 {
		config.Interval = 5 * time.Minute
	}
	if config.ScanLimit == 0 {
		config.ScanLimit = 10000
	}
	if config.Lease == 0 {
		config.Lease = 2 * time.Minute
	}
	if config.PassTimeout == 0 {
		config.PassTimeout = 90 * time.Second
	}
	if !jobScopePattern.MatchString(config.Scope) || config.Interval < time.Millisecond || config.Interval > 24*time.Hour || config.ScanLimit < 1 || config.ScanLimit > 10000 ||
		config.Lease < time.Millisecond || config.Lease > 24*time.Hour || config.PassTimeout < time.Millisecond || config.PassTimeout >= config.Lease ||
		nilDependency(jobs) || scan == nil {
		return nil, ErrJobInvalid
	}
	return &Runner{config: config, jobs: jobs, scan: scan, now: time.Now}, nil
}

// Run performs an immediate activation/pass, then waits Interval after every
// outcome before retrying. Transient failures do not hot-retry or terminate the
// worker. Context cancellation stops waiting and bounds cooperative callbacks.
func (r *Runner) Run(ctx context.Context) error {
	if r == nil || ctx == nil {
		return ErrJobInvalid
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		report, _ := r.RunOnce(ctx)
		if r.config.Report != nil {
			r.config.Report(report)
		}
		timer := time.NewTimer(r.config.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// RunOnce acquires one lease and either activates or scans one bounded window.
// It never advances after cancellation, a partial scan, a callback failure or
// uncertain persistence. Cleanup uses its own bounded context so cancellation
// cannot silently abandon a still-owned lease until expiry.
func (r *Runner) RunOnce(ctx context.Context) (report RunReport, resultErr error) {
	report.State = RunFailed
	if r == nil || ctx == nil {
		return report, ErrJobInvalid
	}
	if err := ctx.Err(); err != nil {
		return RunReport{State: RunCanceled}, err
	}
	pass, cancel := context.WithTimeout(ctx, r.config.PassTimeout)
	defer cancel()
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return report, ErrJobUnavailable
	}
	now := r.now().UTC().Truncate(time.Millisecond)
	token := hex.EncodeToString(tokenBytes)
	lease, err := r.jobs.Claim(pass, r.config.Scope, token, now, r.config.Lease)
	if err != nil {
		if errors.Is(err, ErrJobBusy) {
			return RunReport{State: RunBusy}, ErrJobBusy
		}
		return runnerFailure(pass, err)
	}
	if !validJobLease(lease) || lease.Scope != r.config.Scope || lease.Token != token {
		return report, ErrJobUnavailable
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		defer stop()
		if err := r.jobs.Release(cleanup, lease); err != nil && resultErr == nil {
			report.State = RunFailed
			resultErr = ErrJobUnavailable
		}
	}()
	if err := pass.Err(); err != nil {
		return RunReport{State: RunCanceled}, err
	}
	if !lease.Initialized {
		if err := r.jobs.Complete(pass, lease, lease.Checkpoint); err != nil {
			return runnerFailure(pass, err)
		}
		return RunReport{State: RunActivated}, nil
	}
	through := now
	if through.After(lease.Checkpoint.Add(maxReconcileWindow)) {
		through = lease.Checkpoint.Add(maxReconcileWindow)
	}
	if !through.After(lease.Checkpoint) {
		return RunReport{State: RunIdle}, nil
	}
	complete, err := r.scan(pass, lease.Checkpoint, through, r.config.ScanLimit)
	var delayed RetryDelayError
	if err != nil && errors.As(err, &delayed) && delayed.RetryDelay() > 0 {
		// Protocol delays remain durable even when the pass timed out just as
		// the endpoint replied. This is bounded cleanup, never a new scan; the
		// repository still requires the original unexpired lease.
		cleanup, stop := context.WithTimeout(context.Background(), 3*time.Second)
		deferErr := r.jobs.Defer(cleanup, lease, delayed.RetryDelay())
		stop()
		if deferErr != nil {
			return RunReport{State: RunFailed}, ErrJobUnavailable
		}
		if ctxErr := pass.Err(); ctxErr != nil {
			return RunReport{State: RunCanceled}, ctxErr
		}
		return RunReport{State: RunDeferred}, ErrJobScanFailed
	}
	if ctxErr := pass.Err(); ctxErr != nil {
		return RunReport{State: RunCanceled}, ctxErr
	}
	if err != nil {
		return RunReport{State: RunFailed}, ErrJobScanFailed
	}
	if !complete {
		return RunReport{State: RunIncomplete}, ErrJobIncomplete
	}
	if err := r.jobs.Complete(pass, lease, through); err != nil {
		return runnerFailure(pass, err)
	}
	return RunReport{State: RunCompleted, Windows: 1}, nil
}

func runnerFailure(ctx context.Context, err error) (RunReport, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return RunReport{State: RunCanceled}, ctxErr
	}
	if errors.Is(err, context.Canceled) {
		return RunReport{State: RunCanceled}, context.Canceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return RunReport{State: RunCanceled}, context.DeadlineExceeded
	}
	return RunReport{State: RunFailed}, ErrJobUnavailable
}
