package billinglifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func nativeStatusBinder(t *testing.T, f *statusNativeFixture, r *RecordExecutionRepository, clock *executionTestClock, actor string) (*StatusBinder, context.Context) {
	t.Helper()
	a, err := partneraccess.NewLifecycleWorkerAuthority("hostapp", actor, inputNativeIdentity{}, f.base.policy, []billing.RevenueScope{f.p.Scope})
	require.NoError(t, err)
	m, err := (&billingmanager.Service{}).WithRevenueServices(statusNativeRegistry{f.provider}, f.owner, f.base.native)
	require.NoError(t, err)
	_, err = m.WithSubscriptionStatusAuthority(a)
	require.NoError(t, err)
	ctx, err := a.Bind(f.base.ctx)
	require.NoError(t, err)
	cfg := schedulerTestConfig(f.p.Scope)
	cfg.ActorID = actor
	s, err := NewScheduler(r, a, clock, cfg)
	require.NoError(t, err)
	b, err := NewStatusBinder(s, r, m)
	require.NoError(t, err)
	return b, ctx
}

// Actual owning manager validation, current native grants/bound worker context
// and encrypted multi-kind transactions. Provider and API-service identity are
// controlled. These are package flows, not installed runtime/platform E2E.
func TestStatusBinderEncryptedCurrentAuthority(t *testing.T) {
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
			f, r, clock, j := nativeStatusBindingFixture(t)
			b, ctx := nativeStatusBinder(t, f, r, clock, "worker-original")
			input := StatusInput{Preparation: f.p}
			if tc.name == "acknowledged_evidence" || tc.name == "unknown_evidence_then_revoked" || tc.name == "confirmed_input_capture_lost_reply_recovery" || tc.name == "replacement_reuses_original" {
				first, err := b.Bind(ctx, JobLease(j), input)
				require.NoError(t, err)
				j = first.Job
				e := f.evidence(t)
				input.Evidence = &e
			}
			originalHandle := JobLease(j)
			switch tc.name {
			case "unknown_preparation_then_revoked", "unknown_evidence_then_revoked", "post_commit_revoked":
				f.base.reply.lose = tc.want == recordstore.ErrUncertain
				f.base.reply.after = func() { f.base.revoke(t) }
			case "unknown_preparation_then_canceled":
				c, cancel := context.WithCancel(ctx)
				ctx = c
				f.base.reply.lose = true
				f.base.reply.after = cancel
			case "post_commit_lease_expired":
				f.base.reply.after = func() { clock.set(j.LeasedUntil) }
			case "other_instance_context":
				_, ctx = nativeStatusBinder(t, f, r, clock, "worker-original")
			case "replacement_reuses_original":
				clock.set(j.LeasedUntil)

			}
			if tc.name == "replacement_reuses_original" {
				// Replacement grants are explicitly installed by the fixture; the binder
				// never creates one. Original authorship remains worker-original.
				g := f.base.grant
				g.Subject.ID = "worker-replacement"
				g.Revision = 0
				_, err := f.base.policy.ReplaceGrant(ctx, g, 0)
				require.NoError(t, err)
				next, err := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: j.Revision, Actor: "worker-replacement", Token: "controlled-replacement", Until: clock.at.Add(time.Minute)})
				require.NoError(t, err)
				j = next
				b, ctx = nativeStatusBinder(t, f, r, clock, "worker-replacement")
				input.Preparation = *j.OriginalStatus
			}
			beforeProvider := f.provider.calls
			out, err := b.Bind(ctx, JobLease(j), input)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusBinding{}, out)
				if tc.name == "unknown_preparation_then_revoked" || tc.name == "unknown_evidence_then_revoked" {
					require.ErrorIs(t, err, partnermanager.ErrDenied)
				}
				if tc.name == "unknown_preparation_then_canceled" {
					require.ErrorIs(t, err, context.Canceled)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, f.p, *out.Job.OriginalStatus)
				require.Equal(t, j.Revision+1, out.Job.Revision)
			}
			require.Equal(t, beforeProvider, f.provider.calls, "binding must not perform provider lookup")
			f.base.reply.lose = false
			f.base.reply.after = nil
			stored, err := r.ReadJob(context.WithoutCancel(f.base.ctx), j.Source)
			require.NoError(t, err)
			if tc.name == "other_instance_context" {
				require.Equal(t, j, stored)
				return
			}
			require.Equal(t, j.Revision+1, stored.Revision)
			require.Equal(t, f.p, *stored.OriginalStatus)
			// Reading committed state is inspection, not permission to execute. The old
			// handle cannot be used after an acknowledged or lost-reply stage.
			_, err = r.BindStatus(context.WithoutCancel(f.base.ctx), originalHandle, input)
			require.ErrorIs(t, err, recordstore.ErrConflict)
			if tc.name == "confirmed_input_capture_lost_reply_recovery" {
				require.NotNil(t, out.Input.Evidence)
				// Explicit owning capture is a separate transaction. Lose its real reply,
				// then recover the exact retained input without another provider GET.
				f.base.reply.lose = true
				_, err = f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", out.Input.Preparation, *out.Input.Evidence)
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				f.base.reply.lose = false
				receipt, err := f.manager.CaptureSubscriptionStatus(f.ctx, "worker-original", out.Input.Preparation, *out.Input.Evidence)
				require.NoError(t, err)
				require.Equal(t, f.p, receipt.Preparation)
				require.Equal(t, beforeProvider, f.provider.calls)
			}
		})
	}
}
