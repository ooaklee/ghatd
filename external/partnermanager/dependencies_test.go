package partnermanager

import (
	"context"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new named construction/authority boundary cases; these
// establish missing capability failures, not host RBAC integration.
func TestManagerRejectsMissingCapabilities(t *testing.T) {
	type testCase struct {
		name    string
		replace func(*Dependencies)
	}
	cases := []testCase{
		{name: "typed_nil_program", replace: func(d *Dependencies) { var v *programStub; d.Program = v }},
		{name: "typed_nil_referral", replace: func(d *Dependencies) { var v *referralStub; d.Referral = v }},
		{name: "typed_nil_earnings", replace: func(d *Dependencies) { var v *earningsStub; d.Earnings = v }},
		{name: "typed_nil_identity", replace: func(d *Dependencies) { var v *identityStub; d.Identity = v }},
		{name: "typed_nil_authority", replace: func(d *Dependencies) { var v *authorityStub; d.Authority = v }},
		{name: "typed_nil_groups", replace: func(d *Dependencies) { var v *groupsStub; d.Groups = v }},
		{name: "typed_nil_revenue", replace: func(d *Dependencies) { var v *revenueStub; d.Revenue = v }},
		{name: "typed_nil_evidence_signer", replace: func(d *Dependencies) { var v *referral.EvidenceSigner; d.Evidence = v }},
		{name: "typed_nil_clock", replace: func(d *Dependencies) { var v *managerClock; d.Clock = v }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			d := m.deps
			tc.replace(&d)
			got, err := NewManager(d)
			require.ErrorIs(t, err, ErrUnavailable)
			require.Nil(t, got)
		})
	}
}
func TestManagerAuthorityInputBounds(t *testing.T) {
	type testCase struct {
		name, actor, target string
		want                error
		calls               int
	}
	cases := []testCase{
		{name: "scoped_resource", actor: "operator", target: "claim", calls: 1},
		{name: "collection_scope_is_explicit_capability", actor: "operator", calls: 1},
		{name: "empty_actor", target: "claim", want: ErrDenied},
		{name: "actor_not_normalized_from_transport", actor: " operator ", target: "claim", want: ErrDenied},
		{name: "oversized_actor", actor: strings.Repeat("a", 257), target: "claim", want: ErrDenied},
		{name: "oversized_target", actor: "operator", target: strings.Repeat("t", 257), want: ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, a, _, _ := managerFixture(t)
			err := m.authorize(context.Background(), tc.actor, CapabilityProcessing, tc.target)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, a.calls, tc.calls)
		})
	}
}
