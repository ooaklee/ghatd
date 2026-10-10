package partnermanagerhelper

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
	partnerruntime "github.com/ooaklee/ghatd/external/partnermanager/runtime"
)

func TestExecutionInactiveAndIncompleteWiring(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  ExecutionConfig
		want error
	}{
		{"inactive_does_not_acquire_owners", ExecutionConfig{}, nil},
		{"capture_requires_prepared_owners", ExecutionConfig{RevenueCapture: true}, partnermanager.ErrUnavailable},
		{"worker_requires_prepared_owners", ExecutionConfig{Worker: &partnerruntime.WorkerConfig{}}, partnermanager.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			worker, err := NewExecution(context.Background(), tc.cfg, ExecutionDependencies{})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
			require.Nil(t, worker)
		})
	}
}
func TestExecutionZeroValuesNeverBindOrRun(t *testing.T) {
	for _, tc := range []struct {
		name string
		pass *Execution
	}{{"nil", nil}, {"zero", &Execution{}}} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := tc.pass.RunOnce(t.Context())
			require.ErrorIs(t, err, partnermanager.ErrUnavailable)
			require.Zero(t, report)
			require.Zero(t, tc.pass.Interval())
		})
	}
}
