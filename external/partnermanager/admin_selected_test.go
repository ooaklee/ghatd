package partnermanager

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Narrow observation ports test facade binding, error propagation and current
// authority. They implement no grants, policy resolution or financial formula.
// Native tests separately exercise the actual owning services and persistence.
type selectedProgramRead struct {
	ProgramService
	partner                    partnerprogram.Partner
	destination                partnerprogram.Destination
	versions                   []partnerprogram.PolicyVersion
	partnerErr, destinationErr error
	policyErr                  error
	reads                      []string
	after                      func(string)
}

func (p *selectedProgramRead) observed(stage, target string) {
	p.reads = append(p.reads, stage+":"+target)
	if p.after != nil {
		p.after(stage)
	}
}
func (p *selectedProgramRead) GetPartner(_ context.Context, target string) (partnerprogram.Partner, error) {
	p.observed("partner", target)
	return p.partner, p.partnerErr
}
func (p *selectedProgramRead) GetPayoutDestination(_ context.Context, target string) (partnerprogram.Destination, error) {
	p.observed("destination", target)
	return p.destination, p.destinationErr
}
func (p *selectedProgramRead) ListPolicyVersions(context.Context) ([]partnerprogram.PolicyVersion, error) {
	p.observed("policies", "")
	return p.versions, p.policyErr
}

type selectedIdentityRead struct {
	Identity
	principal Principal
	err       error
	reads     []string
	after     func()
}

func (i *selectedIdentityRead) GetPartnerPrincipal(_ context.Context, target string) (Principal, error) {
	i.reads = append(i.reads, target)
	if i.after != nil {
		i.after()
	}
	return i.principal, i.err
}

type selectedBalanceRead struct {
	EarningsService
	balance partnerearnings.Balances
	err     error
	reads   []string
	after   func()
}

func (e *selectedBalanceRead) Balances(_ context.Context, target string) (partnerearnings.Balances, error) {
	e.reads = append(e.reads, target)
	if e.after != nil {
		e.after()
	}
	return e.balance, e.err
}

func selectedReadFixture(t *testing.T, capability, target string) (*Manager, *selectedProgramRead, *selectedIdentityRead, *selectedBalanceRead, *paymentClaimAuthority, *programStub) {
	t.Helper()
	m, base, _, _, _, identity, _ := managerFixture(t)
	base.partner.Revision = 1
	base.cfg.CurrencyExponent = 2
	p := &selectedProgramRead{ProgramService: base, partner: base.partner, destination: partnerprogram.Destination{ID: "destination", CustomerID: "owner", Method: "paypal", Email: "owner@example.test", Version: 3}}
	i := &selectedIdentityRead{Identity: identity, principal: identity.principal}
	e := &selectedBalanceRead{balance: partnerearnings.Balances{AvailableMinor: 1234, ReservedMinor: 99, PaidOutMinor: 987}}
	a := &paymentClaimAuthority{permission: capability, target: target}
	m.deps.Program, m.deps.Identity, m.deps.Earnings, m.deps.Authority = p, i, e, a
	m.deps.Claims.MinimumMinor = 100
	return m, p, i, e, a, base
}

func TestAdminPartnerStatusUsesPartnerTargetAndCurrentOwner(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
		reads      int
		checks     int
	}{
		{"selected_partner", "", nil, 1, 2},
		{"paused_controls_preserve_read", "paused", nil, 1, 2},
		{"customer_grant_is_not_partner_grant", "target", ErrDenied, 0, 1},
		{"reporting_cannot_supply_policy", "capability", ErrDenied, 0, 1},
		{"revoked_before_read", "revoked", ErrDenied, 0, 1},
		{"revoked_during_read", "late", ErrDenied, 1, 2},
		{"missing_partner_retains_owner_error", "absent", partnerprogram.ErrNotFound, 1, 1},
		{"owner_outage", "outage", partnerprogram.ErrUnavailable, 1, 1},
		{"wrong_partner", "partner", ErrUnavailable, 1, 1},
		{"wrong_program", "program", ErrUnavailable, 1, 1},
		{"missing_customer", "customer", ErrUnavailable, 1, 1},
		{"invalid_revision", "revision", ErrUnavailable, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, i, e, a, _ := selectedReadFixture(t, CapabilityPolicy, "partner")
			switch tc.mode {
			case "paused":
				m.deps.Controls = Controls{}
			case "target":
				a.target = "owner"
			case "capability":
				a.permission = CapabilityReporting
			case "revoked":
				a.revoked = true
			case "late":
				p.after = func(string) { a.revoked = true }
			case "absent":
				p.partnerErr = partnerprogram.ErrNotFound
			case "outage":
				p.partnerErr = partnerprogram.ErrUnavailable
			case "partner":
				p.partner.ID = "other"
			case "program":
				p.partner.ProgramID = "other"
			case "customer":
				p.partner.CustomerID = ""
			case "revision":
				p.partner.Revision = 0
			}
			out, err := m.AdminPartnerStatus(t.Context(), "operator", "partner")
			require.ErrorIs(t, err, tc.want)
			require.Len(t, p.reads, tc.reads)
			require.Equal(t, tc.checks, a.checks)
			require.Empty(t, i.reads)
			require.Empty(t, e.reads)
			if tc.want == nil {
				require.Equal(t, p.partner, out)
				require.Equal(t, []string{"partner:partner"}, p.reads)
			} else {
				require.Equal(t, partnerprogram.Partner{}, out)
			}
		})
	}
}

func TestAdminClaimPreparationPreservesAdmissionDataAndOwnerFailures(t *testing.T) {
	joined := errors.Join(partnerprogram.ErrNotFound, partnerprogram.ErrUnavailable)
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"current_destination_and_available", "", nil},
		{"paused_claims_are_data", "paused", nil},
		{"partner_payout_pause_is_data", "partner_paused", nil},
		{"inactive_identity_is_data", "inactive", nil},
		{"unverified_identity_is_data", "unverified", nil},
		{"organization_identity_is_data", "organization", nil},
		{"region_is_not_a_new_withdrawal_gate", "region", nil},
		{"zero_available_is_valid", "zero", nil},
		{"zero_minimum_is_valid", "minimum", nil},
		{"sole_destination_absence_is_null", "absent", nil},
		{"wrapped_sole_destination_absence_is_null", "wrapped", nil},
		{"joined_destination_error_is_not_absence", "joined", joined},
		{"destination_outage", "destination_outage", partnerprogram.ErrUnavailable},
		{"identity_outage", "identity_outage", ErrUnavailable},
		{"identity_absence_is_not_ineligible", "identity_absent", ErrNotFound},
		{"balance_outage_is_not_zero", "balance_outage", partnerearnings.ErrUnavailable},
		{"partner_outage", "partner_outage", partnerprogram.ErrUnavailable},
		{"different_principal_rejected", "identity", ErrUnavailable},
		{"different_partner_rejected", "partner", ErrUnavailable},
		{"different_program_rejected", "program", ErrUnavailable},
		{"different_destination_owner_rejected", "destination_owner", ErrUnavailable},
		{"missing_destination_id_rejected", "destination_id", ErrUnavailable},
		{"zero_destination_version_rejected", "destination_version", ErrUnavailable},
		{"unsupported_destination_method_rejected", "destination_method", ErrUnavailable},
		{"missing_destination_email_rejected", "destination_email", ErrUnavailable},
		{"negative_available_rejected", "negative", ErrUnavailable},
		{"invalid_currency_rejected", "currency", ErrUnavailable},
		{"invalid_exponent_rejected", "exponent", ErrUnavailable},
		{"selected_customer_grant_cannot_be_borrowed", "target", ErrDenied},
		{"reporting_cannot_prepare_withdrawal", "capability", ErrDenied},
		{"revoked_during_partner_read", "late_partner", ErrDenied},
		{"revoked_during_identity_read", "late_identity", ErrDenied},
		{"revoked_during_destination_read", "late_destination", ErrDenied},
		{"revoked_during_balance_read", "late_balance", ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, i, e, a, base := selectedReadFixture(t, CapabilityCreateClaimOnBehalf, "partner")
			switch tc.mode {
			case "paused":
				m.deps.Controls.Claims = false
			case "partner_paused":
				p.partner.CanRequestPayouts = false
			case "inactive":
				i.principal.Active = false
			case "unverified":
				i.principal.EmailVerified = false
			case "organization":
				i.principal.Individual = false
			case "region":
				i.principal.RegionEligible = false
			case "zero":
				e.balance.AvailableMinor = 0
			case "minimum":
				m.deps.Claims.MinimumMinor = 0
			case "absent":
				p.destinationErr = partnerprogram.ErrNotFound
			case "wrapped":
				p.destinationErr = fmt.Errorf("owner: %w", partnerprogram.ErrNotFound)
			case "joined":
				p.destinationErr = joined
			case "destination_outage":
				p.destinationErr = partnerprogram.ErrUnavailable
			case "identity_outage":
				i.err = ErrUnavailable
			case "identity_absent":
				i.err = ErrNotFound
			case "balance_outage":
				e.err = partnerearnings.ErrUnavailable
			case "partner_outage":
				p.partnerErr = partnerprogram.ErrUnavailable
			case "identity":
				i.principal.ID = "other"
			case "partner":
				p.partner.ID = "other"
			case "program":
				p.partner.ProgramID = "other"
			case "destination_owner":
				p.destination.CustomerID = "other"
			case "destination_id":
				p.destination.ID = ""
			case "destination_version":
				p.destination.Version = 0
			case "destination_method":
				p.destination.Method = "wire"
			case "destination_email":
				p.destination.Email = ""
			case "negative":
				e.balance.AvailableMinor = -1
			case "currency":
				base.cfg.Currency = "eur"
			case "exponent":
				base.cfg.CurrencyExponent = 4
			case "target":
				a.target = "owner"
			case "capability":
				a.permission = CapabilityReporting
			case "late_partner", "late_destination":
				p.after = func(stage string) {
					if "late_"+stage == tc.mode {
						a.revoked = true
					}
				}
			case "late_identity":
				i.after = func() { a.revoked = true }
			case "late_balance":
				e.after = func() { a.revoked = true }
			}
			out, err := m.AdminClaimPreparation(t.Context(), "operator", "partner")
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Equal(t, ClaimPreparation{}, out, "a failed read cannot expose partial preparation")
				if strings.HasPrefix(tc.mode, "late_") {
					require.Equal(t, 2, a.checks)
				} else {
					require.Equal(t, 1, a.checks)
				}
				return
			}
			require.Equal(t, 2, a.checks)
			require.Equal(t, []string{"partner:partner", "destination:owner"}, p.reads)
			require.Equal(t, []string{"owner"}, i.reads)
			require.Equal(t, []string{"partner"}, e.reads)
			require.Equal(t, "EUR", out.Currency)
			require.Equal(t, 2, out.CurrencyExponent)
			require.Equal(t, m.deps.Claims.MinimumMinor, out.MinimumMinor)
			require.Equal(t, m.deps.Controls.Claims, out.ClaimsEnabled)
			require.Equal(t, p.partner.CanRequestPayouts, out.CanRequestPayouts)
			require.Equal(t, i.principal.Active && i.principal.EmailVerified && i.principal.Individual, out.IdentityEligible)
			require.Equal(t, e.balance.AvailableMinor, out.AvailableMinor)
			if p.destinationErr != nil {
				require.Nil(t, out.Destination)
			} else {
				require.Equal(t, &ClaimPreparationDestination{ID: "destination", Method: "paypal", Email: "owner@example.test", Version: 3}, out.Destination)
			}
		})
	}
}

func TestAdminIndividualPolicyHistoryUsesCustomerTargetAndMaximumRevision(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"selected_history_includes_future_and_expired", "", nil},
		{"empty_is_first_publication", "empty", nil},
		{"paused_controls_preserve_history", "paused", nil},
		{"partner_grant_is_not_customer_grant", "target", ErrDenied},
		{"reporting_cannot_supply_policy", "capability", ErrDenied},
		{"owner_outage_not_empty", "outage", partnerprogram.ErrUnavailable},
		{"revoked_during_history_read", "late", ErrDenied},
		{"foreign_owner_program_rejected", "program", ErrUnavailable},
		{"missing_matching_policy_id_rejected", "id", ErrUnavailable},
		{"invalid_matching_revision_rejected", "revision", ErrUnavailable},
		{"individual_cannot_have_group_identity", "group", ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, p, i, e, a, _ := selectedReadFixture(t, CapabilityPolicy, "owner")
			at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			until, months := at.Add(time.Hour), 6
			p.versions = []partnerprogram.PolicyVersion{
				{ID: "global", ProgramID: partnerprogram.ProgramID, Scope: "global", Revision: 90},
				{ID: "group", ProgramID: partnerprogram.ProgramID, Scope: "group", GroupID: "group", Revision: 80},
				{ID: "other", ProgramID: partnerprogram.ProgramID, Scope: "individual", PartnerCustomer: "other", Revision: 70},
				{ID: "expired", ProgramID: partnerprogram.ProgramID, Scope: "individual", PartnerCustomer: "owner", Revision: 7, EffectiveFrom: at.Add(-time.Hour), EffectiveTo: &until, RecurringMonths: &months, EligiblePlanIDs: []string{"plan"}},
				{ID: "future", ProgramID: partnerprogram.ProgramID, Scope: "individual", PartnerCustomer: "owner", Revision: 3, EffectiveFrom: at.Add(24 * time.Hour), EligiblePlanIDs: []string{}},
			}
			switch tc.mode {
			case "empty":
				p.versions = nil
			case "paused":
				m.deps.Controls = Controls{}
			case "target":
				a.target = "partner"
			case "capability":
				a.permission = CapabilityReporting
			case "outage":
				p.policyErr = partnerprogram.ErrUnavailable
			case "late":
				p.after = func(string) { a.revoked = true }
			case "program":
				p.versions[0].ProgramID = "foreign"
			case "id":
				p.versions[3].ID = ""
			case "revision":
				p.versions[3].Revision = 0
			case "group":
				p.versions[3].GroupID = "unexpected"
			}
			out, err := m.AdminIndividualPolicyVersions(t.Context(), "operator", "owner")
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, i.reads)
			require.Empty(t, e.reads)
			if tc.want != nil {
				require.Equal(t, IndividualPolicyHistory{}, out)
				if tc.mode == "late" {
					require.Equal(t, 2, a.checks)
				} else {
					require.Equal(t, 1, a.checks)
				}
				return
			}
			require.Equal(t, 2, a.checks)
			require.Equal(t, []string{"policies:"}, p.reads)
			if tc.mode == "empty" {
				require.NotNil(t, out.Versions)
				require.Empty(t, out.Versions)
				require.Zero(t, out.Revision)
				return
			}
			require.EqualValues(t, 7, out.Revision, "date/list ordering is not the CAS head")
			require.Equal(t, p.versions[3:], out.Versions)
			out.Versions[0].EligiblePlanIDs[0] = "edited"
			*out.Versions[0].EffectiveTo = at
			*out.Versions[0].RecurringMonths = 99
			require.Equal(t, "plan", p.versions[3].EligiblePlanIDs[0])
			require.Equal(t, at.Add(time.Hour), *p.versions[3].EffectiveTo)
			require.Equal(t, 6, *p.versions[3].RecurringMonths)
			require.NotNil(t, out.Versions[1].EligiblePlanIDs, "explicit exclusion is not inheritance")
		})
	}
}

func TestSelectedOperatorReadsRejectMalformedTargetsBeforeAuthority(t *testing.T) {
	for _, operation := range []string{"status", "preparation", "individual_policy"} {
		for _, tc := range []struct{ name, target string }{
			{"empty_program_list", ""}, {"oversized", strings.Repeat("x", 257)},
			{"padded", " selected"}, {"newline", "selected\nvalue"}, {"nul", "selected\x00value"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				m, p, i, e, a, _ := selectedReadFixture(t, CapabilityPolicy, tc.target)
				var err error
				switch operation {
				case "status":
					_, err = m.AdminPartnerStatus(t.Context(), "operator", tc.target)
				case "preparation":
					_, err = m.AdminClaimPreparation(t.Context(), "operator", tc.target)
				case "individual_policy":
					_, err = m.AdminIndividualPolicyVersions(t.Context(), "operator", tc.target)
				}
				require.ErrorIs(t, err, ErrInvalid)
				require.Zero(t, a.checks)
				require.Empty(t, p.reads)
				require.Empty(t, i.reads)
				require.Empty(t, e.reads)
			})
		}
	}
}
