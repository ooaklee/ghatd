package partnerhttp

import (
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestAcceptedPaymentRequiresOriginalOwningEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		valid        bool
	}{
		{"original_full_payment", "none", true}, {"later_display_amendment_does_not_replace_original", "amended", true},
		{"other_claim", "id", false}, {"no_advanced_revision", "revision", false}, {"missing_payment", "missing", false},
		{"another_actor_payment", "actor", false}, {"changed_reference", "reference", false}, {"changed_date", "date", false}, {"unrecorded_payment", "recorded", false},
		{"original_unknown_observation_after_paid", "observation", true}, {"another_actor_observation", "observation-actor", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			req := partnerearnings.RecordPaymentRequest{ClaimID: "claim", ActorID: "actor", Method: "bank", Reference: "original", PaidAt: at, AmountMinor: 100, Currency: "GBP", State: "full", ExpectedRevision: 2, IdempotencyKey: "key"}
			c := partnerearnings.Claim{ID: "claim", Revision: 8, State: "paid", Payment: &partnerearnings.ManualPayment{Method: "bank", Reference: "original", PaidAt: at, RecordedBy: "actor", RecordedAt: at, AmountMinor: 100, Currency: "GBP", State: "full"}}
			switch tc.change {
			case "id":
				c.ID = "another"
			case "revision":
				c.Revision = 2
			case "missing":
				c.Payment = nil
			case "actor":
				c.Payment.RecordedBy = "another"
			case "reference":
				c.Payment.Reference = "changed"
			case "date":
				c.Payment.PaidAt = at.Add(time.Second)
			case "recorded":
				c.Payment.RecordedAt = time.Time{}
			case "amended":
				c.PaymentAmendments = []partnerearnings.PaymentAmendment{{Reference: "later", By: "another", At: at, IdempotencyKey: "later"}}
			case "observation", "observation-actor":
				req.State = "unknown"
				req.AmountMinor = 0
				req.Currency = ""
				c.PaymentObservations = []partnerearnings.PaymentObservation{{Method: "bank", Reference: "original", PaidAt: at, RecordedBy: "actor", RecordedAt: at, AmountMinor: 0, Currency: "", State: "unknown"}}
				if tc.change == "observation-actor" {
					c.PaymentObservations[0].RecordedBy = "another"
				}
			}
			evidence := acceptedPayment(c, req)
			require.Equal(t, tc.valid, evidence != nil)
			if evidence != nil {
				require.NotContains(t, evidence, "recorded_by")
				require.Equal(t, "original", evidence["reference"])
			}
			_, err := operatorReceipt(c, evidence)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
			}
		})
	}
}
func TestAcceptedAmendmentAndReturnSelectCallerAndOriginalKey(t *testing.T) {
	for _, tc := range []struct {
		name, change string
		valid        bool
	}{
		{"original_append_after_later_change", "none", true}, {"wrong_actor", "actor", false}, {"wrong_key", "key", false}, {"wrong_reason", "reason", false}, {"wrong_recorded_time", "date", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			amend := partnerearnings.AmendPaymentRequest{ClaimID: "claim", ActorID: "actor", IdempotencyKey: "original", Method: "bank", Reference: "first", PaidAt: at, Reason: "first reason", ExpectedRevision: 3}
			returned := partnerearnings.ReturnRequest{ClaimID: "claim", ActorID: "actor", IdempotencyKey: "original", AmountMinor: 40, Currency: "GBP", Reference: "first", ReturnedAt: at, Reason: "first reason", ExpectedRevision: 3}
			a := partnerearnings.PaymentAmendment{Method: "bank", Reference: "first", PaidAt: at, Reason: "first reason", By: "actor", At: at, IdempotencyKey: "original"}
			r := partnerearnings.ReturnedAdjustment{AmountMinor: 40, Currency: "GBP", Reference: "first", ReturnedAt: at, Reason: "first reason", By: "actor", At: at, IdempotencyKey: "original"}
			switch tc.change {
			case "actor":
				a.By = "another"
				r.By = "another"
			case "key":
				a.IdempotencyKey = "another"
				r.IdempotencyKey = "another"
			case "reason":
				a.Reason = "another"
				r.Reason = "another"
			case "date":
				a.At = time.Time{}
				r.At = time.Time{}
			}
			c := partnerearnings.Claim{ID: "claim", Revision: 8, PaymentAmendments: []partnerearnings.PaymentAmendment{a, {Method: "bank", Reference: "later", By: "actor", At: at, IdempotencyKey: "later"}}, ReturnedAdjustments: []partnerearnings.ReturnedAdjustment{r, {AmountMinor: 60, Currency: "GBP", By: "actor", At: at, IdempotencyKey: "later"}}}
			require.Equal(t, tc.valid, acceptedAmendment(c, amend) != nil)
			require.Equal(t, tc.valid, acceptedReturn(c, returned) != nil)
		})
	}
}
func TestPolicyProjectionPreservesAllPlanInheritanceStates(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plans []string
	}{{"inherit", nil}, {"exclude_all", []string{}}, {"explicit", []string{"plan"}}} {
		t.Run(tc.name, func(t *testing.T) {
			out := policyView(partnerprogram.PolicyVersion{EligiblePlanIDs: tc.plans})
			require.Equal(t, tc.plans, out["eligible_plan_ids"])
			if len(tc.plans) > 0 {
				out["eligible_plan_ids"].([]string)[0] = "changed"
				require.Equal(t, "plan", tc.plans[0])
			}
		})
	}
}
