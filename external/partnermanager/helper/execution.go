package partnermanagerhelper

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	billingmanagerhelper "github.com/ooaklee/ghatd/external/billingmanager/helper"
	partnerruntime "github.com/ooaklee/ghatd/external/partnermanager/runtime"
)

// ExecutionConfig binds explicit billing capture and service-worker controls.
// Payer account types/statuses and the privileged policy system are host choices.
type ExecutionConfig struct {
	RevenueCapture bool
	Worker         *partnerruntime.WorkerConfig
	Payer          billingmanager.UserCheckoutPayerConfig
	System         string
}

// ExecutionDependencies borrows already prepared native financial owners.
// WorkerIdentity and Policy are required only when an explicit worker is enabled.
type ExecutionDependencies struct {
	Runtime        *partnerruntime.Runtime
	Users          billingmanager.CheckoutPayerUserService
	Providers      billingmanager.ProviderRegistry
	Billing        *billingmanager.Service
	WorkerIdentity partneraccess.WorkerIdentity
	Policy         partneraccess.PolicyService
}

// Execution owns one trusted bind-per-pass worker over the same financial owners.
// Construction creates no grant or account and starts no goroutine or pass.
type Execution struct {
	worker    *partnermanager.Worker
	authority *partneraccess.WorkerAuthority
	interval  time.Duration
}

func (w *Execution) RunOnce(ctx context.Context) (partnermanager.WorkerReport, error) {
	if w == nil || w.worker == nil || w.authority == nil {
		return partnermanager.WorkerReport{}, partnermanager.ErrUnavailable
	}
	bound, err := w.authority.Bind(ctx)
	if err != nil {
		return partnermanager.WorkerReport{}, err
	}
	return w.worker.RunOnce(bound)
}

// Configure before handlers or goroutines. All financial state stays in the
// already prepared native store. A second manager changes authority only; it
// uses the same owning services as customer and operator transport.
func NewExecution(ctx context.Context, cfg ExecutionConfig, owners ExecutionDependencies) (*Execution, error) {
	if !cfg.RevenueCapture && cfg.Worker == nil {
		return nil, nil
	}
	if cfg.Worker != nil {
		workerConfig := *cfg.Worker
		cfg.Worker = &workerConfig
	}
	runtime := owners.Runtime
	if ctx == nil || runtime == nil || owners.Billing == nil || nilHelperPort(owners.Providers) || nilHelperPort(owners.Users) || nilHelperPort(runtime.CheckoutRepository()) {
		return nil, partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	revenueRegistry, ok := owners.Providers.(billingmanager.RevenueProviderRegistry)
	if !ok || nilHelperPort(revenueRegistry) {
		return nil, billing.ErrRevenueUnavailable
	}
	checkoutRegistry, ok := owners.Providers.(billingmanager.CheckoutProviderRegistry)
	if !ok || nilHelperPort(checkoutRegistry) {
		return nil, billing.ErrRevenueUnavailable
	}
	checkout, err := billing.NewCheckoutService(runtime.CheckoutRepository(), runtime.Clock(), billingmanagerhelper.NewCheckoutEvidence(checkoutRegistry))
	if err != nil {
		return nil, err
	}
	identityConfig := cfg.Payer
	payer, err := billingmanager.NewUserCheckoutPayerAuthority(owners.Users, identityConfig)
	if err != nil {
		return nil, err
	}
	var workerRuntime *Execution
	if cfg.Worker != nil {
		if cfg.Worker.Interval < time.Second || cfg.Worker.Interval > time.Hour {
			return nil, partnermanager.ErrInvalid
		}
		authority, err := partneraccess.NewWorkerAuthority(cfg.System, cfg.Worker.Native.ActorID, owners.WorkerIdentity, owners.Policy)
		if err != nil {
			return nil, err
		}
		manager, err := runtime.ManagerWithAuthority(authority)
		if err != nil {
			return nil, err
		}
		signups, ok := owners.Users.(partnermanager.SignupFeed)
		if !ok || nilHelperPort(signups) {
			return nil, partnermanager.ErrUnavailable
		}
		worker, err := partnermanager.NewWorker(manager, runtime.Work, signups, runtime.Revenue, owners.Billing, cfg.Worker.Native)
		if err != nil {
			return nil, err
		}
		bound, err := authority.Bind(ctx)
		if err != nil {
			return nil, err
		}
		for _, capability := range []string{partnermanager.CapabilitySignupWorker, partnermanager.CapabilityRevenueWorker, partnermanager.CapabilityMaturityWorker} {
			if err := authority.CheckPartners(bound, cfg.Worker.Native.ActorID, capability, ""); err != nil {
				return nil, err
			}
		}
		workerRuntime = &Execution{worker: worker, authority: authority, interval: cfg.Worker.Interval}
	}
	if _, err := owners.Billing.WithRevenueServices(revenueRegistry, runtime.Revenue, checkout); err != nil {
		return nil, err
	}
	if cfg.RevenueCapture {
		if _, err := owners.Billing.WithCheckoutRevenueCapture(checkout, payer); err != nil {
			return nil, err
		}
	}
	if workerRuntime != nil {
		if _, err := owners.Billing.WithRevenueReconciliationAuthority(workerRuntime.authority); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return workerRuntime, nil
}

// Interval is the validated host cadence; the host owns scheduling and shutdown.
func (w *Execution) Interval() time.Duration {
	if w == nil {
		return 0
	}
	return w.interval
}
