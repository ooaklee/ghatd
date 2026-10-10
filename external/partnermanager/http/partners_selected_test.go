package partnerhttp

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Nil owner deliberately makes valid dispatch unavailable. Malformed input
// must reject before that boundary. Real selected reads and safe HTTP response
// shapes are exercised by the native composed server table, not these fixtures.
func TestPartnersSelectedReadRejectsAmbiguousAndUntrustedSelection(t *testing.T) {
	for _, tc := range []struct {
		name, operation, target string
		query                   url.Values
		body                    []byte
		status                  int
	}{
		{"processing_dispatch", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityProcessing}}, nil, 503},
		{"recording_dispatch", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityRecordPayment}}, nil, 503},
		{"amendment_dispatch", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityAmendPayment}}, nil, 503},
		{"return_dispatch", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityReturnPayment}}, nil, 503},
		{"status_dispatch", "admin.partners.status.read", "partner", nil, nil, 503},
		{"preparation_dispatch", "admin.partners.claim-preparation.read", "partner", nil, nil, 503},
		{"individual_history_dispatch", "admin.partners.policy.individual.read", "customer", nil, nil, 503},
		{"no_capability", "admin.partners.claim.read", "claim", nil, nil, 400},
		{"wrong_query_key", "admin.partners.claim.read", "claim", url.Values{"cap": {partnermanager.CapabilityProcessing}}, nil, 400},
		{"duplicate_capability", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityProcessing, partnermanager.CapabilityRecordPayment}}, nil, 400},
		{"empty_capability_values", "admin.partners.claim.read", "claim", url.Values{"capability": {}}, nil, 400},
		{"forged_actor_query", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityRecordPayment}, "actor": {"other"}}, nil, 400},
		{"reporting_is_not_a_claim_action", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityReporting}}, nil, 400},
		{"generic_admin_is_not_a_claim_action", "admin.partners.claim.read", "claim", url.Values{"capability": {"ADMIN"}}, nil, 400},
		{"worker_is_not_a_claim_action", "admin.partners.claim.read", "claim", url.Values{"capability": {partnermanager.CapabilityMaturityWorker}}, nil, 400},
		{"status_cannot_choose_capability", "admin.partners.status.read", "partner", url.Values{"capability": {partnermanager.CapabilityReporting}}, nil, 400},
		{"preparation_cannot_choose_customer", "admin.partners.claim-preparation.read", "partner", url.Values{"customer_id": {"other"}}, nil, 400},
		{"history_cannot_change_scope", "admin.partners.policy.individual.read", "customer", url.Values{"scope": {"global"}}, nil, 400},
		{"empty_list_target", "admin.partners.status.read", "", nil, nil, 400},
		{"oversized_target", "admin.partners.claim-preparation.read", strings.Repeat("x", 257), nil, nil, 400},
		{"control_target", "admin.partners.policy.individual.read", "customer\nvalue", nil, nil, 400},
		{"unknown_operation", "admin.partners.unknown", "partner", nil, nil, 400},
		{"body_cannot_supply_actor", "admin.partners.status.read", "partner", nil, []byte(`{"actor":"other"}`), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := partnersSelectedRead(t.Context(), nil, Principal{ActorID: "resolved-actor"}, Request{Operation: tc.operation, ID: tc.target, Query: tc.query, Body: tc.body})
			var mapped *Error
			require.ErrorAs(t, partnerError(err), &mapped)
			require.Equal(t, tc.status, mapped.Status)
			require.Empty(t, response.Body)
		})
	}
}

func TestSelectedIndividualPolicyProjectionPreservesInheritanceWithoutAuditIdentity(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plans []string
		json  string
	}{
		{"inherit", nil, "null"}, {"exclude_all", []string{}, "[]"}, {"explicit_plans", []string{"plan"}, `["plan"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := partnerprogram.PolicyVersion{ID: "policy", ProgramID: "private-program", Scope: "individual", PartnerCustomer: "selected-customer", Revision: 2, EligiblePlanIDs: tc.plans, PublishedBy: "private-operator", GroupID: "private-group", PublishedAt: time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)}
			out := selectedIndividualPolicyView(v)
			require.Len(t, out, 14)
			for _, key := range []string{"published_by", "group_id", "program_id"} {
				require.NotContains(t, out, key)
			}
			raw, err := json.Marshal(out["eligible_plan_ids"])
			require.NoError(t, err)
			require.JSONEq(t, tc.json, string(raw))
			if len(tc.plans) != 0 {
				out["eligible_plan_ids"].([]string)[0] = "edited"
				require.Equal(t, "plan", v.EligiblePlanIDs[0])
			}
		})
	}
}
