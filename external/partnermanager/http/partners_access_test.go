package partnerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// These fixtures supply stored grants and live credential state to the actual
// shared policy service and human authority. They implement no financial state,
// policy decisions, mutation receipts or authentication cryptography.
type operatorAccessGrants struct {
	grant         accesspolicy.Grant
	err           error
	onRead        func()
	reads, writes int
}

func (s *operatorAccessGrants) Read(_ context.Context, subject accesspolicy.Subject) (accesspolicy.Grant, error) {
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
func (s *operatorAccessGrants) Replace(context.Context, accesspolicy.Grant, int64, string, time.Time) (accesspolicy.Grant, error) {
	s.writes++
	return accesspolicy.Grant{}, accesspolicy.ErrConfiguration
}
func (s *operatorAccessGrants) WithGrant(context.Context, accesspolicy.Subject, time.Time, func(context.Context, accesspolicy.Grant) error) error {
	s.writes++
	return accesspolicy.ErrConfiguration
}
func (s *operatorAccessGrants) Consume(context.Context, accesspolicy.Consumption, time.Time) (accesspolicy.Usage, error) {
	s.writes++
	return accesspolicy.Usage{}, accesspolicy.ErrConfiguration
}
func (s *operatorAccessGrants) WithConsumption(context.Context, accesspolicy.Consumption, time.Time, accesspolicy.ConsumptionAction) (accesspolicy.Usage, error) {
	s.writes++
	return accesspolicy.Usage{}, accesspolicy.ErrConfiguration
}

type operatorAccessSession struct {
	calls   int
	revoked bool
	failAt  int
	err     error
}

func (s *operatorAccessSession) CheckPartnerSession(ctx context.Context, actor, credential string) error {
	s.calls++
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.err != nil {
		return s.err
	}
	if s.revoked || s.calls == s.failAt || actor != "operator-a" || credential != "test-current-session" {
		return partnermanager.ErrDenied
	}
	return nil
}

type operatorAccessDenialAlias struct{}

func (operatorAccessDenialAlias) Error() string        { return "private-driver-denial-alias" }
func (operatorAccessDenialAlias) Is(target error) bool { return target == partnermanager.ErrDenied }

func operatorAccessFixture(t *testing.T, capability, scope string) (*Service, *operatorAccessGrants, *operatorAccessSession, context.Context, Principal) {
	t.Helper()
	grants := &operatorAccessGrants{grant: accesspolicy.Grant{Subject: accesspolicy.Subject{System: "fixture", Kind: accesspolicy.UserSubject, ID: "operator-a"}, Revision: 1, Enabled: true, Scopes: []string{scope}, Permissions: []string{capability}}}
	policy, err := accesspolicy.NewService(grants, nil)
	require.NoError(t, err)
	sessions := &operatorAccessSession{}
	authority, err := partneraccess.NewAuthority("fixture", sessions, policy)
	require.NoError(t, err)
	principal := Principal{ActorID: "operator-a", Verified: true, Credential: "test-current-session"}
	ctx, err := partneraccess.WithVerifiedSession(t.Context(), principal.ActorID, principal.Credential)
	require.NoError(t, err)
	return &Service{authority: authority, sessions: sessions}, grants, sessions, ctx, principal
}

func requireOperatorAccessError(t *testing.T, err error, status int) {
	t.Helper()
	var mapped *Error
	require.ErrorAs(t, partnerError(err), &mapped)
	require.Equal(t, status, mapped.Status)
	require.NotContains(t, mapped.Error(), "private")
}

func TestPartnersOperatorAccessUsesActualCurrentActionAndTarget(t *testing.T) {
	for _, tc := range []struct {
		name, capability, target, grantTarget string
		program, allowed, adminOnly           bool
	}{
		{name: "selected_reporting_without_admin", capability: partnermanager.CapabilityReporting, target: "partner-a", grantTarget: "partner-a", allowed: true},
		{name: "selected_target_at_256_bytes", capability: partnermanager.CapabilityReporting, target: strings.Repeat("a", 256), grantTarget: strings.Repeat("a", 256), allowed: true},
		{name: "selected_multibyte_target_under_256_bytes", capability: partnermanager.CapabilityReporting, target: strings.Repeat("€", 85), grantTarget: strings.Repeat("€", 85), allowed: true},
		{name: "selected_grant_cannot_change_partner", capability: partnermanager.CapabilityReporting, target: "partner-b", grantTarget: "partner-a"},
		{name: "program_reporting_can_read_selected_partner", capability: partnermanager.CapabilityReporting, target: "partner-a", program: true, allowed: true},
		{name: "program_policy_list", capability: partnermanager.CapabilityPolicy, program: true, allowed: true},
		{name: "selected_policy_does_not_grant_program_list", capability: partnermanager.CapabilityPolicy, grantTarget: "partner-a"},
		{name: "selected_partner_status", capability: partnermanager.CapabilityPolicy, target: "partner-a", grantTarget: "partner-a", allowed: true},
		{name: "selected_policy_customer", capability: partnermanager.CapabilityPolicy, target: "customer-a", grantTarget: "customer-a", allowed: true},
		{name: "program_processing_queue", capability: partnermanager.CapabilityProcessing, program: true, allowed: true},
		{name: "selected_processing_does_not_grant_queue", capability: partnermanager.CapabilityProcessing, grantTarget: "claim-a"},
		{name: "selected_processing_claim", capability: partnermanager.CapabilityProcessing, target: "claim-a", grantTarget: "claim-a", allowed: true},
		{name: "actual_operations_program", capability: partnermanager.CapabilityOperations, target: partnerprogram.ProgramID, program: true, allowed: true},
		{name: "selected_on_behalf_partner", capability: partnermanager.CapabilityCreateClaimOnBehalf, target: "partner-a", grantTarget: "partner-a", allowed: true},
		{name: "selected_attribution_customer", capability: partnermanager.CapabilityAttribution, target: "customer-a", grantTarget: "customer-a", allowed: true},
		{name: "attribution_partner_grant_does_not_grant_referred_customer", capability: partnermanager.CapabilityAttribution, target: "customer-a", grantTarget: "partner-a"},
		{name: "selected_payment_claim", capability: partnermanager.CapabilityRecordPayment, target: "claim-a", grantTarget: "claim-a", allowed: true},
		{name: "selected_amendment_claim", capability: partnermanager.CapabilityAmendPayment, target: "claim-a", grantTarget: "claim-a", allowed: true},
		{name: "selected_return_claim", capability: partnermanager.CapabilityReturnPayment, target: "claim-a", grantTarget: "claim-a", allowed: true},
		{name: "generic_admin_confers_no_recording_grant", capability: partnermanager.CapabilityRecordPayment, target: "claim-a", program: true, adminOnly: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope := partneraccess.ProgramScope
			if !tc.program {
				var err error
				scope, err = partneraccess.TargetScope(tc.capability, tc.grantTarget)
				require.NoError(t, err)
			}
			m, grants, sessions, ctx, principal := operatorAccessFixture(t, tc.capability, scope)
			if tc.adminOnly {
				grants.grant.Permissions = []string{"ADMIN"}
			}
			response, err := m.partnersOperatorAccess(ctx, principal, Request{Query: url.Values{"capability": {tc.capability}, "target": {tc.target}}})
			require.NoError(t, err)
			require.Equal(t, 200, response.Status)
			require.Empty(t, response.ETag)
			var view struct {
				Data map[string]json.RawMessage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(response.Body, &view))
			require.Len(t, view.Data, 3)
			require.JSONEq(t, fmt.Sprintf(`{"data":{"capability":%q,"target":%q,"allowed":%t}}`, tc.capability, tc.target, tc.allowed), string(response.Body))
			require.NotContains(t, string(response.Body), principal.Credential)
			require.NotContains(t, string(response.Body), principal.ActorID)
			require.Zero(t, grants.writes)
			require.GreaterOrEqual(t, sessions.calls, 3)
		})
	}
}

func TestPartnersOperatorAccessDoesNotHideRevocationOrPolicyFailures(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		status     int
	}{
		{"revoked_before_check", "revoked", 403},
		{"revoked_during_granted_policy_read", "revoke-read", 403},
		{"revoked_during_denied_policy_read", "revoke-denied-read", 403},
		{"revoked_at_final_granted_session_check", "late-granted", 403},
		{"revoked_at_final_denied_session_check", "late-denied", 403},
		{"dependency_outage", "unavailable", 503},
		{"unknown_private_error", "unknown", 500},
		{"denial_joined_with_unavailable", "joined-unavailable", 503},
		{"denial_joined_with_private_error", "joined-unknown", 500},
		{"is_only_denial_alias_is_not_known_denial", "alias", 500},
		{"session_outage", "session-outage", 503},
		{"different_actor", "actor", 403},
		{"different_credential", "credential", 403},
		{"cancelled_context", "cancelled", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, grants, sessions, ctx, principal := operatorAccessFixture(t, partnermanager.CapabilityPolicy, partneraccess.ProgramScope)
			switch tc.mode {
			case "revoked":
				sessions.revoked = true
			case "revoke-read", "revoke-denied-read":
				grants.onRead = func() { sessions.revoked = true }
				if tc.mode == "revoke-denied-read" {
					grants.grant.Permissions = nil
				}
			case "late-granted":
				sessions.failAt = 4
			case "late-denied":
				grants.grant.Permissions = nil
				sessions.failAt = 3
			case "unavailable":
				grants.err = partnermanager.ErrUnavailable
			case "unknown":
				grants.err = errors.New("private-driver-detail")
			case "joined-unavailable":
				grants.err = errors.Join(accesspolicy.ErrDenied, partnermanager.ErrUnavailable)
			case "joined-unknown":
				grants.err = errors.Join(accesspolicy.ErrDenied, errors.New("private-driver-detail"))
			case "alias":
				grants.err = operatorAccessDenialAlias{}
			case "session-outage":
				sessions.err = partnermanager.ErrUnavailable
			case "actor":
				principal.ActorID = "other-operator"
			case "credential":
				principal.Credential = "different-session"
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			response, err := m.partnersOperatorAccess(ctx, principal, Request{Query: url.Values{"capability": {partnermanager.CapabilityPolicy}}})
			requireOperatorAccessError(t, err, tc.status)
			require.Empty(t, response.Body)
			require.Zero(t, grants.writes)
		})
	}
}

func TestPartnersOperatorAccessRejectsUnsupportedOrAmbiguousQueriesBeforePolicy(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query url.Values
	}{
		{"missing_capability", nil},
		{"empty_capability", url.Values{"capability": {""}}},
		{"duplicate_capability", url.Values{"capability": {partnermanager.CapabilityPolicy, partnermanager.CapabilityPolicy}}},
		{"duplicate_target", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {"partner-a", "partner-b"}}},
		{"unknown_query_actor", url.Values{"capability": {partnermanager.CapabilityPolicy}, "actor_id": {"other-operator"}}},
		{"empty_query_values", url.Values{"capability": nil}},
		{"oversized_capability", url.Values{"capability": {strings.Repeat("x", 65)}}},
		{"unknown_operator_action", url.Values{"capability": {"partner.admin.unknown"}, "target": {"partner-a"}}},
		{"customer_self", url.Values{"capability": {partnermanager.CapabilitySelf}, "target": {"operator-a"}}},
		{"customer_enroll", url.Values{"capability": {partnermanager.CapabilityEnroll}, "target": {"operator-a"}}},
		{"customer_claim", url.Values{"capability": {partnermanager.CapabilityClaims}, "target": {"operator-a"}}},
		{"worker_revenue", url.Values{"capability": {partnermanager.CapabilityRevenueWorker}, "target": {"fact-a"}}},
		{"worker_signup", url.Values{"capability": {partnermanager.CapabilitySignupWorker}, "target": {"signup-a"}}},
		{"worker_maturity", url.Values{"capability": {partnermanager.CapabilityMaturityWorker}, "target": {"partner-a"}}},
		{"missing_selected_recording_target", url.Values{"capability": {partnermanager.CapabilityRecordPayment}}},
		{"missing_selected_reporting_target", url.Values{"capability": {partnermanager.CapabilityReporting}}},
		{"wrong_operations_program", url.Values{"capability": {partnermanager.CapabilityOperations}, "target": {"other-program"}}},
		{"oversized_target", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {strings.Repeat("x", 257)}}},
		{"multibyte_oversized_target", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {strings.Repeat("€", 86)}}},
		{"target_padding", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {" partner-a"}}},
		{"target_unicode_space", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {"partner\u00a0a"}}},
		{"target_control", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {"partner\x00a"}}},
		{"target_invalid_utf8", url.Values{"capability": {partnermanager.CapabilityPolicy}, "target": {string([]byte{0xff})}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, grants, sessions, ctx, principal := operatorAccessFixture(t, partnermanager.CapabilityPolicy, partneraccess.ProgramScope)
			_, err := m.partnersOperatorAccess(ctx, principal, Request{Query: tc.query})
			requireOperatorAccessError(t, err, 400)
			require.Zero(t, sessions.calls)
			require.Zero(t, grants.reads)
			require.Zero(t, grants.writes)
		})
	}
}

func TestPartnersOperatorAccessFailsClosedWithoutComposedPorts(t *testing.T) {
	for _, mode := range []string{"nil-authority", "typed-nil-authority", "nil-session", "typed-nil-session"} {
		t.Run(mode, func(t *testing.T) {
			m, grants, _, ctx, principal := operatorAccessFixture(t, partnermanager.CapabilityPolicy, partneraccess.ProgramScope)
			switch mode {
			case "nil-authority":
				m.authority = nil
			case "typed-nil-authority":
				m.authority = (*partneraccess.Authority)(nil)
			case "nil-session":
				m.sessions = nil
			case "typed-nil-session":
				m.sessions = (*operatorAccessSession)(nil)
			}
			_, err := m.partnersOperatorAccess(ctx, principal, Request{Query: url.Values{"capability": {partnermanager.CapabilityPolicy}}})
			requireOperatorAccessError(t, err, 503)
			require.Zero(t, grants.reads)
			require.Zero(t, grants.writes)
		})
	}
}

func TestPartnersOperatorAccessBindsTrustedPrincipalAndDoesNotCacheGrants(t *testing.T) {
	for _, mode := range []string{"unbound-context", "stale-bound-context", "wrapped-known-denial"} {
		t.Run(mode, func(t *testing.T) {
			m, grants, _, ctx, principal := operatorAccessFixture(t, partnermanager.CapabilityPolicy, partneraccess.ProgramScope)
			switch mode {
			case "unbound-context":
				ctx = t.Context()
			case "stale-bound-context":
				var err error
				ctx, err = partneraccess.WithVerifiedSession(t.Context(), "former-operator", "former-session")
				require.NoError(t, err)
			case "wrapped-known-denial":
				grants.err = fmt.Errorf("private-wrapped-diagnostic: %w", accesspolicy.ErrDenied)
			}
			query := Request{Query: url.Values{"capability": {partnermanager.CapabilityPolicy}}}
			response, err := m.partnersOperatorAccess(ctx, principal, query)
			require.NoError(t, err)
			require.Contains(t, string(response.Body), fmt.Sprintf(`"allowed":%t`, mode != "wrapped-known-denial"))
			grants.err = nil
			grants.grant.Permissions = nil
			response, err = m.partnersOperatorAccess(ctx, principal, query)
			require.NoError(t, err)
			require.Contains(t, string(response.Body), `"allowed":false`)
			grants.grant.Permissions = []string{partnermanager.CapabilityPolicy}
			response, err = m.partnersOperatorAccess(ctx, principal, query)
			require.NoError(t, err)
			require.Contains(t, string(response.Body), `"allowed":true`)
			require.Zero(t, grants.writes)
		})
	}
}
