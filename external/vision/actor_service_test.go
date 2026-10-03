package vision

import (
	"context"
	"errors"
	"fmt"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// actorVisionStore records the boundary without turning failed writes into
// in-memory mutations. Every test case owns its store and selected record.
type actorVisionStore struct {
	*memoryVisionRepository
	reads, writes         int
	actor, target         string
	readError, writeError error
	afterRead             func()
	written               *Vision
}

func newActorVisionStore() *actorVisionStore {
	return &actorVisionStore{memoryVisionRepository: &memoryVisionRepository{item: &Vision{ID: "record-id", NanoID: "vision-1", Title: "Original", Type: VisionTypeFeedback, CreatedByUserID: "owner", CreatedAt: "original-created", Voters: newVisionVoteBuckets(), Comments: []VisionComment{{ID: "comment-1", Voters: newVisionVoteBuckets()}}}}}
}
func (m *actorVisionStore) GetVisionByNanoID(context.Context, string) (*Vision, error) {
	m.reads++
	if m.afterRead != nil {
		m.afterRead()
	}
	return m.item, m.readError
}
func (m *actorVisionStore) record(target, actor string) error {
	m.writes++
	m.target = target
	m.actor = actor
	return m.writeError
}
func (m *actorVisionStore) CreateVision(_ context.Context, v *Vision) (*Vision, error) {
	m.written = v
	return v, m.record(v.ID, v.CreatedByUserID)
}
func (m *actorVisionStore) UpdateVision(_ context.Context, v *Vision) error {
	m.written = v
	return m.record(v.ID, v.UpdatedByUserID)
}
func (m *actorVisionStore) UpdateVisionStatus(_ context.Context, id string, _ VisionStatus, actor, _ string) error {
	return m.record(id, actor)
}
func (m *actorVisionStore) DeleteVisionByID(_ context.Context, id string) error {
	return m.record(id, "")
}
func (m *actorVisionStore) SetVisionVote(_ context.Context, id, actor string, _ VisionVote, _ string) error {
	return m.record(id, actor)
}
func (m *actorVisionStore) RemoveVisionVote(_ context.Context, id, actor, _ string) error {
	return m.record(id, actor)
}
func (m *actorVisionStore) AddVisionComment(_ context.Context, id string, c *VisionComment) error {
	return m.record(id, c.UserID)
}
func (m *actorVisionStore) SetVisionCommentVote(_ context.Context, id, _, actor string, _ VisionVote, _ string) error {
	return m.record(id, actor)
}
func (m *actorVisionStore) RemoveVisionCommentVote(_ context.Context, id, _, actor, _ string) error {
	return m.record(id, actor)
}

var visionMutationNames = []string{"create", "update", "status", "vote", "remove vote", "comment", "comment vote", "remove comment vote", "delete"}

// mutationRequest provides a typed-nil variant for the same public command.
func mutationRequest[T any](request *T, absent bool) *T {
	if absent {
		return nil
	}
	return request
}

// invokeVisionMutation keeps the operation matrix on actual service entrypoints.
func invokeVisionMutation(s *Service, ctx context.Context, op, actor string, absent bool) error {
	title := "Updated"
	var err error
	switch op {
	case "create":
		_, err = s.CreateVision(ctx, mutationRequest(&CreateVisionRequest{ActorID: actor, Title: title, Type: VisionTypeFeedback}, absent))
	case "update":
		_, err = s.UpdateVision(ctx, mutationRequest(&UpdateVisionRequest{ActorID: actor, NanoID: "vision-1", Title: &title}, absent))
	case "status":
		_, err = s.UpdateVisionStatus(ctx, mutationRequest(&UpdateVisionStatusRequest{ActorID: actor, NanoID: "vision-1", Status: VisionStatusUnderReview}, absent))
	case "vote":
		_, err = s.SetVisionVote(ctx, mutationRequest(&SetVisionVoteRequest{ActorID: actor, NanoID: "vision-1", Vote: VisionVoteUpvote}, absent))
	case "remove vote":
		_, err = s.RemoveVisionVote(ctx, mutationRequest(&RemoveVisionVoteRequest{ActorID: actor, NanoID: "vision-1"}, absent))
	case "comment":
		_, err = s.AddVisionComment(ctx, mutationRequest(&AddVisionCommentRequest{ActorID: actor, NanoID: "vision-1", Message: "Comment"}, absent))
	case "comment vote":
		_, err = s.SetVisionCommentVote(ctx, mutationRequest(&SetVisionCommentVoteRequest{ActorID: actor, NanoID: "vision-1", CommentID: "comment-1", Vote: VisionVoteUpvote}, absent))
	case "remove comment vote":
		_, err = s.RemoveVisionCommentVote(ctx, mutationRequest(&RemoveVisionCommentVoteRequest{ActorID: actor, NanoID: "vision-1", CommentID: "comment-1"}, absent))
	case "delete":
		_, err = s.DeleteVision(ctx, mutationRequest(&DeleteVisionRequest{ActorID: actor, NanoID: "vision-1"}, absent))
	default:
		panic("unknown vision operation")
	}
	return err
}

func TestVisionMutationActorAndEntryBoundaries(t *testing.T) {
	for _, op := range visionMutationNames {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"trusted in-process", nil}, {"authenticated", nil}, {"matching cached user", nil},
			{"empty actor", ErrVisionUserIDIsRequired}, {"conflicting actor", ErrVisionUserIDIsRequired},
			{"padded actor", ErrVisionUserIDIsRequired},
			{"anonymous", ErrVisionUserIDIsRequired}, {"ID only", ErrVisionUserIDIsRequired},
			{"cached user only", ErrVisionUserIDIsRequired}, {"conflicting cached user", ErrVisionUserIDIsRequired}, {"nil cached user", ErrVisionUserIDIsRequired},
			{"nil context", ErrVisionInvalidPayload}, {"nil request", ErrVisionInvalidPayload},
			{"nil service", ErrVisionUnavailable}, {"nil repository", ErrVisionUnavailable}, {"typed nil repository", ErrVisionUnavailable},
			{"nil config", ErrVisionConfigNotSet}, {"cancelled", context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := newActorVisionStore()
				s := mustVisionService(t, port)
				ctx := context.Background()
				actor := "caller"
				switch tc.name {
				case "authenticated":
					ctx = authenticatedActor(ctx, actor)
				case "matching cached user":
					ctx = accesshelpers.TransitUserWith(authenticatedActor(ctx, actor), &userv2.UniversalUser{ID: actor})
				case "empty actor":
					actor = ""
				case "padded actor":
					actor = " caller "
				case "conflicting actor":
					ctx = authenticatedActor(ctx, "other")
				case "anonymous":
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), false)
				case "ID only":
					ctx = accesshelpers.TransitWith(ctx, actor)
				case "cached user only":
					ctx = accesshelpers.TransitUserWith(ctx, &userv2.UniversalUser{ID: actor})
				case "conflicting cached user":
					ctx = accesshelpers.TransitUserWith(authenticatedActor(ctx, actor), &userv2.UniversalUser{ID: "other"})
				case "nil cached user":
					ctx = accesshelpers.TransitUserWith(authenticatedActor(ctx, actor), nil)
				case "nil context":
					ctx = nil
				case "nil service":
					s = nil
				case "nil repository":
					s.VisionRepository = nil
				case "typed nil repository":
					s.VisionRepository = (*actorVisionStore)(nil)
				case "nil config":
					s.Config = nil
				case "cancelled":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				err := invokeVisionMutation(s, ctx, op, actor, tc.name == "nil request")
				if tc.want != nil {
					require.ErrorIs(t, err, tc.want)
					require.Zero(t, port.reads)
					require.Zero(t, port.writes)
					return
				}
				require.NoError(t, err)
				require.Equal(t, 1, port.writes)
				if op != "delete" {
					require.Equal(t, actor, port.actor)
				}
				if op != "create" {
					require.Equal(t, "record-id", port.target)
				}
			})
		}
	}
}

func TestVisionSelectedRecordFailures(t *testing.T) {
	for _, op := range visionMutationNames[1:] {
		for _, tc := range []struct {
			name    string
			failure error
			want    error
		}{
			{"native outage", errors.New("private storage outage"), nil},
			{"wrapped absence", fmt.Errorf("private: %w", ErrVisionResourceNotFound), nil},
			{"mixed absence", errors.Join(ErrVisionResourceNotFound, errors.New("private outage")), nil},
			{"nil record", nil, ErrVisionUnavailable}, {"foreign NanoID", nil, ErrVisionUnavailable}, {"empty internal ID", nil, ErrVisionUnavailable},
			{"cancel after read", nil, context.Canceled},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := newActorVisionStore()
				port.readError = tc.failure
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				switch tc.name {
				case "nil record":
					port.item = nil
				case "foreign NanoID":
					port.item.NanoID = "other"
				case "empty internal ID":
					port.item.ID = ""
				case "cancel after read":
					port.afterRead = cancel
				}
				err := invokeVisionMutation(mustVisionService(t, port), ctx, op, "caller", false)
				if tc.failure != nil {
					require.Same(t, tc.failure, err)
				} else {
					require.ErrorIs(t, err, tc.want)
				}
				require.Equal(t, 1, port.reads)
				require.Zero(t, port.writes)
			})
		}
	}
}

func TestVisionMutationWriteFailuresAndSnapshots(t *testing.T) {
	for _, op := range visionMutationNames {
		for _, tc := range []struct {
			name    string
			failure error
		}{
			{"native", errors.New("private write failure")}, {"wrapped", fmt.Errorf("private: %w", ErrVisionInvalidStatusTransition)},
			{"cancelled", context.Canceled}, {"deadline", context.DeadlineExceeded},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				port := newActorVisionStore()
				original := *port.item
				port.writeError = tc.failure
				err := invokeVisionMutation(mustVisionService(t, port), context.Background(), op, "caller", false)
				require.True(t, tc.failure == err, "native error identity changed: %v", err)
				require.Equal(t, 1, port.writes)
				require.Equal(t, original, *port.item)
				if op == "update" {
					require.NotSame(t, port.item, port.written)
					require.Equal(t, "caller", port.written.UpdatedByUserID)
					require.Equal(t, original.CreatedByUserID, port.written.CreatedByUserID)
				}
				if op == "create" {
					require.Equal(t, "caller", port.written.CreatedByUserID)
				}
			})
		}
	}
}

// createReceiptVisionStore simulates a completed insert with a malformed return
// value. The service must not retry an insert whose outcome may already exist.
type createReceiptVisionStore struct {
	*actorVisionStore
	mode string
}

func (m *createReceiptVisionStore) CreateVision(ctx context.Context, item *Vision) (*Vision, error) {
	result, err := m.actorVisionStore.CreateVision(ctx, item)
	if err != nil {
		return nil, err
	}
	switch m.mode {
	case "nil":
		return nil, nil
	case "foreign ID":
		result.ID = "other"
	case "foreign NanoID":
		result.NanoID = "other"
	}
	return result, nil
}

func TestVisionCreateReceipt(t *testing.T) {
	for _, tc := range []struct {
		name    string
		invalid bool
	}{{"valid", false}, {"nil", true}, {"foreign ID", true}, {"foreign NanoID", true}} {
		t.Run(tc.name, func(t *testing.T) {
			port := &createReceiptVisionStore{actorVisionStore: newActorVisionStore(), mode: tc.name}
			err := invokeVisionMutation(mustVisionService(t, port), context.Background(), "create", "caller", false)
			if tc.invalid {
				require.ErrorIs(t, err, ErrVisionUnavailable)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, port.writes)
		})
	}
}
