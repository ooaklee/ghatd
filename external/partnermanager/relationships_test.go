package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type relationshipReadStub struct {
	ReferralService
	page      referral.RelationshipPage
	err       error
	read      func()
	calls     int
	requested string
}

// Audit disposition: named current owning identity/enrollment changes test
// symmetric self checks on both history and financial-summary read paths.
func TestCustomerReferralReadsRecheckCurrentPrincipal(t *testing.T) {
	for _, method := range []string{"history", "summary"} {
		for _, tc := range []struct {
			name, mode string
			want       error
		}{
			{name: "current_bound_identity_is_allowed"},
			{name: "inactive_account_after_read", mode: "inactive", want: ErrDenied},
			{name: "verification_revoked_after_read", mode: "unverified", want: ErrDenied},
			{name: "account_type_changed_after_read", mode: "type", want: ErrDenied},
			{name: "enrollment_binding_changed_after_read", mode: "binding", want: ErrDenied},
			{name: "current_identity_outage", mode: "outage", want: ErrUnavailable},
		} {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				m, program, _, _, _, identity, _ := managerFixture(t)
				program.cfg.CurrencyExponent = 2
				at := m.deps.Clock.Now()
				r := &relationshipReadStub{page: managerRelationshipPage(at)}
				r.read = func() {
					switch tc.mode {
					case "inactive":
						identity.principal.Active = false
					case "unverified":
						identity.principal.EmailVerified = false
					case "type":
						identity.principal.Individual = false
					case "binding":
						program.partner.CustomerID = "other-customer"
					case "outage":
						identity.fail = ErrUnavailable
					}
				}
				m.deps.Referral = r
				m.deps.Earnings = &summaryEarningsStub{report: partnerearnings.ReferralAmountReport{ProgramID: program.partner.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "financial", AsOf: at, Items: []partnerearnings.ReferralAmounts{{ID: r.page.Items[0].ID}}}}
				var out any
				var err error
				if method == "history" {
					out, err = m.ReferralHistory(context.Background(), "owner", referral.RelationshipQuery{Limit: 1})
				} else {
					out, err = m.ReferralSummaries(context.Background(), "owner", ReferralSummaryQuery{Limit: 1})
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out)
				} else {
					require.NotEmpty(t, out)
				}
			})
		}
	}
}

func (r *relationshipReadStub) ListRelationships(_ context.Context, partner string, _ referral.RelationshipQuery) (referral.RelationshipPage, error) {
	r.calls++
	r.requested = partner
	if r.read != nil {
		r.read()
	}
	return r.page, r.err
}

func managerRelationshipPage(at time.Time) referral.RelationshipPage {
	until := at.Add(time.Hour)
	item := referral.Relationship{ID: referral.RelationshipReferenceID(referral.ProgramID, "partner", "private-customer"), ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: "private-customer", FirstOwnedAt: at, Periods: []referral.OwnershipPeriod{{ReferralID: "private-revision", From: at, Until: &until, Terms: referral.TermsSnapshot{Currency: "EUR", CurrencyExponent: 2, RateBasisPoints: 2000, HoldDurationDays: 7, TermsVersion: "fixture-terms", PolicyVersionIDs: []string{"private-policy"}, EligiblePlanIDs: []string{"private-plan"}}}}}
	return referral.RelationshipPage{ProgramID: referral.ProgramID, PartnerID: "partner", Items: []referral.Relationship{item}}
}

// Audit disposition: named customer/admin scope, current authority and safe
// projection cases with fresh fixtures. Failing reads always discard data.
func TestManagerRelationshipReadBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "former_owner_can_read_retained_periods"},
		{name: "current_owner_can_read_current_period", mode: "current"},
		{name: "paused_admission_preserves_reads", mode: "paused"},
		{name: "denied_before_read", mode: "denied", want: ErrDenied},
		{name: "revoked_during_read", mode: "revoked", want: ErrDenied},
		{name: "joined_absence_and_outage", mode: "outage", want: ErrUnavailable},
		{name: "wrong_partner", mode: "partner", want: ErrUnavailable},
		{name: "wrong_program", mode: "program", want: ErrUnavailable},
		{name: "wrong_enrollment_program", mode: "enrollment", want: ErrUnavailable},
		{name: "currency_mismatch", mode: "currency", want: ErrUnavailable},
		{name: "relationship_identity_mismatch", mode: "identity", want: ErrUnavailable},
		{name: "false_current_claim", mode: "state", want: ErrUnavailable},
		{name: "cursor_mismatch", mode: "cursor", want: ErrUnavailable},
		{name: "duplicate_row", mode: "duplicate", want: ErrUnavailable},
		{name: "missing_owned_period", mode: "period", want: ErrUnavailable},
		{name: "invalid_limit", mode: "limit", want: ErrInvalid},
	}
	for _, admin := range []bool{false, true} {
		prefix := "customer/"
		if admin {
			prefix = "operator/"
		}
		for _, tc := range cases {
			t.Run(prefix+tc.name, func(t *testing.T) {
				m, program, _, _, a, _, _ := managerFixture(t)
				program.cfg.CurrencyExponent = 2
				r := &relationshipReadStub{page: managerRelationshipPage(m.deps.Clock.Now())}
				m.deps.Referral = r
				q := referral.RelationshipQuery{Limit: 1}
				switch tc.mode {
				case "current":
					r.page.Items[0].Current = true
					r.page.Items[0].Periods[0].Until = nil
				case "paused":
					m.deps.Controls = Controls{}
				case "denied":
					a.deny = true
				case "revoked":
					r.read = func() { a.deny = true }
				case "outage":
					r.err = errors.Join(referral.ErrNotFound, ErrUnavailable)
				case "partner":
					r.page.Items[0].PartnerID = "other"
				case "program":
					r.page.ProgramID = "other"
				case "enrollment":
					program.partner.ProgramID = "other"
				case "currency":
					r.page.Items[0].Periods[0].Terms.Currency = "USD"
				case "identity":
					r.page.Items[0].ID = "wrong"
				case "state":
					r.page.Items[0].Current = true
				case "cursor":
					r.page.HasMore, r.page.NextAfter = true, "wrong"
				case "duplicate":
					q.Limit = 2
					r.page.Items = append(r.page.Items, r.page.Items[0])
				case "period":
					r.page.Items[0].Periods = nil
				case "limit":
					q.Limit = 101
				}
				var out any
				var err error
				if admin {
					out, err = m.AdminReferralHistory(context.Background(), "operator", "partner", q)
				} else {
					out, err = m.ReferralHistory(context.Background(), "owner", q)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Empty(t, out)
					if tc.mode == "denied" || tc.mode == "limit" || tc.mode == "enrollment" {
						require.Zero(t, r.calls)
					}
					return
				}
				require.Equal(t, "partner", r.requested)
				if admin {
					require.Equal(t, []string{CapabilityReporting, CapabilityReporting}, a.calls)
				} else {
					page := out.(CustomerReferralPage)
					require.Len(t, page.Items, 1)
					require.Equal(t, r.page.Items[0].ID, page.Items[0].ID)
					require.Equal(t, tc.mode == "current", page.Items[0].Current)
					wire, err := json.Marshal(page)
					require.NoError(t, err)
					for _, private := range []string{"private-customer", "private-revision", "private-policy", "private-plan", "partner_id", "source_code", "correction_reason", "referred_customer"} {
						require.NotContains(t, string(wire), private)
					}
					require.Equal(t, []string{CapabilitySelf, CapabilitySelf}, a.calls)
				}
			})
		}
	}
}
