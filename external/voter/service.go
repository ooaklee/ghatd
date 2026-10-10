package voter

import (
	"context"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// VoteRepository is the driver-free persistence port. Set and Remove are atomic
// for one actor/target. Implementations must not fabricate successful receipts
// or retry uncertain writes. Summaries includes every requested target.
type VoteRepository interface {
	// SetVote atomically sets one actor's vote value for a target through the
	// VoteRepository persistence port; the reference implementation upserts by
	// deterministic identity and fails closed on inconsistent or unacknowledged
	// receipts.
	SetVote(context.Context, string, Target, Value) error
	// RemoveVote atomically removes one actor's vote for a target through the
	// VoteRepository persistence port; the reference implementation treats an
	// already absent vote as success, deletes only that actor's vote, and never
	// retries uncertain results.
	RemoveVote(context.Context, string, Target) error
	// GetSummaries returns per-target vote counts and the viewer's own vote through
	// the VoteRepository persistence port, covering every requested target; the
	// reference implementation aggregates totals with the caller's vote in one
	// query and fails closed on malformed stored values.
	GetSummaries(context.Context, string, []Target) (map[Target]Summary, error)
}

// Service owns generic voting invariants, not resource authorization. Consumers
// must establish target access/existence and bind actors before invoking it.
type Service struct {
	// Repository owns all storage operations and retains native failure causes.
	Repository VoteRepository
}

// NewService composes a persistence port; missing wiring fails on use.
func NewService(repository VoteRepository) *Service { return &Service{Repository: repository} }

// SetVote atomically replaces one vote. Repeating an acknowledged assignment
// does not add another vote. An error does not prove the write was rolled back.
func (s *Service) SetVote(ctx context.Context, req *SetVoteRequest) error {
	if err := s.validate(ctx, req); err != nil {
		return err
	}
	input := *req
	if err := validateActor(ctx, input.ActorID, false); err != nil {
		return err
	}
	if !validTarget(input.Target) || !input.Vote.Valid() {
		return ErrInvalidRequest
	}
	return s.Repository.SetVote(ctx, input.ActorID, input.Target, input.Vote)
}

// RemoveVote removes only the caller's vote. An already absent vote is success;
// dependency errors retain their native identity and are never automatically retried.
func (s *Service) RemoveVote(ctx context.Context, req *RemoveVoteRequest) error {
	if err := s.validate(ctx, req); err != nil {
		return err
	}
	input := *req
	if err := validateActor(ctx, input.ActorID, false); err != nil {
		return err
	}
	if !validTarget(input.Target) {
		return ErrInvalidRequest
	}
	return s.Repository.RemoveVote(ctx, input.ActorID, input.Target)
}

// GetSummaries returns detached count projections, including zero counts for
// targets with no votes. Zero votes do not establish that a target exists.
// Empty ActorID requests aggregate-only results, including on authenticated calls.
func (s *Service) GetSummaries(ctx context.Context, req *GetSummariesRequest) (map[Target]Summary, error) {
	if err := s.validate(ctx, req); err != nil {
		return nil, err
	}
	actor := req.ActorID
	targets := append([]Target(nil), req.Targets...)
	if err := validateActor(ctx, actor, true); err != nil {
		return nil, err
	}
	if err := validateTargets(targets); err != nil {
		return nil, err
	}
	rows, err := s.Repository.GetSummaries(ctx, actor, targets)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) != len(targets) {
		return nil, ErrUnavailable
	}
	result := make(map[Target]Summary, len(targets))
	for _, target := range targets {
		row, ok := rows[target]
		if !ok || row.Up < 0 || row.Down < 0 {
			return nil, ErrUnavailable
		}
		if row.ViewerVote != nil {
			value := *row.ViewerVote
			if actor == "" || !value.Valid() || (value == Up && row.Up == 0) || (value == Down && row.Down == 0) {
				return nil, ErrUnavailable
			}
			row.ViewerVote = &value
		}
		result[target] = row
	}
	return result, nil
}

// validate checks entry wiring before logging or invoking dependencies.
func (s *Service) validate(ctx context.Context, req any) error {
	if ctx == nil || nilDependency(req) {
		return ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilDependency(s.Repository) {
		return ErrUnavailable
	}
	return nil
}

// nilDependency rejects typed-nil implementations as well as absent interfaces.
func nilDependency(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Interface, reflect.Chan:
		return r.IsNil()
	}
	return false
}

// validIdentity bounds opaque IDs without silently normalizing their identity.
func validIdentity(s string, optional bool) bool {
	if s == "" {
		return optional
	}
	if len(s) > 256 || !utf8.ValidString(s) || strings.TrimSpace(s) != s {
		return false
	}
	return !strings.ContainsFunc(s, unicode.IsControl)
}

// validTarget accepts only bounded, unambiguous structured target values.
func validTarget(t Target) bool {
	return validIdentity(t.Scope, true) && validIdentity(t.Domain, false) && validIdentity(t.ResourceID, false) && validIdentity(t.ChildID, true)
}

// validateTargets prevents duplicate or unbounded work before datastore access.
func validateTargets(targets []Target) error {
	if len(targets) == 0 || len(targets) > MaxTargets {
		return ErrInvalidRequest
	}
	seen := make(map[Target]bool, len(targets))
	for _, target := range targets {
		if !validTarget(target) || seen[target] {
			return ErrInvalidRequest
		}
		seen[target] = true
	}
	return nil
}

// validateActor follows blueprint's trusted in-process composition contract.
// A bare context does not authenticate an actor: that is the integrating domain's
// responsibility. Published middleware/cache identities must always agree.
func validateActor(ctx context.Context, actor string, optional bool) error {
	if actor == "" && optional {
		return nil
	}
	if !validIdentity(actor, false) {
		return ErrActorInvalid
	}
	if ctx.Value(accesshelpers.RequestorUserKey) != nil {
		user := accesshelpers.AcquireUserFrom(ctx)
		if user == nil || user.ID != actor || accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return ErrActorInvalid
		}
	}
	if ctx.Value(accesshelpers.RequestorKey) != nil || ctx.Value(accesshelpers.RequestorAuthenticatedKey) != nil {
		if accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return ErrActorInvalid
		}
	}
	return nil
}
