package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRevenueDiagnosticPrivacyAndConservativeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, reason, wantReason string
		err                      error
		wantCause                string
	}{
		{"original_pending", "historical_payer_plan_pending", "historical_payer_plan_pending", billing.ErrRevenueUnassessable, "revenue_unassessable"},
		{"new_current_reason", "original_payment_pending", "original_payment_pending", billing.ErrRevenueConflict, "revenue_conflict"},
		{"private_provider_reason", "user@example.test signed-provider-payload", "unknown", errors.New("private-error"), "unclassified"},
		{"empty_reason", "", "none", context.Canceled, "cancelled"},
		{"wrapped", "no_subscription_revenue", "no_subscription_revenue", fmt.Errorf("private: %w", billing.ErrRevenueUncertain), "revenue_uncertain"},
		{"joined_refusal_and_outage", "invoice_economics_unassessable", "invoice_economics_unassessable", errors.Join(billing.ErrRevenueUnassessable, billing.ErrRevenueUnavailable), "unclassified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantReason, diagnosticQuarantineReason(tc.reason))
			require.Equal(t, tc.wantCause, revenueDiagnosticCause(tc.err))
		})
	}
}
