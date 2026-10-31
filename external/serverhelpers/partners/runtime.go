package partners

import (
	"context"
	"crypto/rand"
	"reflect"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/billinglifecycle"
	billinglifecyclehelper "github.com/ooaklee/ghatd/external/billinglifecycle/helper"
	"github.com/ooaklee/ghatd/external/billingmanager"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	partnermanagerhelper "github.com/ooaklee/ghatd/external/partnermanager/helper"
	partnerhttp "github.com/ooaklee/ghatd/external/partnermanager/http"
	partnerruntime "github.com/ooaklee/ghatd/external/partnermanager/runtime"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.uber.org/zap"
)

// Config holds resolved host choices. It does not enable a feature implicitly.
type Config struct {
	Runtime                         partnerruntime.Config
	System                          string
	Identity                        partnermanager.UserIdentityConfig
	ActiveStatus, WorkerAccountType string
	PublicOrigin, ReferralPath      string
	AllowLocalHTTP                  bool
}

// Dependencies are existing live owners and host-selected admission policy.
type Dependencies struct {
	Users                  partnermanager.UserIdentityService
	Groups                 partneraccess.DirectGroups
	Policy, MemberPolicy   partneraccess.PolicyService
	Sessions               partneraccess.SessionAuthenticator
	Clock                  partnermanager.Clock
	IDs                    partnerprogram.IDGenerator
	Billing                *billingmanager.Service
	Providers              billingmanager.ProviderRegistry
	AccountAdmission       partneraccess.AccountAdmission
	AcquisitionEligibility partnermanager.AcquisitionEligibility
}

// Runtime owns composition and worker lifetime while borrowing the database.
type Runtime struct {
	*partnerruntime.Runtime
	API       *partnerhttp.Service
	Sessions  partneraccess.SessionVerifier
	Lifecycle *billinglifecyclehelper.Runtime
	Worker    *partnermanagerhelper.Execution
	mu        sync.Mutex
	cancel    context.CancelFunc
	done      []<-chan struct{}
	closed    bool
}

func absent(v any) bool {
	if v == nil {
		return true
	}
	x := reflect.ValueOf(v)
	switch x.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return x.IsNil()
	}
	return false
}

// BoundDependencies holds the admitted identity, cohorts and live session verifier.
type BoundDependencies struct {
	partnerruntime.Dependencies
	Sessions partneraccess.SessionVerifier
}

// BindDependencies validates and composes live access owners without database I/O.
func BindDependencies(ctx context.Context, cfg Config, deps Dependencies) (*BoundDependencies, error) {
	if ctx == nil || absent(deps.Users) || absent(deps.Groups) || absent(deps.Policy) || absent(deps.Sessions) || absent(deps.Clock) || absent(deps.IDs) || deps.AccountAdmission == nil {
		return nil, partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	memberPolicy := deps.Policy
	if deps.MemberPolicy != nil && absent(deps.MemberPolicy) {
		return nil, partnermanager.ErrUnavailable
	}
	if !absent(deps.MemberPolicy) {
		memberPolicy = deps.MemberPolicy
	}
	regions, err := partneraccess.NewRegions(cfg.System, memberPolicy)
	if err != nil {
		return nil, err
	}
	identity, err := partnermanager.NewUserIdentityAdapter(deps.Users, regions, cfg.Identity)
	if err != nil {
		return nil, err
	}
	groups, err := partneraccess.NewCohorts(cfg.System, deps.Groups, deps.Policy)
	if err != nil {
		return nil, err
	}
	sessions, err := partneraccess.NewMemberSessionVerifier(deps.Sessions, deps.AccountAdmission, cfg.ActiveStatus)
	if err != nil {
		return nil, err
	}
	authority, err := partneraccess.NewAuthority(cfg.System, sessions, memberPolicy)
	if err != nil {
		return nil, err
	}
	admitted, err := partnermanagerhelper.NewAccountIdentity(identity, deps.AccountAdmission)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &BoundDependencies{Dependencies: partnerruntime.Dependencies{Identity: admitted, Groups: groups, Authority: authority, Clock: deps.Clock, IDs: deps.IDs, AcquisitionEligibility: deps.AcquisitionEligibility}, Sessions: sessions}, nil
}

// NewRuntime prepares native owners with host authority and admission without starting workers.
func NewRuntime(ctx context.Context, db *mongo.Database, cfg Config, deps Dependencies) (*Runtime, error) {
	bound, err := BindDependencies(ctx, cfg, deps)
	if err != nil {
		return nil, err
	}
	owner, err := partnerruntime.NewRuntime(db, cfg.Runtime, bound.Dependencies)
	if err != nil {
		return nil, err
	}
	if err = owner.Prepare(ctx); err != nil {
		return nil, err
	}
	return CompleteRuntime(ctx, owner, cfg, deps, bound)
}

// CompleteRuntime assembles transport and workers over an already prepared owner.
// The caller must prepare and own the database; this starts no workers.
func CompleteRuntime(ctx context.Context, owner *partnerruntime.Runtime, cfg Config, deps Dependencies, bound *BoundDependencies) (*Runtime, error) {
	if ctx == nil || owner == nil || bound == nil || absent(bound.Sessions) || absent(bound.Authority) {
		return nil, partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var err error
	var workerIdentity partneraccess.WorkerIdentity
	if cfg.Runtime.Worker != nil || cfg.Runtime.Lifecycle != nil {
		workerIdentity, err = partneraccess.NewUserWorkerIdentity(deps.Users, deps.AccountAdmission, cfg.WorkerAccountType, cfg.ActiveStatus)
		if err != nil {
			return nil, err
		}
	}
	execution, err := partnermanagerhelper.NewExecution(ctx, partnermanagerhelper.ExecutionConfig{RevenueCapture: cfg.Runtime.RevenueCapture, Worker: cfg.Runtime.Worker, System: cfg.System, Payer: billingmanager.UserCheckoutPayerConfig{OwningAccountTypes: cfg.Identity.IndividualAccountTypes, ActiveAccountStatuses: cfg.Identity.ActiveAccountStatuses}}, partnermanagerhelper.ExecutionDependencies{Runtime: owner, Users: deps.Users, Billing: deps.Billing, Providers: deps.Providers, WorkerIdentity: workerIdentity, Policy: deps.Policy})
	if err != nil {
		return nil, err
	}
	var lifecycle *billinglifecyclehelper.Runtime
	if cfg.Runtime.Lifecycle != nil {
		policy := *cfg.Runtime.Lifecycle
		workerAuthority, e := partneraccess.NewLifecycleWorkerAuthority(cfg.System, policy.ActorID, workerIdentity, deps.Policy, policy.Scopes)
		if e != nil {
			return nil, e
		}
		lifecycle, e = billinglifecyclehelper.NewRuntime(ctx, policy, billinglifecyclehelper.Dependencies{Revenue: owner.Revenue, Records: owner.RecordStore(), Checkout: owner.CheckoutRepository(), Clock: owner.Clock(), Providers: deps.Providers, Authority: workerAuthority})
		if e != nil {
			return nil, e
		}
	}
	api, err := partnerhttp.NewService(partnerhttp.ServiceConfig{Manager: owner.Manager, Sessions: bound.Sessions, Authority: bound.Authority, PublicOrigin: cfg.PublicOrigin, ReferralPath: cfg.ReferralPath, AllowLocalHTTP: cfg.AllowLocalHTTP, Program: partnerhttp.ProgramView{TermsVersion: cfg.Runtime.Program.TermsVersion, EnrollmentEnabled: cfg.Runtime.Controls.Enrollment, ApprovalRequired: cfg.Runtime.Program.EnrollmentApprovalRequired, Currency: cfg.Runtime.Program.Currency, CurrencyExponent: cfg.Runtime.Program.CurrencyExponent, MinimumMinor: cfg.Runtime.Claims.MinimumMinor, ClaimsEnabled: cfg.Runtime.Controls.Claims}})
	if err != nil {
		return nil, err
	}
	return &Runtime{Runtime: owner, API: api, Sessions: bound.Sessions, Lifecycle: lifecycle, Worker: execution}, nil
}

// Start begins each configured sequential loop after successful composition.
func (r *Runtime) Start(ctx context.Context, logger *zap.Logger) error {
	if r == nil || ctx == nil {
		return partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return partnermanager.ErrUnavailable
	}
	if r.cancel != nil {
		return nil
	}
	if (r.Worker != nil && r.Worker.Interval() <= 0) || (r.Lifecycle != nil && (r.Lifecycle.Interval() <= 0 || r.Lifecycle.PassTimeout() <= 0)) {
		return partnermanager.ErrInvalid
	}
	work, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	if r.Worker != nil {
		done := make(chan struct{})
		r.done = append(r.done, done)
		go RunWorker(work, r.Worker, r.Worker.Interval(), done, logger)
	}
	if r.Lifecycle != nil {
		done := make(chan struct{})
		r.done = append(r.done, done)
		go RunLifecycle(work, r.Lifecycle, r.Lifecycle.Interval(), r.Lifecycle.PassTimeout(), done, logger)
	}
	return nil
}

// Close cancels and drains both workers; failure means borrowed stores must stay open.
func (r *Runtime) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	if ctx == nil {
		return partnermanager.ErrUnavailable
	}
	r.mu.Lock()
	r.closed = true
	cancel := r.cancel
	done := append([]<-chan struct{}(nil), r.done...)
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	for _, ch := range done {
		select {
		case <-ch:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// WorkerPass is the worker's sequential pass port.
type WorkerPass interface {
	RunOnce(context.Context) (partnermanager.WorkerReport, error)
}

// LifecyclePass is the financial lifecycle's sequential pass port.
type LifecyclePass interface {
	RunOnce(context.Context) (billinglifecycle.WorkerReport, error)
}

// RunWorker starts immediately and waits for the interval after each completed pass.
func RunWorker(ctx context.Context, worker WorkerPass, interval time.Duration, done chan struct{}, logger *zap.Logger) {
	defer close(done)
	if ctx == nil || absent(worker) || interval <= 0 {
		return
	}
	for ctx.Err() == nil {
		passLogger := logger
		passCtx := ctx
		if passLogger != nil {
			passLogger = passLogger.With(zap.String("worker_pass_id", rand.Text()))
			passCtx = ghatdlogger.TransitWith(passCtx, passLogger)
		}
		report, err := worker.RunOnce(passCtx)
		if logger != nil && ctx.Err() == nil {
			if err != nil {
				passLogger.Warn("partners-worker-pass-pending", zap.Int("discovered", report.Discovered), zap.Int("completed", report.Completed), zap.Int("retried", report.Retried), zap.Int("issues", len(report.Issues)), zap.Any("issue_counts", workerIssueCounts(report.Issues)))
			} else {
				passLogger.Debug("partners-worker-pass-completed", zap.Int("discovered", report.Discovered), zap.Int("completed", report.Completed), zap.Int("retried", report.Retried))
			}
		}
		if !wait(ctx, interval) {
			return
		}
	}
}

// RunLifecycle starts immediately, bounds each pass and waits after completion.
func RunLifecycle(ctx context.Context, worker LifecyclePass, interval, timeout time.Duration, done chan struct{}, logger *zap.Logger) {
	defer close(done)
	if ctx == nil || absent(worker) || interval <= 0 || timeout <= 0 {
		return
	}
	for ctx.Err() == nil {
		pass, cancel := context.WithTimeout(ctx, timeout)
		passLogger := logger
		if passLogger != nil {
			passLogger = passLogger.With(zap.String("worker_pass_id", rand.Text()))
			pass = ghatdlogger.TransitWith(pass, passLogger)
		}
		report, err := worker.RunOnce(pass)
		cancel()
		if logger != nil && ctx.Err() == nil {
			if err != nil {
				passLogger.Warn("partners-lifecycle-pass-pending")
			} else {
				passLogger.Debug("partners-lifecycle-pass-completed", zap.Int("discovered", report.Discovered), zap.Int("examined", report.Examined), zap.Int("conflicts", report.Conflicts), zap.Int("completed", report.Completed), zap.Int("pending", report.Pending), zap.Int("expired", report.Expired), zap.Int("retried", report.Retried), zap.Int("superseded", report.Superseded))
			}
		}
		if !wait(ctx, interval) {
			return
		}
	}
}
func workerIssueCounts(issues []partnermanager.WorkerIssue) map[string]int {
	counts := make(map[string]int)
	for _, issue := range issues {
		kind, code := "unknown", "unknown"
		switch issue.Kind {
		case partnermanager.WorkSignup, partnermanager.WorkRevenueSource, partnermanager.WorkRevenue, partnermanager.WorkMaturity:
			kind = issue.Kind
		}
		switch issue.Code {
		case "discovery_pending", "lease_pending", "lease_fenced", "work_receipt_pending", "source_conflict", "admission_or_evidence_pending", "review_pending", "owning_evidence_pending", "dependency_pending":
			code = issue.Code
		}
		counts[kind+"."+code]++
	}
	return counts
}
func wait(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
