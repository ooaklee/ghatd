package partners

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type lifecyclePassFixture struct {
	calls                    atomic.Int32
	active                   atomic.Int32
	overlap                  atomic.Bool
	started                  chan int32
	fail, block, expireFirst bool
	pending, expired         int
}

func (p *lifecyclePassFixture) RunOnce(ctx context.Context) (billinglifecycle.WorkerReport, error) {
	if p.active.Add(1) != 1 {
		p.overlap.Store(true)
	}
	defer p.active.Add(-1)
	n := p.calls.Add(1)
	select {
	case p.started <- n:
	case <-ctx.Done():
		return billinglifecycle.WorkerReport{}, ctx.Err()
	}
	if p.block || p.expireFirst && n == 1 {
		<-ctx.Done()
		return billinglifecycle.WorkerReport{Completed: 99}, ctx.Err()
	}
	if p.fail && n == 1 {
		return billinglifecycle.WorkerReport{Completed: 99}, errors.New("private-source secret@example.test")
	}
	return billinglifecycle.WorkerReport{Completed: 1, Pending: p.pending, Expired: p.expired}, nil
}

func TestPartnersLifecycleLoopDeadlinesRecoveryAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		fail, block, expireFirst, cancelled bool
		pending, expired                    int
	}{
		{name: "sequential_passes"}, {name: "failure_recovers", fail: true},
		{name: "deadline_recovers", expireFirst: true}, {name: "inflight_cancellation", block: true},
		{name: "cancelled_start", cancelled: true},
		{name: "open_checkout_pending", pending: 2},
		{name: "unpaid_checkout_expired", expired: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pass := &lifecyclePassFixture{started: make(chan int32, 8), fail: tc.fail, block: tc.block, expireFirst: tc.expireFirst, pending: tc.pending, expired: tc.expired}
			done := make(chan struct{})
			core, logs := observer.New(zap.DebugLevel)
			if tc.cancelled {
				cancel()
			}
			go RunLifecycle(ctx, pass, 5*time.Millisecond, 20*time.Millisecond, done, zap.New(core))
			if !tc.cancelled {
				select {
				case n := <-pass.started:
					require.Equal(t, int32(1), n)
				case <-time.After(time.Second):
					t.Fatal("pass did not start")
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
				t.Fatal("loop did not drain")
			}
			require.Zero(t, pass.active.Load())
			require.False(t, pass.overlap.Load())
			if tc.cancelled {
				require.Zero(t, pass.calls.Load())
			}
			if tc.block {
				require.Equal(t, int32(1), pass.calls.Load())
			}
			for _, entry := range logs.All() {
				require.NotContains(t, entry.Message, "private-source")
				require.NotContains(t, entry.Message, "secret@example.test")
				if entry.Message == "partners-lifecycle-pass-pending" {
					require.Len(t, entry.Context, 1, "errored reports disclose only opaque correlation, no progress")
					require.Contains(t, entry.ContextMap(), "worker_pass_id")
					continue
				}
				require.Equal(t, "partners-lifecycle-pass-completed", entry.Message)
				require.Equal(t, int64(1), entry.ContextMap()["completed"])
				require.Equal(t, int64(tc.pending), entry.ContextMap()["pending"])
				require.Equal(t, int64(tc.expired), entry.ContextMap()["expired"])
				for _, field := range entry.Context {
					require.Contains(t, []string{"discovered", "examined", "conflicts", "completed", "pending", "expired", "retried", "superseded", "worker_pass_id"}, field.Key)
				}
			}
			if tc.fail || tc.expireFirst {
				require.Equal(t, 1, logs.FilterMessage("partners-lifecycle-pass-pending").Len())
			}
		})
	}
}
