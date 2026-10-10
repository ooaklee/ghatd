package partneraccess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// The fixture supplies only stored grants. The real access-policy service owns
// current grant eligibility and exact AND scope/permission checks in these tests.
type grantStore struct {
	grant  accesspolicy.Grant
	err    error
	reads  int
	writes int
	onRead func()
}

func (s *grantStore) Read(_ context.Context, subject accesspolicy.Subject) (accesspolicy.Grant, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead()
	}
	if s.err != nil {
		return accesspolicy.Grant{}, s.err
	}
	if subject != s.grant.Subject {
		return accesspolicy.Grant{}, accesspolicy.ErrDenied
	}
	return s.grant, nil
}
func (s *grantStore) Replace(context.Context, accesspolicy.Grant, int64, string, time.Time) (accesspolicy.Grant, error) {
	s.writes++
	return accesspolicy.Grant{}, accesspolicy.ErrConfiguration
}
func (s *grantStore) WithGrant(context.Context, accesspolicy.Subject, time.Time, func(context.Context, accesspolicy.Grant) error) error {
	s.writes++
	return accesspolicy.ErrConfiguration
}
func (s *grantStore) Consume(context.Context, accesspolicy.Consumption, time.Time) (accesspolicy.Usage, error) {
	s.writes++
	return accesspolicy.Usage{}, accesspolicy.ErrConfiguration
}
func (s *grantStore) WithConsumption(context.Context, accesspolicy.Consumption, time.Time, accesspolicy.ConsumptionAction) (accesspolicy.Usage, error) {
	s.writes++
	return accesspolicy.Usage{}, accesspolicy.ErrConfiguration
}

type sessions struct {
	calls   int
	revoked bool
}

func (s *sessions) CheckPartnerSession(_ context.Context, actor, credential string) error {
	s.calls++
	if s.revoked || actor != "user-a" || credential != "test-session" {
		return partnermanager.ErrDenied
	}
	return nil
}

func authorityFixture(t *testing.T, scopes, permissions []string) (*Authority, *grantStore, *sessions, context.Context) {
	t.Helper()
	store := &grantStore{grant: accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "user-a"}, Revision: 1, Enabled: true, Scopes: scopes, Permissions: permissions}}
	policy, err := accesspolicy.NewService(store, nil)
	require.NoError(t, err)
	live := &sessions{}
	authority, err := NewAuthority("hostapp", live, policy)
	require.NoError(t, err)
	ctx, err := WithVerifiedSession(t.Context(), "user-a", "test-session")
	require.NoError(t, err)
	return authority, store, live, ctx
}

func TestAuthorityCurrentActionAndResource(t *testing.T) {
	scopedReport, err := TargetScope(partnermanager.CapabilityReporting, "partner-a")
	require.NoError(t, err)
	type testCase struct {
		name, actor, capability, target string
		scopes, permissions             []string
		allowed                         bool
		reads                           int
	}
	cases := []testCase{
		{name: "self_read", actor: "user-a", capability: partnermanager.CapabilitySelf, target: "user-a", scopes: []string{SelfScope}, permissions: []string{partnermanager.CapabilitySelf}, allowed: true, reads: 1},
		{name: "self_scope_cannot_read_another_owner", actor: "user-a", capability: partnermanager.CapabilitySelf, target: "user-b", scopes: []string{SelfScope}, permissions: []string{partnermanager.CapabilitySelf}},
		{name: "bound_session_cannot_change_actor", actor: "user-b", capability: partnermanager.CapabilitySelf, target: "user-b", scopes: []string{SelfScope}, permissions: []string{partnermanager.CapabilitySelf}},
		{name: "enrollment_requires_its_own_permission", actor: "user-a", capability: partnermanager.CapabilityEnroll, target: "user-a", scopes: []string{SelfScope}, permissions: []string{partnermanager.CapabilitySelf}, reads: 1},
		{name: "claims_require_their_own_permission", actor: "user-a", capability: partnermanager.CapabilityClaims, target: "user-a", scopes: []string{SelfScope}, permissions: []string{partnermanager.CapabilitySelf}, reads: 1},
		{name: "explicit_selected_partner_reporting", actor: "user-a", capability: partnermanager.CapabilityReporting, target: "partner-a", scopes: []string{scopedReport}, permissions: []string{partnermanager.CapabilityReporting}, allowed: true, reads: 1},
		{name: "selected_partner_scope_cannot_change_target", actor: "user-a", capability: partnermanager.CapabilityReporting, target: "partner-b", scopes: []string{scopedReport}, permissions: []string{partnermanager.CapabilityReporting}, reads: 2},
		{name: "program_reporting_does_not_allow_recording", actor: "user-a", capability: partnermanager.CapabilityRecordPayment, target: "claim-a", scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityReporting}, reads: 2},
		{name: "administrator_label_does_not_grant_payment_authority", actor: "user-a", capability: partnermanager.CapabilityRecordPayment, target: "claim-a", scopes: []string{ProgramScope}, permissions: []string{"ADMIN"}, reads: 2},
		{name: "explicit_program_manual_recording", actor: "user-a", capability: partnermanager.CapabilityRecordPayment, target: "claim-a", scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityRecordPayment}, allowed: true, reads: 2},
		{name: "policy_queue_scope_is_explicit", actor: "user-a", capability: partnermanager.CapabilityPolicy, scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityPolicy}, allowed: true, reads: 1},
		{name: "processing_queue_scope_is_explicit", actor: "user-a", capability: partnermanager.CapabilityProcessing, scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityProcessing}, allowed: true, reads: 1},
		{name: "empty_manual_recording_target_denied", actor: "user-a", capability: partnermanager.CapabilityRecordPayment, scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityRecordPayment}},
		{name: "operations_require_owning_program", actor: "user-a", capability: partnermanager.CapabilityOperations, target: partnerprogram.ProgramID, scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityOperations}, allowed: true, reads: 1},
		{name: "operations_reject_another_program", actor: "user-a", capability: partnermanager.CapabilityOperations, target: "another-program", scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityOperations}},
		{name: "human_session_cannot_become_revenue_worker", actor: "user-a", capability: partnermanager.CapabilityRevenueWorker, target: "fact-a", scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityRevenueWorker}},
		{name: "human_session_cannot_become_maturity_worker", actor: "user-a", capability: partnermanager.CapabilityMaturityWorker, target: "partner-a", scopes: []string{ProgramScope}, permissions: []string{partnermanager.CapabilityMaturityWorker}},
		{name: "unknown_action_denied", actor: "user-a", capability: "partner.admin.unknown", target: "partner-a", scopes: []string{ProgramScope}, permissions: []string{"partner.admin.unknown"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authority, store, live, ctx := authorityFixture(t, tc.scopes, tc.permissions)
			err := authority.CheckPartners(ctx, tc.actor, tc.capability, tc.target)
			if tc.allowed {
				require.NoError(t, err)
				require.Equal(t, 2, live.calls)
			} else {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			require.Equal(t, tc.reads, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestAuthorityReplayAndLateFailure(t *testing.T) {
	outage := errors.New("policy-storage-outage")
	type testCase struct {
		name  string
		state string
		want  error
	}
	for _, tc := range []testCase{
		{name: "permission_revoked", state: "permission", want: partnermanager.ErrDenied},
		{name: "grant_disabled", state: "disabled", want: partnermanager.ErrDenied},
		{name: "grant_expired", state: "expired", want: partnermanager.ErrDenied},
		{name: "session_revoked", state: "session", want: partnermanager.ErrDenied},
		{name: "revoked_during_policy_read", state: "during-read", want: partnermanager.ErrDenied},
		{name: "policy_outage", state: "outage", want: outage},
		{name: "joined_denial_and_outage_never_falls_back", state: "joined", want: outage},
		{name: "request_cancelled", state: "cancelled", want: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority, store, live, ctx := authorityFixture(t, []string{ProgramScope}, []string{partnermanager.CapabilityRecordPayment})
			require.NoError(t, authority.CheckPartners(ctx, "user-a", partnermanager.CapabilityRecordPayment, "claim-a"))
			store.reads, live.calls = 0, 0
			switch tc.state {
			case "permission":
				store.grant.Permissions = nil
			case "disabled":
				store.grant.Enabled = false
			case "expired":
				store.grant.ExpiresAt = time.Now().Add(-time.Hour)
			case "session":
				live.revoked = true
			case "during-read":
				store.onRead = func() { live.revoked = true }
			case "outage":
				store.err = outage
			case "joined":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			require.ErrorIs(t, authority.CheckPartners(ctx, "user-a", partnermanager.CapabilityRecordPayment, "claim-a"), tc.want)
			if tc.state == "joined" || tc.state == "outage" {
				require.Equal(t, 1, store.reads, "an unavailable result cannot try program authority")
			}
			if tc.state == "session" || tc.state == "cancelled" {
				require.Zero(t, store.reads, "invalid sessions and cancelled requests must not consult policy")
			}
			require.Zero(t, store.writes)
		})
	}
}

func TestAuthorityMissingSessionAndConstruction(t *testing.T) {
	type testCase struct {
		name string
		kind string
	}
	for _, tc := range []testCase{
		{name: "no_bound_session", kind: "missing"},
		{name: "nil_context", kind: "nil-context"},
		{name: "nil_authority", kind: "nil-authority"},
		{name: "nil_session_port", kind: "nil-session"},
		{name: "typed_nil_policy_port", kind: "typed-nil-policy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority, store, _, ctx := authorityFixture(t, []string{SelfScope}, []string{partnermanager.CapabilitySelf})
			want := partnermanager.ErrDenied
			switch tc.kind {
			case "missing":
				ctx = t.Context()
			case "nil-context":
				ctx = nil
			case "nil-authority":
				authority = nil
				want = partnermanager.ErrUnavailable
			case "nil-session":
				_, err := NewAuthority("hostapp", nil, authority.policy)
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
				return
			case "typed-nil-policy":
				var policy *accesspolicy.Service
				_, err := NewAuthority("hostapp", authority.sessions, policy)
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
				return
			}
			require.ErrorIs(t, authority.CheckPartners(ctx, "user-a", partnermanager.CapabilitySelf, "user-a"), want)
			require.Zero(t, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestSessionBindingRejectsMalformedIdentity(t *testing.T) {
	type testCase struct {
		name, actor, credential string
	}
	for _, tc := range []testCase{
		{name: "empty_actor", credential: "test-session"},
		{name: "actor_with_space", actor: "user a", credential: "test-session"},
		{name: "actor_with_control", actor: "user\x00a", credential: "test-session"},
		{name: "invalid_utf8_actor", actor: "user\xff", credential: "test-session"},
		{name: "empty_credential", actor: "user-a"},
		{name: "credential_with_linebreak", actor: "user-a", credential: "test\nsession"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, err := WithVerifiedSession(t.Context(), tc.actor, tc.credential)
			require.ErrorIs(t, err, partnermanager.ErrDenied)
			require.Nil(t, ctx)
		})
	}
}
