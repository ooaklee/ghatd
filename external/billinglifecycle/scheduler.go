package billinglifecycle

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

var ErrExecutionCounterExhausted = errors.New("partnerlifecycle/execution-counter-exhausted")

type SchedulerRepository interface {
	ScheduleRepository
	ExecutionRepository
}
type ExecutionAuthority interface {
	billingmanager.SubscriptionStatusAuthority
	billingmanager.CheckoutLifecycleAuthority
}

// SchedulerConfig bounds independent lane work and retry policy. Scopes and
// actor must match the bound native worker authority; no permission is created.
type SchedulerConfig struct {
	ActorID                              string
	Scopes                               []billing.RevenueScope
	PageLimit, ColdBudget, RefreshBudget int
	LeaseDuration, RetryBase, RetryMax   time.Duration
}

// ScheduleBatch contains only confirmed live acquisitions and cursor progress.
// Every error withholds it; a withheld job may still have a committed lease.
type ScheduleBatch struct {
	Jobs                []ScheduledJob `json:"-"`
	Cursor              ScheduleCursor `json:"-"`
	Examined, Conflicts int            `json:"-"`
}

type Scheduler struct {
	repo      SchedulerRepository
	authority ExecutionAuthority
	clock     LifecycleClock
	cfg       SchedulerConfig
}

func NewScheduler(repo SchedulerRepository, authority ExecutionAuthority, clock LifecycleClock, cfg SchedulerConfig) (*Scheduler, error) {
	if nilPort(repo) || nilPort(authority) || nilPort(clock) {
		return nil, billing.ErrRevenueUnavailable
	}
	if !scheduleText(cfg.ActorID) || cfg.PageLimit < 1 || cfg.PageLimit > 200 || cfg.ColdBudget < 1 || cfg.ColdBudget > cfg.PageLimit || cfg.RefreshBudget < 1 || cfg.RefreshBudget > cfg.PageLimit || cfg.LeaseDuration < time.Second || cfg.LeaseDuration > 15*time.Minute || cfg.RetryBase < time.Second || cfg.RetryBase > time.Minute || cfg.RetryMax < cfg.RetryBase || cfg.RetryMax > 24*time.Hour {
		return nil, billing.ErrRevenueInvalid
	}
	if err := billing.ValidateRevenueHistoryScopes(cfg.Scopes); err != nil {
		return nil, err
	}
	cfg.Scopes = append([]billing.RevenueScope(nil), cfg.Scopes...)
	return &Scheduler{repo, authority, clock, cfg}, nil
}

func (s *Scheduler) begin(ctx context.Context, scope billing.RevenueScope) error {
	if ctx == nil {
		return billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilPort(s.repo) || nilPort(s.authority) || nilPort(s.clock) {
		return billing.ErrRevenueUnavailable
	}
	for _, allowed := range s.cfg.Scopes {
		if scope == allowed {
			return s.authorize(ctx, nil)
		}
	}
	return billing.ErrRevenueInvalid
}

func (s *Scheduler) authorize(ctx context.Context, source *ScheduledSource) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var err error
	if source == nil {
		err = s.authority.AuthorizeSubscriptionStatus(ctx, s.cfg.ActorID, billingmanager.SubscriptionStatusRefresh, billingmanager.SubscriptionStatusTarget{})
	} else if source.Kind == billing.LifecycleCheckoutSources {
		err = s.authority.AuthorizeCheckoutLifecycle(ctx, s.cfg.ActorID, billingmanager.SubscriptionStatusRefresh, billingmanager.CheckoutLifecycleTarget{Scope: source.Scope, PrincipalID: source.PrincipalID, IntentID: source.Checkout.ID})
	} else {
		err = s.authority.AuthorizeSubscriptionStatus(ctx, s.cfg.ActorID, billingmanager.SubscriptionStatusRefresh, billingmanager.SubscriptionStatusTarget{Scope: source.Scope, PrincipalID: source.PrincipalID, SubscriptionID: source.SubscriptionID})
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

func (s *Scheduler) finish(ctx context.Context, sources []ScheduledSource, operationErr error) error {
	withhold := func(err error) error {
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return errors.Join(err, operationErr)
		}
		return err
	}
	if err := s.authorize(ctx, nil); err != nil {
		return withhold(err)
	}
	for _, source := range sources {
		if err := s.authorize(ctx, &source); err != nil {
			return withhold(err)
		}
	}
	if len(sources) > 0 {
		if err := s.authorize(ctx, nil); err != nil {
			return withhold(err)
		}
	}
	return operationErr
}

func soleConflict(err error) bool {
	for n := 0; n < 64 && err != nil; n++ {
		if err == recordstore.ErrConflict {
			return true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

func sameScheduledSource(a, b ScheduledSource) bool {
	if a.Scope != b.Scope || a.Kind != b.Kind || a.SourceID != b.SourceID || a.PrincipalID != b.PrincipalID || a.SubscriptionID != b.SubscriptionID || a.FactID != b.FactID || (a.Checkout == nil) != (b.Checkout == nil) {
		return false
	}
	return a.Checkout == nil || a.Checkout.ValidateAcknowledgedInput(*b.Checkout) == nil
}

func sameOriginalInputs(a, b ScheduledJob) bool {
	if a.LastSupersessionID != b.LastSupersessionID || a.LastCompletionID != b.LastCompletionID || !a.CadenceAnchor.Equal(b.CadenceAnchor) || a.CheckoutPrepared != b.CheckoutPrepared {
		return false
	}
	if (a.OriginalStatus == nil) != (b.OriginalStatus == nil) {
		return false
	}
	if a.OriginalStatus == nil {
		return true
	}
	p, q := *a.OriginalStatus, *b.OriginalStatus
	pa, qa := p.RequestedAt, q.RequestedAt
	p.RequestedAt, q.RequestedAt = time.Time{}, time.Time{}
	return p == q && pa.Equal(qa)
}

// Scan advances only over examined jobs, so a spent budget cannot permanently
// skip a due suffix. Known failures advance past that job and return their cause;
// unknown acquisitions do not advance. Neither outcome authorizes provider work.
func (s *Scheduler) Scan(ctx context.Context, scope billing.RevenueScope, lane string) (ScheduleBatch, error) {
	if !executionLane(lane) {
		return ScheduleBatch{}, billing.ErrRevenueInvalid
	}
	if err := s.begin(ctx, scope); err != nil {
		return ScheduleBatch{}, err
	}
	q := ScheduleScan{Scope: scope, Lane: lane, Limit: s.cfg.PageLimit}
	snapshot, err := s.repo.ReadScan(ctx, q)
	if err := s.finish(ctx, nil, err); err != nil {
		return ScheduleBatch{}, err
	}
	if snapshot.Cursor.Revision < 0 || snapshot.Cursor.Revision == math.MaxInt64 || !schedulePosition(snapshot.Cursor.AfterID) || (snapshot.Cursor.Revision == 0 && snapshot.Cursor.AfterID != "") || len(snapshot.Jobs) > q.Limit {
		return ScheduleBatch{}, s.finish(ctx, nil, billing.ErrRevenueUnavailable)
	}
	position := snapshot.Cursor.AfterID
	for _, job := range snapshot.Jobs {
		id := scheduledIdentity(job.Source)
		if validateScheduledJob(job) != nil || job.Source.Scope != scope || job.Lane != lane || id <= position {
			return ScheduleBatch{}, s.finish(ctx, nil, billing.ErrRevenueUnavailable)
		}
		position = id
	}
	budget := s.cfg.ColdBudget
	if lane == RefreshLane {
		budget = s.cfg.RefreshBudget
	}
	result := ScheduleBatch{}
	position = snapshot.Cursor.AfterID
	var sources []ScheduledSource
	var operationErr error
	attempts := 0
	for _, job := range snapshot.Jobs {
		if attempts == budget {
			break
		}
		if err := s.finish(ctx, []ScheduledSource{job.Source}, nil); err != nil {
			return ScheduleBatch{}, err
		}
		now := s.clock.Now()
		if now.IsZero() {
			return ScheduleBatch{}, s.finish(ctx, sources, billing.ErrRevenueUnavailable)
		}
		position = scheduledIdentity(job.Source)
		result.Examined++
		sources = append(sources, job.Source)
		if job.NextAttemptAt.After(now) || job.LeasedUntil.After(now) {
			continue
		}
		attempts++
		if job.Revision == math.MaxInt64 || job.Attempts == math.MaxInt64 || job.Fence == math.MaxInt64 {
			operationErr = ErrExecutionCounterExhausted
			break
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return ScheduleBatch{}, s.finish(ctx, sources, err)
		}
		leased, err := s.repo.Acquire(ctx, LeaseRequest{Source: job.Source, ExpectedRevision: job.Revision, Actor: s.cfg.ActorID, Token: base64.RawURLEncoding.EncodeToString(token[:]), Until: now.Add(s.cfg.LeaseDuration)})
		if current := s.finish(ctx, []ScheduledSource{job.Source}, nil); current != nil {
			if errors.Is(err, recordstore.ErrUncertain) {
				current = errors.Join(current, err)
			}
			return ScheduleBatch{}, current
		}
		if err != nil {
			if errors.Is(err, recordstore.ErrUncertain) {
				return ScheduleBatch{}, s.finish(ctx, sources, err)
			}
			// Only sole native conflict is a known competing acquisition. Joined
			// failures never become a benign skip or authorize another stage.
			if soleConflict(err) {
				// A conflict alone cannot distinguish a competing acquisition from
				// a changed immutable source. Inspect under current authority.
				currentJob, readErr := s.repo.ReadJob(ctx, job.Source)
				if authErr := s.finish(ctx, []ScheduledSource{job.Source}, nil); authErr != nil {
					if errors.Is(readErr, recordstore.ErrUncertain) {
						authErr = errors.Join(authErr, readErr)
					}
					return ScheduleBatch{}, authErr
				}
				if readErr != nil {
					if errors.Is(readErr, recordstore.ErrUncertain) {
						return ScheduleBatch{}, s.finish(ctx, sources, readErr)
					}
					operationErr = readErr
					break
				}
				if validateScheduledJob(currentJob) != nil || !sameScheduledSource(currentJob.Source, job.Source) {
					operationErr = billing.ErrRevenueUnavailable
					break
				}
				result.Conflicts++
				continue
			}
			operationErr = err
			break
		}
		if validateScheduledJob(leased) != nil || !sameScheduledSource(leased.Source, job.Source) || !sameOriginalInputs(leased, job) || !leased.CreatedAt.Equal(job.CreatedAt) || !leased.NextAttemptAt.Equal(job.NextAttemptAt) || leased.Revision != job.Revision+1 || leased.Fence != job.Fence+1 || leased.Attempts != job.Attempts+1 || leased.LeaseActor != s.cfg.ActorID || leased.LeaseToken != base64.RawURLEncoding.EncodeToString(token[:]) || !leased.LeasedUntil.Equal(now.Add(s.cfg.LeaseDuration)) || leased.Lane != lane {
			return ScheduleBatch{}, s.finish(ctx, sources, billing.ErrRevenueUnavailable)
		}
		result.Jobs = append(result.Jobs, leased)
	}
	if operationErr == nil && result.Examined == len(snapshot.Jobs) && len(snapshot.Jobs) < q.Limit {
		position = ""
	}
	if err := s.finish(ctx, sources, nil); err != nil {
		return ScheduleBatch{}, err
	}
	cursorErr := s.repo.CommitScan(ctx, ScheduleCommit{Scope: scope, Lane: lane, Expected: snapshot.Cursor, NextAfterID: position})
	if operationErr != nil || cursorErr != nil {
		return ScheduleBatch{}, s.finish(ctx, sources, errors.Join(operationErr, cursorErr))
	}
	result.Cursor = ScheduleCursor{Revision: snapshot.Cursor.Revision + 1, AfterID: position}
	for _, job := range result.Jobs {
		currentJob, err := s.repo.CheckLease(ctx, JobLease(job))
		if err != nil {
			return ScheduleBatch{}, s.finish(ctx, sources, err)
		}
		if validateScheduledJob(currentJob) != nil || !sameScheduledSource(currentJob.Source, job.Source) || !sameOriginalInputs(currentJob, job) || !exactLease(currentJob, JobLease(job), s.clock.Now()) {
			return ScheduleBatch{}, s.finish(ctx, sources, billing.ErrRevenueUnavailable)
		}
	}
	if err := s.finish(ctx, sources, nil); err != nil {
		return ScheduleBatch{}, err
	}
	return result, nil
}

// Check requires current refresh authority and the exact persisted lease. It is
// a stage boundary check, not a fence for billing's separate capture transaction.
func (s *Scheduler) Check(ctx context.Context, h LeaseHandle) (ScheduledJob, error) {
	if !leaseShape(h) {
		return ScheduledJob{}, billing.ErrRevenueInvalid
	}
	if err := s.begin(ctx, h.Source.Scope); err != nil {
		return ScheduledJob{}, err
	}
	if h.Actor != s.cfg.ActorID {
		return ScheduledJob{}, s.finish(ctx, nil, billing.ErrRevenueInvalid)
	}
	if err := s.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return ScheduledJob{}, err
	}
	j, err := s.repo.CheckLease(ctx, h)
	if err := s.finish(ctx, []ScheduledSource{h.Source}, err); err != nil {
		return ScheduledJob{}, err
	}
	if validateScheduledJob(j) != nil || !sameScheduledSource(j.Source, h.Source) || !exactLease(j, h, s.clock.Now()) {
		return ScheduledJob{}, s.finish(ctx, []ScheduledSource{h.Source}, billing.ErrRevenueUnavailable)
	}
	return j, nil
}

func retryDelay(attempts int64, base, maximum time.Duration) time.Duration {
	for attempts > 1 && base < maximum {
		if base > maximum/2 {
			return maximum
		}
		base *= 2
		attempts--
	}
	return base
}

// Retry releases only the exact current execution and retains unresolved inputs.
// Backoff derives from the durable acquisition count, with a bounded ceiling.
func (s *Scheduler) Retry(ctx context.Context, h LeaseHandle) (ScheduledJob, error) {
	j, err := s.Check(ctx, h)
	if err != nil {
		return ScheduledJob{}, err
	}
	now := s.clock.Now()
	if now.IsZero() {
		return ScheduledJob{}, s.finish(ctx, []ScheduledSource{h.Source}, billing.ErrRevenueUnavailable)
	}
	q := LeaseDisposition{Handle: h, Lane: j.Lane, NextAttemptAt: now.Add(retryDelay(j.Attempts, s.cfg.RetryBase, s.cfg.RetryMax))}
	if err := s.finish(ctx, []ScheduledSource{h.Source}, nil); err != nil {
		return ScheduledJob{}, err
	}
	out, err := s.repo.Release(ctx, q)
	if err := s.finish(ctx, []ScheduledSource{h.Source}, err); err != nil {
		return ScheduledJob{}, err
	}
	if validateScheduledJob(out) != nil || !sameScheduledSource(out.Source, j.Source) || !sameOriginalInputs(out, j) || !out.CreatedAt.Equal(j.CreatedAt) || out.Revision != j.Revision+1 || out.Fence != j.Fence || out.Attempts != j.Attempts || out.Lane != j.Lane || out.LeaseActor != "" || out.LeaseToken != "" || !out.LeasedUntil.IsZero() || !out.NextAttemptAt.Equal(q.NextAttemptAt) {
		return ScheduledJob{}, s.finish(ctx, []ScheduledSource{h.Source}, billing.ErrRevenueUnavailable)
	}
	return out, nil
}

// Inspect reads the current private job under current global and selected
// authority. It is recovery evidence, not a lease acquisition or execution grant.
// Callers must check the returned exact lease before any execution or disposition.
func (s *Scheduler) Inspect(ctx context.Context, source ScheduledSource) (ScheduledJob, error) {
	if !scheduleSourceShape(source) {
		return ScheduledJob{}, billing.ErrRevenueInvalid
	}
	if err := s.begin(ctx, source.Scope); err != nil {
		return ScheduledJob{}, err
	}
	if err := s.finish(ctx, []ScheduledSource{source}, nil); err != nil {
		return ScheduledJob{}, err
	}
	j, err := s.repo.ReadJob(ctx, source)
	if err = s.finish(ctx, []ScheduledSource{source}, err); err != nil {
		return ScheduledJob{}, err
	}
	if validateScheduledJob(j) != nil || !sameScheduledSource(j.Source, source) {
		return ScheduledJob{}, billing.ErrRevenueUnavailable
	}
	return j, nil
}
