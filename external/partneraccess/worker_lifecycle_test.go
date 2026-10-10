package partneraccess

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
)

func TestLifecycleWorkerAuthority(t *testing.T) {
	outage := errors.New("policy unavailable")
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"subscription_discovery_scope", "", nil}, {"subscription_discovery_selected", "selected", nil},
		{"checkout_discovery_selected", "checkout", nil}, {"status_read_selected", "read", nil}, {"status_refresh_selected", "refresh", nil},
		{"status_prelookup_all_scopes", "empty", nil}, {"all_scope_grants_required", "missing-second", partnermanager.ErrDenied},
		{"exact_selected_scope_grant", "selected-one-grant", nil}, {"wrong_merchant", "account", partnermanager.ErrDenied},
		{"wrong_provider", "provider", partnermanager.ErrDenied}, {"wrong_live_mode", "mode", partnermanager.ErrDenied},
		{"financial_grants_insufficient", "financial", partnermanager.ErrDenied}, {"read_cannot_refresh", "read-only", partnermanager.ErrDenied},
		{"refresh_cannot_read", "refresh-only", partnermanager.ErrDenied}, {"status_cannot_discover", "status-only", partnermanager.ErrDenied},
		{"unbound_context", "unbound", partnermanager.ErrDenied}, {"other_authority_context", "other", partnermanager.ErrDenied},
		{"human_session_context", "human", partnermanager.ErrDenied}, {"different_actor", "actor", partnermanager.ErrDenied},
		{"financial_constructor_no_lifecycle", "legacy", partnermanager.ErrDenied}, {"malformed_principal", "principal", partnermanager.ErrDenied},
		{"missing_subscription", "missing-sub", partnermanager.ErrDenied}, {"mixed_intent_subscription", "mixed", partnermanager.ErrDenied},
		{"scope_only_selected_status_invalid", "partial", partnermanager.ErrDenied}, {"unknown_kind", "kind", partnermanager.ErrDenied},
		{"unknown_action", "action", partnermanager.ErrDenied}, {"identity_revoked_before", "identity", partnermanager.ErrDenied},
		{"identity_revoked_during_policy", "revoke", partnermanager.ErrDenied}, {"policy_outage", "outage", outage},
		{"joined_denial_outage_preserved", "joined", outage}, {"cancel_before", "cancel", context.Canceled},
		{"cancel_during_policy", "cancel-during", context.Canceled}, {"nil_context", "nil", partnermanager.ErrDenied},
		{"nil_authority", "nil-authority", billing.ErrRevenueUnavailable},
		{"lifecycle_grants_cannot_process_financial_jobs", "reverse", partnermanager.ErrDenied},
		{"checkout_missing_intent", "missing-intent", partnermanager.ErrDenied},
		{"checkout_mixed_subscription", "checkout-mixed", partnermanager.ErrDenied},
		{"discover_cannot_read_status", "status-action", partnermanager.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "merchant-a"}
			second := scope
			second.LiveMode = true
			gs1, err := LifecycleScope(scope)
			require.NoError(t, err)
			gs2, err := LifecycleScope(second)
			require.NoError(t, err)
			store := &grantStore{grant: accesspolicy.Grant{Subject: workerSubject("hostapp", "service-a"), Revision: 1, Enabled: true, Scopes: []string{gs1, gs2}, Permissions: []string{billingmanager.LifecycleDiscovery, billingmanager.SubscriptionStatusRead, billingmanager.SubscriptionStatusRefresh}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			identity := &workerIdentityFixture{}
			scopes := []billing.RevenueScope{scope, second}
			a, err := NewLifecycleWorkerAuthority("hostapp", "service-a", identity, policy, scopes)
			require.NoError(t, err)
			// Mutating caller configuration cannot retarget the installed authority.
			scopes[0].AccountID = "replacement"
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			ctx, err = a.Bind(ctx)
			require.NoError(t, err)
			actor := "service-a"
			action := billingmanager.LifecycleDiscovery
			target := billingmanager.LifecycleDiscoveryTarget{Scope: scope, Kind: billing.LifecycleSubscriptionSources}
			status := billingmanager.SubscriptionStatusTarget{Scope: scope, PrincipalID: "payer-a", SubscriptionID: "sub-a"}
			useStatus := false
			switch tc.state {
			case "selected":
				target.PrincipalID = "payer-a"
				target.SubscriptionID = "sub-a"
			case "checkout":
				target.Kind = billing.LifecycleCheckoutSources
				target.PrincipalID = "payer-a"
				target.IntentID = "intent-a"
			case "read":
				useStatus = true
				action = billingmanager.SubscriptionStatusRead
			case "refresh":
				useStatus = true
				action = billingmanager.SubscriptionStatusRefresh
			case "empty", "missing-second":
				useStatus = true
				action = billingmanager.SubscriptionStatusRead
				status = billingmanager.SubscriptionStatusTarget{}
				if tc.state == "missing-second" {
					store.grant.Scopes = []string{gs1}
				}
			case "selected-one-grant":
				useStatus = true
				action = billingmanager.SubscriptionStatusRead
				store.grant.Scopes = []string{gs1}
			case "account":
				target.Scope.AccountID = "merchant-other"
			case "provider":
				target.Scope.Provider = "other"
			case "mode":
				target.Scope.LiveMode = true
				store.grant.Scopes = []string{gs1}
			case "financial":
				store.grant.Scopes = []string{ProgramScope}
				store.grant.Permissions = []string{partnermanager.CapabilityRevenueWorker}
			case "read-only":
				useStatus = true
				action = billingmanager.SubscriptionStatusRefresh
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRead}
			case "refresh-only":
				useStatus = true
				action = billingmanager.SubscriptionStatusRead
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRefresh}
			case "status-only":
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRead, billingmanager.SubscriptionStatusRefresh}
			case "unbound":
				ctx = t.Context()
			case "other":
				other, e := NewLifecycleWorkerAuthority("hostapp", "service-a", identity, policy, []billing.RevenueScope{scope})
				require.NoError(t, e)
				ctx, e = other.Bind(t.Context())
				require.NoError(t, e)
			case "human":
				ctx = context.WithValue(ctx, sessionKey{}, true)
			case "actor":
				actor = "payer-a"
			case "legacy":
				a, err = NewWorkerAuthority("hostapp", "service-a", identity, policy)
				require.NoError(t, err)
				ctx, err = a.Bind(t.Context())
				require.NoError(t, err)
			case "principal":
				target.PrincipalID = "payer\n"
				target.SubscriptionID = "sub-a"
			case "missing-sub":
				target.PrincipalID = "payer-a"
			case "mixed":
				target.PrincipalID = "payer-a"
				target.SubscriptionID = "sub-a"
				target.IntentID = "intent-a"
			case "partial":
				useStatus = true
				action = billingmanager.SubscriptionStatusRead
				status.PrincipalID = ""
				status.SubscriptionID = ""
			case "kind":
				target.Kind = "other"
			case "action":
				action = partnermanager.CapabilityRevenueWorker
			case "identity":
				identity.err = partnermanager.ErrDenied
			case "revoke":
				store.onRead = func() { identity.err = partnermanager.ErrDenied }
			case "outage":
				store.err = outage
			case "joined":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "cancel":
				cancel()
			case "cancel-during":
				store.onRead = cancel
			case "nil":
				ctx = nil
			case "nil-authority":
				a = nil
			case "missing-intent":
				target.Kind = billing.LifecycleCheckoutSources
				target.PrincipalID = "payer-a"
			case "checkout-mixed":
				target.Kind = billing.LifecycleCheckoutSources
				target.PrincipalID = "payer-a"
				target.IntentID = "intent-a"
				target.SubscriptionID = "sub-a"
			case "status-action":
				useStatus = true

			}
			if tc.state == "reverse" {
				err = a.CheckPartners(ctx, actor, partnermanager.CapabilityRevenueWorker, "")
			} else if useStatus {
				err = a.AuthorizeSubscriptionStatus(ctx, actor, action, status)
			} else {
				err = a.AuthorizeLifecycleDiscovery(ctx, actor, action, target)
			}
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, store.writes)
			if tc.want == nil {
				require.Equal(t, 2, identity.calls)
				require.Equal(t, 1, store.reads)
			}
			if tc.state == "joined" {
				require.ErrorIs(t, err, accesspolicy.ErrDenied)
				require.NotErrorIs(t, err, partnermanager.ErrDenied)
			}
		})
	}
}

func TestLifecycleWorkerConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, state string }{{"empty", "empty"}, {"duplicate", "duplicate"}, {"invalid_provider", "provider"}, {"invalid_account", "account"}, {"too_many_native_scopes", "many"}} {
		t.Run(tc.name, func(t *testing.T) {
			scopes := []billing.RevenueScope{{Provider: "stripe", AccountID: "merchant-a"}}
			switch tc.state {
			case "empty":
				scopes = nil
			case "duplicate":
				scopes = append(scopes, scopes[0])
			case "provider":
				scopes[0].Provider = ""
			case "account":
				scopes[0].AccountID = "a\n"
			case "many":
				scopes = nil
				for i := 0; i < 11; i++ {
					scopes = append(scopes, billing.RevenueScope{Provider: "stripe", AccountID: fmt.Sprintf("account-%d", i)})
				}
			}
			policy, e := accesspolicy.NewService(&grantStore{}, nil)
			require.NoError(t, e)
			a, e := NewLifecycleWorkerAuthority("hostapp", "service-a", &workerIdentityFixture{}, policy, scopes)
			require.ErrorIs(t, e, billing.ErrRevenueInvalid)
			require.Nil(t, a)
		})
	}
}
