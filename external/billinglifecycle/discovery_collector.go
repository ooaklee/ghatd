package billinglifecycle

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// DiscoveryManager must be the configured billing manager, which owns source
// provenance and current discovery permission. A raw revenue read is insufficient.
type DiscoveryManager interface {
	DiscoverLifecycleSources(context.Context, string, billing.LifecycleDiscoveryQuery) (billing.LifecycleDiscoveryPage, error)
}

type LifecycleClock interface{ Now() time.Time }

// DiscoveryConfig freezes the verified service actor and allowed native scopes.
// Configuration creates neither service identity nor discovery grants.
type DiscoveryConfig struct {
	ActorID   string
	Scopes    []billing.RevenueScope
	PageLimit int
}

// DiscoveryAdmission is a private confirmed handoff, not source completeness,
// subscription freshness, eligibility or proof of external delivery.
type DiscoveryAdmission struct {
	Checkpoint DiscoveryCheckpoint `json:"-"`
	Sources    int                 `json:"-"`
}

// DiscoveryCollector admits one bounded owning page per invocation. It borrows
// the configured manager and its SAME current worker authority. No goroutine,
// migration, provider lookup, capture or financial worker is started here.
type DiscoveryCollector struct {
	repo      DiscoveryRepository
	manager   DiscoveryManager
	authority billingmanager.LifecycleDiscoveryAuthority
	clock     LifecycleClock
	actor     string
	scopes    []billing.RevenueScope
	limit     int
}

func NewDiscoveryCollector(repo DiscoveryRepository, manager DiscoveryManager, authority billingmanager.LifecycleDiscoveryAuthority, clock LifecycleClock, cfg DiscoveryConfig) (*DiscoveryCollector, error) {
	if nilPort(repo) || nilPort(manager) || nilPort(authority) || nilPort(clock) {
		return nil, billing.ErrRevenueUnavailable
	}
	if !scheduleText(cfg.ActorID) || cfg.PageLimit < 1 || cfg.PageLimit > 200 {
		return nil, billing.ErrRevenueInvalid
	}
	if err := billing.ValidateRevenueHistoryScopes(cfg.Scopes); err != nil {
		return nil, err
	}
	return &DiscoveryCollector{repo: repo, manager: manager, authority: authority, clock: clock, actor: cfg.ActorID, scopes: append([]billing.RevenueScope(nil), cfg.Scopes...), limit: cfg.PageLimit}, nil
}

func discoveryTargets(q billing.LifecycleDiscoveryQuery, page billing.LifecycleDiscoveryPage) []billingmanager.LifecycleDiscoveryTarget {
	result := make([]billingmanager.LifecycleDiscoveryTarget, 0, len(page.Items))
	for _, item := range page.Items {
		t := billingmanager.LifecycleDiscoveryTarget{Scope: item.Scope, Kind: q.Kind, PrincipalID: item.PrincipalID, SubscriptionID: item.SubscriptionID}
		if q.Kind == billing.LifecycleCheckoutSources {
			t.IntentID = item.Intent.ID
		}
		result = append(result, t)
	}
	return result
}

func (s *DiscoveryCollector) authorize(ctx context.Context, target billingmanager.LifecycleDiscoveryTarget) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.authority.AuthorizeLifecycleDiscovery(ctx, s.actor, billingmanager.LifecycleDiscovery, target); err != nil {
		return err
	}
	return ctx.Err()
}

// finish runs outside retryable callbacks for every storage/manager outcome.
// Current authority withholds data, but cannot erase an observed unknown commit.
func (s *DiscoveryCollector) finish(ctx context.Context, scope billingmanager.LifecycleDiscoveryTarget, targets []billingmanager.LifecycleDiscoveryTarget, result DiscoveryAdmission, operationErr error) (DiscoveryAdmission, error) {
	withhold := func(err error) (DiscoveryAdmission, error) {
		if errors.Is(operationErr, recordstore.ErrUncertain) {
			return DiscoveryAdmission{}, errors.Join(err, operationErr)
		}
		return DiscoveryAdmission{}, err
	}
	if err := s.authorize(ctx, scope); err != nil {
		return withhold(err)
	}
	for _, target := range targets {
		if err := s.authorize(ctx, target); err != nil {
			return withhold(err)
		}
	}
	if len(targets) > 0 {
		if err := s.authorize(ctx, scope); err != nil {
			return withhold(err)
		}
	}
	if err := ctx.Err(); err != nil {
		return withhold(err)
	}
	if operationErr != nil {
		return DiscoveryAdmission{}, operationErr
	}
	return result, nil
}

// Admit uses the persisted native checkpoint, never a caller-supplied cursor.
// Unknown results require re-reading durable state under current authority on
// the next invocation; this method does not blindly retry a page or infer rollback.
func (s *DiscoveryCollector) Admit(ctx context.Context, scope billing.RevenueScope, kind string) (DiscoveryAdmission, error) {
	if ctx == nil {
		return DiscoveryAdmission{}, billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return DiscoveryAdmission{}, err
	}
	if s == nil || nilPort(s.repo) || nilPort(s.manager) || nilPort(s.authority) || nilPort(s.clock) {
		return DiscoveryAdmission{}, billing.ErrRevenueUnavailable
	}
	allowed := false
	for _, configured := range s.scopes {
		allowed = allowed || configured == scope
	}
	if !allowed {
		return DiscoveryAdmission{}, billing.ErrRevenueInvalid
	}
	q := billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind, Limit: s.limit}
	if _, err := q.AfterID(); err != nil {
		return DiscoveryAdmission{}, err
	}
	scopeTarget := billingmanager.LifecycleDiscoveryTarget{Scope: scope, Kind: kind}
	if err := s.authorize(ctx, scopeTarget); err != nil {
		return DiscoveryAdmission{}, err
	}
	checkpoint, err := s.repo.ReadDiscovery(ctx, q)
	if _, finishErr := s.finish(ctx, scopeTarget, nil, DiscoveryAdmission{}, err); finishErr != nil {
		return DiscoveryAdmission{}, finishErr
	}
	q.Cursor = checkpoint.Cursor
	if checkpoint.Revision < 0 || checkpoint.Revision == math.MaxInt64 || (checkpoint.Revision == 0 && checkpoint.Cursor != "") {
		return s.finish(ctx, scopeTarget, nil, DiscoveryAdmission{}, billing.ErrRevenueUnavailable)
	}
	if _, err := q.AfterID(); err != nil {
		return s.finish(ctx, scopeTarget, nil, DiscoveryAdmission{}, billing.ErrRevenueUnavailable)
	}
	page, err := s.manager.DiscoverLifecycleSources(ctx, s.actor, q)
	if err != nil {
		return s.finish(ctx, scopeTarget, nil, DiscoveryAdmission{}, err)
	}
	if err := page.Validate(q); err != nil {
		return s.finish(ctx, scopeTarget, nil, DiscoveryAdmission{}, err)
	}
	targets := discoveryTargets(q, page)
	if _, err := s.finish(ctx, scopeTarget, targets, DiscoveryAdmission{}, nil); err != nil {
		return DiscoveryAdmission{}, err
	}
	now := s.clock.Now()
	if now.IsZero() {
		return s.finish(ctx, scopeTarget, targets, DiscoveryAdmission{}, billing.ErrRevenueInvalid)
	}
	err = s.repo.CommitDiscovery(ctx, DiscoveryCommit{Query: q, Expected: checkpoint, Page: page, Now: now})
	result := DiscoveryAdmission{Checkpoint: DiscoveryCheckpoint{Revision: checkpoint.Revision + 1, Cursor: page.NextCursor}, Sources: len(page.Items)}
	return s.finish(ctx, scopeTarget, targets, result, err)
}
