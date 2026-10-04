package vision

import (
	"context"

	"github.com/ooaklee/ghatd/external/voter"
)

// VoterService is the lower-domain port; vision retains target validation and
// downvote policy. Implementations return all requested summaries or an error.
type VoterService interface {
	SetVote(context.Context, *voter.SetVoteRequest) error
	RemoveVote(context.Context, *voter.RemoveVoteRequest) error
	GetSummaries(context.Context, *voter.GetSummariesRequest) (map[voter.Target]voter.Summary, error)
}

// visionVoteTarget uses resolved IDs and a package-owned domain discriminator.
func visionVoteTarget(id, comment string) voter.Target {
	return voter.Target{Domain: "vision", ResourceID: id, ChildID: comment}
}

// getVoteResponse keeps the actor from trusted in-process mutation commands;
// read-only public calls instead derive their optional viewer from context.
func (s *Service) getVoteResponse(ctx context.Context, nanoID, actor string) (*VisionResponse, error) {
	item, err := s.getVisionByNanoID(ctx, nanoID)
	if err != nil {
		return nil, err
	}
	return s.projectVotes(ctx, item, actor)
}

// projectVotes attaches transient summaries without persisting voter identities.
func (s *Service) projectVotes(ctx context.Context, item *Vision, actor string) (*VisionResponse, error) {
	if err := s.hydrateVotes(ctx, []*Vision{item}, actor); err != nil {
		return nil, err
	}
	return &VisionResponse{Vision: item}, nil
}

// hydrateVotes snapshots comment slices before enrichment and chunks all target
// lookups. Summaries are not a transaction with the preceding parent read/write.
func (s *Service) hydrateVotes(ctx context.Context, items []*Vision, actor string) error {
	if ctx == nil {
		return ErrVisionInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilVisionDependency(s.VoterService) {
		return ErrVisionUnavailable
	}
	targets := make([]voter.Target, 0, len(items))
	seen := make(map[voter.Target]bool)
	for _, item := range items {
		if item == nil || item.ID == "" {
			return ErrVisionUnavailable
		}
		item.Comments = append([]VisionComment(nil), item.Comments...)
		add := func(target voter.Target) {
			if !seen[target] {
				seen[target] = true
				targets = append(targets, target)
			}
		}
		add(visionVoteTarget(item.ID, ""))
		for _, comment := range item.Comments {
			if comment.ID == "" {
				return ErrVisionUnavailable
			}
			add(visionVoteTarget(item.ID, comment.ID))
		}
	}
	all := make(map[voter.Target]voter.Summary, len(targets))
	for start := 0; start < len(targets); start += voter.MaxTargets {
		batch := targets[start:min(start+voter.MaxTargets, len(targets))]
		rows, err := s.VoterService.GetSummaries(ctx, &voter.GetSummariesRequest{ActorID: actor, Targets: batch})
		if err != nil {
			return err
		}
		if len(rows) != len(batch) {
			return ErrVisionUnavailable
		}
		for _, target := range batch {
			row, ok := rows[target]
			if !ok || row.Up < 0 || row.Down < 0 {
				return ErrVisionUnavailable
			}
			if row.ViewerVote != nil {
				v := *row.ViewerVote
				if actor == "" || !v.Valid() || (v == voter.Up && row.Up == 0) || (v == voter.Down && row.Down == 0) {
					return ErrVisionUnavailable
				}
				row.ViewerVote = &v
			}
			all[target] = row
		}
	}
	for _, item := range items {
		item.VoteSummary = all[visionVoteTarget(item.ID, "")]
		item.VoteViewerID = actor
		for i := range item.Comments {
			item.Comments[i].VoteSummary = all[visionVoteTarget(item.ID, item.Comments[i].ID)]
			item.Comments[i].VoteViewerID = actor
		}
	}
	return nil
}
