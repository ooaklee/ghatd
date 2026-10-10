package partnermanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/partnerprogram"
)

// WorkBacklogService is an optional owning-service read capability. Managers
// neither access its repository nor perform discovery or leases to report it.
type WorkBacklogService interface {
	// GetBacklog reports the aggregate WorkBacklog snapshot; the WorkQueue
	// implementation performs no mutation or discovery and discards the entire
	// aggregate on any failed scope instead of returning zero.
	GetBacklog(context.Context) (WorkBacklog, error)
}

// AdminWorkerBacklog requires current program-scoped operations permission
// before and after the owning read. Commercial pause does not hide obligations.
// Reporting permission for one partner does not grant program operations access.
func (m *Manager) AdminWorkerBacklog(ctx context.Context, actor string) (WorkBacklog, error) {
	if err := m.authorize(ctx, actor, CapabilityOperations, partnerprogram.ProgramID); err != nil {
		return WorkBacklog{}, err
	}
	if nilManagerDependency(m.deps.WorkReporting) {
		return WorkBacklog{}, ErrUnavailable
	}
	out, err := m.deps.WorkReporting.GetBacklog(ctx)
	if err != nil {
		return WorkBacklog{}, err
	}
	if nilManagerDependency(m.deps.Clock) {
		return WorkBacklog{}, ErrUnavailable
	}
	checkedAt := m.deps.Clock.Now().UTC()
	if out.ProgramID != partnerprogram.ProgramID || out.Validate() != nil || checkedAt.IsZero() || out.AsOf.After(checkedAt) {
		return WorkBacklog{}, ErrUnavailable
	}
	if err := m.authorize(ctx, actor, CapabilityOperations, partnerprogram.ProgramID); err != nil {
		return WorkBacklog{}, err
	}
	return out, nil
}
