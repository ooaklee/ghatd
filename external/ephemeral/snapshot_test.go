package ephemeral

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// cloneFixture detaches every byte of the mutable fixture value.
func cloneFixture(value []int) []int { return append([]int(nil), value...) }

func TestMemorySnapshotAtomicity(t *testing.T) {
	failure := errors.New("callback failed")
	for _, tc := range []struct {
		name     string
		mode     string
		expected int
		want     error
	}{
		{"commit", "commit", 2, nil}, {"error rollback", "error", 1, failure},
		{"cancel rollback", "cancel", 1, context.Canceled}, {"panic rollback", "panic", 1, nil},
		{"view discards mutations", "view", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []int{1}
			s, err := NewMemorySnapshot(input, cloneFixture)
			require.NoError(t, err)
			input[0] = 99
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var escaped []int
			callback := func(context.Context, *[]int) error { return nil }
			callback = func(_ context.Context, value *[]int) error {
				(*value)[0] = 2
				escaped = *value
				switch tc.mode {
				case "error":
					return failure
				case "cancel":
					cancel()
				case "panic":
					panic("test panic")
				}
				return nil
			}
			if tc.mode == "panic" {
				require.Panics(t, func() { _ = s.Update(ctx, callback) })
			} else if tc.mode == "view" {
				require.NoError(t, s.View(ctx, callback))
			} else {
				require.ErrorIs(t, s.Update(ctx, callback), tc.want)
			}
			escaped[0] = 88
			require.NoError(t, s.View(context.Background(), func(_ context.Context, value *[]int) error { require.Equal(t, tc.expected, (*value)[0]); return nil }))
		})
	}
}

func TestMemorySnapshotConcurrency(t *testing.T) {
	for _, workers := range []int{1, 8, 32} {
		t.Run(strconv.Itoa(workers)+" writers", func(t *testing.T) {
			s, err := NewMemorySnapshot([]int{0}, cloneFixture)
			require.NoError(t, err)
			var wg sync.WaitGroup
			failures := make(chan error, workers)
			for range workers {
				wg.Go(func() {
					failures <- s.Update(context.Background(), func(_ context.Context, value *[]int) error { (*value)[0]++; return nil })
				})
			}
			wg.Wait()
			close(failures)
			for err := range failures {
				require.NoError(t, err)
			}
			require.NoError(t, s.View(context.Background(), func(_ context.Context, value *[]int) error { require.Equal(t, workers, (*value)[0]); return nil }))
		})
	}
}

func TestMemorySnapshotEntryGuards(t *testing.T) {
	for _, tc := range []string{"cancelled waiter", "nested same store", "nil callback", "zero store", "nil context"} {
		t.Run(tc, func(t *testing.T) {
			s, err := NewMemorySnapshot([]int{1}, cloneFixture)
			require.NoError(t, err)
			noop := func(context.Context, *[]int) error { return nil }
			switch tc {
			case "cancelled waiter":
				entered := make(chan struct{})
				release := make(chan struct{})
				done := make(chan error, 1)
				go func() {
					done <- s.Update(context.Background(), func(context.Context, *[]int) error { close(entered); <-release; return nil })
				}()
				<-entered
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				defer cancel()
				err = s.Update(ctx, noop)
				close(release)
				require.NoError(t, <-done)
				require.ErrorIs(t, err, context.DeadlineExceeded)
			case "nested same store":
				require.ErrorIs(t, s.Update(context.Background(), func(ctx context.Context, _ *[]int) error { return s.View(ctx, noop) }), ErrInvalidSnapshot)
			case "nil callback":
				require.ErrorIs(t, s.Update(context.Background(), nil), ErrInvalidSnapshot)
			case "zero store":
				s = &MemorySnapshot[[]int]{}
				require.ErrorIs(t, s.Update(context.Background(), noop), ErrInvalidSnapshot)
			case "nil context":
				require.ErrorIs(t, s.Update(nil, noop), ErrInvalidSnapshot)
			}
		})
	}
}
