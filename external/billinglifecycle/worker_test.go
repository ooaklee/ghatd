package billinglifecycle

import (
	"context"
	"errors"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

func TestWorkerCompositionIdentity(t *testing.T) {
	for _, name := range []string{"same_pointer", "different_pointer", "pointer_with_map", "comparable_value", "different_type", "noncomparable_value", "nil", "typed_nil"} {
		t.Run(name, func(t *testing.T) {
			type box struct{ data map[string]int }
			value := &box{data: map[string]int{"one": 1}}
			var a, b any = value, value
			want := false
			switch name {
			case "same_pointer", "pointer_with_map":
				want = true
			case "different_pointer":
				b = &box{data: map[string]int{"one": 1}}
			case "comparable_value":
				a, b = 1, 1
				want = true
			case "different_type":
				a, b = 1, int64(1)
			case "noncomparable_value":
				a, b = box{}, box{}
			case "nil":
				a = nil
			case "typed_nil":
				var p *box
				a = p
			}
			require.Equal(t, want, sameWorkerPort(a, b))
		})
	}
}

// Isolated inspection authority/withholding, not native storage or lease proof.
func TestSchedulerInspectCurrentAuthority(t *testing.T) {
	outage := errors.New("controlled inspection outage")
	for _, tc := range []struct {
		name string
		want error
	}{
		{"current", nil}, {"read_outage", outage}, {"initial_denied", partnermanager.ErrDenied},
		{"read_then_denied", partnermanager.ErrDenied}, {"unknown_then_denied", recordstore.ErrUncertain},
		{"changed_owner", billing.ErrRevenueUnavailable}, {"invalid_job", billing.ErrRevenueUnavailable}, {"unconfigured_scope", billing.ErrRevenueInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, a, _ := schedulerServiceFixture(t)
			source := r.snapshot.Jobs[0].Source
			switch tc.name {
			case "read_outage":
				r.readErr = outage
			case "initial_denied":
				a.denyStage = ""
			case "read_then_denied":
				a.denyStage = "read-job"
			case "unknown_then_denied":
				r.readErr = recordstore.ErrUncertain
				a.denyStage = "read-job"
			case "changed_owner":
				r.readMode = "changed_owner"
			case "invalid_job":
				r.snapshot.Jobs[0].Revision = 0
			case "unconfigured_scope":
				source.Scope.AccountID = "other"
			}
			out, err := s.Inspect(context.Background(), source)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, ScheduledJob{}, out)
			} else {
				require.NoError(t, err)
				require.True(t, sameExecutionJob(r.snapshot.Jobs[0], out))
			}
			if tc.name == "unknown_then_denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			require.Zero(t, r.acquireCalls, "inspection must not grant a lease")
			require.Zero(t, r.cursorCalls)
		})
	}
}

func TestLifecycleWorkerFinalAuthorityWithholdsCounts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		original error
	}{
		{"known_success", nil}, {"known_failure", billing.ErrRevenueUnavailable},
		{"native_unknown", billing.ErrRevenueUncertain}, {"store_unknown", recordstore.ErrUncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, r, a, _ := schedulerServiceFixture(t)
			a.selected = true
			a.denyStage = "" // Scope-wide permission remains allowed.
			w := &Worker{scheduler: s}
			out, err := w.finish(context.Background(), []ScheduledSource{r.snapshot.Jobs[0].Source}, WorkerReport{Completed: 1}, tc.original)
			require.ErrorIs(t, err, partnermanager.ErrDenied)
			require.Equal(t, WorkerReport{}, out)
			if unknownExecution(tc.original) {
				require.ErrorIs(t, err, tc.original)
			} else {
				require.NotErrorIs(t, err, recordstore.ErrUncertain)
				require.NotErrorIs(t, err, billing.ErrRevenueUncertain)
			}
		})
	}
}
