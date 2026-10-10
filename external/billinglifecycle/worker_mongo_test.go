package billinglifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Actual owning readiness/discovery/current grants, encrypted scheduling/input/
// receipt/history transactions and bounded worker orchestration. Provider and
// API-service identity are controlled; these are not host runtime/platform E2E.
func nativeLifecycleWorker(t *testing.T, f *statusNativeFixture, r *RecordExecutionRepository, clock *executionTestClock, x *StatusExecution) (*Worker, context.Context) {
	t.Helper()
	m := x.owner.(*billingmanager.Service)
	a := x.scheduler.authority
	grant := f.base.grant
	grant.Permissions = append(grant.Permissions, billingmanager.LifecycleDiscovery)
	var err error
	f.base.grant, err = f.base.policy.ReplaceGrant(f.base.ctx, grant, grant.Revision)
	require.NoError(t, err)
	ready := false
	for n := 0; n < 30; n++ {
		progress, e := f.owner.PrepareLifecycleDiscovery(f.base.ctx, f.p.Scope, 200)
		require.NoError(t, e)
		if progress.State.Phase == billing.LifecyclePreparationComplete {
			ready = true
			break
		}
	}
	require.True(t, ready, "fixture explicitly prepares owning discovery before worker construction")
	_, err = m.WithLifecycleDiscoveryAuthority(a.(billingmanager.LifecycleDiscoveryAuthority))
	require.NoError(t, err)
	_, err = m.WithCheckoutLifecycleAuthority(a.(billingmanager.CheckoutLifecycleAuthority))
	require.NoError(t, err)
	ctx, err := a.(interface {
		Bind(context.Context) (context.Context, error)
	}).Bind(f.base.ctx)
	require.NoError(t, err)
	d, err := NewDiscoveryCollector(r, m, a.(billingmanager.LifecycleDiscoveryAuthority), clock, DiscoveryConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{f.p.Scope}, PageLimit: 3})
	require.NoError(t, err)
	box, err := NewCheckoutOutbox(f.base.reply, m)
	require.NoError(t, err)
	c, err := NewCheckoutExecution(x.scheduler, r, box)
	require.NoError(t, err)
	cc, err := NewCheckoutCompletion(c, r)
	require.NoError(t, err)
	sc, err := NewStatusCompletion(x, r, 30*time.Second)
	require.NoError(t, err)
	resolver, err := NewStatusOriginalResolver(x, r)
	require.NoError(t, err)
	w, err := NewWorker(d, x.scheduler, c, x, cc, sc, resolver)
	require.NoError(t, err)
	return w, ctx
}

func TestLifecycleWorkerRetainedOriginalFlows(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		evidence    bool
		wantGets    int
	}{
		{"pending_preparation_progresses", billing.SubscriptionStatusPending, false, 1},
		{"pending_retained_evidence_no_get", billing.SubscriptionStatusPending, true, 0},
		{"captured_receipt_no_get", billing.SubscriptionStatusCaptured, true, 0},
		{"superseded_then_fresh_observation", billing.SubscriptionStatusSuperseded, true, 1},
		{"missing_bound_input_no_repair", billing.SubscriptionStatusPending, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, resolver, _, j, input := nativeOriginalResolverFixture(t, tc.state, tc.evidence)
			w, ctx := nativeLifecycleWorker(t, f, r, clock, resolver.execution)
			released, err := r.Release(ctx, LeaseDisposition{Handle: JobLease(j), NextAttemptAt: clock.at.Add(time.Second), Lane: j.Lane})
			require.NoError(t, err)
			clock.set(released.NextAttemptAt)
			if tc.name == "missing_bound_input_no_repair" {
				id, _ := statusIdentity(input.Preparation)
				_, err = f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusKind, "id": id})
				require.NoError(t, err)
			}
			gets := f.provider.calls
			report, err := w.RunOnce(ctx)
			current, e := r.ReadJob(ctx, j.Source)
			require.NoError(t, e)
			if tc.name == "missing_bound_input_no_repair" {
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				require.Equal(t, WorkerReport{}, report)
				require.NotNil(t, current.OriginalStatus)
				require.Equal(t, input.Preparation.CaptureID, current.OriginalStatus.CaptureID)
			} else {
				require.NoError(t, err)
				require.Nil(t, current.OriginalStatus)
				require.Equal(t, RefreshLane, current.Lane)
				require.NotEmpty(t, current.LastCompletionID)
				require.Empty(t, current.LeaseToken)
				require.Zero(t, current.Attempts)
				require.Equal(t, 1, report.Completed, "cold budget allows only one of two source kinds")
				if tc.state == billing.SubscriptionStatusSuperseded {
					require.NotEmpty(t, current.LastSupersessionID)
					require.Equal(t, 1, report.Superseded)
				}
			}
			require.Equal(t, gets+tc.wantGets, f.provider.calls)
			// Existing financial/payment fact count is unchanged by lifecycle work.
			count, e := f.base.db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, e)
			require.Zero(t, count)
		})
	}
}

func TestLifecycleWorkerUnknownStopsAndRecovers(t *testing.T) {
	for _, stage := range []string{"discovery", "supersession", "completion"} {
		t.Run(stage, func(t *testing.T) {
			state := billing.SubscriptionStatusCaptured
			if stage == "supersession" {
				state = billing.SubscriptionStatusSuperseded
			}
			f, r, clock, s, _, j, _ := nativeOriginalResolverFixture(t, state, true)
			w, ctx := nativeLifecycleWorker(t, f, r, clock, s.execution)
			released, err := r.Release(ctx, LeaseDisposition{Handle: JobLease(j), NextAttemptAt: clock.at.Add(time.Second), Lane: j.Lane})
			require.NoError(t, err)
			clock.set(released.NextAttemptAt)
			switch stage {
			case "discovery":
				f.base.reply.lose = true
			case "supersession":
				w.resolver.repo = supersessionReplyBoundary{StatusSupersessionRepository: r, store: f.base.reply, lose: true}
			case "completion":
				w.statusCompletion.repo = completionReplyBoundary{CompletionRepository: r, store: f.base.reply, lose: true}
			}
			gets := f.provider.calls
			report, err := w.RunOnce(ctx)
			require.ErrorIs(t, err, recordstore.ErrUncertain)
			require.Equal(t, WorkerReport{}, report)
			require.Equal(t, gets, f.provider.calls, "unknown stops before fresh lookup")
			f.base.reply.lose = false
			w.resolver.repo = r
			w.statusCompletion.repo = r
			current, e := r.ReadJob(ctx, j.Source)
			require.NoError(t, e)
			if stage == "supersession" {
				require.Nil(t, current.OriginalStatus)
				require.NotEmpty(t, current.LastSupersessionID)
				_, e = w.resolver.InspectLast(ctx, j.Source)
				require.NoError(t, e)
			}
			if stage == "completion" {
				require.Nil(t, current.OriginalStatus)
				require.NotEmpty(t, current.LastCompletionID)
				_, e = w.statusCompletion.InspectLast(ctx, j.Source)
				require.NoError(t, e)
			}
			// New pass reacquires only after expiry and reads persisted discovery
			// continuation. An unknown acknowledgement was never rollback evidence.
			clock.set(clock.at.Add(time.Minute))
			_, e = w.RunOnce(ctx)
			require.NoError(t, e)
		})
	}
}

func TestLifecycleWorkerCurrentAdmissionAndOverlap(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"denied", partnermanager.ErrDenied}, {"canceled", context.Canceled}, {"overlap", recordstore.ErrConflict}, {"wrong_instance_context", partnermanager.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, s, _, _, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusPending, false)
			w, ctx := nativeLifecycleWorker(t, f, r, clock, s.execution)
			switch tc.name {
			case "denied":
				f.base.revoke(t)
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "overlap":
				w.mu.Lock()
				t.Cleanup(w.mu.Unlock)
			case "wrong_instance_context":
				ctx = f.ctx
			}
			gets := f.provider.calls
			report, err := w.RunOnce(ctx)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, WorkerReport{}, report)
			require.Equal(t, gets, f.provider.calls)
		})
	}
}

// The provider read is a controlled fault; every binder, native preparation,
// authority check and current-job inspection still uses the real configured owners.
type workerFailureProvider struct {
	*statusNativeProvider
	err   error
	after func()
}

func (p *workerFailureProvider) LookupRevenueSubscription(ctx context.Context, scope paymentprovider.RevenueScope, id string) (paymentprovider.RevenueSubscriptionEvidence, error) {
	_, err := p.statusNativeProvider.LookupRevenueSubscription(ctx, scope, id)
	if err != nil {
		return paymentprovider.RevenueSubscriptionEvidence{}, err
	}
	if p.after != nil {
		p.after()
	}
	return paymentprovider.RevenueSubscriptionEvidence{}, p.err
}

type workerFailureRegistry struct {
	p paymentprovider.RevenueProvider
}

func (r workerFailureRegistry) GetRevenueProvider(string) (paymentprovider.RevenueProvider, error) {
	return r.p, nil
}

func TestLifecycleWorkerKnownFailureBackoff(t *testing.T) {
	for _, name := range []string{"retained_original", "fresh_binding_revision", "late_revocation", "late_expiry"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, s, _, j, input := nativeOriginalResolverFixture(t, billing.SubscriptionStatusPending, false)
			w, ctx := nativeLifecycleWorker(t, f, r, clock, s.execution)
			if name == "fresh_binding_revision" {
				// Separate fixture job revision transition is deliberately private; model a
				// new unbound cycle through a valid job codec, not a forged native original.
				changed := j
				changed.Revision++
				changed.OriginalStatus = nil
				row, e := jobRecord(changed)
				require.NoError(t, e)
				require.NoError(t, f.base.reply.Transact(ctx, "fixture-unbound-cycle", func(tx recordstore.Tx) error { return tx.Replace(ctx, row, j.Revision) }))
				j = changed
			}
			released, err := r.Release(ctx, LeaseDisposition{Handle: JobLease(j), NextAttemptAt: clock.at.Add(time.Second), Lane: j.Lane})
			require.NoError(t, err)
			clock.set(released.NextAttemptAt)
			outage := errors.New("controlled provider status outage")
			provider := &workerFailureProvider{statusNativeProvider: f.provider, err: outage}
			if name == "late_revocation" {
				provider.after = func() { f.base.revoke(t) }
			}
			if name == "late_expiry" {
				provider.after = func() { clock.set(clock.at.Add(time.Minute)) }
			}
			m := w.status.owner.(*billingmanager.Service)
			_, err = m.WithRevenueServices(workerFailureRegistry{provider}, f.owner, f.base.native)
			require.NoError(t, err)
			gets := f.provider.calls
			report, err := w.RunOnce(ctx)
			require.Error(t, err)
			require.Equal(t, WorkerReport{}, report)
			require.Equal(t, gets+1, f.provider.calls)
			current, e := r.ReadJob(context.WithoutCancel(ctx), j.Source)
			require.NoError(t, e)
			require.NotNil(t, current.OriginalStatus)
			require.Equal(t, input.Preparation.CaptureID, current.OriginalStatus.CaptureID)
			require.Equal(t, j.Attempts+1, current.Attempts, "failed observation does not reset execution credit")
			switch name {
			case "late_revocation":
				require.ErrorIs(t, err, partnermanager.ErrDenied)
				require.NotEmpty(t, current.LeaseToken)
			case "late_expiry":
				require.NotEmpty(t, current.LeaseToken)
			default:
				require.ErrorIs(t, err, outage)
				require.Empty(t, current.LeaseToken, "known failure uses current inspected revision for backoff")
				require.True(t, current.NextAttemptAt.After(clock.at))
			}
		})
	}
}

func TestLifecycleWorkerConstructorConservation(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"valid", nil}, {"nil_resolver", billing.ErrRevenueUnavailable}, {"nil_discovery_repo", billing.ErrRevenueUnavailable},
		{"different_manager", billing.ErrRevenueInvalid}, {"different_clock", billing.ErrRevenueInvalid},
		{"different_scheduler", billing.ErrRevenueInvalid}, {"different_actor", billing.ErrRevenueInvalid}, {"different_scope", billing.ErrRevenueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, s, _, _, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusPending, false)
			w, _ := nativeLifecycleWorker(t, f, r, clock, s.execution)
			resolver := w.resolver
			switch tc.name {
			case "nil_resolver":
				resolver = nil
			case "nil_discovery_repo":
				w.discovery.repo = nil
			case "different_manager":
				w.discovery.manager = &collectorManager{}
			case "different_clock":
				w.discovery.clock = &executionTestClock{at: clock.at}
			case "different_scheduler":
				copy := *w.scheduler
				w.status.scheduler = &copy
			case "different_actor":
				w.discovery.actor = "other"
			case "different_scope":
				w.discovery.scopes[0].AccountID = "other"
			}
			out, err := NewWorker(w.discovery, w.scheduler, w.checkout, w.status, w.checkoutCompletion, w.statusCompletion, resolver)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, out)
			} else {
				require.NoError(t, err)
				require.NotNil(t, out)
			}
		})
	}
}
