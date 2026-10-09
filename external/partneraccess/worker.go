package partneraccess

import (
	"context"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// WorkerIdentity verifies the configured, currently active service account
// through the identity owner. A stored grant alone is not a live identity.
type WorkerIdentity interface {
	CheckWorkerIdentity(context.Context, string) error
}

type workerContextKey struct{}

// WorkerAuthority permits only the scheduler bound to this instance and the
// real configured service account. It never grants customer/operator actions,
// manufactures an account, or interprets a role as a permission.
type WorkerAuthority struct {
	system, actor   string
	identity        WorkerIdentity
	policy          PolicyService
	lifecycleScopes []billing.RevenueScope
}

func NewWorkerAuthority(system, actor string, identity WorkerIdentity, policy PolicyService) (*WorkerAuthority, error) {
	if !validID(system) || !validID(actor) || nilPort(identity) || nilPort(policy) {
		return nil, partnermanager.ErrUnavailable
	}
	return &WorkerAuthority{system: system, actor: actor, identity: identity, policy: policy}, nil
}

// Bind is an in-process scheduler boundary, never an HTTP authenticator. The
// private value is instance-bound; another worker authority cannot reuse it.
// A human session remains ineligible even when wrapped by trusted code.
func (a *WorkerAuthority) Bind(ctx context.Context) (context.Context, error) {
	if ctx == nil || a == nil {
		return nil, partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if ctx.Value(sessionKey{}) != nil {
		return nil, partnermanager.ErrDenied
	}
	return context.WithValue(ctx, workerContextKey{}, a), nil
}

func (a *WorkerAuthority) CheckPartners(ctx context.Context, actor, capability, target string) error {
	if ctx == nil {
		return partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || nilPort(a.identity) || nilPort(a.policy) {
		return partnermanager.ErrUnavailable
	}
	if ctx.Value(workerContextKey{}) != a || ctx.Value(sessionKey{}) != nil || actor != a.actor || !workerCapability(capability) || (target != "" && !validID(target)) {
		return partnermanager.ErrDenied
	}
	if err := a.identity.CheckWorkerIdentity(ctx, actor); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	// The account is a real user/v2 API-service identity. Use its existing user
	// subject namespace; neither invented subjects nor administrator grants apply.
	err := a.policy.Authorize(ctx, workerSubject(a.system, actor), []string{ProgramScope}, []string{capability})
	if err != nil {
		if soleDenial(err) {
			return partnermanager.ErrDenied
		}
		return err
	}
	if err := a.identity.CheckWorkerIdentity(ctx, actor); err != nil {
		return err
	}
	return ctx.Err()
}

func workerCapability(capability string) bool {
	switch capability {
	case partnermanager.CapabilitySignupWorker, partnermanager.CapabilityRevenueWorker, partnermanager.CapabilityMaturityWorker:
		return true
	}
	return false
}

func workerSubject(system, actor string) accesspolicy.Subject {
	return accesspolicy.Subject{System: system, Kind: accesspolicy.UserSubject, ID: actor}
}

func (a *WorkerAuthority) AuthorizeRevenueReconciliation(ctx context.Context, actor string) error {
	return a.CheckPartners(ctx, actor, partnermanager.CapabilityRevenueWorker, "")
}

var _ partnermanager.Authority = (*WorkerAuthority)(nil)
var _ billingmanager.RevenueReconciliationAuthority = (*WorkerAuthority)(nil)
