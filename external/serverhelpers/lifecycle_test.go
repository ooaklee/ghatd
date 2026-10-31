package serverhelpers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type testContextKey struct{}

func TestLifecycleShutdownOrdering(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		failDrain, failRelease, cancel bool
		want                           []string
		wantErr                        bool
	}{
		{name: "normal shutdown", want: []string{"worker", "store", "earlier", "telemetry"}},
		{name: "startup rollback after later acquisition", cancel: true, want: []string{"worker", "store", "earlier", "telemetry"}},
		{name: "failed drain protects store but continues", failDrain: true, wantErr: true, want: []string{"worker", "earlier", "telemetry"}},
		{name: "failed release continues", failRelease: true, wantErr: true, want: []string{"worker", "store", "earlier", "telemetry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []string
			cb := func(name string, fail bool) Cleanup {
				return func(ctx context.Context) error {
					events = append(events, name)
					if ctx.Err() != nil || ctx.Value(testContextKey{}) != "value" {
						t.Errorf("%s got invalid context: %v", name, ctx.Err())
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Errorf("%s missing deadline", name)
					}
					if fail {
						return errors.New("private secret")
					}
					return nil
				}
			}
			lifecycle := NewLifecycle(LifecycleConfig{Timeout: time.Second, Telemetry: cb("telemetry", false)})
			if err := lifecycle.AddResource(cb("earlier", false)); err != nil {
				t.Fatal(err)
			}
			if err := lifecycle.AddResource(cb("store", tc.failRelease), cb("worker", tc.failDrain)); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), testContextKey{}, "value"))
			defer cancel()
			if tc.cancel {
				cancel()
			}
			err := lifecycle.Close(ctx)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v", err)
			}
			if err != nil && strings.Contains(err.Error(), "private secret") {
				t.Fatal("private error leaked")
			}
			if !reflect.DeepEqual(events, tc.want) {
				t.Fatalf("events = %v, want %v", events, tc.want)
			}
			if err2 := lifecycle.Close(ctx); (err2 != nil) != tc.wantErr {
				t.Fatalf("second close = %v", err2)
			}
			if !reflect.DeepEqual(events, tc.want) {
				t.Fatalf("repeated callbacks: %v", events)
			}
		})
	}
}

func TestLifecycleTimeoutAndTelemetryBudget(t *testing.T) {
	var events []string
	l := NewLifecycle(LifecycleConfig{Timeout: 10 * time.Millisecond, Telemetry: func(ctx context.Context) error {
		events = append(events, "telemetry")
		if ctx.Err() != nil {
			t.Error("telemetry budget expired")
		}
		return errors.New("private exporter")
	}})
	if err := l.AddResource(func(context.Context) error { events = append(events, "independent"); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := l.AddResource(func(context.Context) error { events = append(events, "store"); return nil }, func(ctx context.Context) error { events = append(events, "drain"); <-ctx.Done(); return ctx.Err() }); err != nil {
		t.Fatal(err)
	}
	err := l.Close(nil)
	if err == nil || !strings.Contains(err.Error(), "worker drain failed") || !strings.Contains(err.Error(), "telemetry shutdown failed") || strings.Contains(err.Error(), "private exporter") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("cleanup error lost deadline classification")
	}
	if want := []string{"drain", "independent", "telemetry"}; !reflect.DeepEqual(events, want) {
		t.Fatalf("events = %v", events)
	}
}

func TestLifecycleTelemetryRunsOnPanic(t *testing.T) {
	for _, tc := range []struct {
		name                          string
		resourcePanic, telemetryPanic bool
	}{
		{name: "resource panic", resourcePanic: true},
		{name: "telemetry panic", telemetryPanic: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			l := NewLifecycle(LifecycleConfig{Telemetry: func(context.Context) error {
				calls++
				if tc.telemetryPanic {
					panic("failure")
				}
				return nil
			}})
			if err := l.AddResource(func(context.Context) error {
				if tc.resourcePanic {
					panic("failure")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					if got := recover(); got != "failure" {
						t.Errorf("panic = %v", got)
					}
				}()
				_ = l.Close(context.Background())
			}()
			if calls != 1 {
				t.Fatalf("telemetry calls = %d", calls)
			}
			if err := l.Close(context.Background()); err == nil || !strings.Contains(err.Error(), "cleanup interrupted") {
				t.Fatalf("repeated Close must report incomplete cleanup: %v", err)
			}
			if calls != 1 {
				t.Fatal("telemetry repeated after panic")
			}
		})
	}
}

func TestLifecycleErrorClassification(t *testing.T) {
	for _, phase := range []string{"drain", "release", "telemetry"} {
		t.Run(phase, func(t *testing.T) {
			cause := errors.New("private provider details")
			callback := func(current string) Cleanup {
				return func(context.Context) error {
					if current == phase {
						return cause
					}
					return nil
				}
			}
			l := NewLifecycle(LifecycleConfig{Telemetry: callback("telemetry")})
			if err := l.AddResource(callback("release"), callback("drain")); err != nil {
				t.Fatal(err)
			}
			err := l.Close(context.Background())
			if !errors.Is(err, cause) {
				t.Fatalf("lost cleanup cause: %v", err)
			}
			if strings.Contains(err.Error(), cause.Error()) {
				t.Fatal("private callback details exposed")
			}
		})
	}
}

func TestLifecycleRegistrationAndConcurrentClose(t *testing.T) {
	l := NewLifecycle(LifecycleConfig{})
	if l.AddResource(nil) == nil || l.AddResource(func(context.Context) error { return nil }, nil) == nil {
		t.Fatal("invalid callback accepted")
	}
	calls := 0
	if err := l.AddResource(func(context.Context) error { calls++; return nil }); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _ = l.Close(nil) }()
	}
	wg.Wait()
	if calls != 1 {
		t.Fatalf("release calls = %d", calls)
	}
	if l.AddResource(func(context.Context) error { return nil }) == nil {
		t.Fatal("late registration accepted")
	}
}

func TestLifecycleStartupFailureRollsBackAcquiredResources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
		want   []string
	}{
		{name: "first acquisition fails", failAt: 0, want: []string{"telemetry"}},
		{name: "later acquisition fails", failAt: 2, want: []string{"second", "first", "telemetry"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []string
			startup := func() error {
				life := NewLifecycle(LifecycleConfig{Telemetry: func(context.Context) error { events = append(events, "telemetry"); return nil }})
				defer life.Close(context.Background())
				for i, name := range []string{"first", "second", "third"} {
					if i == tc.failAt {
						return errors.New("injected acquisition failure")
					}
					name := name
					if err := life.AddResource(func(context.Context) error { events = append(events, name); return nil }); err != nil {
						return err
					}
				}
				return nil
			}
			if err := startup(); err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if !reflect.DeepEqual(events, tc.want) {
				t.Fatalf("rollback = %v, want %v", events, tc.want)
			}
		})
	}
}
