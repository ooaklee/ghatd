package billinglifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func TestExecutionStageAbsenceClassification(t *testing.T) {
	outage := errors.New("controlled outage")
	for _, tc := range []struct {
		name   string
		err    error
		absent bool
	}{
		{"native_absence", billing.ErrRevenueNotFound, true},
		{"single_wrapped_absence", fmt.Errorf("owning read: %w", billing.ErrRevenueNotFound), true},
		{"joined_absence_outage", errors.Join(billing.ErrRevenueNotFound, outage), false},
		{"joined_absence_uncertainty", errors.Join(billing.ErrRevenueNotFound, billing.ErrRevenueUncertain), false},
		{"store_absence_not_native_absence", recordstore.ErrNotFound, false},
		{"outage", outage, false}, {"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.absent, soleRevenueNotFound(tc.err)) })
	}
}

func TestExecutionStageUnavailableDependencies(t *testing.T) {
	for _, name := range []string{"checkout_nil_outbox", "status_nil_outbox", "checkout_missing_owner", "status_missing_owner", "nil_checkout_execution", "nil_status_execution", "private_checkout_observation", "private_status_observation"} {
		t.Run(name, func(t *testing.T) {
			switch name {
			case "checkout_nil_outbox":
				x, err := NewCheckoutExecution(nil, nil, nil)
				require.Nil(t, x)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "status_nil_outbox":
				x, err := NewStatusExecution(nil, nil, nil)
				require.Nil(t, x)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "checkout_missing_owner":
				x, err := NewCheckoutExecution(nil, nil, &CheckoutOutbox{})
				require.Nil(t, x)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "status_missing_owner":
				x, err := NewStatusExecution(nil, nil, &StatusOutbox{})
				require.Nil(t, x)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "nil_checkout_execution":
				var x *CheckoutExecution
				out, err := x.Observe(context.Background(), LeaseHandle{})
				require.Equal(t, CheckoutObservation{}, out)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "nil_status_execution":
				var x *StatusExecution
				out, err := x.Observe(context.Background(), LeaseHandle{})
				require.Equal(t, StatusObservation{}, out)
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			case "private_checkout_observation":
				raw, err := json.Marshal(CheckoutObservation{Job: ScheduledJob{LeaseActor: "private"}, Input: CheckoutInput{Intent: billing.CheckoutIntent{ID: "private"}}})
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(raw))
			case "private_status_observation":
				raw, err := json.Marshal(StatusObservation{Job: ScheduledJob{LeaseActor: "private"}, Input: StatusInput{Preparation: billing.SubscriptionStatusPreparation{ActorID: "private"}}})
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(raw))
			}
		})
	}
}

func TestExecutionStepConservesAcknowledgedJob(t *testing.T) {
	for _, name := range []string{"unchanged_job", "lost_original_pointer", "changed_attempts", "changed_creation", "changed_due", "unknown_native_then_denied", "unknown_host_then_denied"} {
		t.Run(name, func(t *testing.T) {
			s, r, a, _ := schedulerServiceFixture(t)
			j := r.snapshot.Jobs[0]
			j.Revision = 2
			j.Fence = 1
			j.Attempts = 1
			j.LeaseActor = "worker-original"
			j.LeaseToken = "execution-step-token"
			j.LeasedUntil = j.CreatedAt.Add(time.Minute)
			r.leased = j
			var cause error
			switch name {
			case "lost_original_pointer":
				r.leased.OriginalStatus = nil
			case "changed_attempts":
				r.leased.Attempts++
			case "changed_creation":
				r.leased.CreatedAt = r.leased.CreatedAt.Add(time.Second)
			case "changed_due":
				r.leased.NextAttemptAt = r.leased.NextAttemptAt.Add(time.Second)
			case "unknown_native_then_denied":
				cause = billing.ErrRevenueUncertain
				a.denyStage = "check"
			case "unknown_host_then_denied":
				cause = recordstore.ErrUncertain
				a.denyStage = "check"
			}
			out, err := executionStep(context.Background(), s, j, cause)
			if name == "unchanged_job" {
				require.NoError(t, err)
				require.Equal(t, j, out)
			} else {
				require.Error(t, err)
				require.Equal(t, ScheduledJob{}, out)
			}
			if cause != nil {
				require.ErrorIs(t, err, cause)
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
		})
	}
}
