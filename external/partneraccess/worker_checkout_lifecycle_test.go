package partneraccess

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
)

func TestWorkerCheckoutLifecycleAuthority(t *testing.T) {
	outage := errors.New("identity/policy unavailable")
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"current_scoped_refresh", "", nil}, {"read_grant_cannot_complete", "read-grant", partnermanager.ErrDenied},
		{"discovery_grant_cannot_complete", "discover-grant", partnermanager.ErrDenied}, {"read_action_denied", "read-action", partnermanager.ErrDenied},
		{"discovery_action_denied", "discover-action", partnermanager.ErrDenied}, {"empty_principal", "principal", partnermanager.ErrDenied},
		{"empty_intent", "intent", partnermanager.ErrDenied}, {"malformed_intent", "bad-intent", partnermanager.ErrDenied},
		{"other_merchant", "account", partnermanager.ErrDenied}, {"other_provider", "provider", partnermanager.ErrDenied},
		{"other_live_mode", "mode", partnermanager.ErrDenied}, {"unbound_invocation", "unbound", partnermanager.ErrDenied},
		{"human_session", "human", partnermanager.ErrDenied}, {"other_authority_invocation", "other", partnermanager.ErrDenied},
		{"payer_cannot_impersonate_worker", "actor", partnermanager.ErrDenied}, {"identity_revoked_during_policy", "revoke", partnermanager.ErrDenied},
		{"cancel_during_policy", "cancel", context.Canceled}, {"joined_denial_outage_preserved", "joined", outage},
		{"nil_context", "nil", partnermanager.ErrDenied}, {"nil_authority", "nil-authority", billing.ErrRevenueUnavailable},
		{"financial_constructor_no_completion_scope", "legacy", partnermanager.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "merchant-a"}
			grant, err := LifecycleScope(scope)
			require.NoError(t, err)
			store := &grantStore{grant: accesspolicy.Grant{Subject: workerSubject("hostapp", "service-a"), Revision: 1, Enabled: true, Scopes: []string{grant}, Permissions: []string{billingmanager.SubscriptionStatusRefresh}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			identity := &workerIdentityFixture{}
			a, err := NewLifecycleWorkerAuthority("hostapp", "service-a", identity, policy, []billing.RevenueScope{scope})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			ctx, err = a.Bind(ctx)
			require.NoError(t, err)
			actor := "service-a"
			action := billingmanager.SubscriptionStatusRefresh
			target := billingmanager.CheckoutLifecycleTarget{Scope: scope, PrincipalID: "payer-a", IntentID: "checkout-original"}
			switch tc.state {
			case "read-grant":
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRead}
			case "discover-grant":
				store.grant.Permissions = []string{billingmanager.LifecycleDiscovery}
			case "read-action":
				action = billingmanager.SubscriptionStatusRead
			case "discover-action":
				action = billingmanager.LifecycleDiscovery
			case "principal":
				target.PrincipalID = ""
			case "intent":
				target.IntentID = ""
			case "bad-intent":
				target.IntentID = "intent\n"
			case "account":
				target.Scope.AccountID = "other"
			case "provider":
				target.Scope.Provider = "other"
			case "mode":
				target.Scope.LiveMode = true
			case "unbound":
				ctx = t.Context()
			case "human":
				ctx = context.WithValue(ctx, sessionKey{}, true)
			case "other":
				other, e := NewLifecycleWorkerAuthority("hostapp", "service-a", identity, policy, []billing.RevenueScope{scope})
				require.NoError(t, e)
				ctx, e = other.Bind(t.Context())
				require.NoError(t, e)
			case "actor":
				actor = target.PrincipalID
			case "revoke":
				store.onRead = func() { identity.err = partnermanager.ErrDenied }
			case "cancel":
				store.onRead = cancel
			case "joined":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "nil":
				ctx = nil
			case "nil-authority":
				a = nil
			case "legacy":
				a, err = NewWorkerAuthority("hostapp", "service-a", identity, policy)
				require.NoError(t, err)
				ctx, err = a.Bind(t.Context())
				require.NoError(t, err)
			}
			err = a.AuthorizeCheckoutLifecycle(ctx, actor, action, target)
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
