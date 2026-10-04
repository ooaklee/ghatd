package vision

import (
	"context"
	"fmt"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/voter"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// memoryVoteRepository is a test persistence port, never a production fallback.
type memoryVoteRepository struct {
	values  map[voteKey]voter.Value
	onWrite func(string, string) error
	batches []int
}
type voteKey struct {
	actor  string
	target voter.Target
}

// sharedVisionPage simulates an adapter reusing a cached list snapshot.
type sharedVisionPage struct {
	*memoryVisionRepository
	items []Vision
}

func (m *sharedVisionPage) GetVisions(context.Context, *GetVisionsRequest) ([]Vision, error) {
	return m.items, nil
}

func (m *memoryVoteRepository) SetVote(_ context.Context, a string, t voter.Target, v voter.Value) error {
	if m.onWrite != nil {
		if err := m.onWrite(t.ResourceID, a); err != nil {
			return err
		}
	}
	m.values[voteKey{a, t}] = v
	return nil
}
func (m *memoryVoteRepository) RemoveVote(_ context.Context, a string, t voter.Target) error {
	if m.onWrite != nil {
		if err := m.onWrite(t.ResourceID, a); err != nil {
			return err
		}
	}
	delete(m.values, voteKey{a, t})
	return nil
}
func (m *memoryVoteRepository) GetSummaries(_ context.Context, a string, targets []voter.Target) (map[voter.Target]voter.Summary, error) {
	m.batches = append(m.batches, len(targets))
	rows := map[voter.Target]voter.Summary{}
	for _, t := range targets {
		row := voter.Summary{}
		for key, value := range m.values {
			if key.target != t {
				continue
			}
			if value == voter.Up {
				row.Up++
			} else {
				row.Down++
			}
			if a != "" && key.actor == a {
				v := value
				row.ViewerVote = &v
			}
		}
		rows[t] = row
	}
	return rows, nil
}

func TestSharedVoteProjectionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name     string
		comments int
		viewer   string
	}{
		{"anonymous", 0, ""}, {"authenticated", 1, "actor"}, {"chunked comments", 450, "actor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := &Vision{ID: "record", NanoID: "public", Comments: []VisionComment{}}
			for i := 0; i < tc.comments; i++ {
				item.Comments = append(item.Comments, VisionComment{ID: fmt.Sprintf("comment-%d", i)})
			}
			votes := &memoryVoteRepository{values: map[voteKey]voter.Value{{"actor", visionVoteTarget("record", "")}: voter.Up}}
			s, err := NewService(&memoryVisionRepository{item: item}, voter.NewService(votes))
			require.NoError(t, err)
			ctx := context.Background()
			if tc.viewer != "" {
				ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, tc.viewer), true)
			}
			result, err := s.GetVisionByNanoID(ctx, &GetVisionByNanoIDRequest{NanoID: "public"})
			require.NoError(t, err)
			require.Equal(t, 1, result.Vision.VoteSummary.Up)
			require.Equal(t, tc.viewer, result.Vision.VoteViewerID)
			if tc.viewer == "" {
				require.Nil(t, result.Vision.VoteSummary.ViewerVote)
			} else {
				require.Equal(t, voter.Up, *result.Vision.VoteSummary.ViewerVote)
			}
			for _, size := range votes.batches {
				require.LessOrEqual(t, size, voter.MaxTargets)
			}
			require.Empty(t, item.VoteViewerID)
			require.Zero(t, item.VoteSummary.Up)
			if tc.comments > 0 {
				require.Empty(t, item.Comments[0].VoteViewerID)
			}
			encoded, err := bson.Marshal(result.Vision)
			require.NoError(t, err)
			var stored bson.M
			require.NoError(t, bson.Unmarshal(encoded, &stored))
			require.NotContains(t, stored, "voters")
			require.NotContains(t, stored, "votes")
			require.NotContains(t, stored, "voteviewerid")
		})
	}
}

func TestVoteTargetValidationPrecedesSharedMutation(t *testing.T) {
	for _, tc := range []struct {
		name, nano, comment string
		downDisabled        bool
		want                error
	}{
		{"missing parent", "missing", "", false, ErrVisionUnavailable},
		{"unknown comment", "public", "missing", false, ErrVisionCommentNotFound},
		{"downvote policy", "public", "", true, ErrVisionDownvotingDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			votes := &memoryVoteRepository{values: map[voteKey]voter.Value{}}
			s, err := NewService(&memoryVisionRepository{item: &Vision{ID: "record", NanoID: "public"}}, voter.NewService(votes), DefaultVisionConfig().WithDownvoting(!tc.downDisabled))
			require.NoError(t, err)
			if tc.comment != "" {
				_, err = s.SetVisionCommentVote(context.Background(), &SetVisionCommentVoteRequest{ActorID: "actor", NanoID: tc.nano, CommentID: tc.comment, Vote: VisionVoteDownvote})
			} else {
				_, err = s.SetVisionVote(context.Background(), &SetVisionVoteRequest{ActorID: "actor", NanoID: tc.nano, Vote: VisionVoteDownvote})
			}
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, votes.values)
		})
	}
}

func TestSharedListProjectionDoesNotMutateAdapter(t *testing.T) {
	for _, actor := range []string{"", "actor"} {
		name := "anonymous"
		if actor != "" {
			name = "authenticated"
		}
		t.Run(name, func(t *testing.T) {
			page := &sharedVisionPage{memoryVisionRepository: &memoryVisionRepository{}, items: []Vision{{ID: "item", NanoID: "public", Comments: []VisionComment{{ID: "child"}}}}}
			votes := &memoryVoteRepository{values: map[voteKey]voter.Value{{"actor", visionVoteTarget("item", "")}: voter.Up}}
			s, err := NewService(page, voter.NewService(votes))
			require.NoError(t, err)
			ctx := context.Background()
			if actor != "" {
				ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, actor), true)
			}
			result, err := s.GetVisions(ctx, nil)
			require.NoError(t, err)
			require.Equal(t, 1, result.Visions[0].VoteSummary.Up)
			require.Equal(t, actor, result.Visions[0].VoteViewerID)
			require.Zero(t, page.items[0].VoteSummary.Up)
			require.Empty(t, page.items[0].VoteViewerID)
			require.Empty(t, page.items[0].Comments[0].VoteViewerID)
		})
	}
}
