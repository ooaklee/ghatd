package billinglifecycle

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
)

// PreparationOwner advances native history through its existing owning service.
// It owns projection/state transactions; this orchestration supplies no records.
type PreparationOwner interface {
	// PrepareLifecycleDiscovery advances native discovery history through the
	// owning service for the revenue scope and page size, returning the preparation
	// result; orchestration supplies no records.
	PrepareLifecycleDiscovery(context.Context, billing.RevenueScope, int) (billing.LifecyclePreparationResult, error)
}

// PreparationAuthority is deliberately distinct from recurring read/refresh.
// Nil scope requires current preparation permission on ALL configured scopes.
type PreparationAuthority interface {
	// AuthorizeLifecyclePreparation checks the actor's current preparation
	// permission for the scope, where a nil scope requires permission across all
	// configured scopes.
	AuthorizeLifecyclePreparation(context.Context, string, *billing.RevenueScope) error
}

var ErrPreparationBudget = errors.New("partners/lifecycle-preparation-budget-exhausted: rerun to resume native progress")
var ErrPreparationOverlap = errors.New("partners/lifecycle-preparation-already-running")

// PreparationConfig carries the validated actor, scopes, page limits and
// timeout for one preparation sweep.
type PreparationConfig struct {
	ActorID            string
	Scopes             []billing.RevenueScope
	PageSize, MaxPages int
	Timeout            time.Duration
}

// ValidateLimits permits CLI preflight before loading identities or opening storage.
func (c PreparationConfig) ValidateLimits() error {
	if c.PageSize < 1 || c.PageSize > 200 || c.MaxPages < 1 || c.MaxPages > 10000 || c.Timeout < time.Second || c.Timeout > time.Hour {
		return billing.ErrRevenueInvalid
	}
	return nil
}

// Validate checks the actor and revenue scopes plus the page limits and
// timeout, returning ErrRevenueInvalid on any malformed field.
func (c PreparationConfig) Validate() error {
	if !scheduleText(c.ActorID) || billing.ValidateRevenueHistoryScopes(c.Scopes) != nil {
		return billing.ErrRevenueInvalid
	}
	return c.ValidateLimits()
}

// PreparationReport discloses bounded operational counts, never source IDs.
// Ordinary errors withhold it; budget exhaustion returns authorized progress
// with Complete=false. Neither a withheld result nor timeout implies rollback.
type PreparationReport struct {
	Pages, Restarts, ScopesCompleted int
	Complete                         bool
}

// Preparation runs bounded native history sweeps over a borrowed owning service
// and preparation authority. Its mutex serializes sweep attempts on the shared
// instance.
type Preparation struct {
	owner     PreparationOwner
	authority PreparationAuthority
	cfg       PreparationConfig
	running   sync.Mutex
}

// NewPreparation validates the configuration and copies the scope slice so
// later caller mutation cannot change the sweep. Nil owner or authority is
// rejected.
func NewPreparation(owner PreparationOwner, authority PreparationAuthority, cfg PreparationConfig) (*Preparation, error) {
	if nilPort(owner) || nilPort(authority) {
		return nil, billing.ErrRevenueUnavailable
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	cfg.Scopes = append([]billing.RevenueScope(nil), cfg.Scopes...)
	return &Preparation{owner: owner, authority: authority, cfg: cfg}, nil
}

// check authorizes the configured actor for preparation, once globally and once
// for a selected scope when given. Context cancellation is checked before and
// after.
func (p *Preparation) check(ctx context.Context, scope *billing.RevenueScope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := p.authority.AuthorizeLifecyclePreparation(ctx, p.cfg.ActorID, nil); err != nil {
		return err
	}
	if scope != nil {
		if err := p.authority.AuthorizeLifecyclePreparation(ctx, p.cfg.ActorID, scope); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// RunOnce advances one explicitly admitted bounded sweep attempt, round-robin
// across scopes. Restarted means current writer epoch drift, not proof that
// older writers are drained; that remains an operator prerequisite. A subsequent
// invocation resumes owning progress, including committed lost replies.
func (p *Preparation) RunOnce(ctx context.Context) (PreparationReport, error) {
	if ctx == nil || p == nil || nilPort(p.owner) || nilPort(p.authority) {
		return PreparationReport{}, billing.ErrRevenueUnavailable
	}
	if !p.running.TryLock() {
		return PreparationReport{}, ErrPreparationOverlap
	}
	defer p.running.Unlock()
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	if err := p.check(ctx, nil); err != nil {
		return PreparationReport{}, err
	}
	report := PreparationReport{}
	complete := make([]bool, len(p.cfg.Scopes))
	for report.Pages < p.cfg.MaxPages && report.ScopesCompleted < len(complete) {
		for i, scope := range p.cfg.Scopes {
			if complete[i] || report.Pages == p.cfg.MaxPages {
				continue
			}
			if err := p.check(ctx, &scope); err != nil {
				return PreparationReport{}, err
			}
			result, err := p.owner.PrepareLifecycleDiscovery(ctx, scope, p.cfg.PageSize)
			// Preserve every owning error (including uncertainty) after late denial.
			final := p.check(ctx, &scope)
			if err != nil || final != nil {
				return PreparationReport{}, errors.Join(err, final)
			}
			v := result.State
			if v.Scope != scope || v.Revision < 1 || v.Sweeps < 1 || v.Epoch < 0 || v.Scanned < 0 || v.Selected < 0 || v.Selected > v.Scanned || v.CustomerlessPayments < 0 || v.CustomerlessPayments > v.Selected || v.Phase < 0 || v.Phase > billing.LifecyclePreparationComplete || (v.Phase == billing.LifecyclePreparationComplete) != !v.PreparedAt.IsZero() || v.Phase == billing.LifecyclePreparationComplete && v.AfterID != "" || result.Restarted && (v.Phase != 0 || v.AfterID != "") {
				return PreparationReport{}, billing.ErrRevenueUnavailable
			}
			report.Pages++
			if result.Restarted {
				report.Restarts++
			}
			if v.Phase == billing.LifecyclePreparationComplete {
				complete[i] = true
				report.ScopesCompleted++
			}
		}
	}
	if err := p.check(ctx, nil); err != nil {
		return PreparationReport{}, err
	}
	if report.ScopesCompleted < len(complete) {
		return report, ErrPreparationBudget
	}
	report.Complete = true
	return report, nil
}
