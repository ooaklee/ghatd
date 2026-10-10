package billingmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
)

// Actual owning checkout services/codecs per case; authority outcomes and lost
// replies are controlled. Native encryption and runtime qualification are separate.
func TestCheckoutLifecycleManagerUncertainOutcomes(t *testing.T) {
	type testCase struct {
		name, stage, late string
		unknown           bool
	}
	var cases []testCase
	for _, stage := range []string{"prepare", "validate", "lookup", "capture", "receipt"} {
		for _, late := range []string{"allowed", "denied", "canceled", "known_denied"} {
			cases = append(cases, testCase{fmt.Sprintf("%s_%s", stage, late), stage, late, late != "known_denied"})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, owner, provider, a, records, i := completionManagerFixture(t)
			cause := error(billing.ErrRevenueUncertain)
			if !tc.unknown {
				cause = errors.New("controlled known owning failure")
			}
			target := tc.stage
			if target == "validate" {
				target = "prepare"
			}
			switch target {
			case "prepare":
				owner.prepareErr = cause
			case "lookup":
				owner.lookupErr = cause
			case "capture":
				owner.captureErr = cause
			case "receipt":
				owner.receiptErr = cause
			}
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			owner.onStage = func(stage string) {
				if stage != target {
					return
				}
				if tc.late == "denied" || tc.late == "known_denied" {
					a.denyAt = a.calls + 1
				}
				if tc.late == "canceled" {
					cancel()
				}
			}
			var err error
			switch tc.stage {
			case "prepare":
				out, e := s.PrepareCheckoutLifecycle(ctx, "current-worker", i)
				err = e
				require.Zero(t, out)
			case "validate":
				err = s.ValidateCheckoutLifecycle(ctx, "current-worker", i)
			case "lookup":
				out, e := s.LookupCheckoutLifecycleEvidence(ctx, "current-worker", i)
				err = e
				require.Zero(t, out)
			case "capture":
				out, e := s.CaptureCheckoutLifecycleEvidence(ctx, "current-worker", i, provider.e)
				err = e
				require.Zero(t, out)
			case "receipt":
				out, e := s.FindCheckoutLifecycleReceipt(ctx, "current-worker", i)
				err = e
				require.Zero(t, out)
			}
			if tc.unknown {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
			} else {
				require.NotErrorIs(t, err, billing.ErrRevenueUncertain)
			}
			if tc.late == "denied" || tc.late == "known_denied" {
				require.ErrorIs(t, err, a.denied)
			} else if tc.late == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, cause)
			}
			if tc.stage == "capture" {
				// The actual owner already committed, even though its wrapper lost the
				// reply. Restore current authority and recover the exact receipt/inputs.
				owner.onStage = nil
				owner.captureErr = nil
				a.denyAt = 0
				receipt, e := s.FindCheckoutLifecycleReceipt(t.Context(), "replacement-worker", i)
				require.NoError(t, e)
				require.NoError(t, receipt.ValidateCapturedEvidence(i, provider.e))
				again, e := s.CaptureCheckoutLifecycleEvidence(t.Context(), "replacement-worker", i, provider.e)
				require.NoError(t, e)
				require.Equal(t, receipt, again)
				require.Zero(t, provider.calls)
			}
			for _, row := range records.records {
				require.NotEqual(t, "billing_revenue_fact", row.Kind)
			}
		})
	}
}
