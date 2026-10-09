package billinglifecycle

import (
	"context"
	"errors"
	"reflect"
	"sync"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// WorkerReport exposes operational counts only. An error withholds every count:
// committed work remains recoverable, but partial output grants no permission.
type WorkerReport struct {
	Discovered, Examined, Conflicts, Completed, Retried, Superseded int `json:"-"`
}

// Worker composes one bounded sequential discovery/execution pass. All services
// must use the same scheduler and configured manager. Construction starts no
// goroutine, creates no grants and performs no migration or native preparation.
type Worker struct {
	discovery          *DiscoveryCollector
	scheduler          *Scheduler
	checkout           *CheckoutExecution
	status             *StatusExecution
	checkoutCompletion *CheckoutCompletion
	statusCompletion   *StatusCompletionService
	resolver           *StatusOriginalResolver
	mu                 sync.Mutex
	nextScope          int
}

func NewWorker(d *DiscoveryCollector, s *Scheduler, c *CheckoutExecution, status *StatusExecution, cc *CheckoutCompletion, sc *StatusCompletionService, resolver *StatusOriginalResolver) (*Worker, error) {
	if d == nil || s == nil || c == nil || status == nil || cc == nil || sc == nil || resolver == nil || !resolver.ready() || nilPort(d.repo) || nilPort(s.repo) || nilPort(cc.repo) || nilPort(sc.repo) {
		return nil, billing.ErrRevenueUnavailable
	}
	if c.scheduler != s || status.scheduler != s || cc.execution != c || sc.execution != status || resolver.execution != status || d.actor != s.cfg.ActorID || len(d.scopes) != len(s.cfg.Scopes) {
		return nil, billing.ErrRevenueInvalid
	}
	// Require the actual configured manager instance, not merely equal settings.
	if !sameWorkerPort(d.manager, c.owner) || !sameWorkerPort(d.manager, status.owner) || !sameWorkerPort(d.authority, s.authority) || !sameWorkerPort(d.clock, s.clock) {
		return nil, billing.ErrRevenueInvalid
	}
	for i, scope := range d.scopes {
		if scope != s.cfg.Scopes[i] {
			return nil, billing.ErrRevenueInvalid
		}
	}
	return &Worker{discovery: d, scheduler: s, checkout: c, status: status, checkoutCompletion: cc, statusCompletion: sc, resolver: resolver}, nil
}

func unknownExecution(err error) bool {
	return errors.Is(err, billing.ErrRevenueUncertain) || errors.Is(err, recordstore.ErrUncertain)
}
func preserveExecutionError(later, original error) error {
	if later == nil {
		return original
	}
	if unknownExecution(original) {
		return errors.Join(later, original)
	}
	return later
}
func (w *Worker) finish(ctx context.Context, sources []ScheduledSource, report WorkerReport, err error) (WorkerReport, error) {
	if final := w.scheduler.finish(ctx, sources, nil); final != nil {
		return WorkerReport{}, preserveExecutionError(final, err)
	}
	if err != nil {
		return WorkerReport{}, err
	}
	return report, nil
}

// RunOnce examines one page per kind and scope and one page per execution lane.
// Its shared-instance lock fails promptly on overlap. Scope start rotates between
// passes, including failed ones, so one unavailable scope cannot starve all others.
// Unknown results stop the pass; later passes inspect durable state rather than
// replaying a withheld cursor, acquisition or stage acknowledgement.
func (w *Worker) RunOnce(ctx context.Context) (WorkerReport, error) {
	if ctx == nil {
		return WorkerReport{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return WorkerReport{}, err
	}
	if w == nil || w.scheduler == nil || w.discovery == nil {
		return WorkerReport{}, billing.ErrRevenueUnavailable
	}
	if !w.mu.TryLock() {
		return WorkerReport{}, recordstore.ErrConflict
	}
	defer w.mu.Unlock()
	if err := w.scheduler.authorize(ctx, nil); err != nil {
		return WorkerReport{}, err
	}
	count := len(w.scheduler.cfg.Scopes)
	start := w.nextScope
	w.nextScope = (start + 1) % count
	report := WorkerReport{}
	var sources []ScheduledSource
	var issues error
	for n := 0; n < count; n++ {
		scope := w.scheduler.cfg.Scopes[(start+n)%count]
		for _, kind := range []string{billing.LifecycleCheckoutSources, billing.LifecycleSubscriptionSources} {
			admitted, err := w.discovery.Admit(ctx, scope, kind)
			if err != nil {
				return w.finish(ctx, sources, report, errors.Join(issues, err))
			}
			report.Discovered += admitted.Sources
		}
		for _, lane := range []string{ColdLane, RefreshLane} {
			batch, err := w.scheduler.Scan(ctx, scope, lane)
			if err != nil {
				return w.finish(ctx, sources, report, errors.Join(issues, err))
			}
			report.Examined += batch.Examined
			report.Conflicts += batch.Conflicts
			for _, job := range batch.Jobs {
				sources = append(sources, job.Source)
				latest, superseded, err := w.execute(ctx, job)
				report.Superseded += superseded
				if err == nil {
					report.Completed++
					continue
				}
				issues = errors.Join(issues, err)
				if unknownExecution(err) || ctx.Err() != nil || errors.Is(err, partnermanager.ErrDenied) {
					return w.finish(ctx, sources, report, issues)
				}
				retried, retryErr := w.retryCurrent(ctx, latest)
				if retried {
					report.Retried++
				}
				if retryErr != nil {
					issues = errors.Join(issues, retryErr)
					// Every disposition failure may leave a commit. Do not execute any
					// more stages under an unacknowledged disposition in this pass.
					return w.finish(ctx, sources, report, issues)
				}
			}
		}
	}
	return w.finish(ctx, sources, report, issues)
}

func (w *Worker) inspectHistory(ctx context.Context, j ScheduledJob) error {
	if j.LastCompletionID != "" {
		v, err := w.statusCompletion.InspectLast(ctx, j.Source)
		if err != nil {
			return err
		}
		if v.Job.LastCompletionID != j.LastCompletionID || v.Job.Revision > j.Revision || v.Job.Fence > j.Fence {
			return billing.ErrRevenueUnavailable
		}
	}
	if j.LastSupersessionID != "" {
		v, err := w.resolver.InspectLast(ctx, j.Source)
		if err != nil {
			return err
		}
		if v.Job.LastSupersessionID != j.LastSupersessionID || v.Job.Revision > j.Revision || v.Job.Fence > j.Fence {
			return billing.ErrRevenueUnavailable
		}
	}
	return nil
}

func (w *Worker) execute(ctx context.Context, j ScheduledJob) (ScheduledJob, int, error) {
	current, err := executionStep(ctx, w.scheduler, j, nil)
	if err != nil {
		return j, 0, err
	}
	if current.Source.Kind == billing.LifecycleCheckoutSources {
		observed, err := w.checkout.Observe(ctx, JobLease(current))
		if err != nil {
			return current, 0, err
		}
		_, err = w.checkoutCompletion.Complete(ctx, observed)
		return observed.Job, 0, err
	}
	if err = w.inspectHistory(ctx, current); err != nil {
		return current, 0, err
	}
	superseded := 0
	if current.OriginalStatus != nil {
		resolved, err := w.resolver.Resolve(ctx, JobLease(current))
		if err != nil {
			return current, 0, err
		}
		current = resolved.Job
		switch resolved.State {
		case billing.SubscriptionStatusCaptured:
			if resolved.Observation == nil {
				return current, 0, billing.ErrRevenueUnavailable
			}
			_, err = w.statusCompletion.Complete(ctx, *resolved.Observation)
			return current, 0, err
		case billing.SubscriptionStatusPending:
			// Pending has not yet captured: progress using the exact original, not
			// a new preparation. Stopping here would permanently strand first work.
		case billing.SubscriptionStatusSuperseded:
			superseded = 1
		default:
			return current, 0, billing.ErrRevenueUnavailable
		}
	}
	observed, err := w.status.Observe(ctx, JobLease(current))
	if err != nil {
		return current, superseded, err
	}
	_, err = w.statusCompletion.Complete(ctx, observed)
	return observed.Job, superseded, err
}

// retryCurrent inspects durable stage progress before a known-failure backoff.
// Same-epoch binding revisions may legitimately advance; they are rejoined to
// owning provenance. Changed epochs or cleared/expired leases grant no retry.
func (w *Worker) retryCurrent(ctx context.Context, expected ScheduledJob) (bool, error) {
	current, err := w.scheduler.Inspect(ctx, expected.Source)
	if err != nil {
		return false, err
	}
	if current.Fence != expected.Fence || current.LeaseActor != expected.LeaseActor || current.LeaseToken != expected.LeaseToken || !current.LeasedUntil.Equal(expected.LeasedUntil) || !current.LeasedUntil.After(w.scheduler.clock.Now()) {
		return false, nil
	}
	if current.Revision < expected.Revision || current.Attempts != expected.Attempts || current.Lane != expected.Lane || !current.CreatedAt.Equal(expected.CreatedAt) || !current.NextAttemptAt.Equal(expected.NextAttemptAt) || current.LastCompletionID != expected.LastCompletionID {
		return false, billing.ErrRevenueUnavailable
	}
	if current.Source.Kind == billing.LifecycleCheckoutSources {
		if expected.CheckoutPrepared && !current.CheckoutPrepared {
			return false, billing.ErrRevenueUnavailable
		}
		if current.CheckoutPrepared {
			input, err := w.checkout.outbox.Find(ctx, w.scheduler.cfg.ActorID, *current.Source.Checkout)
			if err != nil {
				return false, boundInputError(err)
			}
			if !checkoutMatchesSource(input, current.Source) {
				return false, billing.ErrRevenueUnavailable
			}
		}
	} else {
		if err = w.inspectHistory(ctx, current); err != nil {
			return false, err
		}
		if expected.OriginalStatus != nil && (current.OriginalStatus == nil || !sameStatusPreparation(*expected.OriginalStatus, *current.OriginalStatus)) {
			if current.LastSupersessionID == expected.LastSupersessionID {
				return false, billing.ErrRevenueUnavailable
			}
			historical, err := w.resolver.InspectLast(ctx, current.Source)
			if err != nil {
				return false, err
			}
			if !sameStatusPreparation(historical.Input.Preparation, *expected.OriginalStatus) {
				return false, billing.ErrRevenueUnavailable
			}
		}
		if current.OriginalStatus != nil {
			input, err := w.status.outbox.Find(ctx, w.scheduler.cfg.ActorID, *current.OriginalStatus)
			if err != nil {
				return false, boundInputError(err)
			}
			if !sameStatusPreparation(input.Preparation, *current.OriginalStatus) {
				return false, billing.ErrRevenueUnavailable
			}
		}
	}
	if _, err = executionStep(ctx, w.scheduler, current, nil); err != nil {
		return false, err
	}
	_, err = w.scheduler.Retry(ctx, JobLease(current))
	return err == nil, err
}

// Comparable injected instances establish shared composition without reflecting
// over private state. Noncomparable adapters cannot establish this invariant.
func sameWorkerPort(a, b any) bool {
	if nilPort(a) || nilPort(b) {
		return false
	}
	t := reflect.TypeOf(a)
	return t == reflect.TypeOf(b) && t.Comparable() && a == b
}
