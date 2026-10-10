package partneraccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// LifecycleScope identifies an explicit provider/account/mode grant. It creates
// no permission and cannot be substituted by a financial program grant.
func LifecycleScope(scope billing.RevenueScope) (string, error) {
	if billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{scope}) != nil {
		return "", billing.ErrRevenueInvalid
	}
	raw, err := json.Marshal([]any{"partners.lifecycle.scope.v1", scope.Provider, scope.AccountID, scope.LiveMode})
	if err != nil {
		return "", billing.ErrRevenueInvalid
	}
	digest := sha256.Sum256(raw)
	return "partners.lifecycle." + hex.EncodeToString(digest[:]), nil
}

// NewLifecycleWorkerAuthority adds lifecycle permissions for an explicit native
// scope set. Configuration and grants remain independent; no account is created.
// The configured set is copied and immutable after construction.
func NewLifecycleWorkerAuthority(system, actor string, identity WorkerIdentity, policy PolicyService, scopes []billing.RevenueScope) (*WorkerAuthority, error) {
	if err := billing.ValidateRevenueHistoryScopes(scopes); err != nil {
		return nil, err
	}
	a, err := NewWorkerAuthority(system, actor, identity, policy)
	if err != nil {
		return nil, err
	}
	a.lifecycleScopes = append([]billing.RevenueScope(nil), scopes...)
	return a, nil
}

// AuthorizeLifecycleDiscovery checks the source shape from the owning manager
// and the exact configured scope. Selected principals never become the actor.
func (a *WorkerAuthority) AuthorizeLifecycleDiscovery(ctx context.Context, actor, action string, target billingmanager.LifecycleDiscoveryTarget) error {
	if action != billingmanager.LifecycleDiscovery || (target.Kind != billing.LifecycleCheckoutSources && target.Kind != billing.LifecycleSubscriptionSources) {
		return partnermanager.ErrDenied
	}
	if target.PrincipalID == "" {
		if target.IntentID != "" || target.SubscriptionID != "" {
			return partnermanager.ErrDenied
		}
	} else {
		if !validID(target.PrincipalID) {
			return partnermanager.ErrDenied
		}
		if target.Kind == billing.LifecycleCheckoutSources {
			if !validID(target.IntentID) || target.SubscriptionID != "" {
				return partnermanager.ErrDenied
			}
		} else if !validID(target.SubscriptionID) || target.IntentID != "" {
			return partnermanager.ErrDenied
		}
	}
	return a.checkLifecycle(ctx, actor, action, &target.Scope)
}

// AuthorizeSubscriptionStatus independently enforces read or refresh on every
// manager stage. An empty pre-lookup target requires ALL configured scope grants;
// selected stages additionally constrain the native payer/subscription shape.
func (a *WorkerAuthority) AuthorizeSubscriptionStatus(ctx context.Context, actor, action string, target billingmanager.SubscriptionStatusTarget) error {
	if action != billingmanager.SubscriptionStatusRead && action != billingmanager.SubscriptionStatusRefresh {
		return partnermanager.ErrDenied
	}
	if target == (billingmanager.SubscriptionStatusTarget{}) {
		return a.checkLifecycle(ctx, actor, action, nil)
	}
	if !validID(target.PrincipalID) || !validID(target.SubscriptionID) {
		return partnermanager.ErrDenied
	}
	return a.checkLifecycle(ctx, actor, action, &target.Scope)
}

// checkLifecycle enforces the bound scheduler context, selected scope
// membership and at least one configured lifecycle grant, verifying live worker
// identity before and after the policy authorize call. Sole policy denials
// become ErrDenied; other errors, including context errors, are returned
// unchanged.
func (a *WorkerAuthority) checkLifecycle(ctx context.Context, actor, action string, selected *billing.RevenueScope) error {
	if ctx == nil {
		return partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || nilPort(a.identity) || nilPort(a.policy) {
		return billing.ErrRevenueUnavailable
	}
	if ctx.Value(workerContextKey{}) != a || ctx.Value(sessionKey{}) != nil || actor != a.actor || len(a.lifecycleScopes) == 0 {
		return partnermanager.ErrDenied
	}
	var grants []string
	for _, scope := range a.lifecycleScopes {
		if selected != nil && scope != *selected {
			continue
		}
		grant, err := LifecycleScope(scope)
		if err != nil {
			return err
		}
		grants = append(grants, grant)
	}
	if len(grants) == 0 {
		return partnermanager.ErrDenied
	}
	if err := a.identity.CheckWorkerIdentity(ctx, actor); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.policy.Authorize(ctx, workerSubject(a.system, actor), grants, []string{action}); err != nil {
		if soleDenial(err) {
			return partnermanager.ErrDenied
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := a.identity.CheckWorkerIdentity(ctx, actor); err != nil {
		return err
	}
	return ctx.Err()
}

var _ billingmanager.LifecycleDiscoveryAuthority = (*WorkerAuthority)(nil)
var _ billingmanager.SubscriptionStatusAuthority = (*WorkerAuthority)(nil)

// AuthorizeCheckoutLifecycle confines acknowledged checkout completion to the
// current scoped refresh grant. Discovery/read and the original payer cannot
// supply worker authority; every native stage checks the current invocation.
func (a *WorkerAuthority) AuthorizeCheckoutLifecycle(ctx context.Context, actor, action string, target billingmanager.CheckoutLifecycleTarget) error {
	if action != billingmanager.SubscriptionStatusRefresh || !validID(target.PrincipalID) || !validID(target.IntentID) {
		return partnermanager.ErrDenied
	}
	return a.checkLifecycle(ctx, actor, action, &target.Scope)
}

var _ billingmanager.CheckoutLifecycleAuthority = (*WorkerAuthority)(nil)
