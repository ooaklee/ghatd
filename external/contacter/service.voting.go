package contacter

import (
	"context"
	"encoding/hex"
	"strings"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/voter"
)

// VoterService owns vote storage, not contact access or transport handling.
type VoterService interface {
	// SetVote atomically assigns one actor's direction to a validated target.
	SetVote(context.Context, *voter.SetVoteRequest) error
	// RemoveVote idempotently removes only the actor's vote for the target.
	RemoveVote(context.Context, *voter.RemoveVoteRequest) error
	// GetSummaries returns counts and only the selected actor's own direction.
	GetSummaries(context.Context, *voter.GetSummariesRequest) (map[voter.Target]voter.Summary, error)
}

// VotingRepository is the optional contact-store capability for bounded entry
// membership. User lookup and vote persistence belong to separate services.
type VotingRepository interface {
	// FindCommsEntry returns the selected persisted entry, not an invented success.
	FindCommsEntry(context.Context, string, string) (*CommsEntry, error)
	// FindCommsEntries returns the requested parent's entries without message bodies.
	FindCommsEntries(context.Context, string, []string) ([]CommsEntry, error)
}

// WithVoterService installs optional voting before serving requests. Contact
// CRUD does not require this port; voting fails closed when it is absent.
func (s *Service) WithVoterService(votes VoterService) *Service {
	s.voterService = votes
	return s
}

// GetCommsVotes checks actor consistency and resolves all targets before reading counts.
// Unknown targets never become authoritative zeros.
func (s *Service) GetCommsVotes(ctx context.Context, req *GetCommsVotesRequest) (*CommsVoteResult, error) {
	if req == nil || len(req.EntryIDs) > 100 {
		return nil, ErrCommsVoteInvalid
	}
	actor := req.ActorID
	if err := s.validateVoteActor(ctx, actor); err != nil {
		return nil, err
	}
	ids := append([]string(nil), req.EntryIDs...)
	if _, err := s.resolveVoteTarget(ctx, req.CommsID, ""); err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] || !validVoteTarget(req.CommsID, id) {
			return nil, ErrCommsVoteInvalid
		}
		seen[id] = true
	}
	authors := make(map[string]string, len(ids))
	if len(ids) > 0 {
		entries, err := s.contacterRepository.(VotingRepository).FindCommsEntries(ctx, req.CommsID, append([]string(nil), ids...))
		if err != nil {
			return nil, err
		}
		if len(entries) != len(ids) {
			return nil, ErrCommsVoteUnavailable
		}
		for _, entry := range entries {
			if !seen[entry.ID] || entry.CommsID != req.CommsID {
				return nil, ErrCommsVoteUnavailable
			}
			delete(seen, entry.ID)
			authors[entry.ID] = entry.ActorID
		}
	}
	page, err := s.voteSummaries(ctx, actor, req.CommsID, append([]string{""}, ids...))
	if err != nil {
		return nil, err
	}
	page.EntryAuthors = authors
	return page, nil
}

// SetCommsVote sets the authorized actor's vote without changing contact history.
func (s *Service) SetCommsVote(ctx context.Context, req *ChangeCommsVoteRequest) (*CommsVoteResult, error) {
	return s.changeVote(ctx, req, false)
}

// RemoveCommsVote removes only the authorized actor's vote; absence is success.
func (s *Service) RemoveCommsVote(ctx context.Context, req *ChangeCommsVoteRequest) (*CommsVoteResult, error) {
	return s.changeVote(ctx, req, true)
}

// changeVote preserves domain admission before delegating generic mechanics.
// Failed post-write reads do not prove rollback or authorize automatic retry.
func (s *Service) changeVote(ctx context.Context, req *ChangeCommsVoteRequest, remove bool) (*CommsVoteResult, error) {
	if req == nil || (!remove && !req.Vote.Valid()) {
		return nil, ErrCommsVoteInvalid
	}
	input := *req
	actor := input.ActorID
	if err := s.validateVoteActor(ctx, actor); err != nil {
		return nil, err
	}
	var err error
	if _, err := s.resolveVoteTarget(ctx, input.CommsID, input.EntryID); err != nil {
		return nil, err
	}
	target := voteTarget(input.CommsID, input.EntryID)
	if remove {
		err = s.voterService.RemoveVote(ctx, &voter.RemoveVoteRequest{ActorID: actor, Target: target})
	} else {
		err = s.voterService.SetVote(ctx, &voter.SetVoteRequest{ActorID: actor, Target: target, Vote: input.Vote})
	}
	if err != nil {
		return nil, err
	}
	return s.voteSummaries(ctx, actor, input.CommsID, []string{input.EntryID})
}

// voteTarget owns the stable discriminator; clients cannot choose the domain.
func voteTarget(comms, entry string) voter.Target {
	return voter.Target{Domain: "contacter", ResourceID: comms, ChildID: entry}
}

// voteSummaries translates a lower-domain projection without exposing voter IDs.
func (s *Service) voteSummaries(ctx context.Context, actor, comms string, ids []string) (*CommsVoteResult, error) {
	targets := make([]voter.Target, len(ids))
	for i, id := range ids {
		targets[i] = voteTarget(comms, id)
	}
	rows, err := s.voterService.GetSummaries(ctx, &voter.GetSummariesRequest{ActorID: actor, Targets: targets})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rows) != len(targets) {
		return nil, ErrCommsVoteUnavailable
	}
	page := &CommsVoteResult{CommsID: comms, ViewerActorID: actor, ByEntry: map[string]CommsVoteSummary{}}
	for _, target := range targets {
		row, ok := rows[target]
		if !ok || row.Up < 0 || row.Down < 0 {
			return nil, ErrCommsVoteUnavailable
		}
		summary := CommsVoteSummary{CommsID: comms, EntryID: target.ChildID, Up: row.Up, Down: row.Down}
		if row.ViewerVote != nil {
			if !row.ViewerVote.Valid() || (*row.ViewerVote == voter.Up && row.Up == 0) || (*row.ViewerVote == voter.Down && row.Down == 0) {
				return nil, ErrCommsVoteUnavailable
			}
			value := int(*row.ViewerVote)
			summary.ViewerVote = &value
		}
		page.ByEntry[target.ChildID] = summary
	}
	return page, nil
}

// validateVoteActor rejects contradictory middleware identity and invalid wiring.
// Bare-context calls are trusted in-process composition; an ActorID alone never
// authenticates or authorizes access. User Manager owns live session admission.
func (s *Service) validateVoteActor(ctx context.Context, actor string) error {
	if ctx == nil {
		return ErrCommsVoteInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilConversationPort(s.voterService) || nilConversationPort(s.contacterRepository) {
		return ErrCommsVoteUnavailable
	}
	if port, ok := s.contacterRepository.(VotingRepository); !ok || nilConversationPort(port) {
		return ErrCommsVoteUnavailable
	}
	if actor == "" || len(actor) > 128 || strings.TrimSpace(actor) != actor {
		return voter.ErrActorInvalid
	}
	if ctx.Value(helpers.RequestorUserKey) != nil {
		u := helpers.AcquireUserFrom(ctx)
		if u == nil || u.ID != actor || helpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return voter.ErrActorInvalid
		}
	}
	if ctx.Value(helpers.RequestorKey) != nil || ctx.Value(helpers.RequestorAuthenticatedKey) != nil {
		if helpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return voter.ErrActorInvalid
		}
	}
	return nil
}

// validVoteTarget preserves the existing private contact/entry wire contract.
func validVoteTarget(id, entry string) bool {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	if entry == "" {
		return true
	}
	if len(entry) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(entry)
	return err == nil && hex.EncodeToString(decoded) == entry
}

// resolveVoteTarget resolves existence and membership without knowing database schemas.
func (s *Service) resolveVoteTarget(ctx context.Context, comms, entry string) (*CommsEntry, error) {
	if !validVoteTarget(comms, entry) {
		return nil, ErrCommsVoteInvalid
	}
	rows, err := s.contacterRepository.GetCommsByIds(ctx, []string{comms})
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 || rows[0].Id != comms {
		return nil, ErrCommsNotFound
	}
	if entry == "" {
		return nil, nil
	}
	row, err := s.contacterRepository.(VotingRepository).FindCommsEntry(ctx, comms, entry)
	if err != nil {
		return nil, err
	}
	if row == nil || row.ID != entry || row.CommsID != comms {
		return nil, ErrCommsVoteUnavailable
	}
	return row, nil
}
