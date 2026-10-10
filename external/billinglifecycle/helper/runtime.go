package billinglifecyclehelper

import (
	"context"
	"reflect"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/billingmanager"
	billingmanagerhelper "github.com/ooaklee/ghatd/external/billingmanager/helper"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// Authority binds a trusted service invocation and checks every lifecycle action.
// It must be the same current worker authority supplied to the pipeline; a
// human HTTP facade must never implement the binding by granting worker access.
type Authority interface {
	billinglifecycle.ExecutionAuthority
	billingmanager.LifecycleDiscoveryAuthority
	// Bind returns a context carrying the trusted bound worker invocation for
	// checking lifecycle actions; implementations enforce current authority rather
	// than granting worker access from a human facade.
	Bind(context.Context) (context.Context, error)
}

// Dependencies borrows prepared native owners, providers and current authority.
// A separate billing manager is built over these owners; no human facade is mutated.
type Dependencies struct {
	Revenue   *billing.RevenueService
	Records   recordstore.Store
	Checkout  billing.CheckoutRepository
	Clock     billinglifecycle.LifecycleClock
	Providers billingmanager.ProviderRegistry
	Authority Authority
}

// Runtime binds each explicit pass to its trusted worker authority. The host
// supplies cadence, per-pass deadline, cancellation and borrowed-resource drain.
type Runtime struct {
	worker                *billinglifecycle.Worker
	authority             Authority
	interval, passTimeout time.Duration
}

// RunOnce binds the runtime's trusted worker authority to the context before
// running one bounded worker pass. Nil runtime or missing authority returns an
// empty report with ErrRevenueUnavailable.
func (w *Runtime) RunOnce(ctx context.Context) (billinglifecycle.WorkerReport, error) {
	if w == nil || w.worker == nil || nilPort(w.authority) {
		return billinglifecycle.WorkerReport{}, billing.ErrRevenueUnavailable
	}
	bound, err := w.authority.Bind(ctx)
	if err != nil {
		return billinglifecycle.WorkerReport{}, err
	}
	return w.worker.RunOnce(bound)
}

// NewRuntime builds a separate worker manager over prepared native owners.
// Bounded current-grant/discovery probes and registry capability checks occur
// during construction. It never prepares storage, looks up remote provider
// evidence, replaces human authority, creates grants or starts a goroutine.
func NewRuntime(ctx context.Context, policy billinglifecycle.RuntimeConfig, deps Dependencies) (*Runtime, error) {
	if ctx == nil || deps.Revenue == nil || nilPort(deps.Records) || nilPort(deps.Checkout) || nilPort(deps.Clock) || nilPort(deps.Providers) || nilPort(deps.Authority) {
		return nil, billing.ErrRevenueUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	policy.Scopes = append([]billing.RevenueScope(nil), policy.Scopes...)
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	ctx, finishStartup := context.WithTimeout(ctx, policy.PassTimeout)
	defer finishStartup()
	revenueRegistry, ok := deps.Providers.(billingmanager.RevenueProviderRegistry)
	if !ok || nilPort(revenueRegistry) {
		return nil, billing.ErrRevenueUnavailable
	}
	checkoutRegistry, ok := deps.Providers.(billingmanager.CheckoutProviderRegistry)
	if !ok || nilPort(checkoutRegistry) {
		return nil, billing.ErrRevenueUnavailable
	}
	authority := deps.Authority
	bound, err := authority.Bind(ctx)
	if err != nil {
		return nil, err
	}
	// Current scope grants are mandatory even when discovery is currently empty.
	if err = authority.AuthorizeSubscriptionStatus(bound, policy.ActorID, billingmanager.SubscriptionStatusRefresh, billingmanager.SubscriptionStatusTarget{}); err != nil {
		return nil, err
	}
	if err = authority.AuthorizeSubscriptionStatus(bound, policy.ActorID, billingmanager.SubscriptionStatusRead, billingmanager.SubscriptionStatusTarget{}); err != nil {
		return nil, err
	}
	for _, scope := range policy.Scopes {
		provider, e := revenueRegistry.GetRevenueProvider(scope.Provider)
		if e != nil {
			return nil, e
		}
		if _, ok := provider.(paymentprovider.RevenueSubscriptionProvider); !ok || nilPort(provider) {
			return nil, billing.ErrRevenueUnavailable
		}
		checkout, e := checkoutRegistry.GetCheckoutProvider(scope.Provider)
		if e != nil {
			return nil, e
		}
		if _, ok := checkout.(paymentprovider.RevenueCheckoutSessionEvidenceProvider); !ok || nilPort(checkout) {
			return nil, billing.ErrRevenueUnavailable
		}
	}
	clock := deps.Clock
	checkout, err := billing.NewCheckoutService(deps.Checkout, clock, billingmanagerhelper.NewCheckoutEvidence(checkoutRegistry))
	if err != nil {
		return nil, err
	}
	manager, err := (&billingmanager.Service{}).WithRevenueServices(revenueRegistry, deps.Revenue, checkout)
	if err != nil {
		return nil, err
	}
	if _, err = manager.WithSubscriptionStatusAuthority(authority); err != nil {
		return nil, err
	}
	if _, err = manager.WithCheckoutLifecycleAuthority(authority); err != nil {
		return nil, err
	}
	if _, err = manager.WithLifecycleDiscoveryAuthority(authority); err != nil {
		return nil, err
	}
	repository, err := billinglifecycle.NewRecordExecutionRepository(deps.Records, clock)
	if err != nil {
		return nil, err
	}
	scheduler, err := billinglifecycle.NewScheduler(repository, authority, clock, billinglifecycle.SchedulerConfig{ActorID: policy.ActorID, Scopes: policy.Scopes, PageLimit: policy.PageSize, ColdBudget: policy.ColdBudget, RefreshBudget: policy.RefreshBudget, LeaseDuration: policy.Lease, RetryBase: policy.RetryBase, RetryMax: policy.RetryMax})
	if err != nil {
		return nil, err
	}
	discovery, err := billinglifecycle.NewDiscoveryCollector(repository, manager, authority, clock, billinglifecycle.DiscoveryConfig{ActorID: policy.ActorID, Scopes: policy.Scopes, PageLimit: policy.PageSize})
	if err != nil {
		return nil, err
	}
	checkoutOutbox, err := billinglifecycle.NewCheckoutOutbox(deps.Records, manager)
	if err != nil {
		return nil, err
	}
	statusOutbox, err := billinglifecycle.NewStatusOutbox(deps.Records, manager)
	if err != nil {
		return nil, err
	}
	checkoutExecution, err := billinglifecycle.NewCheckoutExecution(scheduler, repository, checkoutOutbox)
	if err != nil {
		return nil, err
	}
	statusExecution, err := billinglifecycle.NewStatusExecution(scheduler, repository, statusOutbox)
	if err != nil {
		return nil, err
	}
	checkoutCompletion, err := billinglifecycle.NewCheckoutCompletion(checkoutExecution, repository)
	if err != nil {
		return nil, err
	}
	statusCompletion, err := billinglifecycle.NewStatusCompletion(statusExecution, repository, policy.Cadence)
	if err != nil {
		return nil, err
	}
	resolver, err := billinglifecycle.NewStatusOriginalResolver(statusExecution, repository)
	if err != nil {
		return nil, err
	}
	worker, err := billinglifecycle.NewWorker(discovery, scheduler, checkoutExecution, statusExecution, checkoutCompletion, statusCompletion, resolver)
	if err != nil {
		return nil, err
	}
	// Bounded read-only probes prove explicit owning preparation and current
	// discovery authority. They cannot prove provider availability or writer drain.
	startup := bound // Shares the total startup deadline, including identity/grant checks.
	for _, scope := range policy.Scopes {
		for _, kind := range []string{billing.LifecycleCheckoutSources, billing.LifecycleSubscriptionSources} {
			q := billing.LifecycleDiscoveryQuery{Scope: scope, Kind: kind, Limit: 1}
			page, e := manager.DiscoverLifecycleSources(startup, policy.ActorID, q)
			if e != nil {
				return nil, e
			}
			if e = page.Validate(q); e != nil {
				return nil, e
			}
		}
	}
	if err = authority.AuthorizeSubscriptionStatus(startup, policy.ActorID, billingmanager.SubscriptionStatusRead, billingmanager.SubscriptionStatusTarget{}); err != nil {
		return nil, err
	}
	if err = authority.AuthorizeSubscriptionStatus(startup, policy.ActorID, billingmanager.SubscriptionStatusRefresh, billingmanager.SubscriptionStatusTarget{}); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return &Runtime{worker: worker, authority: authority, interval: policy.Interval, passTimeout: policy.PassTimeout}, nil
}

// Interval returns validated host cadence, without starting a scheduler.
func (r *Runtime) Interval() time.Duration {
	if r == nil {
		return 0
	}
	return r.interval
}

// PassTimeout is the validated bound the caller applies to each RunOnce context.
func (r *Runtime) PassTimeout() time.Duration {
	if r == nil {
		return 0
	}
	return r.passTimeout
}

// nilPort detects nil values behind interfaces and the nilable reflect kinds,
// so typed-nil ports are not mistaken for present dependencies.
func nilPort(port any) bool {
	if port == nil {
		return true
	}
	value := reflect.ValueOf(port)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}
