package partnerruntime

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/partnerstore"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Dependencies are owning services and host authority policy.
// Billing revenue is deliberately absent: this composition constructs its real
// owning service over prepared shared storage, never over legacy access facts.
type Dependencies struct {
	Identity  partnermanager.Identity
	Authority partnermanager.Authority
	Groups    partnermanager.Groups
	Clock     partnermanager.Clock
	IDs       partnerprogram.IDGenerator
}

// Runtime retains the owning capabilities needed by transport,
// billing verification and scheduling. It borrows the host's database and does
// not close its pool, register routes, start workers or activate commercial use.
type Runtime struct {
	recordStore         recordstore.Store
	Manager             *partnermanager.Manager
	Program             *partnerprogram.Service
	Referral            *referral.Service
	Earnings            *partnerearnings.Service
	Revenue             *billing.RevenueService
	Work                *partnermanager.WorkQueue
	Evidence            *referral.EvidenceSigner
	checkoutRepository  billing.CheckoutRepository
	managerDependencies partnermanager.Dependencies
}

// NewRuntime composes GHATD owning services. Additive index
// preparation and a native read/write/commit probe are explicit via Prepare.
// No bootstrap actor bypasses policy authority and no policy version is seeded.
// The host explicitly prepares this exact composition before admitting its HTTP adapter.
// Native composition alone does not establish platform delivery.
func NewRuntime(db *mongo.Database, cfg Config, deps Dependencies) (*Runtime, error) {
	if db == nil || nilRuntimePort(deps.Identity) || nilRuntimePort(deps.Authority) || nilRuntimePort(deps.Groups) || nilRuntimePort(deps.Clock) || nilRuntimePort(deps.IDs) {
		return nil, partnermanager.ErrUnavailable
	}
	if err := cfg.Program.Validate(); err != nil {
		return nil, err
	}
	// Reject startup admission configuration before acquiring storage handles;
	// the owning manager also enforces this invariant at its public constructor.
	if cfg.Claims.MinimumMinor < 0 {
		return nil, partnermanager.ErrInvalid
	}
	if cfg.VisitWindow > 0 && cfg.Analytics == nil {
		return nil, partnermanager.ErrInvalid
	}
	// Reject the published owner's bounds before acquiring database handles;
	// WithAnalytics below also validates the actual composed service.
	if cfg.Analytics != nil && (cfg.Analytics.ObservationRetention < 24*time.Hour || cfg.Analytics.ObservationRetention > 365*24*time.Hour) {
		return nil, referral.ErrInvalid
	}
	if cfg.Evidence.ProgramID != partnerprogram.ProgramID || cfg.Queue.ProgramID != partnerprogram.ProgramID || cfg.Evidence.Window != cfg.Program.AttributionWindow || !validReservedKeys(cfg.ReservedKeys) || !independentPartnersKey(cfg.PayloadKey, cfg.ReservedKeys) {
		return nil, partnermanager.ErrInvalid
	}
	// A verification key retained for rotation is still a signing-purpose key;
	// it cannot be reused for durable payload encryption or another host purpose.
	reserved := append([][]byte{cfg.PayloadKey}, cfg.ReservedKeys...)
	for _, key := range cfg.Evidence.Keys {
		if !independentPartnersKey(key, reserved) {
			return nil, partnermanager.ErrInvalid
		}
	}
	evidence := cfg.Evidence
	evidence.VisitWindow, evidence.VisitIDs = cfg.VisitWindow, deps.IDs
	signer, err := referral.NewEvidenceSigner(evidence, deps.Clock)
	if err != nil {
		return nil, err
	}
	// Validate optional reporting before touching storage, using the owning
	// public validators rather than inventing a source scope or freshness window.
	if cfg.RevenueReporting != nil && (billing.ValidateRevenueHistoryScopes(cfg.RevenueReporting.Scopes) != nil || cfg.RevenueReporting.StatusMaxAge < time.Second || cfg.RevenueReporting.StatusMaxAge > 24*time.Hour) {
		return nil, partnermanager.ErrInvalid
	}
	cipher, err := encryption.NewPayloadCipher(cfg.PayloadKey)
	if err != nil {
		return nil, err
	}
	store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
	if err != nil {
		return nil, err
	}
	workRepo, err := partnerstore.NewWorkRepository(store)
	if err != nil {
		return nil, err
	}
	work, err := partnermanager.NewWorkQueue(workRepo, deps.Clock, cfg.Queue)
	if err != nil {
		return nil, err
	}
	programRepo, err := partnerstore.NewProgramRepository(store)
	if err != nil {
		return nil, err
	}
	program, err := partnerprogram.NewService(programRepo, deps.Clock, deps.IDs, cfg.Program)
	if err != nil {
		return nil, err
	}
	referralRepo, err := partnerstore.NewReferralRepository(store)
	if err != nil {
		return nil, err
	}
	referrals, err := referral.NewService(referralRepo, deps.Clock, deps.IDs, cfg.Program.AttributionWindow)
	if err != nil {
		return nil, err
	}
	if cfg.Analytics != nil {
		referrals, err = referrals.WithAnalytics(*cfg.Analytics)
		if err != nil {
			return nil, err
		}
	}
	financial := partnerearnings.Config{ProgramID: partnerprogram.ProgramID, Currency: cfg.Program.Currency}
	earningsRepo, err := partnerstore.NewEarningsRepository(store, financial)
	if err != nil {
		return nil, err
	}
	earnings, err := partnerearnings.NewService(earningsRepo, deps.Clock, deps.IDs, financial)
	if err != nil {
		return nil, err
	}
	revenueRepo, err := revenuestore.NewRepository(store)
	if err != nil {
		return nil, err
	}
	revenue, err := billing.NewRevenueService(revenueRepo, deps.Clock)
	if err != nil {
		return nil, err
	}
	managerDeps := partnermanager.Dependencies{Program: program, Referral: referrals, Earnings: earnings, Identity: deps.Identity, Authority: deps.Authority, Groups: deps.Groups, Revenue: revenue, Evidence: signer, Clock: deps.Clock, Controls: cfg.Controls, Claims: cfg.Claims, WorkReporting: work}
	manager, err := partnermanager.NewManager(managerDeps)
	if err != nil {
		return nil, err
	}
	if cfg.RevenueReporting != nil {
		manager, err = manager.WithRevenueReporting(*cfg.RevenueReporting)
		if err != nil {
			return nil, err
		}
	}

	return &Runtime{recordStore: store, Manager: manager, Program: program, Referral: referrals, Earnings: earnings, Revenue: revenue, Work: work, Evidence: signer, checkoutRepository: revenueRepo, managerDependencies: managerDeps}, nil
}

// independentPartnersKey accepts only a 32-byte non-zero key that differs from
// every reserved key; reserved entries must themselves be valid.
func independentPartnersKey(key []byte, reserved [][]byte) bool {
	if len(key) != 32 || bytes.Equal(key, make([]byte, 32)) {
		return false
	}
	for _, other := range reserved {
		if len(other) != 32 || bytes.Equal(other, make([]byte, 32)) || bytes.Equal(key, other) {
			return false
		}
	}
	return true
}

// Prepare explicitly installs additive indexes and probes a native transaction.
// Call before admitting HTTP or workers. It does not seed policy or create
// identity/grants; its deadline is bounded by the caller and thirty seconds.
func (r *Runtime) Prepare(ctx context.Context) error {
	if ctx == nil || r == nil || nilRuntimePort(r.recordStore) {
		return partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	store, ok := r.recordStore.(interface {
		EnsureIndexes(context.Context) error
		Probe(context.Context) error
	})
	if !ok {
		return partnermanager.ErrUnavailable
	}
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := store.EnsureIndexes(startup); err != nil {
		return fmt.Errorf("partners shared index preparation: %w", err)
	}
	if err := store.Probe(startup); err != nil {
		return fmt.Errorf("partners shared transaction probe: %w", err)
	}
	return ctx.Err()
}

// RecordStore returns the borrowed encrypted substrate for lifecycle adapters.
// The caller remains responsible for readiness and database pool lifetime.
func (r *Runtime) RecordStore() recordstore.Store { return r.recordStore }

// CheckoutRepository is billing's owning repository over this same substrate.
func (r *Runtime) CheckoutRepository() billing.CheckoutRepository { return r.checkoutRepository }

// Clock returns the exact clock shared by all owning services.
func (r *Runtime) Clock() partnermanager.Clock { return r.managerDependencies.Clock }

// ManagerWithAuthority builds a separate trusted worker facade over the same
// financial owners. It neither mutates the human facade nor creates grants.
// Human revenue-reporting configuration is deliberately absent: worker actions
// do not expose customer/operator reports. Do not attach this facade to HTTP.
func (r *Runtime) ManagerWithAuthority(authority partnermanager.Authority) (*partnermanager.Manager, error) {
	if r == nil || nilRuntimePort(authority) {
		return nil, partnermanager.ErrUnavailable
	}
	deps := r.managerDependencies
	deps.Authority = authority
	return partnermanager.NewManager(deps)
}

// validReservedKeys accepts 1..32 keys that are each independently valid and
// distinct from all earlier entries.
func validReservedKeys(keys [][]byte) bool {
	if len(keys) == 0 || len(keys) > 32 {
		return false
	}
	for i, key := range keys {
		if !independentPartnersKey(key, keys[:i]) {
			return false
		}
	}
	return true
}

// nilRuntimePort detects nil values including nil pointers stored in non-nil
// interfaces, which plain == nil misses.
func nilRuntimePort(port any) bool {
	if port == nil {
		return true
	}
	value := reflect.ValueOf(port)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
