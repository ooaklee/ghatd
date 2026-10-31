package partners

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type partnersPassFixture struct {
	started chan int32
	calls   atomic.Int32
	fail    bool
	block   bool
}

func (p *partnersPassFixture) RunOnce(ctx context.Context) (partnermanager.WorkerReport, error) {
	n := p.calls.Add(1)
	ghatdlogger.AcquireFrom(ctx).Debug("partners-pass-diagnostic-probe")
	select {
	case p.started <- n:
	case <-ctx.Done():
		return partnermanager.WorkerReport{}, ctx.Err()
	}
	if p.block {
		<-ctx.Done()
		return partnermanager.WorkerReport{}, ctx.Err()
	}
	if p.fail && n == 1 {
		return partnermanager.WorkerReport{Discovered: 2, Completed: 1, Retried: 1, Issues: []partnermanager.WorkerIssue{{Kind: "private-kind", Code: "private-code"}}}, errors.New("private-provider-payload signed-cookie@example.test")
	}
	return partnermanager.WorkerReport{Completed: 1}, nil
}

func TestPartnersWorkerSchedulerLifecycleAndPrivateLogs(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		fail, block, cancelled bool
	}{
		{name: "sequential_passes_and_cancellation"}, {name: "failed_pass_does_not_stop_recovery", fail: true}, {name: "inflight_pass_drains_on_cancellation", block: true}, {name: "cancelled_start_never_invokes_owner", cancelled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pass := &partnersPassFixture{started: make(chan int32, 2), fail: tc.fail, block: tc.block}
			done := make(chan struct{})
			core, logs := observer.New(zap.DebugLevel)
			if tc.cancelled {
				cancel()
			}
			go RunWorker(ctx, pass, 5*time.Millisecond, done, zap.New(core))
			if !tc.cancelled {
				select {
				case n := <-pass.started:
					require.Equal(t, int32(1), n)
				case <-time.After(time.Second):
					t.Fatal("first pass did not start")
				}
				if !tc.block {
					select {
					case n := <-pass.started:
						require.Equal(t, int32(2), n)
					case <-time.After(time.Second):
						t.Fatal("next pass did not recover")
					}
				}
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("worker did not drain")
			}
			if tc.cancelled {
				require.Zero(t, pass.calls.Load())
			}
			if tc.block {
				require.Equal(t, int32(1), pass.calls.Load())
			}
			for _, entry := range logs.All() {
				require.NotContains(t, entry.Message, "signed-cookie")
				require.NotContains(t, entry.ContextMap(), "error")
				require.NotContains(t, entry.ContextMap(), "actor")
				require.NotContains(t, entry.ContextMap(), "source")
				for _, field := range entry.Context {
					require.Contains(t, []string{"discovered", "completed", "retried", "issues", "issue_counts", "worker_pass_id"}, field.Key)
				}
			}
			if tc.fail {
				require.Equal(t, 1, logs.FilterMessage("partners-worker-pass-pending").Len())
				failure := logs.FilterMessage("partners-worker-pass-pending").All()[0].ContextMap()
				firstProbe := logs.FilterMessage("partners-pass-diagnostic-probe").All()[0].ContextMap()
				require.Equal(t, firstProbe["worker_pass_id"], failure["worker_pass_id"])
				require.NotEmpty(t, failure["worker_pass_id"])
				require.Equal(t, map[string]int{"unknown.unknown": 1}, failure["issue_counts"])
				require.NotContains(t, failure, "private-kind")
			}
		})
	}
}

func TestPartnersWorkerIssueCountsPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name   string
		issues []partnermanager.WorkerIssue
		want   map[string]int
	}{
		{"bounded_and_aggregated", []partnermanager.WorkerIssue{{Kind: partnermanager.WorkRevenueSource, Code: "review_pending"}, {Kind: partnermanager.WorkRevenueSource, Code: "review_pending"}}, map[string]int{"revenue_source.review_pending": 2}},
		{"untrusted_port_strings", []partnermanager.WorkerIssue{{Kind: "user@example.test", Code: "provider-private-payload"}}, map[string]int{"unknown.unknown": 1}},
		{"empty", nil, map[string]int{}},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, workerIssueCounts(tc.issues)) })
	}
}
