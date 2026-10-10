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

func TestWorkerLifecyclePreparationSeparateCurrentAuthority(t *testing.T) {
	outage := errors.New("policy unavailable")
	for _, tc := range []string{"global_prepare", "selected_prepare", "read_denied", "refresh_denied", "discovery_denied", "other_scope_denied", "other_mode_denied", "unbound_denied", "identity_revoked", "late_identity_revoked", "joined_outage", "all_scopes_required"} {
		t.Run(tc, func(t *testing.T) {
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_fixture"}
			grantScope, err := LifecycleScope(scope)
			require.NoError(t, err)
			scopes := []billing.RevenueScope{scope}
			if tc == "all_scopes_required" {
				scopes = append(scopes, billing.RevenueScope{Provider: "stripe", AccountID: "other"})
			}
			store := &grantStore{grant: accesspolicy.Grant{Subject: workerSubject("hostapp", "service-a"), Revision: 1, Enabled: true, Scopes: []string{grantScope}, Permissions: []string{LifecyclePreparation}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			identity := &workerIdentityFixture{}
			a, err := NewLifecycleWorkerAuthority("hostapp", "service-a", identity, policy, scopes)
			require.NoError(t, err)
			ctx, err := a.Bind(t.Context())
			require.NoError(t, err)
			var selected *billing.RevenueScope
			switch tc {
			case "selected_prepare":
				selected = &scope
			case "read_denied":
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRead}
			case "refresh_denied":
				store.grant.Permissions = []string{billingmanager.SubscriptionStatusRefresh}
			case "discovery_denied":
				store.grant.Permissions = []string{billingmanager.LifecycleDiscovery}
			case "other_scope_denied":
				scope.AccountID = "other"
				selected = &scope
			case "other_mode_denied":
				scope.LiveMode = true
				selected = &scope
			case "unbound_denied":
				ctx = context.Background()
			case "identity_revoked":
				identity.err = partnermanager.ErrDenied
			case "late_identity_revoked":
				store.onRead = func() { identity.err = partnermanager.ErrDenied }
			case "joined_outage":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			}
			err = a.AuthorizeLifecyclePreparation(ctx, "service-a", selected)
			if tc == "global_prepare" || tc == "selected_prepare" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if tc == "joined_outage" {
				require.ErrorIs(t, err, outage)
			}
			require.Zero(t, store.writes)
		})
	}
}
