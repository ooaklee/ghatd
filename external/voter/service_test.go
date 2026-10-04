package voter

import (
	"context"
	"errors"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/stretchr/testify/require"
)

type serviceProbe struct {
	calls  int
	err    error
	actor  string
	target Target
	value  Value
	rows   map[Target]Summary
}

func (p *serviceProbe) SetVote(_ context.Context, a string, t Target, v Value) error {
	p.calls++
	p.actor, p.target, p.value = a, t, v
	return p.err
}
func (p *serviceProbe) RemoveVote(_ context.Context, a string, t Target) error {
	p.calls++
	p.actor, p.target = a, t
	return p.err
}
func (p *serviceProbe) GetSummaries(_ context.Context, a string, _ []Target) (map[Target]Summary, error) {
	p.calls++
	p.actor = a
	return p.rows, p.err
}

func TestServiceMutationBoundaries(t *testing.T) {
	target := Target{Domain: "vision", ResourceID: "item"}
	for _, op := range []string{"set", "remove"} {
		for _, tc := range []struct {
			name, actor                                                   string
			target                                                        Target
			value                                                         Value
			nilRequest, nilContext, canceled, nilPort, typedNil, mismatch bool
			want                                                          error
			calls                                                         int
		}{
			{name: "valid", actor: "actor", target: target, value: Up, calls: 1},
			{name: "zero is down", actor: "actor", target: target, value: Down, calls: 1},
			{name: "empty actor", target: target, want: ErrActorInvalid},
			{name: "padded actor", actor: " actor", target: target, want: ErrActorInvalid},
			{name: "missing domain", actor: "actor", target: Target{ResourceID: "item"}, want: ErrInvalidRequest},
			{name: "missing resource", actor: "actor", target: Target{Domain: "vision"}, want: ErrInvalidRequest},
			{name: "control in target", actor: "actor", target: Target{Domain: "vision", ResourceID: "it\x00em"}, want: ErrInvalidRequest},
			{name: "nil request", nilRequest: true, want: ErrInvalidRequest},
			{name: "nil context", nilContext: true, want: ErrInvalidRequest},
			{name: "canceled", canceled: true, want: context.Canceled},
			{name: "missing port", nilPort: true, want: ErrUnavailable},
			{name: "typed nil port", typedNil: true, want: ErrUnavailable},
			{name: "spoofed published actor", actor: "actor", target: target, mismatch: true, want: ErrActorInvalid},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				p := &serviceProbe{}
				s := NewService(p)
				ctx := context.Background()
				if tc.nilPort {
					s.Repository = nil
				}
				if tc.typedNil {
					s.Repository = (*serviceProbe)(nil)
				}
				if tc.canceled {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				if tc.nilContext {
					ctx = nil
				}
				if tc.mismatch {
					ctx = context.WithValue(ctx, accesshelpers.RequestorKey, "other")
					ctx = context.WithValue(ctx, accesshelpers.RequestorAuthenticatedKey, true)
				}
				var err error
				if op == "set" {
					req := &SetVoteRequest{ActorID: tc.actor, Target: tc.target, Vote: tc.value}
					if tc.nilRequest {
						req = nil
					}
					err = s.SetVote(ctx, req)
				} else {
					req := &RemoveVoteRequest{ActorID: tc.actor, Target: tc.target}
					if tc.nilRequest {
						req = nil
					}
					err = s.RemoveVote(ctx, req)
				}
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, tc.calls, p.calls)
				if tc.calls > 0 {
					require.Equal(t, tc.actor, p.actor)
					require.Equal(t, tc.target, p.target)
				}
			})
		}
	}
}

func TestServiceDirectionsAndNativeFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value Value
	}{
		{"negative invalid", -1}, {"down preserves native failure", Down},
		{"up preserves native failure", Up}, {"above range invalid", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failure := errors.New("private-storage-failure")
			p := &serviceProbe{err: failure}
			err := NewService(p).SetVote(context.Background(), &SetVoteRequest{ActorID: "actor", Target: Target{Domain: "vision", ResourceID: "id"}, Vote: tc.value})
			if tc.value.Valid() {
				require.Same(t, failure, err)
				require.Equal(t, 1, p.calls)
			} else {
				require.ErrorIs(t, err, ErrInvalidRequest)
				require.Zero(t, p.calls)
			}
		})
	}
}

func TestServiceSummaryContracts(t *testing.T) {
	target := Target{Domain: "vision", ResourceID: "item"}
	other := Target{Domain: "contacter", ResourceID: "item"}
	up, invalid := Up, Value(2)
	for _, tc := range []struct {
		name, actor string
		targets     []Target
		rows        map[Target]Summary
		want        error
	}{
		{"empty counts", "", []Target{target}, map[Target]Summary{target: {}}, nil},
		{"viewer", "actor", []Target{target}, map[Target]Summary{target: {Up: 1, ViewerVote: &up}}, nil},
		{"anonymous private vote rejected", "", []Target{target}, map[Target]Summary{target: {Up: 1, ViewerVote: &up}}, ErrUnavailable},
		{"absent row", "actor", []Target{target}, nil, ErrUnavailable},
		{"wrong target", "actor", []Target{target}, map[Target]Summary{other: {}}, ErrUnavailable},
		{"negative count", "actor", []Target{target}, map[Target]Summary{target: {Down: -1}}, ErrUnavailable},
		{"invalid viewer vote", "actor", []Target{target}, map[Target]Summary{target: {Up: 1, ViewerVote: &invalid}}, ErrUnavailable},
		{"own vote exceeds total", "actor", []Target{target}, map[Target]Summary{target: {ViewerVote: &up}}, ErrUnavailable},
		{"duplicates", "actor", []Target{target, target}, nil, ErrInvalidRequest},
		{"empty request", "", nil, nil, ErrInvalidRequest},
		{"too many targets", "", make([]Target, MaxTargets+1), nil, ErrInvalidRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &serviceProbe{rows: tc.rows}
			got, err := NewService(p).GetSummaries(context.Background(), &GetSummariesRequest{ActorID: tc.actor, Targets: tc.targets})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, got)
				return
			}
			require.Equal(t, tc.rows, got)
			if got[target].ViewerVote != nil {
				require.NotSame(t, tc.rows[target].ViewerVote, got[target].ViewerVote)
			}
			delete(got, target)
			require.Contains(t, tc.rows, target)
		})
	}
}
