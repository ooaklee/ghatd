package billing

import (
	"context"
	"math"
	"strings"
	"time"
)

// LifecyclePreparationState retains one scope's restartable native upgrade
// sweep. It is private operational evidence, never financial entitlement.
// Phases scan acknowledgements, payments, first anchors and paid bindings,
// then validate both resulting projection sets against their original sources.
const LifecyclePreparationComplete = 6

// LifecyclePreparationState is the durable per-scope sweep progress: counters,
// current phase and position, and the prepared timestamp set only on
// completion.
type LifecyclePreparationState struct {
	Scope                                                            RevenueScope `json:"-"`
	Revision, Epoch, Sweeps, Scanned, Selected, CustomerlessPayments int64        `json:"-"`
	Phase                                                            int          `json:"-"`
	AfterID                                                          string       `json:"-"`
	PreparedAt                                                       time.Time    `json:"-"`
}

// LifecyclePreparationRow is a bounded original native row, including rows
// outside the selected scope. Original checkout/payment evidence must be
// validated BEFORE filtering, so malformed scope or mode cannot hide history.
// Candidates retain only selected sources; no provider I/O or financial writes.
type LifecyclePreparationRow struct {
	ID             string                       `json:"-"`
	Candidate      *LifecycleDiscoveryCandidate `json:"-"`
	OriginalFact   *RevenueFact                 `json:"-"`
	OriginalIntent *CheckoutIntent              `json:"-"`
}

// LifecyclePreparationTx is borrowed from the SAME configured revenue owner.
// All source joins, projection handoff, progress and final epoch CAS are atomic.
// Callback repetitions must have no effects outside this bound transaction.
type LifecyclePreparationTx interface {
	// State reads the current lifecycle preparation state within the bound
	// transaction; all joins, handoff, progress and final epoch CAS performed
	// through this LifecyclePreparationTx are atomic.
	State(context.Context) (LifecyclePreparationState, error)
	// Epoch reads the current preparation epoch within the bound
	// LifecyclePreparationTx transaction used for the final atomic
	// compare-and-swap.
	Epoch(context.Context) (int64, error)
	// Sources reads a bounded page of lifecycle preparation rows within the bound
	// transaction using the supplied limit and cursor arguments; source joins and
	// projection handoff are atomic.
	Sources(context.Context, int, string, int) ([]LifecyclePreparationRow, error)
	// Retain marks a discovery candidate as retained within the bound
	// LifecyclePreparationTx transaction; repeated callback invocations must have
	// no effects outside this transaction.
	Retain(context.Context, LifecycleDiscoveryCandidate) error
	// Save persists lifecycle preparation progress state and epoch within the bound
	// transaction, keeping source joins, projection handoff and progress atomic.
	Save(context.Context, LifecyclePreparationState, int64) error
	// Complete finalizes lifecycle preparation with the given state and epoch via
	// an atomic compare-and-swap within the bound transaction.
	Complete(context.Context, LifecyclePreparationState, int64) error
}

// LifecyclePreparationRepository provides lifecycle preparation transactions
// scoped to one revenue scope; atomicity depends on the implementation.
type LifecyclePreparationRepository interface {
	// WithLifecyclePreparation runs the callback with a LifecyclePreparationTx
	// scoped to one revenue scope; atomicity of the joins, handoff, progress and
	// epoch CAS depends on the implementation.
	WithLifecyclePreparation(context.Context, RevenueScope, func(LifecyclePreparationTx) error) error
}

// LifecyclePreparationResult reports the sweep's resulting state and whether
// changes during the sweep restarted progress from the beginning.
type LifecyclePreparationResult struct {
	State     LifecyclePreparationState `json:"-"`
	Restarted bool                      `json:"-"`
}

// lifecyclePreparationPosition accepts a continuation ID only when it carries
// the phase's checkout_/revenue_ prefix with a valid 64-hex digest suffix.
func lifecyclePreparationPosition(phase int, id string) bool {
	prefix := ""
	if phase == 0 {
		prefix = "checkout_"
	} else if phase == 1 {
		prefix = "revenue_"
	}
	return strings.HasPrefix(id, prefix) && discoveryDigest(strings.TrimPrefix(id, prefix))
}

// validLifecyclePreparation accepts state matching the scope with consistent
// counters, an in-range phase whose position matches its prefix rules, an empty
// position only at completion, and a prepared timestamp exactly when complete.
func validLifecyclePreparation(v LifecyclePreparationState, scope RevenueScope) bool {
	return v.Scope == scope && v.Revision >= 1 && v.Epoch >= 0 && v.Sweeps >= 1 && v.Scanned >= 0 && v.Selected >= 0 && v.Selected <= v.Scanned && v.CustomerlessPayments >= 0 && v.CustomerlessPayments <= v.Selected && v.Phase >= 0 && v.Phase <= LifecyclePreparationComplete && (v.AfterID == "" || lifecyclePreparationPosition(v.Phase, v.AfterID)) && (v.Phase != LifecyclePreparationComplete || v.AfterID == "") && (v.Phase == LifecyclePreparationComplete) == !v.PreparedAt.IsZero()
}

// PrepareLifecycleDiscovery advances at most ONE bounded original-history
// page. Explicit operator orchestration must drain older writers before calling
// it: only current native writers participate in the source epoch fence.
// Scope readiness requires every phase and an atomic CAS of that same epoch.
// Changes during a sweep reset progress, retaining safe immutable projections.
// Legacy financial-only payments are counted, never given invented customers.
func (s *RevenueService) PrepareLifecycleDiscovery(ctx context.Context, scope RevenueScope, limit int) (LifecyclePreparationResult, error) {
	if err := revenueContext(ctx); err != nil {
		return LifecyclePreparationResult{}, err
	}
	q := LifecycleDiscoveryQuery{Scope: scope, Kind: LifecycleCheckoutSources, Limit: limit}
	if _, err := q.AfterID(); err != nil {
		return LifecyclePreparationResult{}, err
	}
	if s == nil || revenueNil(s.repo) || revenueNil(s.clock) {
		return LifecyclePreparationResult{}, ErrRevenueUnavailable
	}
	repo, ok := s.repo.(LifecyclePreparationRepository)
	if !ok || revenueNil(repo) {
		return LifecyclePreparationResult{}, ErrRevenueUnavailable
	}
	var out LifecyclePreparationResult
	completed := false
	err := repo.WithLifecyclePreparation(ctx, scope, func(tx LifecyclePreparationTx) error {
		out = LifecyclePreparationResult{}
		completed = false
		if revenueNil(tx) {
			return ErrRevenueUnavailable
		}
		v, err := tx.State(ctx)
		expected := v.Revision
		if singleRevenueNotFound(err) {
			v = LifecyclePreparationState{Scope: scope, Revision: 1, Sweeps: 1}
			expected = 0
		} else if err != nil {
			return err
		} else if !validLifecyclePreparation(v, scope) {
			return ErrRevenueUnavailable
		}
		if v.Phase == LifecyclePreparationComplete {
			out.State = v
			completed = true
			return ctx.Err()
		}
		epoch, err := tx.Epoch(ctx)
		if err != nil {
			return err
		}
		if epoch < 0 || (expected != 0 && epoch < v.Epoch) {
			return ErrRevenueUnavailable
		}
		if expected == math.MaxInt64 {
			return ErrRevenueUnavailable
		}
		if expected == 0 {
			v.Epoch = epoch
		} else {
			v.Revision++
		}
		if v.Epoch != epoch {
			if v.Sweeps == math.MaxInt64 {
				return ErrRevenueUnavailable
			}
			v = LifecyclePreparationState{Scope: scope, Revision: v.Revision, Epoch: epoch, Sweeps: v.Sweeps + 1}
			if err := tx.Save(ctx, v, expected); err != nil {
				return err
			}
			out.State, out.Restarted, completed = v, true, true
			return ctx.Err()
		}
		rows, err := tx.Sources(ctx, v.Phase, v.AfterID, limit)
		if err != nil {
			return err
		}
		if len(rows) > limit {
			return ErrRevenueUnavailable
		}
		after := v.AfterID
		for _, row := range rows {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !lifecyclePreparationPosition(v.Phase, row.ID) || row.ID <= after || v.Scanned == math.MaxInt64 {
				return ErrRevenueUnavailable
			}

			if v.Phase == 0 {
				if row.OriginalIntent == nil || row.OriginalFact != nil {
					return ErrRevenueUnavailable
				}
				i := *row.OriginalIntent
				if !validStoredCheckout(i) || i.ID != row.ID || i.SessionID == "" {
					return ErrRevenueUnavailable
				}
				selected := i.Scope == scope && i.Request.Mode == "subscription"
				if selected != (row.Candidate != nil) {
					return ErrRevenueUnavailable
				}
				if selected && (row.Candidate.Intent.ID != i.ID || row.Candidate.Intent.Fingerprint != i.Fingerprint || row.Candidate.Intent.SessionID != i.SessionID || !row.Candidate.Intent.CreatedAt.Equal(i.CreatedAt)) {
					return ErrRevenueUnavailable
				}
			} else if v.Phase == 1 {
				if row.OriginalFact == nil || row.OriginalIntent != nil {
					return ErrRevenueUnavailable
				}
				f := *row.OriginalFact
				canonical, err := canonicalRevenueFact(f)
				if err != nil || f.ID != row.ID || f.Kind != RevenuePayment || f.Sequence < 1 || f.AcceptedAt.IsZero() || canonical.ID != f.ID || canonical.Fingerprint != f.Fingerprint {
					return ErrRevenueUnavailable
				}
				selected := f.Scope == scope && f.ProviderCustomerID != ""
				if selected != (row.Candidate != nil) {
					return ErrRevenueUnavailable
				}
				if selected && (row.Candidate.Fact.ID != f.ID || row.Candidate.Fact.Fingerprint != f.Fingerprint || row.Candidate.Fact.Sequence != f.Sequence || !row.Candidate.Fact.AcceptedAt.Equal(f.AcceptedAt)) {
					return ErrRevenueUnavailable
				}
			} else if row.OriginalIntent != nil || row.OriginalFact != nil || (v.Phase >= 4 && row.Candidate == nil) {
				return ErrRevenueUnavailable
			}
			after = row.ID
			v.Scanned++
			if row.Candidate != nil {
				kind := LifecycleSubscriptionSources
				if v.Phase == 0 || v.Phase == 4 {
					kind = LifecycleCheckoutSources
				}
				if err := validateDiscoveryCandidate(LifecycleDiscoveryQuery{Scope: scope, Kind: kind, Limit: limit}, *row.Candidate); err != nil {
					return err
				}
				if v.Phase < 4 {
					if err := tx.Retain(ctx, *row.Candidate); err != nil {
						return err
					}
					v.Selected++
				}
			} else if v.Phase == 1 && row.OriginalFact.Scope == scope && row.OriginalFact.ProviderCustomerID == "" {
				v.Selected++
				v.CustomerlessPayments++
			}
		}
		v.AfterID = after
		if len(rows) < limit {
			v.Phase++
			v.AfterID = ""
		}
		if v.Phase == LifecyclePreparationComplete {
			v.PreparedAt = s.clock.Now().UTC()
			if v.PreparedAt.IsZero() || epoch == math.MaxInt64 {
				return ErrRevenueUnavailable
			}
			// Completion also writes the epoch. A mere read followed by a separate
			// marker insert would permit write skew with a concurrent native source.
			v.Epoch = epoch + 1
			if err := tx.Complete(ctx, v, expected); err != nil {
				return err
			}
		} else if err := tx.Save(ctx, v, expected); err != nil {
			return err
		}
		out.State, completed = v, true
		return ctx.Err()
	})
	if err != nil {
		return LifecyclePreparationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return LifecyclePreparationResult{}, err
	}
	if !completed {
		return LifecyclePreparationResult{}, ErrRevenueUnavailable
	}
	return out, nil
}

// Only an unambiguous single-cause absence may start a new preparation. Joined
// absence/outage or uncertain reads must retain recovery rather than reset it.
func singleRevenueNotFound(err error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == ErrRevenueNotFound {
			return true
		}
		one, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = one.Unwrap()
	}
	return false
}
