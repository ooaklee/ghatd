package partnerstore

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named native financial/queue journeys, each with its own
// database and clock. No provider submission, host runtime or payout is implied.
func workerMaturitySource(t *testing.T, f *workerFixture, payment, mode string) partnerearnings.MaturitySource {
	t.Helper()
	req := scheduledAccrual(f.clock, payment)
	req.PartnerID = f.partner.ID
	if mode == "zero" {
		req.RateBasisPoints = 0
	}
	_, err := f.earnings.Accrue(f.ctx, req)
	require.NoError(t, err)
	if mode == "refund" {
		_, err = f.earnings.Reverse(f.ctx, partnerearnings.ReversalRequest{PartnerID: f.partner.ID, PaymentID: payment, RefundID: "refund_" + payment, CumulativeRefundedMinor: req.PaymentMinor, Currency: "EUR", OccurredAt: f.clock.Now()})
		require.NoError(t, err)
	}
	if mode == "held" {
		_, err = f.earnings.Dispute(f.ctx, partnerearnings.DisputeRequest{PartnerID: f.partner.ID, PaymentID: payment, DisputeID: "dispute_" + payment, OperationID: "hold_" + payment, Action: "hold", ActorID: "verified_worker", Currency: "EUR", OccurredAt: f.clock.Now(), Reason: "verified"})
		require.NoError(t, err)
	}
	page, err := f.earnings.PendingMaturitySourcesAfter(f.ctx, "", 200)
	require.NoError(t, err)
	for _, source := range page {
		if source.PaymentID == payment {
			return source
		}
	}
	t.Fatal("accepted pending source missing")
	return partnerearnings.MaturitySource{}
}

func TestMongoWorkerMaturityScheduledFinancialJourneys(t *testing.T) {
	for _, tc := range []struct {
		name, mode      string
		available, held int64
	}{
		{name: "deadline_survives_restart_and_paused_admission", available: 1000},
		{name: "zero_rate_still_completes_accepted_journal_work", mode: "zero"},
		{name: "full_refund_keeps_original_gross_maturity", mode: "refund"},
		{name: "dispute_hold_does_not_become_available", mode: "held", held: 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			source := workerMaturitySource(t, f, "original_payment", tc.mode)
			q := f.queue(t, f.queueRepo)
			report, err := f.worker(t, q, f.users, f.revenue, true, "first_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			require.Equal(t, 1, report.Discovered)
			require.Zero(t, report.Completed)
			job, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			require.Equal(t, source.AvailableAt, job.InitialDueAt)
			require.Equal(t, source.AvailableAt, job.NextAttemptAt)
			require.Zero(t, job.Attempts)
			f.clock.now = source.AvailableAt.Add(-time.Nanosecond)
			report, err = f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, true, "replacement_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			require.Zero(t, report.Completed)
			backlog, err := q.GetBacklog(f.ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, backlog.Counts.Delayed)
			require.Zero(t, backlog.Counts.Backoff)
			require.Zero(t, backlog.Counts.AttemptedPending)
			f.clock.now = source.AvailableAt
			report, err = f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, true, "replacement_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			require.Equal(t, 1, report.Completed)
			completed, err := f.earnings.GetMaturitySource(f.ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnerearnings.MaturityCompleted, completed.State)
			done, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			require.Equal(t, source.Fingerprint, done.SourceFingerprint)
			require.Equal(t, source.AvailableAt, done.InitialDueAt)
			require.Equal(t, completed.MaturedEntryID, done.Decision.AcceptanceID)
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			var movements int
			for _, e := range journal {
				if e.Kind == partnerearnings.EntryMatured {
					movements++
					require.Equal(t, completed.MaturedEntryID, e.ID)
					require.Equal(t, source.AccruedAmountMinor, e.AmountMinor)
				}
			}
			require.Equal(t, 1, movements)
			balances, err := f.earnings.Balances(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Equal(t, tc.available, balances.AvailableMinor)
			require.Equal(t, tc.held, balances.DisputeHoldMinor)
			_, err = f.worker(t, q, f.users, f.revenue, true, "third_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			again, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Equal(t, journal, again)
		})
	}
}

type maturityOwningFault struct {
	*partnerearnings.Service
	lostAck, revoke bool
	readFailure     string
	authority       *workerAuthority
	matures         int
}

func (o *maturityOwningFault) Mature(ctx context.Context, partner string) ([]partnerearnings.Entry, error) {
	o.matures++
	result, err := o.Service.Mature(ctx, partner)
	if err == nil && o.revoke {
		o.authority.revoked.Store(true)
	}
	if err == nil && o.lostAck {
		o.lostAck = false
		return nil, partnerearnings.ErrUncertain
	}
	return result, err
}
func (o *maturityOwningFault) GetMaturitySource(ctx context.Context, id string) (partnerearnings.MaturitySource, error) {
	if id == o.readFailure {
		return partnerearnings.MaturitySource{}, partnerearnings.ErrUnavailable
	}
	return o.Service.GetMaturitySource(ctx, id)
}

type maturityQueueFault struct {
	partnermanager.WorkRepository
	stage string
}

func (r *maturityQueueFault) RecordWorkDecision(ctx context.Context, item partnermanager.WorkItem, d partnermanager.WorkDecision, now time.Time) (partnermanager.WorkDecision, error) {
	if item.Kind != partnermanager.WorkMaturity {
		return r.WorkRepository.RecordWorkDecision(ctx, item, d, now)
	}
	if r.stage == "decision_before" {
		r.stage = ""
		return partnermanager.WorkDecision{}, injectedFailure
	}
	accepted, err := r.WorkRepository.RecordWorkDecision(ctx, item, d, now)
	if err == nil && r.stage == "decision_after" {
		r.stage = ""
		return partnermanager.WorkDecision{}, partnermanager.ErrWorkUncertain
	}
	return accepted, err
}
func (r *maturityQueueFault) CompleteWork(ctx context.Context, item partnermanager.WorkItem, decision string, now time.Time) error {
	if item.Kind == partnermanager.WorkMaturity && r.stage == "complete_before" {
		r.stage = ""
		return injectedFailure
	}
	err := r.WorkRepository.CompleteWork(ctx, item, decision, now)
	if err == nil && item.Kind == partnermanager.WorkMaturity && r.stage == "complete_after" {
		r.stage = ""
		return partnermanager.ErrWorkUncertain
	}
	return err
}
func maturityFaultWorker(t *testing.T, f *workerFixture, q *partnermanager.WorkQueue, owner partnermanager.EarningsService, actor string) *partnermanager.Worker {
	t.Helper()
	m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: owner, Identity: f.identity, Authority: f.authority, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock})
	require.NoError(t, err)
	w, err := partnermanager.NewWorker(m, q, f.users, f.revenue, f.reconciler, partnermanager.WorkerConfig{ActorID: actor, ConsumerID: "partner-financial-consumer", PageSize: 100, BatchSize: 100})
	require.NoError(t, err)
	return w
}

func TestMongoWorkerMaturityLostAcknowledgementAndRevocation(t *testing.T) {
	for _, tc := range []struct {
		name, stage            string
		lostFinancial, revoked bool
	}{
		{name: "financial_receipt_committed_reply_lost", lostFinancial: true},
		{name: "financial_commit_before_decision_failure", stage: "decision_before"},
		{name: "decision_receipt_committed_reply_lost", stage: "decision_after"},
		{name: "decision_receipt_before_completion_failure", stage: "complete_before"},
		{name: "queue_completion_committed_reply_lost", stage: "complete_after"},
		{name: "revoked_after_financial_commit_cannot_decide_or_retry", revoked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			source := workerMaturitySource(t, f, "payment", "")
			q := f.queue(t, f.queueRepo)
			_, err := f.worker(t, q, f.users, f.revenue, true, "first_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			f.clock.now = source.AvailableAt
			owner := &maturityOwningFault{Service: f.earnings, lostAck: tc.lostFinancial, revoke: tc.revoked, authority: f.authority}
			fault := &maturityQueueFault{WorkRepository: f.queueRepo, stage: tc.stage}
			report, err := maturityFaultWorker(t, f, f.queue(t, fault), owner, "first_worker").RunOnce(f.ctx)
			require.Error(t, err)
			if tc.revoked {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				require.Zero(t, report.Retried)
			}
			accepted, err := f.earnings.GetMaturitySource(f.ctx, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnerearnings.MaturityCompleted, accepted.State)
			job, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			if tc.revoked || tc.lostFinancial || tc.stage == "decision_before" {
				require.Nil(t, job.Decision)
			} else {
				require.NotNil(t, job.Decision)
			}
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Len(t, journal, 2)
			f.clock.now = f.clock.now.Add(time.Minute + time.Second)
			if tc.revoked {
				_, err = maturityFaultWorker(t, f, q, owner, "replacement_worker").RunOnce(f.ctx)
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				still, err := q.Get(f.ctx, job.Kind, source.ID)
				require.NoError(t, err)
				require.Equal(t, job, still)
				f.authority.revoked.Store(false)
				owner.revoke = false
			}
			_, err = maturityFaultWorker(t, f, f.queue(t, f.queueRepo), owner, "replacement_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			done, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			require.Equal(t, accepted.MaturedEntryID, done.Decision.AcceptanceID)
			require.Equal(t, 1, owner.matures, "restart must recover original receipt without another mutation")
			again, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Equal(t, journal, again)
		})
	}
}

func TestMongoWorkerMaturityMultipleSourcesAndPoisonIsolation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		poison bool
	}{
		{name: "two_jobs_same_partner_share_idempotent_financial_maturity"},
		{name: "source_outage_does_not_starve_later_valid_job", poison: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			sources := []partnerearnings.MaturitySource{workerMaturitySource(t, f, "first_payment", ""), workerMaturitySource(t, f, "second_payment", "")}
			sort.Slice(sources, func(i, j int) bool { return sources[i].ID < sources[j].ID })
			q := f.queue(t, f.queueRepo)
			owner := &maturityOwningFault{Service: f.earnings}
			_, err := maturityFaultWorker(t, f, q, owner, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			f.clock.now = sources[0].AvailableAt
			if tc.poison {
				owner.readFailure = sources[0].ID
			}
			report, err := maturityFaultWorker(t, f, q, owner, "worker").RunOnce(f.ctx)
			if tc.poison {
				require.ErrorIs(t, err, partnerearnings.ErrUnavailable)
				require.Equal(t, 1, report.Completed)
				require.Zero(t, report.Retried, "unknown partner evidence cannot confer retry authority")
				pending, err := q.Get(f.ctx, partnermanager.WorkMaturity, sources[0].ID)
				require.NoError(t, err)
				require.Equal(t, partnermanager.WorkPending, pending.State)
				require.Nil(t, pending.Decision)
				owner.readFailure = ""
				f.clock.now = f.clock.now.Add(time.Minute + time.Second)
				_, err = maturityFaultWorker(t, f, q, owner, "replacement_worker").RunOnce(f.ctx)
				require.NoError(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, report.Completed)
			}
			for _, source := range sources {
				done, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
				require.NoError(t, err)
				proof, err := f.earnings.GetMaturitySource(f.ctx, source.ID)
				require.NoError(t, err)
				require.Equal(t, partnermanager.WorkComplete, done.State)
				require.Equal(t, proof.MaturedEntryID, done.Decision.AcceptanceID)
			}
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Len(t, journal, 4)
			require.Equal(t, 1, owner.matures)
		})
	}
}

func TestMongoMaturityQueueDeadlineReplayAndStaleLease(t *testing.T) {
	for _, tc := range []struct {
		name     string
		lostPage bool
	}{
		{name: "deadline_receipt_replay_after_later_cursor_advance"},
		{name: "lost_page_ack_retains_original_deadline", lostPage: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, _, clock, ctx := mongoEarnings(t)
			q := workQueueFixture(t, store, clock)
			due := clock.Now().Add(time.Hour)
			items := []partnermanager.WorkCandidate{{SourceID: "source", SourceFingerprint: "immutable_source", DueAt: due}}
			start, next := partnermanager.DiscoveryCursor{}, partnermanager.DiscoveryCursor{AfterID: "source"}
			if tc.lostPage {
				err := workQueueFixture(t, workFaultStore{Store: store, uncertain: true}, clock).EnqueuePage(ctx, partnermanager.WorkMaturity, start, next, items)
				require.ErrorIs(t, err, partnermanager.ErrWorkUncertain)
			}
			require.NoError(t, q.EnqueuePage(ctx, partnermanager.WorkMaturity, start, next, items))
			cursor, err := q.Cursor(ctx, partnermanager.WorkMaturity)
			require.NoError(t, err)
			require.NoError(t, q.EnqueuePage(ctx, partnermanager.WorkMaturity, cursor, partnermanager.DiscoveryCursor{}, nil))
			require.NoError(t, q.EnqueuePage(ctx, partnermanager.WorkMaturity, start, next, items))
			changed := append([]partnermanager.WorkCandidate(nil), items...)
			changed[0].DueAt = due.Add(time.Nanosecond)
			require.ErrorIs(t, q.EnqueuePage(ctx, partnermanager.WorkMaturity, start, next, changed), partnermanager.ErrWorkConflict)
			cursor, err = q.Cursor(ctx, partnermanager.WorkMaturity)
			require.NoError(t, err)
			require.ErrorIs(t, q.EnqueuePage(ctx, partnermanager.WorkMaturity, cursor, next, changed), partnermanager.ErrWorkConflict)
			clock.now = due.Add(-time.Nanosecond)
			notDue, err := q.Lease(ctx, partnermanager.WorkMaturity, 10)
			require.NoError(t, err)
			require.Empty(t, notDue)
			clock.now = due
			leases, err := q.Lease(ctx, partnermanager.WorkMaturity, 10)
			require.NoError(t, err)
			require.Len(t, leases, 1)
			old := leases[0]
			clock.now = clock.Now().Add(time.Minute)
			current, err := q.Lease(ctx, partnermanager.WorkMaturity, 10)
			require.NoError(t, err)
			require.Len(t, current, 1)
			require.Equal(t, due, current[0].InitialDueAt)
			require.NotEqual(t, old.LeaseToken, current[0].LeaseToken)
			_, err = q.Decide(ctx, old, "stale_worker", partnermanager.WorkAccepted, "receipt", "financial_matured")
			require.ErrorIs(t, err, partnermanager.ErrWorkLeaseLost)
			require.ErrorIs(t, q.Retry(ctx, old, "owning_unavailable"), partnermanager.ErrWorkLeaseLost)
			clock.now = due.Add(-time.Hour)
			require.NoError(t, q.Retry(ctx, current[0], "owning_unavailable"))
			retained, err := q.Get(ctx, partnermanager.WorkMaturity, "source")
			require.NoError(t, err)
			require.Equal(t, due, retained.NextAttemptAt, "clock rollback must preserve the original deadline")
			require.Equal(t, due, retained.InitialDueAt)
		})
	}
}

func TestMongoWorkerMaturityCursorResetFindsLateSources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		lower bool
	}{
		{name: "late_lower_source_id_is_discovered_after_completed_sweep", lower: true},
		{name: "late_higher_source_id_keeps_existing_future_job"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			payments := []string{"first_payment", "second_payment"}
			id := func(payment string) string {
				return "maturity_" + workDigest(partnerprogram.ProgramID, "EUR", f.partner.ID, payment)
			}
			sort.Slice(payments, func(i, j int) bool { return id(payments[i]) < id(payments[j]) })
			first, late := payments[0], payments[1]
			if tc.lower {
				first, late = late, first
			}
			source := workerMaturitySource(t, f, first, "")
			q := f.queue(t, f.queueRepo)
			_, err := f.worker(t, q, f.users, f.revenue, true, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			original, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			cursor, err := q.Cursor(f.ctx, partnermanager.WorkMaturity)
			require.NoError(t, err)
			require.Equal(t, source.ID, cursor.AfterID)
			_, err = f.worker(t, q, f.users, f.revenue, true, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			reset, err := q.Cursor(f.ctx, partnermanager.WorkMaturity)
			require.NoError(t, err)
			require.Empty(t, reset.AfterID)
			require.Greater(t, reset.Revision, cursor.Revision)
			newSource := workerMaturitySource(t, f, late, "")
			report, err := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, true, "replacement_worker").RunOnce(f.ctx)
			require.NoError(t, err)
			require.Equal(t, 2, report.Discovered, "discovery count includes immutable queued-source replay")
			unchanged, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			require.Equal(t, original, unchanged)
			retained, err := q.Get(f.ctx, partnermanager.WorkMaturity, newSource.ID)
			require.NoError(t, err)
			require.Equal(t, newSource.AvailableAt, retained.InitialDueAt)
			require.Zero(t, retained.Attempts)
		})
	}
}

func TestMongoWorkerMaturityDecisionCannotManufactureFinancialAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name      string
		completed bool
	}{
		{name: "pending_source_with_queue_decision_never_mutates"},
		{name: "different_actual_receipt_never_completes", completed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			source := workerMaturitySource(t, f, "payment", "")
			q := f.queue(t, f.queueRepo)
			_, err := f.worker(t, q, f.users, f.revenue, true, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			f.clock.now = source.AvailableAt
			if tc.completed {
				_, err = f.earnings.Mature(f.ctx, f.partner.ID)
				require.NoError(t, err)
			}
			jobs, err := q.Lease(f.ctx, partnermanager.WorkMaturity, 1)
			require.NoError(t, err)
			require.Len(t, jobs, 1)
			_, err = q.Decide(f.ctx, jobs[0], "original_worker", partnermanager.WorkAccepted, "invented_financial_receipt", "financial_matured")
			require.NoError(t, err)
			require.NoError(t, q.Retry(f.ctx, jobs[0], "owning_unavailable"))
			f.clock.now = f.clock.Now().Add(2 * time.Second)
			before, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			owner := &maturityOwningFault{Service: f.earnings}
			report, err := maturityFaultWorker(t, f, q, owner, "current_worker").RunOnce(f.ctx)
			require.ErrorIs(t, err, partnermanager.ErrWorkConflict)
			require.Zero(t, report.Completed)
			require.Equal(t, 1, report.Retried)
			require.Zero(t, owner.matures)
			job, err := q.Get(f.ctx, partnermanager.WorkMaturity, source.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkPending, job.State)
			require.Equal(t, "source_conflict", job.LastErrorCode)
			after, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
