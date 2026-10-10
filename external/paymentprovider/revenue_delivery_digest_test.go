package paymentprovider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const stripeRefundDigestFixture = `{"id":"evt_refund","type":"charge.refunded","account":"acct_primary","livemode":false,"created":1700000001,"data":{"object":{"id":"ch_paid","object":"charge","payment_intent":"pi_paid","currency":"gbp","amount_refunded":100,"receipt_url":"https://receipts.example.test/original","metadata":{}}}}`

// Fixed compatibility value calculated from the pre-versioned canonical JSON.
const stripeRefundLegacyFixtureDigest = "2f1eb67d2af5133367da13cae650068f82a22c731f84a3f48e8c8bc14338cafd"

// Audit disposition: named snapshot cases isolate the one rendered charge URL
// from all retained economic/identity fields and verify the legacy hash remains
// an exact full-snapshot check. All values are controlled public fixtures.
func TestStripeRevenueDigestIgnoresOnlyRenderedRefundReceiptURL(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*stripeRevenueEvent)
		same   bool
	}{
		{name: "same_snapshot", same: true, mutate: func(*stripeRevenueEvent) {}},
		{name: "rendered_charge_receipt_url", same: true, mutate: func(e *stripeRevenueEvent) {
			e.Data.Object["receipt_url"] = json.RawMessage(`"https://receipts.example.test/rendered-other"`)
		}},
		{name: "missing_charge_receipt_url", same: true, mutate: func(e *stripeRevenueEvent) { delete(e.Data.Object, "receipt_url") }},
		{name: "refund_amount", mutate: func(e *stripeRevenueEvent) { e.Data.Object["amount_refunded"] = json.RawMessage(`200`) }},
		{name: "payment_reference", mutate: func(e *stripeRevenueEvent) { e.Data.Object["payment_intent"] = json.RawMessage(`"pi_other"`) }},
		{name: "currency", mutate: func(e *stripeRevenueEvent) { e.Data.Object["currency"] = json.RawMessage(`"usd"`) }},
		{name: "event_identity", mutate: func(e *stripeRevenueEvent) { e.ID = "evt_other" }},
		{name: "event_type", mutate: func(e *stripeRevenueEvent) { e.Type = "charge.succeeded" }},
		{name: "event_time", mutate: func(e *stripeRevenueEvent) { e.Created++ }},
		{name: "merchant", mutate: func(e *stripeRevenueEvent) { e.Account = "acct_other" }},
		{name: "mode", mutate: func(e *stripeRevenueEvent) { live := true; e.LiveMode = &live }},
		{name: "unrecognised_field_remains_bound", mutate: func(e *stripeRevenueEvent) { e.Data.Object["additional_evidence"] = json.RawMessage(`true`) }},
		{name: "other_url_remains_bound", mutate: func(e *stripeRevenueEvent) {
			e.Data.Object["different_url"] = json.RawMessage(`"https://example.test/other"`)
		}},
		{name: "nested_receipt_url_remains_bound", mutate: func(e *stripeRevenueEvent) { e.Data.Object["metadata"] = json.RawMessage(`{"receipt_url":"changed"}`) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var original stripeRevenueEvent
			require.NoError(t, json.Unmarshal([]byte(stripeRefundDigestFixture), &original))
			encoded, err := json.Marshal(original)
			require.NoError(t, err)
			var changed stripeRevenueEvent
			require.NoError(t, json.Unmarshal(encoded, &changed))
			tc.mutate(&changed)
			before, err := canonicalStripeRevenueDigest(original)
			require.NoError(t, err)
			after, err := canonicalStripeRevenueDigest(changed)
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(before, stripeRevenueDigestV2))
			require.Equal(t, tc.same, before == after)
			legacyBefore, err := legacyStripeRevenueDigest(original)
			require.NoError(t, err)
			require.Equal(t, stripeRefundLegacyFixtureDigest, legacyBefore)
			legacyAfter, err := legacyStripeRevenueDigest(changed)
			require.NoError(t, err)
			require.Equal(t, tc.name == "same_snapshot", legacyBefore == legacyAfter)
			stillOriginal, err := json.Marshal(original)
			require.NoError(t, err)
			require.Equal(t, string(encoded), string(stillOriginal), "canonicalization must not mutate the retained original")
		})
	}
}

func TestStripeRevenueReceiptURLProjectionIsRestricted(t *testing.T) {
	cases := []struct {
		name, eventType, objectType string
		stable                      bool
	}{
		{name: "refund_charge", eventType: "charge.refunded", objectType: "charge", stable: true},
		{name: "refund_non_charge", eventType: "charge.refunded", objectType: "refund"},
		{name: "refund_missing_object_type", eventType: "charge.refunded"},
		{name: "paid_invoice", eventType: "invoice.paid", objectType: "invoice"},
		{name: "dispute", eventType: "charge.dispute.created", objectType: "dispute"},
		{name: "other_charge_event", eventType: "charge.succeeded", objectType: "charge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var event stripeRevenueEvent
			require.NoError(t, json.Unmarshal([]byte(stripeRefundDigestFixture), &event))
			event.Type = tc.eventType
			body, err := json.Marshal(tc.objectType)
			require.NoError(t, err)
			event.Data.Object["object"] = body
			before, err := canonicalStripeRevenueDigest(event)
			require.NoError(t, err)
			event.Data.Object["receipt_url"] = json.RawMessage(`"https://receipts.example.test/changed"`)
			after, err := canonicalStripeRevenueDigest(event)
			require.NoError(t, err)
			require.Equal(t, tc.stable, before == after)
		})
	}
}
