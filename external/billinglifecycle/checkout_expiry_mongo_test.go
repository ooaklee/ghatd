package billinglifecycle

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Actual authenticated local Stripe GETs, owning original joins, current worker
// grants and encrypted host/native transactions. These fixtures never perform
// a purchase and do not attest a production deployment.
func TestCheckoutDispositionEncryptedProviderStates(t *testing.T) {
	for _, tc := range []struct {
		name, status, payment    string
		absent, pending, expired bool
		want                     error
	}{
		{name: "owned_expired_unpaid_is_terminal", status: "expired", payment: "unpaid", absent: true, expired: true},
		{name: "owned_open_unpaid_remains_pending", status: "open", payment: "unpaid", absent: true, pending: true},
		{name: "complete_paid_keeps_capture_path", status: "complete", payment: "paid"},
		{name: "complete_trial_never_claims_payment", status: "complete", payment: "no_payment_required"},
		{name: "expired_with_subscription_refused", status: "expired", payment: "unpaid", want: billing.ErrRevenueUnassessable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeCheckoutHTTPFixture(t, tc.status, tc.payment, tc.absent, false)
			completion, err := NewCheckoutCompletion(f.execution, f.repo)
			require.NoError(t, err)
			out, err := completion.runDisposition(f.ctx, JobLease(f.job))
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, f.writes.Load())
			job, readErr := f.repo.ReadJob(f.ctx, f.job.Source)
			require.NoError(t, readErr)
			if tc.want != nil {
				require.Equal(t, ColdLane, job.Lane)
				require.Empty(t, job.LastCompletionID)
				require.NotEmpty(t, job.LeaseToken)
			} else {
				require.Equal(t, tc.pending, out.Pending)
				require.Equal(t, tc.expired, out.Expired)
				require.Empty(t, job.LeaseToken)
				if tc.pending {
					require.Equal(t, ColdLane, job.Lane)
					require.True(t, job.NextAttemptAt.After(f.clock.Now()))
					require.Empty(t, job.LastCompletionID)
					input, err := f.outbox.Find(f.ctx, "worker-original", f.f.intent)
					require.NoError(t, err)
					require.Nil(t, input.Evidence)
				} else {
					require.Equal(t, RetiredLane, job.Lane)
					require.NotEmpty(t, job.LastCompletionID)
					reads := f.calls.Load()
					recovered, err := completion.InspectLast(f.ctx, f.job.Source)
					require.NoError(t, err)
					require.Equal(t, tc.expired, recovered.Expiry != nil)
					require.Equal(t, reads, f.calls.Load(), "recovery must not repeat provider lookup")
				}
			}
			if tc.expired {
				// Existing captured APIs and retained-evidence writers fail closed;
				// expiry is never decoded as a successful subscription anchor.
				_, err := f.f.native.FindCheckoutLifecycleReceipt(f.ctx, f.f.intent)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				_, err = f.f.native.CaptureCheckoutLifecycleEvidence(f.ctx, f.f.intent, f.f.provider.evidence)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				_, err = f.outbox.RetainEvidence(f.ctx, "worker-original", f.f.intent, f.f.provider.evidence)
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				scan, err := f.execution.scheduler.Scan(f.ctx, f.f.intent.Scope, ColdLane)
				require.NoError(t, err)
				require.Empty(t, scan.Jobs, "unchanged job scan excludes retired checkout")
			}
			for _, kind := range []string{"billing_checkout_lifecycle_anchor", "billing_revenue_fact", "billing_checkout_association", "partners_lifecycle_completion"} {
				count, err := f.f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				want := int64(0)
				if tc.want == nil && !tc.pending && (kind == "partners_lifecycle_completion" || (kind == "billing_checkout_lifecycle_anchor" && !tc.expired)) {
					want = 1
				}
				require.Equal(t, want, count, kind)
			}
		})
	}
}

func nativeExpiryObservation(t *testing.T, supplied ...*inputNativeFixture) (checkoutHTTPFixture, *CheckoutCompletion, CheckoutExpiryObservation) {
	t.Helper()
	f := nativeCheckoutHTTPFixture(t, "expired", "unpaid", true, false, supplied...)
	prepared, captured, err := f.execution.prepareObservation(f.ctx, JobLease(f.job))
	require.NoError(t, err)
	require.False(t, captured)
	status, err := f.execution.owner.(checkoutStatusOwner).LookupCheckoutLifecycleStatus(f.ctx, "worker-original", f.f.intent)
	require.NoError(t, err)
	proof, err := billing.NewCheckoutLifecycleExpiry(f.f.intent, status)
	require.NoError(t, err)
	completion, err := NewCheckoutCompletion(f.execution, f.repo)
	require.NoError(t, err)
	return f, completion, CheckoutExpiryObservation{prepared.Job, prepared.Input, proof}
}

func TestCheckoutExpiryEncryptedFencesAndRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, missingKind string
		committed         bool
		want              error
	}{
		{name: "wrong_revision", want: recordstore.ErrConflict},
		{name: "wrong_fence", want: recordstore.ErrConflict},
		{name: "wrong_actor", want: billing.ErrRevenueInvalid},
		{name: "wrong_token", want: recordstore.ErrConflict},
		{name: "expired_lease", want: recordstore.ErrConflict},
		{name: "future_proof", want: recordstore.ErrConflict},
		{name: "clock_moves_backwards_before_write", want: recordstore.ErrConflict},
		{name: "missing_original", missingKind: "billing_checkout_intent", want: billing.ErrRevenueUnavailable},
		{name: "missing_forward", missingKind: "billing_checkout_acknowledgement", want: billing.ErrRevenueUnavailable},
		{name: "missing_reverse", missingKind: "billing_checkout_session", want: billing.ErrRevenueUnavailable},
		{name: "missing_retained_original", missingKind: checkoutKind, want: recordstore.ErrUnavailable},
		{name: "captured_outcome_wins", want: billing.ErrRevenueConflict},
		{name: "retained_complete_evidence_wins", want: recordstore.ErrConflict},
		{name: "revoked_before_write", want: partnermanager.ErrDenied},
		{name: "cancelled_before_write", want: context.Canceled},
		{name: "lost_commit_reply", committed: true, want: recordstore.ErrUncertain},
		{name: "revoked_after_commit", committed: true, want: partnermanager.ErrDenied},
		{name: "cancelled_after_commit", committed: true, want: context.Canceled},
		{name: "lost_reply_and_revoked", committed: true, want: recordstore.ErrUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, completion, o := nativeExpiryObservation(t)
			ctx := f.ctx
			if tc.missingKind != "" {
				_, err := f.f.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": tc.missingKind})
				require.NoError(t, err)
			}
			switch tc.name {
			case "wrong_revision":
				o.Job.Revision++
			case "wrong_fence":
				o.Job.Fence++
			case "wrong_actor":
				o.Job.LeaseActor = "other"
			case "wrong_token":
				o.Job.LeaseToken = "other"
			case "expired_lease":
				f.clock.set(o.Job.LeasedUntil)
			case "future_proof":
				s := o.Expiry.Status
				s.ObservedAt = f.clock.Now().Add(time.Second)
				var err error
				o.Expiry, err = billing.NewCheckoutLifecycleExpiry(o.Input.Intent, s)
				require.NoError(t, err)
			case "clock_moves_backwards_before_write":
				f.clock.sequence = []time.Time{f.clock.at, f.clock.at.Add(-time.Nanosecond)}
			case "captured_outcome_wins":
				_, err := f.f.native.CaptureCheckoutLifecycleEvidence(ctx, f.f.intent, f.f.provider.evidence)
				require.NoError(t, err)
			case "retained_complete_evidence_wins":
				_, err := f.outbox.RetainEvidence(ctx, "worker-original", f.f.intent, f.f.provider.evidence)
				require.NoError(t, err)
			case "revoked_before_write":
				f.f.revoke(t)
			case "cancelled_before_write":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			case "lost_commit_reply":
				f.f.reply.lose = true
			case "revoked_after_commit", "lost_reply_and_revoked":
				f.f.reply.lose = tc.name == "lost_reply_and_revoked"
				f.f.reply.after = func() { f.f.revoke(t) }
			case "cancelled_after_commit":
				cancelled, cancel := context.WithCancel(ctx)
				t.Cleanup(cancel)
				ctx = cancelled
				f.f.reply.after = cancel
			}
			var out CompletedExecution
			var err error
			if tc.name == "clock_moves_backwards_before_write" {
				out, err = f.repo.ExpireCheckout(ctx, o)
			} else {
				out, err = completion.completeExpiry(ctx, f.repo, o)
			}
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, out)
			if tc.name == "lost_reply_and_revoked" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			f.f.reply.lose, f.f.reply.after = false, nil
			if tc.name == "revoked_after_commit" || tc.name == "lost_reply_and_revoked" {
				f.f.restore(t)
			}
			count, err := f.f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": completionKind})
			require.NoError(t, err)
			if tc.committed {
				require.EqualValues(t, 1, count)
				calls := f.calls.Load()
				recovered, err := completion.InspectLast(f.ctx, o.Job.Source)
				require.NoError(t, err)
				require.NotNil(t, recovered.Expiry)
				require.Equal(t, o.Expiry.Fingerprint, recovered.Expiry.Fingerprint)
				require.Equal(t, calls, f.calls.Load())
				_, err = f.repo.ExpireCheckout(f.ctx, o)
				require.ErrorIs(t, err, recordstore.ErrConflict, "consumed lease cannot replay writes")
			} else {
				require.Zero(t, count)
			}
			facts, err := f.f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, err)
			require.Zero(t, facts)
			require.Zero(t, f.writes.Load())
		})
	}
}

func TestCheckoutExpiryEncryptedCompetingWriters(t *testing.T) {
	for _, competing := range []string{"native_capture", "standalone_retained_evidence"} {
		t.Run(competing, func(t *testing.T) {
			f, completion, o := nativeExpiryObservation(t)
			start := make(chan struct{})
			var expiryErr, otherErr error
			var wg sync.WaitGroup
			wg.Add(2)
			go func() { defer wg.Done(); <-start; _, expiryErr = f.repo.ExpireCheckout(f.ctx, o) }()
			go func() {
				defer wg.Done()
				<-start
				if competing == "native_capture" {
					_, otherErr = f.f.native.CaptureCheckoutLifecycleEvidence(f.ctx, f.f.intent, f.f.provider.evidence)
				} else {
					_, otherErr = f.outbox.RetainEvidence(f.ctx, "worker-original", f.f.intent, f.f.provider.evidence)
				}
			}()
			close(start)
			wg.Wait()
			require.NotEqual(t, expiryErr == nil, otherErr == nil, "exactly one competing outcome may win")
			if expiryErr == nil {
				recovered, err := completion.InspectLast(f.ctx, o.Job.Source)
				require.NoError(t, err)
				require.NotNil(t, recovered.Expiry)
				anchors, err := f.f.db.Collection("ghatd_owned_records").CountDocuments(f.ctx, bson.M{"kind": "billing_checkout_lifecycle_anchor"})
				require.NoError(t, err)
				require.Zero(t, anchors)
			} else {
				// The genuine complete outcome remains recoverable, even though a
				// contradictory expiry attempt competed with it in another transaction.
				observed, err := f.execution.Observe(f.ctx, JobLease(o.Job))
				require.NoError(t, err)
				out, err := completion.Complete(f.ctx, observed)
				require.NoError(t, err)
				require.Nil(t, out.Expiry)
			}
		})
	}
}
