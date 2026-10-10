package partneraccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
)

// Real access-policy enforcement with a narrow mutable identity boundary. The
// host integration suite independently exercises actual service-account reads.
type workerIdentityFixture struct {
	calls int
	err   error
}

func (i *workerIdentityFixture) CheckWorkerIdentity(ctx context.Context, actor string) error {
	i.calls++
	if i.err != nil {
		return i.err
	}
	if actor != "service-a" {
		return partnermanager.ErrDenied
	}
	return ctx.Err()
}

func TestWorkerAuthorityCurrentServiceAndPrivateInvocation(t *testing.T) {
	outage := errors.New("private dependency unavailable")
	type testCase struct {
		name, state, action, target string
		want                        error
	}
	for _, tc := range []testCase{
		{name: "signup_program_discovery", action: partnermanager.CapabilitySignupWorker},
		{name: "signup_selected_source", action: partnermanager.CapabilitySignupWorker, target: "capture-a"},
		{name: "revenue_program_discovery", action: partnermanager.CapabilityRevenueWorker},
		{name: "revenue_selected_source", action: partnermanager.CapabilityRevenueWorker, target: "fact-a"},
		{name: "maturity_program_discovery", action: partnermanager.CapabilityMaturityWorker},
		{name: "maturity_selected_owner", action: partnermanager.CapabilityMaturityWorker, target: "partner-a"},
		{name: "reconciliation_uses_revenue_permission", state: "reconcile"},
		{name: "unbound_background_cannot_claim_service", state: "unbound", want: partnermanager.ErrDenied},
		{name: "human_session_cannot_bind", state: "human-bind", want: partnermanager.ErrDenied},
		{name: "human_session_added_after_binding_denied", state: "human-after", want: partnermanager.ErrDenied},
		{name: "another_authority_instance_cannot_reuse_context", state: "instance", want: partnermanager.ErrDenied},
		{name: "different_actor_denied", state: "actor", want: partnermanager.ErrDenied},
		{name: "customer_action_denied", action: partnermanager.CapabilitySelf, target: "service-a", want: partnermanager.ErrDenied},
		{name: "operator_recording_denied", action: partnermanager.CapabilityRecordPayment, target: "claim-a", want: partnermanager.ErrDenied},
		{name: "unknown_worker_action_denied", action: "partner.worker.other", want: partnermanager.ErrDenied},
		{name: "invalid_target_denied", target: "capture with spaces", want: partnermanager.ErrDenied},
		{name: "unbounded_target_denied", target: strings.Repeat("x", 257), want: partnermanager.ErrDenied},
		{name: "service_revoked_before_policy", state: "identity-denied", want: partnermanager.ErrDenied},
		{name: "identity_outage_preserved", state: "identity-outage", want: outage},
		{name: "missing_exact_worker_permission", state: "missing-permission", want: partnermanager.ErrDenied},
		{name: "generic_admin_permission_insufficient", state: "admin", want: partnermanager.ErrDenied},
		{name: "self_scope_insufficient", state: "self-scope", want: partnermanager.ErrDenied},
		{name: "disabled_grant_denied", state: "disabled", want: partnermanager.ErrDenied},
		{name: "expired_grant_denied", state: "expired", want: partnermanager.ErrDenied},
		{name: "grant_for_another_system_denied", state: "system", want: partnermanager.ErrDenied},
		{name: "credential_grant_not_borrowed", state: "token", want: partnermanager.ErrDenied},
		{name: "wrapped_sole_denial_normalized", state: "wrapped-denial", want: partnermanager.ErrDenied},
		{name: "joined_denial_outage_remains_outage", state: "joined-denial", want: outage},
		{name: "revocation_during_policy_read_denied", state: "revoked-during", want: partnermanager.ErrDenied},
		{name: "cancellation_during_policy_read_preserved", state: "cancel-during", want: context.Canceled},
		{name: "already_cancelled_context_denied", state: "cancelled", want: context.Canceled},
		{name: "nil_context_denied", state: "nil", want: partnermanager.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := &workerIdentityFixture{}
			store := &grantStore{grant: accesspolicy.Grant{Subject: workerSubject("hostapp", "service-a"), Revision: 1, Enabled: true, Scopes: []string{ProgramScope}, Permissions: []string{partnermanager.CapabilitySignupWorker, partnermanager.CapabilityRevenueWorker, partnermanager.CapabilityMaturityWorker}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			a, err := NewWorkerAuthority("hostapp", "service-a", identity, policy)
			require.NoError(t, err)
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			ctx, err := a.Bind(base)
			require.NoError(t, err)
			actor, action := "service-a", tc.action
			if action == "" {
				action = partnermanager.CapabilityRevenueWorker
			}
			switch tc.state {
			case "unbound":
				ctx = base
			case "human-bind":
				human, err := WithVerifiedSession(base, actor, "fixture-credential")
				require.NoError(t, err)
				_, err = a.Bind(human)
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, store.reads)
				return
			case "human-after":
				ctx, err = WithVerifiedSession(ctx, actor, "fixture-credential")
				require.NoError(t, err)
			case "instance":
				a, err = NewWorkerAuthority("hostapp", actor, identity, policy)
				require.NoError(t, err)
			case "actor":
				actor = "service-b"
			case "identity-denied":
				identity.err = partnermanager.ErrDenied
			case "identity-outage":
				identity.err = outage
			case "missing-permission":
				store.grant.Permissions = []string{partnermanager.CapabilitySignupWorker}
			case "admin":
				store.grant.Permissions = []string{"ADMIN"}
			case "self-scope":
				store.grant.Scopes = []string{SelfScope}
			case "disabled":
				store.grant.Enabled = false
			case "expired":
				store.grant.ExpiresAt = time.Now().Add(-time.Hour)
			case "system":
				store.grant.Subject.System = "another-system"
			case "token":
				store.grant.Subject.Kind = accesspolicy.APITokenSubject
			case "wrapped-denial":
				store.err = fmt.Errorf("wrapped: %w", accesspolicy.ErrDenied)
			case "joined-denial":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "revoked-during":
				store.onRead = func() { identity.err = partnermanager.ErrDenied }
			case "cancel-during":
				store.onRead = cancel
			case "cancelled":
				cancel()
			case "nil":
				ctx = nil
			}
			if tc.state == "reconcile" {
				err = a.AuthorizeRevenueReconciliation(ctx, actor)
			} else {
				err = a.CheckPartners(ctx, actor, action, tc.target)
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, 2, identity.calls)
				require.Equal(t, 1, store.reads)
			}
			require.Zero(t, store.writes)
			if tc.state == "joined-denial" {
				require.NotErrorIs(t, err, partnermanager.ErrDenied)
			}
		})
	}
}

func TestWorkerAuthorityConstructorDependencies(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"missing_system", "system"}, {"missing_actor", "actor"}, {"actor_whitespace", "spaces"}, {"nil_identity", "identity"}, {"typed_nil_identity", "typed-identity"}, {"nil_policy", "policy"}, {"typed_nil_policy", "typed-policy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			system, actor := "hostapp", "service-a"
			var identity WorkerIdentity = &workerIdentityFixture{}
			var policy PolicyService
			policy, _ = accesspolicy.NewService(&grantStore{}, nil)
			switch tc.state {
			case "system":
				system = ""
			case "actor":
				actor = ""
			case "spaces":
				actor = "service a"
			case "identity":
				identity = nil
			case "typed-identity":
				identity = (*workerIdentityFixture)(nil)
			case "policy":
				policy = nil
			case "typed-policy":
				policy = (*accesspolicy.Service)(nil)
			}
			_, err := NewWorkerAuthority(system, actor, identity, policy)
			require.ErrorIs(t, err, partnermanager.ErrUnavailable)
		})
	}
}
