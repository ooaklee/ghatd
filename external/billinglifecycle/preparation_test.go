package billinglifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type preparationAuthorityFixture struct {
	calls int
	err   error
	after func(int)
}

func (a *preparationAuthorityFixture) AuthorizeLifecyclePreparation(ctx context.Context, _ string, _ *billing.RevenueScope) error {
	a.calls++
	if a.after != nil {
		a.after(a.calls)
	}
	if a.err != nil {
		return a.err
	}
	return ctx.Err()
}

type preparationOwnerFixture struct {
	phases           map[billing.RevenueScope]int
	order            []billing.RevenueScope
	after            func(context.Context)
	err              error
	corrupt, restart bool
}

func (o *preparationOwnerFixture) PrepareLifecycleDiscovery(ctx context.Context, scope billing.RevenueScope, _ int) (billing.LifecyclePreparationResult, error) {
	if err := ctx.Err(); err != nil {
		return billing.LifecyclePreparationResult{}, err
	}
	o.order = append(o.order, scope)
	restarted := o.restart
	o.restart = false
	n := o.phases[scope]
	if n < 6 {
		n++
	}
	if restarted {
		n = 0
	}
	o.phases[scope] = n
	v := billing.LifecyclePreparationState{Scope: scope, Phase: n, Revision: int64(n + 1), Sweeps: 1}
	if n == 6 {
		v.PreparedAt = time.Now().UTC()
	}
	if o.corrupt {
		v.Scope.AccountID = "another-owner"
	}
	if o.after != nil {
		o.after(ctx)
	}
	return billing.LifecyclePreparationResult{State: v, Restarted: restarted}, o.err
}
func preparationConfigFixture() PreparationConfig {
	return PreparationConfig{ActorID: "worker-original", Scopes: []billing.RevenueScope{{Provider: "stripe", AccountID: "acct_fixture"}}, PageSize: 3, MaxPages: 20, Timeout: time.Second}
}

func TestLifecyclePreparationBoundsAndConstructor(t *testing.T) {
	for _, tc := range []string{"valid", "missing_actor", "empty_scopes", "bad_scope", "zero_page", "page_over_bound", "zero_budget", "budget_over_bound", "subsecond_timeout", "timeout_over_bound", "nil_owner", "nil_authority", "typed_nil_owner", "typed_nil_authority"} {
		t.Run(tc, func(t *testing.T) {
			cfg := preparationConfigFixture()
			var owner PreparationOwner = &preparationOwnerFixture{}
			var authority PreparationAuthority = &preparationAuthorityFixture{}
			switch tc {
			case "missing_actor":
				cfg.ActorID = ""
			case "empty_scopes":
				cfg.Scopes = nil
			case "bad_scope":
				cfg.Scopes[0].Provider = ""
			case "zero_page":
				cfg.PageSize = 0
			case "page_over_bound":
				cfg.PageSize = 201
			case "zero_budget":
				cfg.MaxPages = 0
			case "budget_over_bound":
				cfg.MaxPages = 10001
			case "subsecond_timeout":
				cfg.Timeout = time.Millisecond
			case "timeout_over_bound":
				cfg.Timeout = time.Hour + time.Second
			case "nil_owner":
				owner = nil
			case "nil_authority":
				authority = nil
			case "typed_nil_owner":
				owner = (*preparationOwnerFixture)(nil)
			case "typed_nil_authority":
				authority = (*preparationAuthorityFixture)(nil)
			}
			p, err := NewPreparation(owner, authority, cfg)
			if tc != "valid" {
				require.Error(t, err)
				require.Nil(t, p)
				return
			}
			require.NoError(t, err)
			cfg.Scopes[0].AccountID = "mutated"
			require.Equal(t, "acct_fixture", p.cfg.Scopes[0].AccountID)
		})
	}
}

func TestLifecyclePreparationBoundedAdmissionAndRecovery(t *testing.T) {
	for _, tc := range []string{"complete", "two_scopes_round_robin", "budget_resume", "prepared_replay", "restart_count", "initial_denial", "selected_denial", "late_denial", "unknown_then_resume", "unknown_late_denial", "cancel_after_commit", "parent_deadline", "deadline_after_commit", "corrupt_scope", "nil_context", "nil_service", "final_denial"} {
		t.Run(tc, func(t *testing.T) {
			cfg := preparationConfigFixture()
			scope := cfg.Scopes[0]
			o := &preparationOwnerFixture{phases: map[billing.RevenueScope]int{}}
			a := &preparationAuthorityFixture{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch tc {
			case "two_scopes_round_robin":
				cfg.Scopes = append(cfg.Scopes, billing.RevenueScope{Provider: "stripe", AccountID: "second"})
			case "budget_resume":
				cfg.MaxPages = 2
			case "prepared_replay":
				o.phases[scope] = 6
			case "restart_count":
				o.restart = true
			case "initial_denial":
				a.err = partnermanager.ErrDenied
			case "selected_denial":
				a.after = func(n int) {
					if n == 3 {
						a.err = partnermanager.ErrDenied
					}
				}
			case "late_denial":
				o.after = func(context.Context) { a.err = partnermanager.ErrDenied }
			case "unknown_then_resume":
				o.err = recordstore.ErrUncertain
			case "unknown_late_denial":
				o.err = recordstore.ErrUncertain
				o.after = func(context.Context) { a.err = partnermanager.ErrDenied }
			case "cancel_after_commit":
				o.after = func(context.Context) { cancel() }
			case "parent_deadline":
				var c context.CancelFunc
				ctx, c = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer c()
			case "deadline_after_commit":
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, 10*time.Millisecond)
				defer c()
				o.after = func(pageCtx context.Context) { <-pageCtx.Done() }
			case "corrupt_scope":
				o.corrupt = true
			case "final_denial":
				a.after = func(n int) {
					if n == 26 {
						a.err = partnermanager.ErrDenied
					}
				}
			}
			p, err := NewPreparation(o, a, cfg)
			require.NoError(t, err)
			if tc == "nil_context" {
				ctx = nil
			}
			if tc == "nil_service" {
				p = nil
			}
			report, err := p.RunOnce(ctx)
			switch tc {
			case "complete", "two_scopes_round_robin", "prepared_replay", "restart_count":
				require.NoError(t, err)
				require.True(t, report.Complete)
				require.Equal(t, len(cfg.Scopes), report.ScopesCompleted)
				if tc == "two_scopes_round_robin" {
					require.Equal(t, 12, report.Pages)
					for i, sc := range o.order {
						require.Equal(t, cfg.Scopes[i%2], sc)
					}
				}
				if tc == "prepared_replay" {
					require.Equal(t, 1, report.Pages)
				}
				if tc == "restart_count" {
					require.Equal(t, 1, report.Restarts)
				}
			case "budget_resume":
				require.ErrorIs(t, err, ErrPreparationBudget)
				require.False(t, report.Complete)
				require.Equal(t, 2, report.Pages)
				p.cfg.MaxPages = 20
				report, err = p.RunOnce(ctx)
				require.NoError(t, err)
				require.True(t, report.Complete)
				require.Equal(t, 4, report.Pages)
			case "unknown_then_resume":
				require.ErrorIs(t, err, recordstore.ErrUncertain)
				require.Zero(t, report)
				require.Len(t, o.order, 1)
				o.err = nil
				report, err = p.RunOnce(ctx)
				require.NoError(t, err)
				require.True(t, report.Complete)
				require.Equal(t, 5, report.Pages)
			default:
				require.Error(t, err)
				require.Zero(t, report)
				if tc == "unknown_late_denial" {
					require.ErrorIs(t, err, recordstore.ErrUncertain)
					require.ErrorIs(t, err, partnermanager.ErrDenied)
					require.Len(t, o.order, 1)
				}
				if tc == "cancel_after_commit" {
					require.ErrorIs(t, err, context.Canceled)
					require.Len(t, o.order, 1)
				}
				if tc == "initial_denial" || tc == "selected_denial" || tc == "parent_deadline" {
					require.Empty(t, o.order)
				}
			}
		})
	}
}

func TestLifecyclePreparationConcurrentInvocationFailsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan struct{})
	done := make(chan error, 1)
	// Block on the owner's derived context, so returning from the page means
	// RunOnce already observes cancellation. The parent can close before its
	// cancellation propagates to the child under concurrent scheduling.
	o := &preparationOwnerFixture{phases: map[billing.RevenueScope]int{}, after: func(pageCtx context.Context) { close(started); <-pageCtx.Done() }}
	cfg := preparationConfigFixture()
	cfg.Timeout = time.Minute
	p, err := NewPreparation(o, &preparationAuthorityFixture{}, cfg)
	require.NoError(t, err)
	go func() { _, e := p.RunOnce(ctx); done <- e }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first page not entered")
	}
	report, err := p.RunOnce(t.Context())
	require.ErrorIs(t, err, ErrPreparationOverlap)
	require.Zero(t, report)
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("preparation did not drain")
	}
	require.Len(t, o.order, 1)
}
