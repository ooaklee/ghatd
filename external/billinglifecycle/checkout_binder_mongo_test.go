package billinglifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func nativeCheckoutBinder(t *testing.T, f *inputNativeFixture, r *RecordExecutionRepository, clock *executionTestClock, actor string) (*CheckoutBinder, context.Context) {
	t.Helper()
	a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", actor, inputNativeIdentity{}, f.policy, []billing.RevenueScope{f.intent.Scope})
	require.NoError(t, err)
	repo, err := revenuestore.NewRepository(f.reply)
	require.NoError(t, err)
	revenue, err := billing.NewRevenueService(repo, f.clock)
	require.NoError(t, err)
	m, err := (&billingmanager.Service{}).WithRevenueServices(inputNativeRegistry{}, revenue, f.native)
	require.NoError(t, err)
	_, err = m.WithCheckoutLifecycleAuthority(a)
	require.NoError(t, err)
	ctx, err := a.Bind(f.ctx)
	require.NoError(t, err)
	cfg := schedulerTestConfig(f.intent.Scope)
	cfg.ActorID = actor
	s, err := NewScheduler(r, a, clock, cfg)
	require.NoError(t, err)
	b, err := NewCheckoutBinder(s, r, m)
	require.NoError(t, err)
	return b, ctx
}

// Actual owning manager validation, current native grants/bound worker context
// and encrypted multi-kind transactions. Provider and API-service identity are
// controlled. These are package flows, not installed runtime/platform E2E.
func TestCheckoutBinderEncryptedCurrentAuthority(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"acknowledged_preparation", nil}, {"acknowledged_evidence", nil},
		{"unknown_preparation_then_revoked", recordstore.ErrUncertain}, {"unknown_evidence_then_revoked", recordstore.ErrUncertain},
		{"unknown_preparation_then_canceled", recordstore.ErrUncertain}, {"post_commit_revoked", partnermanager.ErrDenied},
		{"post_commit_lease_expired", recordstore.ErrConflict}, {"other_instance_context", partnermanager.ErrDenied},
		{"replacement_reuses_original", nil}, {"confirmed_input_capture_lost_reply_recovery", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			b, ctx := nativeCheckoutBinder(t, f, r, clock, "worker-original")
			input := CheckoutInput{Intent: f.intent}
			if tc.name == "acknowledged_evidence" || tc.name == "unknown_evidence_then_revoked" || tc.name == "confirmed_input_capture_lost_reply_recovery" || tc.name == "replacement_reuses_original" {
				first, err := b.Bind(ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e, err := f.manager.LookupCheckoutLifecycleEvidence(f.ctx, "worker-original", f.intent)
				require.NoError(t, err)
				input.Evidence = &e
			}
			originalHandle := JobLease(j)
			switch tc.name {
			case "unknown_preparation_then_revoked", "unknown_evidence_then_revoked", "post_commit_revoked":
				f.reply.lose = tc.want == recordstore.ErrUncertain
				f.reply.after = func() { f.revoke(t) }
			case "unknown_preparation_then_canceled":
				c, cancel := context.WithCancel(ctx)
				ctx = c
				f.reply.lose = true
				f.reply.after = cancel
			case "post_commit_lease_expired":
				f.reply.after = func() { clock.set(j.LeasedUntil) }
			case "other_instance_context":
				_, ctx = nativeCheckoutBinder(t, f, r, clock, "worker-original")
			case "replacement_reuses_original":
				clock.set(j.LeasedUntil)

			}
			if tc.name == "replacement_reuses_original" {
				// Replacement grants are explicitly installed by the fixture; the binder
				// never creates one. Original authorship remains worker-original.
				g := f.grant
				g.Subject.ID = "worker-replacement"
				g.Revision = 0
				_, err := f.policy.ReplaceGrant(ctx, g, 0)
				require.NoError(t, err)
				next, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-replacement", Token: "controlled-replacement", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
				j = next
				b, ctx = nativeCheckoutBinder(t, f, r, clock, "worker-replacement")
				input.Intent = *j.Source.Checkout
			}
			beforeProvider := f.provider.calls
			out, err := b.Bind(ctx, JobLease(j), input)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, CheckoutBinding{}, out)
				if tc.name == "unknown_preparation_then_revoked" || tc.name == "unknown_evidence_then_revoked" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
				if tc.name == "unknown_preparation_then_canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			} else {
				require.NoError(t, err)
				require.True(t, out.Job.CheckoutPrepared)
				require.Equal(t, f.intent, out.Input.Intent)
				require.Equal(t, j.Revision+1, out.Job.Revision)
			}
			require.Equal(t, beforeProvider, f.provider.calls, "binding must not perform provider lookup")
			f.reply.lose = false
			f.reply.after = nil
			stored, err := r.ReadJob(context.WithoutCancel(f.ctx), j.Source)
			require.NoError(t, err)
			if tc.name == "other_instance_context" {
				require.Equal(t, j, stored)
				return
			}
			require.Equal(t, j.Revision+1, stored.Revision)
			require.True(t, stored.CheckoutPrepared)
			// Reading committed state is inspection, not permission to execute. The old
			// handle cannot be used after an acknowledged or lost-reply stage.
			_, err = r.BindCheckout(context.WithoutCancel(f.ctx), originalHandle, input)
			require.ErrorIs(t, err, recordstore.ErrConflict)
			if tc.name == "confirmed_input_capture_lost_reply_recovery" {
				require.NotNil(t, out.Input.Evidence)
				// Explicit owning capture is a separate transaction. Lose its real reply,
				// then recover the exact retained input without another provider GET.
				f.reply.lose = true
				_, err = f.manager.CaptureCheckoutLifecycleEvidence(f.ctx, "worker-original", out.Input.Intent, *out.Input.Evidence)
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				f.reply.lose = false
				receipt, err := f.manager.CaptureCheckoutLifecycleEvidence(f.ctx, "worker-original", out.Input.Intent, *out.Input.Evidence)
				require.NoError(t, err)
				require.Equal(t, f.intent.ID, receipt.IntentID)
				require.Equal(t, beforeProvider, f.provider.calls)
			}
		})
	}
}

func TestCheckoutBindingEncryptedOwningRediscovery(t *testing.T) {
	for _, name := range []string{"prepared_binding_preserved", "evidenced_binding_preserved"} {
		t.Run(name, func(t *testing.T) {
			f, r, clock, j := nativeCheckoutBindingFixture(t)
			repo, err := revenuestore.NewRepository(f.reply)
			require.NoError(t, err)
			owner, err := billing.NewRevenueService(repo, f.clock)
			require.NoError(t, err)
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
			g := f.grant
			g.Permissions = append(g.Permissions, billingmanager.LifecycleDiscovery)
			f.grant, err = f.policy.ReplaceGrant(f.ctx, g, g.Revision)
			require.NoError(t, err)
			b, ctx := nativeCheckoutBinder(t, f, r, clock, "worker-original")
			m := b.validator.(*billingmanager.Service)
			authority := b.scheduler.authority.(billingmanager.LifecycleDiscoveryAuthority)
			_, err = m.WithLifecycleDiscoveryAuthority(authority)
			require.NoError(t, err)
			c, err := NewDiscoveryCollector(r, m, authority, f.clock, DiscoveryConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{f.intent.Scope}, PageLimit: 200})
			require.NoError(t, err)
			_, err = c.Admit(ctx, f.intent.Scope, billing.LifecycleCheckoutSources)
			require.NoError(t, err)
			first, err := b.Bind(ctx, JobLease(j), CheckoutInput{Intent: f.intent})
			require.NoError(t, err)
			if name == "evidenced_binding_preserved" {
				e, err := m.LookupCheckoutLifecycleEvidence(ctx, "worker-original", f.intent)
				require.NoError(t, err)
				first, err = b.Bind(ctx, JobLease(first.Job), CheckoutInput{Intent: f.intent, Evidence: &e})
				require.NoError(t, err)
			}
			calls := f.provider.calls
			_, err = c.Admit(ctx, f.intent.Scope, billing.LifecycleCheckoutSources)
			require.NoError(t, err)
			actual, err := r.ReadJob(ctx, j.Source)
			require.NoError(t, err)
			require.Equal(t, first.Job, actual)
			require.True(t, actual.CheckoutPrepared)
			require.Equal(t, calls, f.provider.calls)
		})
	}
}
