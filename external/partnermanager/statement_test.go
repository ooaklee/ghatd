package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named response scope/current-authority cases. They test
// manager privacy boundaries rather than the separately verified calculation.
type statementOwnerStub struct {
	EarningsService
	result    partnerearnings.Statement
	afterRead func()
	reads     int
}

func (s *statementOwnerStub) GetStatement(context.Context, string, partnerearnings.StatementQuery) (partnerearnings.Statement, error) {
	s.reads++
	if s.afterRead != nil {
		s.afterRead()
	}
	return s.result, nil
}
func TestStatementCurrentScopeAndPermission(t *testing.T) {
	cases := []struct {
		name                                                  string
		admin, wrongPartner, wrongCurrency, revokedDuringRead bool
		permission                                            string
		want                                                  error
	}{
		{name: "customer_own_statement"},
		{name: "another_partner_response_is_denied", wrongPartner: true, want: ErrUnavailable},
		{name: "currency_response_is_denied", wrongCurrency: true, want: ErrUnavailable},
		{name: "revocation_during_customer_read_denies_return", revokedDuringRead: true, want: ErrDenied},
		{name: "scoped_admin_reporting", admin: true, permission: CapabilityReporting},
		{name: "policy_permission_does_not_read_finance", admin: true, permission: CapabilityPolicy, want: ErrDenied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, a, _, _ := managerFixture(t)
			owner := &statementOwnerStub{result: partnerearnings.Statement{PartnerID: "partner", Currency: "EUR"}}
			m.deps.Earnings = owner
			if tc.wrongPartner {
				owner.result.PartnerID = "other"
			}
			if tc.wrongCurrency {
				owner.result.Currency = "USD"
			}
			if tc.revokedDuringRead {
				owner.afterRead = func() { a.deny = true }
			}
			var out partnerearnings.Statement
			var err error
			if tc.admin {
				m.deps.Authority = &claimAuthorityStub{permitted: tc.permission}
				out, err = m.AdminStatement(context.Background(), "operator", "partner", partnerearnings.StatementQuery{Limit: 100})
			} else {
				out, err = m.Statement(context.Background(), "owner", partnerearnings.StatementQuery{Limit: 100})
			}
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.Equal(t, "partner", out.PartnerID)
			} else {
				require.Empty(t, out.PartnerID)
			}
		})
	}
}
