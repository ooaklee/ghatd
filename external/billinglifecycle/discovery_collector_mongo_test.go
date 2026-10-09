package billinglifecycle

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

var _ DiscoveryManager = (*billingmanager.Service)(nil)

// Actual owning preparation/discovery, configured manager, native grants,
// instance-bound WorkerAuthority and encrypted transactions. Active API-service
// identity is controlled; this does not certify UMS admission or platform wiring.
func TestDiscoveryCollectorEncryptedCurrentWorker(t *testing.T) {
	for _, tc := range []struct {
		name      string
		want      error
		committed bool
	}{
		{"checkout_admitted", nil, true},
		{"subscription_admitted", nil, true},
		{"empty_subscription_page", nil, true},
		{"unprepared_scope", billing.ErrLifecycleDiscoveryUnprepared, false},
		{"refresh_grant_is_not_discovery", partnermanager.ErrDenied, false},
		{"read_grant_is_not_discovery", partnermanager.ErrDenied, false},
		{"disabled_grant", partnermanager.ErrDenied, false},
		{"wrong_instance_context", partnermanager.ErrDenied, false},
		{"wrong_scope_grant", partnermanager.ErrDenied, false},
		{"post_commit_revoked", partnermanager.ErrDenied, true},
		{"post_commit_canceled", context.Canceled, true},
		{"lost_commit_reply", recordstore.ErrUncertain, true},
		{"lost_reply_then_revoked", recordstore.ErrUncertain, true},
		{"lost_reply_then_canceled", recordstore.ErrUncertain, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f *inputNativeFixture
			kind := billing.LifecycleCheckoutSources
			if tc.name == "subscription_admitted" {
				f = nativeStatusFixture(t).base
				kind = billing.LifecycleSubscriptionSources
			} else {
				f = nativeInputFixture(t)
				if tc.name == "empty_subscription_page" {
					kind = billing.LifecycleSubscriptionSources
				}
			}
			repo, err := revenuestore.NewRepository(f.reply)
			require.NoError(t, err)
			owner, err := billing.NewRevenueService(repo, f.clock)
			require.NoError(t, err)
			if tc.name != "unprepared_scope" {
				ready := false
				for n := 0; n < 30; n++ {
					p, err := owner.PrepareLifecycleDiscovery(f.ctx, f.intent.Scope, 200)
					require.NoError(t, err)
					if p.State.Phase == billing.LifecyclePreparationComplete {
						ready = true
						break
					}
				}
				require.True(t, ready)
			}
			grant := f.grant
			grant.Permissions = []string{billingmanager.LifecycleDiscovery}
			switch tc.name {
			case "refresh_grant_is_not_discovery":
				grant.Permissions = []string{billingmanager.SubscriptionStatusRefresh}
			case "read_grant_is_not_discovery":
				grant.Permissions = []string{billingmanager.SubscriptionStatusRead}
			case "disabled_grant":
				grant.Enabled = false
			case "wrong_scope_grant":
				other := f.intent.Scope
				other.AccountID = "other"
				id, err := partneraccess.LifecycleScope(other)
				require.NoError(t, err)
				grant.Scopes = []string{id}
			}
			f.grant, err = f.policy.ReplaceGrant(f.ctx, grant, grant.Revision)
			require.NoError(t, err)
			a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", "worker-original", inputNativeIdentity{}, f.policy, []billing.RevenueScope{f.intent.Scope})
			require.NoError(t, err)
			manager, err := (&billingmanager.Service{}).WithRevenueServices(inputNativeRegistry{}, owner, f.native)
			require.NoError(t, err)
			_, err = manager.WithLifecycleDiscoveryAuthority(a)
			require.NoError(t, err)
			ctx, err := a.Bind(f.ctx)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.name == "wrong_instance_context" {
				ctx = f.ctx
			}
			r, err := NewRecordScheduleRepository(f.reply)
			require.NoError(t, err)
			s, err := NewDiscoveryCollector(r, manager, a, f.clock, DiscoveryConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{f.intent.Scope}, PageLimit: 1})
			require.NoError(t, err)
			beforeProviderCalls := f.provider.calls
			switch tc.name {
			case "post_commit_revoked":
				f.reply.after = func() { f.revoke(t) }
			case "post_commit_canceled":
				f.reply.after = cancel
			case "lost_commit_reply":
				f.reply.lose = true
			case "lost_reply_then_revoked":
				f.reply.lose = true
				f.reply.after = func() { f.revoke(t) }
			case "lost_reply_then_canceled":
				f.reply.lose = true
				f.reply.after = cancel
			}
			out, err := s.Admit(ctx, f.intent.Scope, kind)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, DiscoveryAdmission{}, out)
			} else {
				require.NoError(t, err)
				if tc.name == "empty_subscription_page" {
					require.Zero(t, out.Sources)
				} else {
					require.Equal(t, 1, out.Sources)
				}
				require.Equal(t, int64(1), out.Checkpoint.Revision)
			}
			if tc.name == "lost_reply_then_revoked" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.name == "lost_reply_then_canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			f.reply.after, f.reply.lose = nil, false
			// Inspect durable state directly for test evidence; this inspection
			// is not a service operation or authorization bypass in the collector.
			inspect := context.WithoutCancel(f.ctx)
			q := billing.LifecycleDiscoveryQuery{Scope: f.intent.Scope, Kind: kind, Limit: 1}
			checkpoint, err := r.ReadDiscovery(inspect, q)
			require.NoError(t, err)
			jobs, err := r.ReadScan(inspect, ScheduleScan{f.intent.Scope, ColdLane, 200})
			require.NoError(t, err)
			if tc.committed {
				require.Equal(t, int64(1), checkpoint.Revision)
				if tc.name == "empty_subscription_page" {
					require.Empty(t, jobs.Jobs)
				} else {
					require.Len(t, jobs.Jobs, 1)
				}
			} else {
				require.Equal(t, DiscoveryCheckpoint{}, checkpoint)
				require.Empty(t, jobs.Jobs)
			}
			require.Equal(t, beforeProviderCalls, f.provider.calls, "discovery must not lookup provider state")
			if tc.name == "lost_commit_reply" {
				// Resume from actual persisted native continuation; empty reached-end
				// page resets it without replacing or duplicating the admitted job.
				recovered, err := s.Admit(ctx, f.intent.Scope, kind)
				require.NoError(t, err)
				require.Equal(t, DiscoveryCheckpoint{Revision: 2}, recovered.Checkpoint)
				require.Zero(t, recovered.Sources)
				after, err := r.ReadScan(inspect, ScheduleScan{f.intent.Scope, ColdLane, 200})
				require.NoError(t, err)
				require.Equal(t, jobs.Jobs, after.Jobs)
			}
		})
	}
}
