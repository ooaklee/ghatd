package usermanager

import (
	"context"
	"sort"
	"strings"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/voter"
)

// CommsVotingService is the lower-domain voting capability injected at startup.
// It owns contact membership and shared voter calls, not admission or user lookup.
type CommsVotingService interface {
	// GetCommsVotes validates parent/child membership and returns viewer-specific counts.
	GetCommsVotes(context.Context, *contacter.GetCommsVotesRequest) (*contacter.CommsVoteResult, error)
	// SetCommsVote replaces the authorized actor's choice for one validated target.
	SetCommsVote(context.Context, *contacter.ChangeCommsVoteRequest) (*contacter.CommsVoteResult, error)
	// RemoveCommsVote removes only that actor's choice; an absent vote is idempotent.
	RemoveCommsVote(context.Context, *contacter.ChangeCommsVoteRequest) (*contacter.CommsVoteResult, error)
}

// WithCommsVotingService installs private conversation voting. Like other
// optional capabilities, a missing or typed-nil dependency fails closed on use.
func (s *Service) WithCommsVotingService(votes CommsVotingService) *Service {
	s.CommsVotingService = votes
	return s
}

// GetCommsVotes rechecks live administrator authority before reading private
// votes and participant labels. Caller-owned target slices are not passed onward.
func (s *Service) GetCommsVotes(ctx context.Context, req *contacter.GetCommsVotesRequest) (*CommsVotePage, error) {
	if req == nil {
		return nil, contacter.ErrCommsVoteInvalid
	}
	r := *req
	r.EntryIDs = append([]string(nil), req.EntryIDs...)
	if err := s.authorizeCommsVoting(ctx, r.ActorID); err != nil {
		return nil, err
	}
	result, err := s.CommsVotingService.GetCommsVotes(ctx, &r)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page, err := validateCommsVotePage(result, req.ActorID, req.CommsID, append([]string{""}, req.EntryIDs...))
	if err != nil {
		return nil, err
	}
	// References must cover precisely the admitted child entries, not the
	// original contact sender or arbitrary voters. Shape checks cannot prove a
	// dishonest adapter's authorship: the injected contact service is trusted.
	if len(result.EntryAuthors) != len(req.EntryIDs) {
		return nil, contacter.ErrCommsVoteUnavailable
	}
	actors := []string{req.ActorID}
	for _, entry := range req.EntryIDs {
		author, ok := result.EntryAuthors[entry]
		if entry == "" || !ok {
			return nil, contacter.ErrCommsVoteUnavailable
		}
		actors = append(actors, author)
	}
	page.Participants = s.enrichCommsParticipants(ctx, actors)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return page, nil
}

// SetCommsVote binds an administrator's choice to the selected contact/entry.
// Authorization and persistence are separate; a failed reply does not undo a vote.
func (s *Service) SetCommsVote(ctx context.Context, req *contacter.ChangeCommsVoteRequest) (*CommsVotePage, error) {
	return s.changeCommsVote(ctx, req, false)
}

// RemoveCommsVote removes only the verified administrator's selected vote.
func (s *Service) RemoveCommsVote(ctx context.Context, req *contacter.ChangeCommsVoteRequest) (*CommsVotePage, error) {
	return s.changeCommsVote(ctx, req, true)
}

// changeCommsVote shares management admission without conflating set and remove.
func (s *Service) changeCommsVote(ctx context.Context, req *contacter.ChangeCommsVoteRequest, remove bool) (*CommsVotePage, error) {
	if req == nil {
		return nil, contacter.ErrCommsVoteInvalid
	}
	r := *req
	if err := s.authorizeCommsVoting(ctx, r.ActorID); err != nil {
		return nil, err
	}
	var result *contacter.CommsVoteResult
	var err error
	if remove {
		result, err = s.CommsVotingService.RemoveCommsVote(ctx, &r)
	} else {
		result, err = s.CommsVotingService.SetCommsVote(ctx, &r)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	page, err := validateCommsVotePage(result, req.ActorID, req.CommsID, []string{req.EntryID})
	if err != nil {
		return nil, err
	}
	if len(result.EntryAuthors) != 0 {
		return nil, contacter.ErrCommsVoteUnavailable
	}
	// Mutations retain their empty participant array and do not introduce an
	// optional user lookup after persistence has already acknowledged the write.
	return page, nil
}

// authorizeCommsVoting requires the same manager verifier as private history.
// Published identity is not enough: demotion/revocation is checked on every call.
func (s *Service) authorizeCommsVoting(ctx context.Context, actor string) error {
	if err := s.administratorActor(ctx, actor, contacter.ErrCommsVoteUnavailable); err != nil {
		return err
	}
	if nilProfilePort(s.CommsVotingService) {
		return contacter.ErrCommsVoteUnavailable
	}
	return nil
}

// validateCommsVotePage rejects wrong-viewer/target receipts from custom ports.
// It neither turns missing results into zero votes nor exposes voter inventories.
func validateCommsVotePage(page *contacter.CommsVoteResult, actor, comms string, entries []string) (*CommsVotePage, error) {
	if page == nil || len(entries) > 101 || page.CommsID != comms || page.ViewerActorID != actor || len(page.ByEntry) != len(entries) {
		return nil, contacter.ErrCommsVoteUnavailable
	}
	result := &CommsVotePage{CommsID: comms, ViewerActorID: actor, ByEntry: make(map[string]contacter.CommsVoteSummary, len(entries)), Participants: []CommsParticipant{}}
	for _, entry := range entries {
		if _, duplicate := result.ByEntry[entry]; duplicate {
			return nil, contacter.ErrCommsVoteUnavailable
		}
		row, ok := page.ByEntry[entry]
		if !ok || row.CommsID != comms || row.EntryID != entry || row.Up < 0 || row.Down < 0 {
			return nil, contacter.ErrCommsVoteUnavailable
		}
		if row.ViewerVote != nil {
			value := voter.Value(*row.ViewerVote)
			if !value.Valid() || (value == voter.Up && row.Up == 0) || (value == voter.Down && row.Down == 0) {
				return nil, contacter.ErrCommsVoteUnavailable
			}
			copied := *row.ViewerVote
			row.ViewerVote = &copied
		}
		result.ByEntry[entry] = row
	}
	return result, nil
}

// enrichCommsParticipants decorates only the verified viewer and admitted entry
// authors. It is never an authorization check. Invalid/missing users and failed
// batches omit labels; raw errors, emails and roles never become display values.
// The domain limits reads to 100 entries plus the viewer, so at most two batches
// are required. The dedicated projection keeps private full names independent
// from the deliberately different group and public Vision views.
func (s *Service) enrichCommsParticipants(ctx context.Context, actors []string) []CommsParticipant {
	allowed := make(map[string]bool, len(actors))
	ids := make([]string, 0, len(actors))
	for _, id := range actors {
		// The shared loader normalizes caller input; do not let a malformed
		// persisted author reference get normalized into another account.
		if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || allowed[id] {
			continue
		}
		allowed[id] = true
		ids = append(ids, id)
	}
	users := s.loadUsersForEnrichment(ctx, ids, "comms-participant-enrichment")
	result := make([]CommsParticipant, 0, len(users))
	for id, person := range users {
		if person == nil || person.ID != id || !allowed[person.ID] || len(person.NanoID) > 128 {
			continue
		}
		participant := CommsParticipant{ID: person.ID, NanoID: person.NanoID}
		if info := person.PersonalInfo; info != nil {
			participant.FullName = info.FullName
			if participant.FullName == "" {
				participant.FullName = strings.TrimSpace(info.FirstName + " " + info.LastName)
			}
		}
		if len(participant.FullName) > 256 {
			participant.FullName = ""
		}
		result = append(result, participant)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
