package partnerstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository"
	helpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Audit disposition: named owning-service/Mongo cases verify cross-owner
// handoff and restart. They do not claim provider HTTP or host runtime coverage.
type workerOwningClock struct{ *mongoTestClock }

func (c workerOwningClock) NowUTC() string {
	return c.Now().Format(user.DefaultTimeFormatRFC3339NanoUTC)
}

type workerAuthority struct{ revoked atomic.Bool }

func (a *workerAuthority) CheckPartners(context.Context, string, string, string) error {
	if a.revoked.Load() {
		return partnermanager.ErrDenied
	}
	return nil
}

type workerGroups struct{}

func (workerGroups) PartnerGroupIDs(context.Context, string) ([]string, error) { return nil, nil }

type workerRegion struct{}

func (workerRegion) IsPartnerRegionEligible(context.Context, string) (bool, error) { return true, nil }

type workerReconciler struct {
	feed    *billing.RevenueService
	calls   atomic.Int64
	lostAck bool
}

func (r *workerReconciler) ReconcileRevenueSource(ctx context.Context, req billingmanager.ReconcileRevenueSourceRequest) (billing.RevenueObservation, error) {
	r.calls.Add(1)
	result, err := r.feed.ResolveQuarantinedRevenue(ctx, billing.ResolveRevenueRequest{ObservationID: req.ObservationID, ExpectedFingerprint: req.ExpectedFingerprint, Reason: req.Reason, ActorID: req.ActorID})
	if err == nil && r.lostAck {
		r.lostAck = false
		return billing.RevenueObservation{}, billing.ErrRevenueUncertain
	}
	return result, err
}

type workerFixture struct {
	store      recordstore.Store
	db         *mongo.Database
	clock      *mongoTestClock
	ctx        context.Context
	users      *user.Service
	program    *partnerprogram.Service
	referrals  *referral.Service
	earnings   *partnerearnings.Service
	revenue    *billing.RevenueService
	signer     *referral.EvidenceSigner
	identity   *partnermanager.UserIdentityAdapter
	authority  *workerAuthority
	reconciler *workerReconciler
	queueRepo  *WorkRepository
	partner    partnerprogram.Partner
}

func owningWorkerFixture(t *testing.T) *workerFixture {
	t.Helper()
	_, _, store, db, clock, ctx := mongoEarnings(t)
	f := &workerFixture{store: store, db: db, clock: clock, ctx: ctx, authority: &workerAuthority{}}
	handler, err := helpers.NewHandler(helpers.DefaultConfig(os.Getenv("GHATD_TEST_MONGO_URI"), db.Name()))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, handler.Close(cleanup))
	})
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	require.NoError(t, migrations.InitUsersSignupAttributionIndexesUp(ctx, db))
	config := user.DefaultUserConfig()
	f.users = user.NewService(user.NewRepository(repository.NewMongoDbRepositoryWithDefaults(handler, db.Name())), nil, config, &user.DefaultIDGenerator{}, workerOwningClock{clock}, &user.DefaultStringUtils{}, "")
	_, err = f.users.WithSignupAttribution(user.SignupAttributionConfig{ProgramID: partnerprogram.ProgramID, IndividualAccountTypes: []string{config.GetType(config)}})
	require.NoError(t, err)
	f.identity, err = partnermanager.NewUserIdentityAdapter(f.users, workerRegion{}, partnermanager.UserIdentityConfig{ProgramID: partnerprogram.ProgramID, IndividualAccountTypes: []string{config.GetType(config)}, ActiveAccountStatuses: []string{"ACTIVE"}})
	require.NoError(t, err)
	programRepo, err := NewProgramRepository(store)
	require.NoError(t, err)
	f.program, err = partnerprogram.NewService(programRepo, clock, randomIDs{}, programConfig())
	require.NoError(t, err)
	_, err = f.program.PublishPolicy(ctx, partnerprogram.PublishPolicyRequest{ActorID: "fixture-operator", Draft: partnerprogram.PolicyDraft{Scope: "global", RateBasisPoints: 2000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"fixture-plan"}, TermsVersion: "fixture-terms", EffectiveFrom: clock.Now().Add(-time.Hour)}})
	require.NoError(t, err)
	f.partner, err = f.program.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: "owner", AcceptedTermsVersion: "fixture-terms"})
	require.NoError(t, err)
	referralRepo, err := NewReferralRepository(store)
	require.NoError(t, err)
	f.referrals, err = referral.NewService(referralRepo, clock, randomIDs{}, 30*24*time.Hour)
	if err == nil {
		f.referrals, err = f.referrals.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	}
	require.NoError(t, err)
	earningsConfig := partnerearnings.Config{ProgramID: partnerprogram.ProgramID, Currency: "EUR"}
	earningsRepo, err := NewEarningsRepository(store, earningsConfig)
	require.NoError(t, err)
	f.earnings, err = partnerearnings.NewService(earningsRepo, clock, randomIDs{}, earningsConfig)
	require.NoError(t, err)
	revenueRepo, err := revenuestore.NewRepository(store)
	require.NoError(t, err)
	f.revenue, err = billing.NewRevenueService(revenueRepo, clock)
	require.NoError(t, err)
	f.queueRepo, err = NewWorkRepository(store)
	require.NoError(t, err)
	f.signer, err = referral.NewEvidenceSigner(referral.EvidenceConfig{ProgramID: referral.ProgramID, ActiveKeyID: "fixture", Keys: map[string][]byte{"fixture": []byte("01234567890123456789012345678901")}, Window: 30 * 24 * time.Hour}, clock)
	require.NoError(t, err)
	f.reconciler = &workerReconciler{feed: f.revenue}
	return f
}

func (f *workerFixture) queue(t *testing.T, repo partnermanager.WorkRepository) *partnermanager.WorkQueue {
	t.Helper()
	q, err := partnermanager.NewWorkQueue(repo, f.clock, partnermanager.WorkQueueConfig{ProgramID: partnerprogram.ProgramID, LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
	require.NoError(t, err)
	return q
}
func (f *workerFixture) manager(t *testing.T, paused bool) *partnermanager.Manager {
	t.Helper()
	m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: f.earnings, Identity: f.identity, Authority: f.authority, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock, Controls: partnermanager.Controls{Enrollment: !paused, Attribution: !paused, Accrual: !paused}})
	require.NoError(t, err)
	return m
}
func (f *workerFixture) worker(t *testing.T, queue *partnermanager.WorkQueue, signups partnermanager.SignupFeed, feed partnermanager.RevenueFeed, paused bool, actor string) *partnermanager.Worker {
	t.Helper()
	w, err := partnermanager.NewWorker(f.manager(t, paused), queue, signups, feed, f.reconciler, partnermanager.WorkerConfig{ActorID: actor, ConsumerID: "partner-financial-consumer", PageSize: 100, BatchSize: 100})
	require.NoError(t, err)
	return w
}
func (f *workerFixture) signup(t *testing.T, email, evidence string) string {
	t.Helper()
	created, err := f.users.CreateUser(f.ctx, &user.CreateUserRequest{Email: email, FirstName: "Fixture", LastName: "Member", GenerateUUID: true, AttributionEvidence: evidence})
	require.NoError(t, err)
	return created.User.ID
}
func (f *workerFixture) signedSignup(t *testing.T) string {
	t.Helper()
	link, err := f.referrals.IssueLink(f.ctx, referral.PartnerState{PartnerID: f.partner.ID, CustomerID: f.partner.CustomerID, CanAcquireReferrals: true})
	require.NoError(t, err)
	token, _, err := f.signer.Issue(link)
	require.NoError(t, err)
	return f.signup(t, "member@example.test", token)
}
func (f *workerFixture) paid(t *testing.T, customer string) billing.RevenueFact {
	t.Helper()
	fact := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "verified-account"}, Kind: billing.RevenuePayment, PaymentID: "paid-allocation", InvoiceID: "invoice", AllocationID: "line", PrincipalID: customer, SubscriptionID: "subscription", PlanID: "fixture-plan", CostID: "cost", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: f.clock.Now()}
	accepted, err := f.revenue.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: "verified-paid-event", Facts: []billing.RevenueFact{fact}})
	require.NoError(t, err)
	fact, err = f.revenue.GetRevenueFact(f.ctx, accepted.FactIDs[0])
	require.NoError(t, err)
	return fact
}

type workerQueueFault struct {
	partnermanager.WorkRepository
	failDecision, failComplete bool
}

func (r *workerQueueFault) RecordWorkDecision(ctx context.Context, item partnermanager.WorkItem, d partnermanager.WorkDecision, now time.Time) (partnermanager.WorkDecision, error) {
	if r.failDecision && item.Kind == partnermanager.WorkRevenue {
		r.failDecision = false
		return partnermanager.WorkDecision{}, injectedFailure
	}
	return r.WorkRepository.RecordWorkDecision(ctx, item, d, now)
}
func (r *workerQueueFault) CompleteWork(ctx context.Context, item partnermanager.WorkItem, decision string, now time.Time) error {
	if r.failComplete && item.Kind == partnermanager.WorkRevenue {
		r.failComplete = false
		return injectedFailure
	}
	return r.WorkRepository.CompleteWork(ctx, item, decision, now)
}

type workerRevenueFault struct {
	*billing.RevenueService
	lostAck bool
}

type workerSignupFault struct {
	*user.Service
	lostAck bool
}

func (f *workerSignupFault) ConsumeSignupAttribution(ctx context.Context, customer string, consumption user.SignupConsumption) error {
	err := f.Service.ConsumeSignupAttribution(ctx, customer, consumption)
	if err == nil && f.lostAck {
		f.lostAck = false
		return user.ErrSignupEvidenceUnavailable
	}
	return err
}

func (f *workerRevenueFault) AcknowledgeRevenueFact(ctx context.Context, ack billing.RevenueAcknowledgement) error {
	err := f.RevenueService.AcknowledgeRevenueFact(ctx, ack)
	if err == nil && f.lostAck {
		f.lostAck = false
		return billing.ErrRevenueUncertain
	}
	return err
}

func TestMongoWorkerFinancialHandoffRecovery(t *testing.T) {
	cases := []struct {
		name                                         string
		failDecision, lostAck, failComplete, revoked bool
	}{{name: "financial_commit_before_queue_decision_failure", failDecision: true}, {name: "owning_ack_committed_but_reply_lost", lostAck: true}, {name: "owning_ack_before_completion_failure", failComplete: true}, {name: "revoked_worker_cannot_replay_committed_decision", lostAck: true, revoked: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			customer := f.signedSignup(t)
			fact := f.paid(t, customer)
			fault := &workerQueueFault{WorkRepository: f.queueRepo, failDecision: tc.failDecision, failComplete: tc.failComplete}
			feed := &workerRevenueFault{RevenueService: f.revenue, lostAck: tc.lostAck}
			queue := f.queue(t, fault)
			first := f.worker(t, queue, f.users, feed, false, "original-worker")
			report, err := first.RunOnce(f.ctx)
			require.Error(t, err)
			require.Equal(t, 1, report.Retried, "run error: %v; report: %+v", err, report)
			job, err := queue.Get(f.ctx, partnermanager.WorkRevenue, fact.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkPending, job.State)
			if tc.failDecision {
				require.Nil(t, job.Decision)
				_, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "partner-financial-consumer", fact.ID)
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
			} else {
				require.NotNil(t, job.Decision)
			}
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Len(t, journal, 1)
			require.EqualValues(t, 2000, journal[0].AmountMinor)
			f.clock.now = f.clock.now.Add(2 * time.Second)
			f.authority.revoked.Store(tc.revoked)
			// New process/current actor and paused admission recover old financial
			// receipts without using the stored original actor as permission.
			restarted := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, true, "replacement-worker")
			_, err = restarted.RunOnce(f.ctx)
			if tc.revoked {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				still, err := queue.Get(f.ctx, job.Kind, fact.ID)
				require.NoError(t, err)
				require.Equal(t, job, still)
				return
			}
			require.NoError(t, err)
			done, err := queue.Get(f.ctx, job.Kind, fact.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			ack, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "partner-financial-consumer", fact.ID)
			require.NoError(t, err)
			require.Equal(t, done.Decision.ID, ack.AcceptanceID)
			require.Equal(t, partnermanager.WorkAccepted, ack.Outcome)
			unchanged, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Equal(t, journal, unchanged)
		})
	}
}

func TestMongoWorkerConclusiveNoAttributionAndPendingEvidence(t *testing.T) {
	cases := []struct {
		name, evidence string
		pending        bool
	}{{name: "owning_no_cookie_decision_is_no_entitlement"}, {name: "invalid_signature_is_retained_for_review", evidence: "invalid-signature", pending: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			customer := f.signup(t, "member@example.test", tc.evidence)
			fact := f.paid(t, customer)
			queue := f.queue(t, f.queueRepo)
			_, err := f.worker(t, queue, f.users, f.revenue, false, "worker").RunOnce(f.ctx)
			capture, readErr := f.users.GetSignupAttribution(f.ctx, customer)
			require.NoError(t, readErr)
			if tc.pending {
				require.Error(t, err)
				require.Equal(t, "pending", capture.State)
				_, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "partner-financial-consumer", fact.ID)
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
				job, err := queue.Get(f.ctx, partnermanager.WorkRevenue, fact.ID)
				require.NoError(t, err)
				require.Nil(t, job.Decision)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "consumed", capture.State)
			require.Equal(t, "no_evidence", capture.Consumption.Outcome)
			ack, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "partner-financial-consumer", fact.ID)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkNoEntitlement, ack.Outcome)
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Empty(t, journal)
		})
	}
}

func TestMongoWorkerSourceResolutionRecovery(t *testing.T) {
	// This single recovery sequence crosses billing's immutable source receipt
	// and the worker decision; a reply lost after resolution must not call the
	// provider adapter again, including from a newly authorized worker actor.
	f := owningWorkerFixture(t)
	source, err := f.revenue.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, EnvelopeID: "source-event", SourceFingerprint: "original-source-digest", QuarantineReason: "no_subscription_revenue"})
	require.NoError(t, err)
	queue := f.queue(t, f.queueRepo)
	f.reconciler.lostAck = true
	_, err = f.worker(t, queue, f.users, f.revenue, false, "previous-worker").RunOnce(f.ctx)
	require.ErrorIs(t, err, billing.ErrRevenueUncertain)
	resolution, err := f.revenue.GetRevenueSourceResolution(f.ctx, source.ID)
	require.NoError(t, err)
	// The source has left the owning pending feed, but its previously retained
	// job survives that transition and can recover without another provider call.
	f.clock.now = f.clock.now.Add(2 * time.Second)
	_, err = f.worker(t, queue, f.users, f.revenue, true, "replacement-worker").RunOnce(f.ctx)
	require.NoError(t, err)
	done, err := queue.Get(f.ctx, partnermanager.WorkRevenueSource, source.ID)
	require.NoError(t, err)
	require.Equal(t, partnermanager.WorkComplete, done.State)
	require.Equal(t, resolution.ID, done.Decision.AcceptanceID)
	require.EqualValues(t, 1, f.reconciler.calls.Load())
}

func TestMongoWorkerSignupConsumptionRecovery(t *testing.T) {
	cases := []struct {
		name   string
		signed bool
	}{
		{name: "signed_referral_is_not_locked_twice", signed: true},
		{name: "no_cookie_receipt_recovers_without_attribution"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			var customer string
			if tc.signed {
				customer = f.signedSignup(t)
			} else {
				customer = f.signup(t, "member@example.test", "")
			}
			queue := f.queue(t, f.queueRepo)
			fault := &workerSignupFault{Service: f.users, lostAck: true}
			_, err := f.worker(t, queue, fault, f.revenue, false, "original-worker").RunOnce(f.ctx)
			require.ErrorIs(t, err, user.ErrSignupEvidenceUnavailable)
			capture, err := f.users.GetSignupAttribution(f.ctx, customer)
			require.NoError(t, err)
			require.Equal(t, "consumed", capture.State)
			job, err := queue.Get(f.ctx, partnermanager.WorkSignup, customer)
			require.NoError(t, err)
			require.NotNil(t, job.Decision)
			require.Equal(t, job.Decision.ID, capture.Consumption.ReceiptID)
			f.clock.now = f.clock.now.Add(2 * time.Second)
			_, err = f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, true, "replacement-worker").RunOnce(f.ctx)
			require.NoError(t, err)
			done, err := queue.Get(f.ctx, job.Kind, customer)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			unchanged, err := f.users.GetSignupAttribution(f.ctx, customer)
			require.NoError(t, err)
			require.Equal(t, capture, unchanged)
			history, err := f.referrals.History(f.ctx, customer)
			require.NoError(t, err)
			if tc.signed {
				require.Len(t, history, 1)
			} else {
				require.Empty(t, history)
			}
		})
	}
}

func TestMongoCompetingWorkersKeepOneFinancialAcceptance(t *testing.T) {
	// A single overlapping worker race is the behavior: both discovery and
	// owning-service timing can differ while one immutable economic acceptance
	// survives. Any unresolved dependency is retried after workers have joined.
	f := owningWorkerFixture(t)
	customer := f.signedSignup(t)
	fact := f.paid(t, customer)
	start := make(chan struct{})
	errorsOut := make(chan error, 4)
	for i := 0; i < 4; i++ {
		worker := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, false, "worker")
		go func() { <-start; _, err := worker.RunOnce(f.ctx); errorsOut <- err }()
	}
	close(start)
	for i := 0; i < 4; i++ {
		err := <-errorsOut
		if err != nil {
			require.True(t, errors.Is(err, partnerearnings.ErrUnresolved) || errors.Is(err, partnermanager.ErrWorkConflict), "unexpected worker error: %v", err)
		}
	}
	f.clock.now = f.clock.now.Add(2 * time.Minute)
	_, err := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, false, "worker").RunOnce(f.ctx)
	require.NoError(t, err)
	journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
	require.NoError(t, err)
	require.Len(t, journal, 1)
	require.EqualValues(t, 2000, journal[0].AmountMinor)
	history, err := f.referrals.History(f.ctx, customer)
	require.NoError(t, err)
	require.Len(t, history, 1)
	ack, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "partner-financial-consumer", fact.ID)
	require.NoError(t, err)
	require.Equal(t, partnermanager.WorkAccepted, ack.Outcome)
}

func TestMongoWorkerPendingPrefixCannotHideLaterSources(t *testing.T) {
	cases := []struct{ name, kind string }{
		{name: "two_hundred_unresolved_signups_then_new_no_cookie_signup", kind: partnermanager.WorkSignup},
		{name: "two_hundred_unresolved_payments_then_proven_no_entitlement", kind: partnermanager.WorkRevenue},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			queue := f.queue(t, f.queueRepo)
			var last string
			if tc.kind == partnermanager.WorkSignup {
				// Valid owning document fixtures isolate feed/worker behavior;
				// password creation and immutable capture are exercised above.
				rows := make([]any, 0, 201)
				for i := 0; i < 201; i++ {
					id := fmt.Sprintf("customer_%04d", i)
					capture := user.SignupAttribution{ProgramID: partnerprogram.ProgramID, CustomerID: id, CreatedAt: f.clock.Now(), CreatedAtUTC: f.clock.Now().Format(time.RFC3339Nano), Individual: true, Evidence: "invalid-signature", State: "pending"}
					if i == 200 {
						capture.Evidence = ""
						last = id
					}
					rows = append(rows, bson.M{"_id": id, "email": id + "@example.test", "signup_attribution": capture})
				}
				_, err := f.db.Collection("users").InsertMany(f.ctx, rows)
				require.NoError(t, err)
			} else {
				customer := f.signup(t, "later@example.test", "")
				for i := 0; i < 201; i++ {
					payer := "unresolved-payer"
					if i == 200 {
						payer = customer
					}
					fact := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "fixture", AccountID: "account"}, Kind: billing.RevenuePayment, PaymentID: fmt.Sprintf("payment_%04d", i), InvoiceID: "invoice", AllocationID: "line", PrincipalID: payer, SubscriptionID: "subscription", PlanID: "fixture-plan", CostID: "cost", Currency: "EUR", CurrencyExponent: 2, PaidMinor: 10000, EffectiveAt: f.clock.Now()}
					receipt, err := f.revenue.AcceptVerified(f.ctx, billing.VerifiedRevenueRequest{Scope: fact.Scope, EnvelopeID: fmt.Sprintf("event_%04d", i), Facts: []billing.RevenueFact{fact}})
					require.NoError(t, err)
					if i == 200 {
						last = receipt.FactIDs[0]
					}
				}
			}
			for i := 0; i < 2; i++ {
				// Each run is a fresh process composition over the persisted cursor.
				_, err := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, false, "worker").RunOnce(f.ctx)
				require.Error(t, err)
			}
			_, err := f.worker(t, f.queue(t, f.queueRepo), f.users, f.revenue, false, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			done, err := queue.Get(f.ctx, tc.kind, last)
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			require.Equal(t, partnermanager.WorkNoEntitlement, done.Decision.Outcome)
			if tc.kind == partnermanager.WorkSignup {
				pending, err := f.users.PendingSignupAttributionsAfter(f.ctx, "", 200)
				require.NoError(t, err)
				require.Len(t, pending, 200)
				require.Equal(t, "customer_0000", pending[0].CustomerID)
			} else {
				pending, err := f.revenue.PendingRevenueFactsAfter(f.ctx, "partner-financial-consumer", 0, 200)
				require.NoError(t, err)
				require.Len(t, pending, 200)
				other, err := f.revenue.GetRevenueAcknowledgement(f.ctx, "independent-consumer", last)
				require.ErrorIs(t, err, billing.ErrRevenueNotFound)
				require.Empty(t, other.AcceptanceID)
			}
		})
	}
}

// Audit disposition for this addition: independent named cases verify an empty
// owning sweep resets its durable cursor, preserving newly committed sources
// which sort behind that cursor. Existing unrelated tests still need full audit.
func TestMongoWorkerCompletedSignupSweepResetsCursor(t *testing.T) {
	for _, tc := range []struct {
		name              string
		insertBeforeReset bool
	}{
		{"new_source_committed_behind_cursor_before_empty_sweep", true},
		{"new_source_committed_after_empty_sweep_reset", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			queue := f.queue(t, f.queueRepo)
			insert := func(id string) {
				capture := user.SignupAttribution{ProgramID: partnerprogram.ProgramID, CustomerID: id, CreatedAt: f.clock.Now(), CreatedAtUTC: f.clock.Now().Format(time.RFC3339Nano), Individual: true, State: "pending"}
				_, err := f.db.Collection("users").InsertOne(f.ctx, bson.M{"_id": id, "email": id + "@example.test", "signup_attribution": capture})
				require.NoError(t, err)
			}
			insert("customer_zz_last")
			_, err := f.worker(t, queue, f.users, f.revenue, false, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			cursor, err := queue.Cursor(f.ctx, partnermanager.WorkSignup)
			require.NoError(t, err)
			require.Equal(t, "customer_zz_last", cursor.AfterID)
			require.EqualValues(t, 1, cursor.Revision)
			if tc.insertBeforeReset {
				insert("customer_aa_new")
			}
			_, err = f.worker(t, queue, f.users, f.revenue, false, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			reset, err := queue.Cursor(f.ctx, partnermanager.WorkSignup)
			require.NoError(t, err)
			require.Empty(t, reset.AfterID)
			require.EqualValues(t, 2, reset.Revision)
			if !tc.insertBeforeReset {
				insert("customer_aa_new")
			}
			_, err = f.worker(t, queue, f.users, f.revenue, false, "worker").RunOnce(f.ctx)
			require.NoError(t, err)
			done, err := queue.Get(f.ctx, partnermanager.WorkSignup, "customer_aa_new")
			require.NoError(t, err)
			require.Equal(t, partnermanager.WorkComplete, done.State)
			require.NotNil(t, done.Decision)
			require.Equal(t, partnermanager.WorkNoEntitlement, done.Decision.Outcome)
			final, err := queue.Cursor(f.ctx, partnermanager.WorkSignup)
			require.NoError(t, err)
			require.Equal(t, "customer_aa_new", final.AfterID)
			require.EqualValues(t, 3, final.Revision)
			journal, err := f.earnings.ListJournal(f.ctx, f.partner.ID)
			require.NoError(t, err)
			require.Empty(t, journal, "discovering a no-evidence signup must not manufacture commission")
		})
	}
}
