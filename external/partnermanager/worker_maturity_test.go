package partnermanager

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named, isolated orchestration boundaries. These fixtures
// model verified owning output; native tests separately prove journal/receipt
// acceptance and durable queue recovery through the real earnings service.
func (*earningsStub) Config() partnerearnings.Config {
	return partnerearnings.Config{ProgramID: partnerprogram.ProgramID, Currency: "EUR"}
}
func (*earningsStub) GetMaturitySource(context.Context, string) (partnerearnings.MaturitySource, error) {
	return partnerearnings.MaturitySource{}, partnerearnings.ErrNotFound
}
func (*earningsStub) PendingMaturitySourcesAfter(context.Context, string, int) ([]partnerearnings.MaturitySource, error) {
	return nil, nil
}

type maturityQueueSpy struct {
	workRepositorySpy
	items     []WorkItem
	completed int
}

func (r *maturityQueueSpy) EnqueueWorkPage(_ context.Context, _, _ string, _, _ DiscoveryCursor, items []WorkItem) error {
	r.calls++
	r.items = items
	return nil
}
func (r *maturityQueueSpy) CompleteWork(context.Context, WorkItem, string, time.Time) error {
	r.completed++
	return nil
}

func TestWorkMaturityDeadlineScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		due        time.Duration
		missing    bool
		want       error
	}{
		{name: "future_deadline_is_retained_and_delayed", kind: WorkMaturity, due: time.Hour},
		{name: "exact_deadline_is_ready", kind: WorkMaturity},
		{name: "already_due_uses_queue_creation_time", kind: WorkMaturity, due: -time.Hour},
		{name: "maturity_requires_owning_deadline", kind: WorkMaturity, missing: true, want: ErrInvalid},
		{name: "signup_cannot_supply_financial_deadline", kind: WorkSignup, want: ErrInvalid},
		{name: "revenue_cannot_supply_financial_deadline", kind: WorkRevenue, want: ErrInvalid},
		{name: "source_recovery_cannot_supply_financial_deadline", kind: WorkRevenueSource, want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 7, 12, 0, 0, 123456789, time.UTC)
			due := at.Add(tc.due).In(time.FixedZone("fixture", 2*60*60))
			if tc.missing {
				due = time.Time{}
			}
			repo := &maturityQueueSpy{}
			q, err := NewWorkQueue(repo, managerClock{at}, workTestConfig())
			require.NoError(t, err)
			err = q.EnqueuePage(context.Background(), tc.kind, DiscoveryCursor{}, DiscoveryCursor{}, []WorkCandidate{{SourceID: "source", SourceFingerprint: "owner_digest", DueAt: due}})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, repo.calls)
				return
			}
			require.Len(t, repo.items, 1)
			item := repo.items[0]
			require.Equal(t, due.UTC(), item.InitialDueAt)
			ready := at
			if due.After(at) {
				ready = due.UTC()
			}
			require.Equal(t, ready, item.NextAttemptAt)
			require.Zero(t, item.Attempts)
		})
	}
}

type maturityEarningsStub struct {
	EarningsService
	source             partnerearnings.MaturitySource
	readErr, matureErr error
	reads, matures     int
	noCompletion       bool
	onRead, onMature   func()
}

func (s *maturityEarningsStub) Config() partnerearnings.Config {
	return partnerearnings.Config{ProgramID: partnerprogram.ProgramID, Currency: "EUR"}
}
func (s *maturityEarningsStub) GetMaturitySource(context.Context, string) (partnerearnings.MaturitySource, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead()
	}
	return s.source, s.readErr
}
func (s *maturityEarningsStub) PendingMaturitySourcesAfter(context.Context, string, int) ([]partnerearnings.MaturitySource, error) {
	return nil, nil
}
func (s *maturityEarningsStub) Mature(context.Context, string) ([]partnerearnings.Entry, error) {
	s.matures++
	if !s.noCompletion {
		s.source.State, s.source.Revision = partnerearnings.MaturityCompleted, 2
		s.source.MaturedEntryID, s.source.MaturedAt = "owning_maturity_receipt", s.source.AvailableAt
	}
	if s.onMature != nil {
		s.onMature()
	}
	// A concurrent/idempotent mutation may return no new journal rows. Its
	// actual retained financial receipt is verified through the subsequent read.
	return nil, s.matureErr
}
func maturityWorkerSource(t *testing.T, at time.Time) partnerearnings.MaturitySource {
	t.Helper()
	s := partnerearnings.MaturitySource{ProgramID: partnerprogram.ProgramID, Currency: "EUR", PartnerID: "verified_partner", PaymentID: "verified_payment", AccruedEntryID: "original_accrual", AccruedFingerprint: "original_accrual_digest", AccruedSequence: 1, AccruedAmountMinor: 100, AvailableAt: at, CreatedAt: at.Add(-time.Hour), State: partnerearnings.MaturityPending, Revision: 1}
	id, err := sourceDigest([]string{s.ProgramID, s.Currency, s.PartnerID, s.PaymentID})
	require.NoError(t, err)
	s.ID = "maturity_" + id
	s.Fingerprint, err = sourceDigest(struct {
		ID, Program, Partner, Currency, Payment, Entry, AccrualFingerprint string
		Sequence, Amount                                                   int64
		AvailableAt, CreatedAt                                             string
	}{s.ID, s.ProgramID, s.PartnerID, s.Currency, s.PaymentID, s.AccruedEntryID, s.AccruedFingerprint, s.AccruedSequence, s.AccruedAmountMinor, s.AvailableAt.UTC().Format(time.RFC3339Nano), s.CreatedAt.UTC().Format(time.RFC3339Nano)})
	require.NoError(t, err)
	require.NoError(t, s.Validate())
	return s
}

func TestWorkerMaturityRequiresActualReceiptAndCurrentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
		matures    int
	}{
		{name: "pending_commits_then_rereads_owning_receipt", matures: 1},
		{name: "completed_source_recovers_without_mutation", mode: "completed"},
		{name: "old_actor_decision_recovers_original_receipt", mode: "decision"},
		{name: "decision_cannot_manufacture_pending_receipt", mode: "pending_decision", want: ErrWorkConflict},
		{name: "decision_cannot_replace_actual_receipt", mode: "wrong_receipt", want: ErrWorkConflict},
		{name: "success_without_completed_proof_is_pending", mode: "no_completion", matures: 1, want: partnerearnings.ErrUnresolved},
		{name: "lost_financial_reply_keeps_receipt_for_retry", mode: "lost_reply", matures: 1, want: partnerearnings.ErrUncertain},
		{name: "absence_is_not_success_or_retry_authority", mode: "missing", want: partnerearnings.ErrNotFound},
		{name: "changed_deadline_is_not_retimed", mode: "due_changed", want: ErrWorkConflict},
		{name: "revoked_program_cannot_read_source", mode: "revoked", want: ErrDenied},
		{name: "revoked_during_financial_commit_cannot_decide", mode: "revoke_mature", matures: 1, want: ErrDenied},
		{name: "future_deadline_cannot_mutate", mode: "future", want: partnerearnings.ErrUnresolved},
		{name: "clock_must_catch_up_to_owning_receipt", mode: "receipt_future", want: partnerearnings.ErrUnresolved},
		{name: "decision_cannot_predate_actual_receipt", mode: "decision_before_receipt", want: ErrWorkConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, authority, _, _ := managerFixture(t)
			at := m.deps.Clock.Now()
			e := &maturityEarningsStub{source: maturityWorkerSource(t, at)}
			m.deps.Earnings = e
			repo := &maturityQueueSpy{}
			q, err := NewWorkQueue(repo, m.deps.Clock, WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
			require.NoError(t, err)
			w, err := NewWorker(m, q, &signupFeedStub{}, &revenueFeedStub{}, &sourceReconcilerStub{}, WorkerConfig{ActorID: "current_worker", ConsumerID: "consumer", PageSize: 10, BatchSize: 10})
			require.NoError(t, err)
			item := WorkItem{ID: workID(partnerprogram.ProgramID, WorkMaturity, e.source.ID), ProgramID: partnerprogram.ProgramID, Kind: WorkMaturity, State: WorkPending, SourceID: e.source.ID, SourceFingerprint: e.source.Fingerprint, InitialDueAt: e.source.AvailableAt, CreatedAt: at.Add(-time.Hour), NextAttemptAt: at, Attempts: 1, LeaseToken: "fence", LeasedUntil: at.Add(time.Minute)}
			if tc.mode == "completed" || tc.mode == "decision" || tc.mode == "wrong_receipt" || tc.mode == "receipt_future" || tc.mode == "decision_before_receipt" {
				e.source.State, e.source.Revision = partnerearnings.MaturityCompleted, 2
				e.source.MaturedEntryID, e.source.MaturedAt = "owning_maturity_receipt", at
				if tc.mode == "receipt_future" || tc.mode == "decision_before_receipt" {
					e.source.MaturedAt = at.Add(time.Second)
				}
			}
			if tc.mode == "decision" || tc.mode == "pending_decision" || tc.mode == "wrong_receipt" || tc.mode == "decision_before_receipt" {
				d := WorkDecision{ActorID: "original_worker", Outcome: WorkAccepted, AcceptanceID: "owning_maturity_receipt", ReasonCode: "financial_matured", RecordedAt: at}
				if tc.mode == "wrong_receipt" {
					d.AcceptanceID = "invented_receipt"
				}
				d.Fingerprint, err = workDecisionDigest(item, d.ActorID, d.Outcome, d.AcceptanceID, d.ReasonCode)
				require.NoError(t, err)
				d.ID = "decision_" + d.Fingerprint
				item.Decision = &d
			}
			switch tc.mode {
			case "no_completion":
				e.noCompletion = true
			case "lost_reply":
				e.matureErr = partnerearnings.ErrUncertain
			case "missing":
				e.readErr = partnerearnings.ErrNotFound
			case "due_changed":
				item.InitialDueAt = item.InitialDueAt.Add(time.Nanosecond)
			case "revoked":
				authority.deny = true
			case "revoke_mature":
				e.onMature = func() { authority.deny = true }
			case "future":
				q.clock = managerClock{at.Add(-time.Nanosecond)}
			}
			target, err := w.processMaturity(context.Background(), item)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.matures, e.matures)
			if tc.want != nil {
				require.Zero(t, repo.completed)
				require.Empty(t, repo.decision)
			} else {
				require.Equal(t, e.source.PartnerID, target)
				require.Equal(t, 1, repo.completed)
				if item.Decision == nil {
					require.Equal(t, e.source.MaturedEntryID, repo.decision.AcceptanceID)
				}
			}
			if tc.mode == "revoked" {
				require.Zero(t, e.reads)
			}
			for _, cap := range authority.calls {
				require.Equal(t, CapabilityMaturityWorker, cap)
			}
		})
	}
}

func TestWorkMaturityRetryPreservesDeadlineUnderClockChanges(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shift     time.Duration
		wantShift time.Duration
	}{
		{name: "normal_retry_uses_bounded_backoff", wantShift: 2 * time.Second},
		{name: "backwards_clock_cannot_retime_original_deadline", shift: -time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			due := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			r := &maturityQueueSpy{}
			q, err := NewWorkQueue(r, managerClock{due.Add(tc.shift)}, workTestConfig())
			require.NoError(t, err)
			item := WorkItem{ID: workID("program", WorkMaturity, "source"), ProgramID: "program", Kind: WorkMaturity, SourceID: "source", SourceFingerprint: "fingerprint", InitialDueAt: due, CreatedAt: due.Add(-time.Hour), Attempts: 2, LeaseToken: "fence", LeasedUntil: due.Add(time.Minute)}
			require.NoError(t, q.Retry(context.Background(), item, "owning_unavailable"))
			require.Equal(t, due.Add(tc.wantShift), r.retryAt)
		})
	}
}

type maturityRollbackClock struct {
	at    time.Time
	calls int
}

func (c *maturityRollbackClock) Now() time.Time {
	c.calls++
	if c.calls >= 3 {
		return c.at.Add(-time.Nanosecond)
	}
	return c.at
}

func TestMaturityDecisionClockEvidenceCannotRegress(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		rollback, zeroComplete bool
	}{
		{name: "accepted_receipt_and_decision_share_valid_chronology"},
		{name: "clock_rolls_back_between_receipt_check_and_decision", rollback: true},
		{name: "zero_completion_clock_cannot_retire_job", zeroComplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			at := m.deps.Clock.Now()
			e := &maturityEarningsStub{source: maturityWorkerSource(t, at)}
			e.source.State, e.source.Revision = partnerearnings.MaturityCompleted, 2
			e.source.MaturedEntryID, e.source.MaturedAt = "actual_receipt", at
			m.deps.Earnings = e
			r := &maturityQueueSpy{}
			q, err := NewWorkQueue(r, managerClock{at}, WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
			require.NoError(t, err)
			w, err := NewWorker(m, q, &signupFeedStub{}, &revenueFeedStub{}, &sourceReconcilerStub{}, WorkerConfig{ActorID: "worker", ConsumerID: "consumer", PageSize: 10, BatchSize: 10})
			require.NoError(t, err)
			item := WorkItem{ID: workID(partnerprogram.ProgramID, WorkMaturity, e.source.ID), ProgramID: partnerprogram.ProgramID, Kind: WorkMaturity, SourceID: e.source.ID, SourceFingerprint: e.source.Fingerprint, InitialDueAt: at, CreatedAt: at.Add(-time.Hour), Attempts: 1, LeaseToken: "fence", LeasedUntil: at.Add(time.Minute)}
			if tc.zeroComplete {
				q.clock = managerClock{}
				require.ErrorIs(t, q.Complete(context.Background(), item, "decision"), ErrInvalid)
				require.Zero(t, r.completed)
				return
			}
			if tc.rollback {
				q.clock = &maturityRollbackClock{at: at}
			}
			_, err = w.processMaturity(context.Background(), item)
			if tc.rollback {
				require.ErrorIs(t, err, ErrInvalid)
				require.Empty(t, r.decision)
				require.Zero(t, r.completed)
			} else {
				require.NoError(t, err)
				require.Equal(t, at, r.decision.RecordedAt)
				require.Equal(t, 1, r.completed)
			}
			require.Zero(t, e.matures)
		})
	}
}

type maturityMissingFeed struct{ EarningsService }

type maturityScopedAuthority struct {
	capability string
	partner    string
	targets    []string
}

func (a *maturityScopedAuthority) CheckPartners(_ context.Context, _, capability, target string) error {
	a.targets = append(a.targets, target)
	if capability != a.capability || (target != "" && target != a.partner) {
		return ErrDenied
	}
	return nil
}

func TestDedicatedMaturityAuthorityDoesNotGrantOtherFinancialOperations(t *testing.T) {
	for _, tc := range []struct {
		name, operation, capability string
		want                        error
	}{
		{name: "maturity_recovers_while_all_admission_is_paused", operation: "mature", capability: CapabilityMaturityWorker},
		{name: "revenue_grant_cannot_mature", operation: "mature", capability: CapabilityRevenueWorker, want: ErrDenied},
		{name: "maturity_grant_cannot_process_revenue", operation: "revenue", capability: CapabilityMaturityWorker, want: ErrDenied},
		{name: "maturity_grant_cannot_create_claim", operation: "claim", capability: CapabilityMaturityWorker, want: ErrDenied},
		{name: "maturity_grant_cannot_record_payment", operation: "payment", capability: CapabilityMaturityWorker, want: ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			e := &maturityEarningsStub{source: maturityWorkerSource(t, m.deps.Clock.Now())}
			m.deps.Earnings = e
			a := &maturityScopedAuthority{capability: tc.capability, partner: e.source.PartnerID}
			m.deps.Authority = a
			m.deps.Controls = Controls{ManualRecording: true}
			var err error
			switch tc.operation {
			case "mature":
				_, err = m.MaturePartner(context.Background(), "worker", e.source.PartnerID)
			case "revenue":
				_, err = m.ProcessRevenueFact(context.Background(), "worker", "billing_fact")
			case "claim":
				_, err = m.RequestClaim(context.Background(), "worker", 1, "claim_key")
			case "payment":
				_, err = m.AdminRecordPayment(context.Background(), partnerearnings.RecordPaymentRequest{ActorID: "worker", ClaimID: "claim", ExpectedRevision: 1})
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, e.matures)
			} else {
				require.Equal(t, 1, e.matures)
				require.Equal(t, []string{e.source.PartnerID, e.source.PartnerID}, a.targets)
			}
		})
	}
}

func TestWorkerMaturityRequiresSameScopedEarningsOwner(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		missing, typedNil, wrongCurrency bool
		want                             error
	}{
		{name: "verified_same_owner"},
		{name: "missing_feed_is_unavailable", missing: true, want: ErrUnavailable},
		{name: "typed_nil_earnings_is_unavailable", typedNil: true, want: ErrUnavailable},
		{name: "different_currency_is_invalid", wrongCurrency: true, want: ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, _, e, _, _, _ := managerFixture(t)
			if tc.missing {
				m.deps.Earnings = maturityMissingFeed{e}
			}
			if tc.typedNil {
				var nilOwner *maturityEarningsStub
				m.deps.Earnings = nilOwner
			}
			if tc.wrongCurrency {
				p.cfg.Currency = "USD"
			}
			q, err := NewWorkQueue(&workRepositorySpy{}, m.deps.Clock, WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
			require.NoError(t, err)
			_, err = NewWorker(m, q, &signupFeedStub{}, &revenueFeedStub{}, &sourceReconcilerStub{}, WorkerConfig{ActorID: "worker", ConsumerID: "consumer", PageSize: 10, BatchSize: 10})
			require.ErrorIs(t, err, tc.want)
		})
	}
}
