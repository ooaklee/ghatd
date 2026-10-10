package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Isolated orchestration fixtures, not storage atomicity or native service
// identity/grant proof. Each named case gets fresh dependencies and context.
type collectorRepository struct {
	checkpoint             DiscoveryCheckpoint
	readErr, commitErr     error
	reads, commits         int
	afterRead, afterCommit func()
	commit                 DiscoveryCommit
}

func (r *collectorRepository) ReadDiscovery(context.Context, billing.LifecycleDiscoveryQuery) (DiscoveryCheckpoint, error) {
	r.reads++
	if r.afterRead != nil {
		r.afterRead()
	}
	return r.checkpoint, r.readErr
}
func (r *collectorRepository) CommitDiscovery(_ context.Context, c DiscoveryCommit) error {
	r.commits++
	r.commit = c
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return r.commitErr
}

type collectorManager struct {
	page  billing.LifecycleDiscoveryPage
	err   error
	calls int
	actor string
	query billing.LifecycleDiscoveryQuery
	after func()
}

func (m *collectorManager) DiscoverLifecycleSources(_ context.Context, actor string, q billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoveryPage, error) {
	m.calls++
	m.actor, m.query = actor, q
	if m.after != nil {
		m.after()
	}
	return m.page, m.err
}

type collectorAuthority struct {
	denied, selectedDenied bool
	targets                []billingmanager.LifecycleDiscoveryTarget
	afterSelected          func()
}

func (a *collectorAuthority) AuthorizeLifecycleDiscovery(ctx context.Context, actor, action string, target billingmanager.LifecycleDiscoveryTarget) error {
	a.targets = append(a.targets, target)
	if err := ctx.Err(); err != nil {
		return err
	}
	if actor != "configured-worker" || action != billingmanager.LifecycleDiscovery || a.denied || (a.selectedDenied && target.PrincipalID != "") {
		return partnermanager.ErrDenied
	}
	if target.PrincipalID != "" && a.afterSelected != nil {
		a.afterSelected()
	}
	return nil
}

func TestDiscoveryCollectorCurrentAdmission(t *testing.T) {
	outage := errors.New("collector dependency outage")
	for _, tc := range []struct {
		name    string
		want    error
		commits int
	}{
		{"confirmed", nil, 1}, {"empty_page", nil, 1}, {"empty_raw_continuation", nil, 1},
		{"initial_scope_denied", partnermanager.ErrDenied, 0},
		{"read_outage", outage, 0}, {"read_outage_then_denied", partnermanager.ErrDenied, 0},
		{"read_unknown_then_denied", recordstore.ErrUncertain, 0},
		{"read_then_denied", partnermanager.ErrDenied, 0},
		{"manager_outage", outage, 0}, {"manager_outage_then_denied", partnermanager.ErrDenied, 0},
		{"malformed_page", billing.ErrRevenueUnavailable, 0},
		{"malformed_page_then_denied", partnermanager.ErrDenied, 0},
		{"selected_denied_before_commit", partnermanager.ErrDenied, 0},
		{"commit_conflict", recordstore.ErrConflict, 1}, {"commit_outage", outage, 1},
		{"commit_outage_then_denied", partnermanager.ErrDenied, 1},
		{"post_commit_scope_denied", partnermanager.ErrDenied, 1},
		{"post_commit_selected_denied", partnermanager.ErrDenied, 1},
		{"post_selected_scope_denied", partnermanager.ErrDenied, 1},
		{"unknown_commit", recordstore.ErrUncertain, 1},
		{"unknown_commit_then_denied", recordstore.ErrUncertain, 1},
		{"unknown_commit_then_canceled", recordstore.ErrUncertain, 1},
		{"negative_checkpoint", billing.ErrRevenueUnavailable, 0},
		{"exhausted_revision", billing.ErrRevenueUnavailable, 0},
		{"wrong_scope_checkpoint", billing.ErrRevenueUnavailable, 0},
		{"nonzero_cursor_at_revision_zero", billing.ErrRevenueUnavailable, 0},
		{"resumes_native_cursor", nil, 1},
		{"zero_clock", billing.ErrRevenueInvalid, 0},
		{"unconfigured_scope", billing.ErrRevenueInvalid, 0},
		{"unknown_kind", billing.ErrRevenueInvalid, 0},
		{"canceled_before_io", context.Canceled, 0},
		{"caller_config_mutation", nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, intent, _ := inputFixture(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			r := &collectorRepository{}
			m := &collectorManager{page: billing.LifecycleDiscoveryPage{Items: []billing.LifecycleDiscoveryCandidate{{ID: billing.LifecycleDiscoverySourceID(intent.Scope, billing.LifecycleCheckoutSources, intent.ID), Revision: 1, Scope: intent.Scope, PrincipalID: intent.Request.UserID, Intent: intent}}, ReachedEnd: true}}
			a := &collectorAuthority{}
			clock := inputNativeClock{intent.CreatedAt}
			cfg := DiscoveryConfig{ActorID: "configured-worker", Scopes: []billing.RevenueScope{intent.Scope}, PageLimit: 2}
			scope, kind := intent.Scope, billing.LifecycleCheckoutSources
			switch tc.name {
			case "empty_page":
				m.page.Items = nil
			case "empty_raw_continuation":
				m.page.NextCursor = (billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind}).CursorFor(m.page.Items[0].ID)
				m.page.Items = nil
				m.page.ReachedEnd = false
			case "initial_scope_denied":
				a.denied = true
			case "read_outage":
				r.readErr = outage
			case "read_outage_then_denied":
				r.readErr = outage
				r.afterRead = func() { a.denied = true }
			case "read_unknown_then_denied":
				r.readErr = recordstore.ErrUncertain
				r.afterRead = func() { a.denied = true }
			case "read_then_denied":
				r.afterRead = func() { a.denied = true }
			case "manager_outage":
				m.err = outage
			case "manager_outage_then_denied":
				m.err = outage
				m.after = func() { a.denied = true }
			case "malformed_page":
				m.page.Items[0].PrincipalID = "other"
			case "malformed_page_then_denied":
				m.page.Items[0].PrincipalID = "other"
				m.after = func() { a.denied = true }
			case "selected_denied_before_commit":
				m.after = func() { a.selectedDenied = true }
			case "commit_conflict":
				r.commitErr = recordstore.ErrConflict
			case "commit_outage":
				r.commitErr = outage
			case "commit_outage_then_denied":
				r.commitErr = outage
				r.afterCommit = func() { a.denied = true }
			case "post_commit_scope_denied":
				r.afterCommit = func() { a.denied = true }
			case "post_commit_selected_denied":
				r.afterCommit = func() { a.selectedDenied = true }
			case "post_selected_scope_denied":
				r.afterCommit = func() { a.afterSelected = func() { a.denied = true } }
			case "unknown_commit":
				r.commitErr = recordstore.ErrUncertain
			case "unknown_commit_then_denied":
				r.commitErr = recordstore.ErrUncertain
				r.afterCommit = func() { a.denied = true }
			case "unknown_commit_then_canceled":
				r.commitErr = recordstore.ErrUncertain
				r.afterCommit = cancel
			case "negative_checkpoint":
				r.checkpoint.Revision = -1
			case "exhausted_revision":
				r.checkpoint.Revision = math.MaxInt64
			case "wrong_scope_checkpoint":
				other := billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind}
				other.Scope.AccountID = "different"
				r.checkpoint = DiscoveryCheckpoint{1, other.CursorFor(m.page.Items[0].ID)}
			case "nonzero_cursor_at_revision_zero":
				r.checkpoint.Cursor = (billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind}).CursorFor(m.page.Items[0].ID)
			case "resumes_native_cursor":
				r.checkpoint = DiscoveryCheckpoint{2, (billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind}).CursorFor(m.page.Items[0].ID)}
				m.page.Items = nil
			case "zero_clock":
				clock.at = time.Time{}
			case "unconfigured_scope":
				scope.AccountID = "other"
			case "unknown_kind":
				kind = "invalid"
			case "canceled_before_io":
				cancel()
			}
			s, err := NewDiscoveryCollector(r, m, a, clock, cfg)
			require.NoError(t, err)
			if tc.name == "caller_config_mutation" {
				cfg.Scopes[0].AccountID = "changed"
				cfg.ActorID = "payer"
			}
			out, err := s.Admit(ctx, scope, kind)
			require.Equal(t, tc.commits, r.commits)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, DiscoveryAdmission{}, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, len(m.page.Items), out.Sources)
				require.Equal(t, DiscoveryCheckpoint{r.checkpoint.Revision + 1, m.page.NextCursor}, out.Checkpoint)
				require.Equal(t, r.checkpoint, r.commit.Expected)
				require.Equal(t, r.checkpoint.Cursor, m.query.Cursor)
				require.Equal(t, "configured-worker", m.actor)
				require.Equal(t, 2, m.query.Limit)
				raw, err := json.Marshal(out)
				require.NoError(t, err)
				require.Equal(t, "{}", string(raw))
			}
			if tc.name == "unknown_commit_then_denied" || tc.name == "read_unknown_then_denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.name == "unknown_commit_then_canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if tc.name == "initial_scope_denied" || tc.name == "canceled_before_io" || tc.name == "unconfigured_scope" || tc.name == "unknown_kind" {
				require.Zero(t, r.reads)
				require.Zero(t, m.calls)
			}
			if tc.name == "negative_checkpoint" || tc.name == "exhausted_revision" || tc.name == "wrong_scope_checkpoint" || tc.name == "nonzero_cursor_at_revision_zero" || tc.name == "read_then_denied" || tc.name == "read_outage_then_denied" {
				require.Zero(t, m.calls)
			}
		})
	}
}

func TestDiscoveryCollectorDependenciesAndConfig(t *testing.T) {
	for _, name := range []string{"nil_repository", "typed_nil_repository", "nil_manager", "typed_nil_manager", "nil_authority", "typed_nil_authority", "nil_clock", "empty_actor", "malformed_actor", "empty_scopes", "duplicate_scopes", "invalid_scope", "zero_limit", "oversized_limit", "maximum_limit", "nil_context", "nil_service"} {
		t.Run(name, func(t *testing.T) {
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
			var repo DiscoveryRepository = &collectorRepository{}
			var manager DiscoveryManager = &collectorManager{}
			var authority billingmanager.LifecycleDiscoveryAuthority = &collectorAuthority{}
			var clock LifecycleClock = inputNativeClock{time.Unix(1700000000, 0)}
			cfg := DiscoveryConfig{ActorID: "configured-worker", Scopes: []billing.RevenueScope{scope}, PageLimit: 1}
			switch name {
			case "nil_repository":
				repo = nil
			case "typed_nil_repository":
				repo = (*collectorRepository)(nil)
			case "nil_manager":
				manager = nil
			case "typed_nil_manager":
				manager = (*collectorManager)(nil)
			case "nil_authority":
				authority = nil
			case "typed_nil_authority":
				authority = (*collectorAuthority)(nil)
			case "nil_clock":
				clock = nil
			case "empty_actor":
				cfg.ActorID = ""
			case "malformed_actor":
				cfg.ActorID = " actor\n"
			case "empty_scopes":
				cfg.Scopes = nil
			case "duplicate_scopes":
				cfg.Scopes = append(cfg.Scopes, scope)
			case "invalid_scope":
				cfg.Scopes[0].Provider = ""
			case "zero_limit":
				cfg.PageLimit = 0
			case "oversized_limit":
				cfg.PageLimit = 201
			case "maximum_limit":
				cfg.PageLimit = 200
			}
			s, err := NewDiscoveryCollector(repo, manager, authority, clock, cfg)
			if name == "maximum_limit" {
				require.NoError(t, err)
				require.Equal(t, 200, s.limit)
				return
			}
			if name == "nil_context" || name == "nil_service" {
				require.NoError(t, err)
				ctx := t.Context()
				want := billing.ErrRevenueUnavailable
				if name == "nil_context" {
					ctx = nil
					want = billing.ErrRevenueInvalid
				} else {
					s = nil
				}
				out, err := s.Admit(ctx, scope, billing.LifecycleCheckoutSources)
				require.ErrorIs(t, err, want)
				require.Equal(t, DiscoveryAdmission{}, out)
				return
			}
			require.Error(t, err)
			require.Nil(t, s)
		})
	}
}
