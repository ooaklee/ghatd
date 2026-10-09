package billinglifecyclehelper

import (
	"context"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// PrepareNative is an explicit operator operation over the selected database,
// stable payload key and already-bound trusted worker context. It validates
// limits and current preparation authority on all scopes before additive index
// and probe writes, then runs one bounded owning sweep. The total timeout covers
// composition as well as the sweep. Hosts own target selection, prerequisites,
// resource lifetime and worker binding; do not invoke this from startup.
//
// No account, grant, checkout or financial fact is fabricated. Reports and
// owning errors pass through unchanged, including authorized progress with
// ErrPreparationBudget. Cancellation or withheld reports do not prove rollback.
func PrepareNative(ctx context.Context, db *mongo.Database, payloadKey []byte, clock billing.RevenueClock, authority billinglifecycle.PreparationAuthority, cfg billinglifecycle.PreparationConfig) (billinglifecycle.PreparationReport, error) {
	if ctx == nil || db == nil || nilPort(clock) || nilPort(authority) {
		return billinglifecycle.PreparationReport{}, billing.ErrRevenueUnavailable
	}
	if err := ctx.Err(); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	cfg.Scopes = append([]billing.RevenueScope(nil), cfg.Scopes...)
	if err := cfg.Validate(); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	if err := authority.AuthorizeLifecyclePreparation(ctx, cfg.ActorID, nil); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	cipher, err := encryption.NewPayloadCipher(payloadKey)
	if err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	if err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	if err = store.EnsureIndexes(ctx); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	if err = store.Probe(ctx); err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	repo, err := revenuestore.NewRepository(store)
	if err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	owner, err := billing.NewRevenueService(repo, clock)
	if err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	prepare, err := billinglifecycle.NewPreparation(owner, authority, cfg)
	if err != nil {
		return billinglifecycle.PreparationReport{}, err
	}
	return prepare.RunOnce(ctx)
}
