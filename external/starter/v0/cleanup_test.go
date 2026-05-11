package starter

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCleanupGroup_Add(t *testing.T) {
	tests := []struct {
		name  string
		fns   []Cleanup
		wantN int
	}{
		{
			name:  "GOOD - add nil only is ignored",
			fns:   []Cleanup{nil},
			wantN: 0,
		},
		{
			name: "GOOD - nil among real cleanups is skipped",
			fns: []Cleanup{
				nil,
				func(ctx context.Context) error { return nil },
				nil,
				func(ctx context.Context) error { return nil },
			},
			wantN: 2,
		},
		{
			name: "GOOD - no nil adds all",
			fns: []Cleanup{
				func(ctx context.Context) error { return nil },
				func(ctx context.Context) error { return nil },
			},
			wantN: 2,
		},
		{
			name:  "GOOD - empty add",
			fns:   nil,
			wantN: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var g CleanupGroup
			g.Add(tt.fns...)
			if len(g.cleanups) != tt.wantN {
				t.Fatalf("expected %d stored cleanups, got %d", tt.wantN, len(g.cleanups))
			}
		})
	}
}

func TestCleanupGroup_AddNilReceiver(t *testing.T) {
	var g *CleanupGroup
	g.Add(func(ctx context.Context) error { return nil })
}

func TestCleanupGroup_Run(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() (*CleanupGroup, []string)
		wantErr string
	}{
		{
			name: "GOOD - nil group produces nil error",
			setup: func() (*CleanupGroup, []string) {
				return nil, nil
			},
		},
		{
			name: "GOOD - no cleanups produces nil error",
			setup: func() (*CleanupGroup, []string) {
				return &CleanupGroup{}, nil
			},
		},
		{
			name: "GOOD - single cleanup that succeeds",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				g.Add(func(ctx context.Context) error { return nil })
				return &g, nil
			},
		},
		{
			name: "GOOD - multiple cleanups called in forward order",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				var order []string
				g.Add(
					func(ctx context.Context) error { order = append(order, "a"); return nil },
					func(ctx context.Context) error { order = append(order, "b"); return nil },
					func(ctx context.Context) error { order = append(order, "c"); return nil },
				)
				return &g, order
			},
		},
		{
			name: "GOOD - nil cleanup functions are silently skipped",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				var order []string
				g.Add(
					nil,
					func(ctx context.Context) error { order = append(order, "a"); return nil },
					nil,
				)
				return &g, order
			},
		},
		{
			name: "BAD - first errors, second runs, errors joined",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				var order []string
				g.Add(
					func(ctx context.Context) error { order = append(order, "err1"); return errors.New("first error") },
					func(ctx context.Context) error { order = append(order, "ok2"); return nil },
				)
				return &g, order
			},
			wantErr: "first error",
		},
		{
			name: "BAD - all error, all errors joined",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				var order []string
				g.Add(
					func(ctx context.Context) error { order = append(order, "err1"); return errors.New("e1") },
					func(ctx context.Context) error { order = append(order, "err2"); return errors.New("e2") },
					func(ctx context.Context) error { order = append(order, "err3"); return errors.New("e3") },
				)
				return &g, order
			},
			wantErr: "e1",
		},
		{
			name: "BAD - all error, joined contains all messages",
			setup: func() (*CleanupGroup, []string) {
				var g CleanupGroup
				g.Add(
					func(ctx context.Context) error { return errors.New("alpha") },
					func(ctx context.Context) error { return errors.New("beta") },
				)
				return &g, nil
			},
			wantErr: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, orderCapture := tt.setup()
			err := g.Run(context.Background())

			if orderCapture != nil {
				expected := testExpectedOrder(tt.name)
				if !stringSliceEqual(orderCapture, expected) {
					t.Fatalf("expected order %v, got %v", expected, orderCapture)
				}
			}

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected nil error, got %v", err)
				}
				return
			}

			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
			}

			if strings.HasPrefix(tt.name, "BAD - all error, all errors joined") {
				if !strings.Contains(err.Error(), "e1") || !strings.Contains(err.Error(), "e2") || !strings.Contains(err.Error(), "e3") {
					t.Fatalf("expected joined error to contain all error messages, got %q", err.Error())
				}
			}
			if strings.HasPrefix(tt.name, "BAD - all error, joined contains") {
				if !strings.Contains(err.Error(), "alpha") || !strings.Contains(err.Error(), "beta") {
					t.Fatalf("expected joined error to contain both messages, got %q", err.Error())
				}
			}
		})
	}
}

func TestCleanupGroup_AssignableToStackCleanup(t *testing.T) {
	var g CleanupGroup
	var s Stack
	s.Cleanup = g.Run
	if s.Cleanup == nil {
		t.Fatal("expected CleanupGroup.Run to be assignable to Stack.Cleanup")
	}
}

func TestCleanupGroup_NeverPanicsOnNil(t *testing.T) {
	var g CleanupGroup
	g.Add(nil, nil, nil)
	if err := g.Run(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestCleanupGroup_RunUsesInsertionOrder(t *testing.T) {
	var g CleanupGroup
	var order []string
	g.Add(
		func(ctx context.Context) error { order = append(order, "first"); return nil },
		func(ctx context.Context) error { order = append(order, "second"); return nil },
		func(ctx context.Context) error { order = append(order, "third"); return nil },
	)
	_ = g.Run(context.Background())

	expected := []string{"first", "second", "third"}
	if !stringSliceEqual(order, expected) {
		t.Fatalf("expected forward order %v, got %v; if reverse order is needed, add cleanups in reverse", expected, order)
	}
}

func TestCleanupGroup_AllRunDespiteErrors(t *testing.T) {
	var g CleanupGroup
	var called atomic.Int64
	g.Add(
		func(ctx context.Context) error { called.Add(1); return errors.New("err1") },
		func(ctx context.Context) error { called.Add(1); return errors.New("err2") },
		func(ctx context.Context) error { called.Add(1); return errors.New("err3") },
	)
	err := g.Run(context.Background())
	if called.Load() != 3 {
		t.Fatalf("expected all 3 cleanups to be called despite errors, got %d", called.Load())
	}
	if err == nil {
		t.Fatal("expected non-nil error")
	}
}

func testExpectedOrder(name string) []string {
	switch {
	case strings.Contains(name, "forward order"):
		return []string{"a", "b", "c"}
	case strings.Contains(name, "nil cleanup"):
		return []string{"a"}
	case strings.Contains(name, "first errors"):
		return []string{"err1", "ok2"}
	case strings.Contains(name, "all error"):
		return []string{"err1", "err2", "err3"}
	}
	return nil
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
