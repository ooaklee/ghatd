package partnermanager

import (
	"errors"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Narrow ports observe facade authorization and owning-read binding. They
// implement no policy grant, claim transition, receipt or financial algorithm.
// Native owner tests separately prove an unchanged stored claim and statement.
func TestAdminClaimForActionUsesSelectedCurrentPermission(t *testing.T) {
	for _, tc := range []struct {
		name, capability, mode string
		want                   error
		reads, checks          int
	}{
		{"processing", CapabilityProcessing, "", nil, 1, 2},
		{"recording", CapabilityRecordPayment, "", nil, 1, 2},
		{"amendment", CapabilityAmendPayment, "", nil, 1, 2},
		{"return", CapabilityReturnPayment, "", nil, 1, 2},
		{"paused_admission_preserves_read", CapabilityRecordPayment, "paused", nil, 1, 2},
		{"another_claim_scope_denied", CapabilityAmendPayment, "target", ErrDenied, 0, 1},
		{"another_action_cannot_be_borrowed", CapabilityAmendPayment, "permission", ErrDenied, 0, 1},
		{"another_actor_denied", CapabilityReturnPayment, "actor", ErrDenied, 0, 1},
		{"revoked_before_read", CapabilityProcessing, "revoked", ErrDenied, 0, 1},
		{"revoked_during_owner_read", CapabilityReturnPayment, "late", ErrDenied, 1, 2},
		{"owner_absence_retained", CapabilityProcessing, "absent", partnerearnings.ErrNotFound, 1, 1},
		{"owner_outage_retained", CapabilityAmendPayment, "outage", partnerearnings.ErrUnavailable, 1, 1},
		{"joined_absence_outage_retained", CapabilityRecordPayment, "joined", partnerearnings.ErrUnavailable, 1, 1},
		{"wrong_claim_rejected", CapabilityReturnPayment, "claim", ErrUnavailable, 1, 1},
		{"wrong_program_rejected", CapabilityProcessing, "program", ErrUnavailable, 1, 1},
		{"wrong_currency_rejected", CapabilityRecordPayment, "currency", ErrUnavailable, 1, 1},
		{"missing_partner_rejected", CapabilityAmendPayment, "partner", ErrUnavailable, 1, 1},
		{"zero_amount_rejected", CapabilityReturnPayment, "amount", ErrUnavailable, 1, 1},
		{"zero_revision_rejected", CapabilityProcessing, "revision", ErrUnavailable, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			a := &paymentClaimAuthority{permission: tc.capability, target: "claim"}
			e := &paymentClaimReadPort{claim: partnerearnings.Claim{ID: "claim", ProgramID: partnerprogram.ProgramID, PartnerID: "partner", AmountMinor: 100, Currency: "EUR", Revision: 1}}
			m.deps.Authority, m.deps.Earnings = a, e
			actor := "operator"
			switch tc.mode {
			case "paused":
				m.deps.Controls.Claims, m.deps.Controls.ManualRecording = false, false
			case "target":
				a.target = "other"
			case "permission":
				a.permission = CapabilityRecordPayment
			case "actor":
				actor = "other"
			case "revoked":
				a.revoked = true
			case "late":
				e.after = func() { a.revoked = true }
			case "absent":
				e.err = partnerearnings.ErrNotFound
			case "outage":
				e.err = partnerearnings.ErrUnavailable
			case "joined":
				e.err = errors.Join(partnerearnings.ErrNotFound, partnerearnings.ErrUnavailable)
			case "claim":
				e.claim.ID = "other"
			case "program":
				e.claim.ProgramID = "other"
			case "currency":
				e.claim.Currency = "USD"
			case "partner":
				e.claim.PartnerID = ""
			case "amount":
				e.claim.AmountMinor = 0
			case "revision":
				e.claim.Revision = 0
			}
			out, err := m.AdminClaimForAction(t.Context(), actor, tc.capability, "claim")
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.reads, e.reads)
			require.Equal(t, tc.checks, a.checks)
			if tc.want == nil {
				require.Equal(t, e.claim, out)
			} else {
				require.Equal(t, partnerearnings.Claim{}, out)
			}
		})
	}
}

func TestAdminClaimForActionRejectsUnselectedOrUnsupportedInput(t *testing.T) {
	for _, tc := range []struct{ name, capability, claim string }{
		{"unknown", "partner.admin.unknown", "claim"},
		{"generic_admin", "ADMIN", "claim"},
		{"self", CapabilitySelf, "claim"},
		{"enrollment", CapabilityEnroll, "claim"},
		{"customer_claims", CapabilityClaims, "claim"},
		{"policy", CapabilityPolicy, "claim"},
		{"reporting", CapabilityReporting, "claim"},
		{"operations", CapabilityOperations, "claim"},
		{"attribution", CapabilityAttribution, "claim"},
		{"on_behalf", CapabilityCreateClaimOnBehalf, "claim"},
		{"revenue_worker", CapabilityRevenueWorker, "claim"},
		{"signup_worker", CapabilitySignupWorker, "claim"},
		{"maturity_worker", CapabilityMaturityWorker, "claim"},
		{"queue_target", CapabilityProcessing, ""},
		{"oversized_target", CapabilityRecordPayment, strings.Repeat("x", 257)},
		{"padded_target", CapabilityAmendPayment, " claim"},
		{"control_target", CapabilityReturnPayment, "claim\x00x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, _, _, _ := managerFixture(t)
			a := &paymentClaimAuthority{permission: tc.capability, target: tc.claim}
			e := &paymentClaimReadPort{}
			m.deps.Authority, m.deps.Earnings = a, e
			out, err := m.AdminClaimForAction(t.Context(), "operator", tc.capability, tc.claim)
			require.ErrorIs(t, err, ErrInvalid)
			require.Equal(t, partnerearnings.Claim{}, out)
			require.Zero(t, a.checks)
			require.Zero(t, e.reads)
		})
	}
}
