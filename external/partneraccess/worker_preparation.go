package partneraccess

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
)

// LifecyclePreparation is an explicit operator capability for native projection
// preparation. Ordinary discovery/read/refresh grants never authorize it.
const LifecyclePreparation = "partner.lifecycle.prepare"

// AuthorizeLifecyclePreparation uses the same current instance-bound real
// service account and exact scope namespace, with a separate grant. Nil scope
// checks every configured scope; selected scopes must belong to that same set.
func (a *WorkerAuthority) AuthorizeLifecyclePreparation(ctx context.Context, actor string, scope *billing.RevenueScope) error {
	return a.checkLifecycle(ctx, actor, LifecyclePreparation, scope)
}
