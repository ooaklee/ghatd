package billinglifecycle

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/stretchr/testify/require"
)

func TestCheckoutDispositionNativeWorkerReports(t *testing.T) {
	for _, status := range []string{"open", "expired"} {
		t.Run(status, func(t *testing.T) {
			f := nativeCheckoutHTTPFixture(t, status, "unpaid", true, false)
			m := f.execution.owner.(*billingmanager.Service)
			a := f.execution.scheduler.authority
			grant := f.f.grant
			grant.Permissions = append(grant.Permissions, billingmanager.LifecycleDiscovery)
			var err error
			f.f.grant, err = f.f.policy.ReplaceGrant(f.f.ctx, grant, grant.Revision)
			require.NoError(t, err)
			native, err := revenuestore.NewRepository(f.f.reply)
			require.NoError(t, err)
			owner, err := billing.NewRevenueService(native, f.f.clock)
			require.NoError(t, err)
			ready := false
			for n := 0; n < 30; n++ {
				p, e := owner.PrepareLifecycleDiscovery(f.f.ctx, f.f.intent.Scope, 200)
				require.NoError(t, e)
				if p.State.Phase == billing.LifecyclePreparationComplete {
					ready = true
					break
				}
			}
			require.True(t, ready)
			_, err = m.WithLifecycleDiscoveryAuthority(a.(billingmanager.LifecycleDiscoveryAuthority))
			require.NoError(t, err)
			d, err := NewDiscoveryCollector(f.repo, m, a.(billingmanager.LifecycleDiscoveryAuthority), f.clock, DiscoveryConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{f.f.intent.Scope}, PageLimit: 3})
			require.NoError(t, err)
			box, err := NewStatusOutbox(f.f.reply, m)
			require.NoError(t, err)
			x, err := NewStatusExecution(f.execution.scheduler, f.repo, box)
			require.NoError(t, err)
			cc, err := NewCheckoutCompletion(f.execution, f.repo)
			require.NoError(t, err)
			sc, err := NewStatusCompletion(x, f.repo, 30*time.Second)
			require.NoError(t, err)
			resolver, err := NewStatusOriginalResolver(x, f.repo)
			require.NoError(t, err)
			worker, err := NewWorker(d, f.execution.scheduler, f.execution, x, cc, sc, resolver)
			require.NoError(t, err)
			_, err = f.repo.Release(f.ctx, LeaseDisposition{Handle: JobLease(f.job), NextAttemptAt: f.clock.at.Add(time.Second), Lane: ColdLane})
			require.NoError(t, err)
			f.clock.set(f.clock.at.Add(time.Second))
			report, err := worker.RunOnce(f.ctx)
			require.NoError(t, err)
			require.Zero(t, report.Completed, "operational expiry and pending are not captured completion")
			require.Zero(t, report.Retried)
			require.Zero(t, report.Superseded)
			if status == "expired" {
				require.Equal(t, 1, report.Expired)
				require.Zero(t, report.Pending)
			} else {
				require.Equal(t, 1, report.Pending)
				require.Zero(t, report.Expired)
			}
			calls := f.calls.Load()
			second, err := worker.RunOnce(f.ctx)
			require.NoError(t, err)
			require.Zero(t, second.Expired)
			require.Zero(t, second.Pending)
			require.Zero(t, second.Completed)
			require.Equal(t, calls, f.calls.Load(), "retired or backoff work does not re-query provider")
			require.Zero(t, f.writes.Load())
		})
	}
}
