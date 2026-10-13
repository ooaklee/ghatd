package billinglifecycle

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type dispositionFailureRepository struct{ *RecordExecutionRepository }

func (r dispositionFailureRepository) CompleteCheckout(context.Context, CheckoutObservation) (CompletedExecution, error) {
	return CompletedExecution{}, recordstore.ErrUnavailable
}
func (r dispositionFailureRepository) ExpireCheckout(context.Context, CheckoutExpiryObservation) (CompletedExecution, error) {
	return CompletedExecution{}, recordstore.ErrUnavailable
}
func (r dispositionFailureRepository) Release(context.Context, LeaseDisposition) (ScheduledJob, error) {
	return ScheduledJob{}, recordstore.ErrUnavailable
}

func TestCheckoutDispositionNativeFailureStages(t *testing.T) {
	for _, tc := range []struct {
		name, status, payment, stage string
		absent                       bool
	}{
		{"complete_failure", "complete", "paid", "checkout_complete", false},
		{"pending_failure", "open", "unpaid", "checkout_pending", true},
		{"expiry_failure", "expired", "unpaid", "checkout_expire", true},
		{"invalid_status_remains_observation", "expired", "unpaid", "checkout_observe", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := nativeCheckoutHTTPFixture(t, tc.status, tc.payment, tc.absent, false)
			repo := dispositionFailureRepository{f.repo}
			f.execution.scheduler.repo = repo
			c, err := NewCheckoutCompletion(f.execution, repo)
			require.NoError(t, err)
			worker := &Worker{scheduler: f.execution.scheduler, checkout: f.execution, checkoutCompletion: c}
			core, logs := observer.New(zap.DebugLevel)
			ctx := logger.TransitWith(f.ctx, zap.New(core))
			_, _, _, err = worker.execute(ctx, f.job)
			require.Error(t, err)
			entries := logs.FilterMessage("billing-lifecycle-execution-failed").All()
			require.Len(t, entries, 1)
			require.Equal(t, tc.stage, entries[0].ContextMap()["stage"])
			require.Equal(t, "billing.lifecycle.execute", entries[0].ContextMap()["owner_operation"])
			require.NotContains(t, entries[0].ContextMap(), "session_id")
			require.Zero(t, f.writes.Load())
		})
	}
}
