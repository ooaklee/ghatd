package contacter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/voter"
	"github.com/stretchr/testify/require"
)

type votingContactsProbe struct {
	contacterRepository // Unrelated CRUD methods must never be called by voting.

	missing, wrongChild bool
	reads               int
	fault               string
}

func (p *votingContactsProbe) GetCommsByIds(context.Context, []string) ([]Comms, error) {
	p.reads++
	if p.missing {
		return nil, nil
	}
	return []Comms{{Id: "contact"}}, nil
}
func (p *votingContactsProbe) FindCommsEntry(_ context.Context, parent, id string) (*CommsEntry, error) {
	if p.wrongChild {
		parent = "other-contact"
	}
	return &CommsEntry{ID: id, CommsID: parent}, nil
}
func (p *votingContactsProbe) FindCommsEntries(ctx context.Context, parent string, ids []string) ([]CommsEntry, error) {
	rows := make([]CommsEntry, 0, len(ids))
	for _, id := range ids {
		row, err := p.FindCommsEntry(ctx, parent, id)
		if err != nil {
			return nil, err
		}
		row.ActorID = "author-" + id
		rows = append(rows, *row)
	}
	if len(rows) > 0 {
		switch p.fault {
		case "missing entries":
			return nil, nil
		case "wrong entry":
			rows[0].ID = "unrequested"
		case "duplicate receipt":
			if len(rows) > 1 {
				rows[1] = rows[0]
			}
		case "mutated request":
			ids[0] = "unrequested"
		}
	}
	return rows, nil
}

type votingServiceProbe struct {
	writes       int
	actor        string
	target       voter.Target
	failure      error
	inconsistent bool
	summaryFault string
	reads        int
	targets      []voter.Target
}

func (p *votingServiceProbe) SetVote(_ context.Context, r *voter.SetVoteRequest) error {
	p.writes++
	p.actor = r.ActorID
	p.target = r.Target
	return p.failure
}
func (p *votingServiceProbe) RemoveVote(_ context.Context, r *voter.RemoveVoteRequest) error {
	p.writes++
	p.actor = r.ActorID
	p.target = r.Target
	return p.failure
}
func (p *votingServiceProbe) GetSummaries(_ context.Context, r *voter.GetSummariesRequest) (map[voter.Target]voter.Summary, error) {
	p.reads++
	p.targets = append([]voter.Target(nil), r.Targets...)
	rows := map[voter.Target]voter.Summary{}
	for _, target := range r.Targets {
		row := voter.Summary{}
		if p.inconsistent {
			up := voter.Up
			row.ViewerVote = &up
		}
		switch p.summaryFault {
		case "negative up":
			row.Up = -1
		case "negative down":
			row.Down = -1
		case "invalid direction":
			value := voter.Value(2)
			row.ViewerVote = &value
		case "impossible down":
			value := voter.Down
			row.ViewerVote = &value
		case "valid up":
			value := voter.Up
			row.Up = 1
			row.ViewerVote = &value
		case "valid down":
			value := voter.Down
			row.Down = 1
			row.ViewerVote = &value
		}
		rows[target] = row
	}
	switch p.summaryFault {
	case "missing rows":
		return nil, nil
	case "wrong target":
		rows = map[voter.Target]voter.Summary{{Domain: "other", ResourceID: "wrong"}: {}}
	case "extra row":
		rows[voter.Target{Domain: "other", ResourceID: "wrong"}] = voter.Summary{}
	}
	return rows, p.failure
}

func TestContactVotingSummaryReceipts(t *testing.T) {
	for _, op := range []string{"read", "set", "remove"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"zero", nil}, {"valid up", nil}, {"valid down", nil},
			{"missing rows", ErrCommsVoteUnavailable}, {"wrong target", ErrCommsVoteUnavailable},
			{"extra row", ErrCommsVoteUnavailable}, {"negative up", ErrCommsVoteUnavailable},
			{"negative down", ErrCommsVoteUnavailable}, {"invalid direction", ErrCommsVoteUnavailable},
			{"impossible down", ErrCommsVoteUnavailable},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				votes := &votingServiceProbe{summaryFault: tc.name}
				s := NewService(&votingContactsProbe{}).WithVoterService(votes)
				var result *CommsVoteResult
				var err error
				req := &ChangeCommsVoteRequest{ActorID: "viewer", CommsID: "contact", Vote: voter.Up}
				switch op {
				case "read":
					result, err = s.GetCommsVotes(context.Background(), &GetCommsVotesRequest{ActorID: "viewer", CommsID: "contact"})
				case "set":
					result, err = s.SetCommsVote(context.Background(), req)
				case "remove":
					result, err = s.RemoveCommsVote(context.Background(), req)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Nil(t, result)
				} else {
					require.Contains(t, result.ByEntry, "")
				}
				require.Equal(t, 1, votes.reads)
				wantWrites := 0
				if op != "read" {
					wantWrites = 1
				}
				require.Equal(t, wantWrites, votes.writes, "no retry after failed post-write read")
			})
		}
	}
}

func TestContactVotingAuthorReferences(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
		want  error
	}{
		{"original only", 0, nil}, {"one entry", 1, nil}, {"full page", 100, nil},
		{"over limit", 101, ErrCommsVoteInvalid}, {"duplicate inputs", 2, ErrCommsVoteInvalid},
		{"invalid entry", 1, ErrCommsVoteInvalid}, {"empty entry", 1, ErrCommsVoteInvalid},
		{"missing entries", 1, ErrCommsVoteUnavailable}, {"wrong entry", 1, ErrCommsVoteUnavailable},
		{"duplicate receipt", 2, ErrCommsVoteUnavailable}, {"mutated request", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			contacts := &votingContactsProbe{fault: tc.name}
			votes := &votingServiceProbe{}
			s := NewService(contacts).WithVoterService(votes)
			ids := make([]string, tc.count)
			for i := range ids {
				ids[i] = fmt.Sprintf("%064x", i+1)
			}
			switch tc.name {
			case "duplicate inputs":
				ids[1] = ids[0]
			case "invalid entry":
				ids[0] = "invalid"
			case "empty entry":
				ids[0] = ""
			}
			before := append([]string{}, ids...)
			page, err := s.GetCommsVotes(context.Background(), &GetCommsVotesRequest{ActorID: "viewer", CommsID: "contact", EntryIDs: ids})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, before, ids)
			require.Zero(t, votes.writes)
			if tc.want != nil {
				require.Nil(t, page)
				require.Zero(t, votes.reads)
				return
			}
			require.Len(t, page.EntryAuthors, tc.count)
			require.Len(t, page.ByEntry, tc.count+1)
			for _, id := range ids {
				require.Equal(t, "author-"+id, page.EntryAuthors[id])
				require.Contains(t, page.ByEntry, id)
			}
			require.NotContains(t, page.EntryAuthors, "")
			require.Len(t, votes.targets, tc.count+1)
			raw, err := json.Marshal(page)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "author-")
			require.NotContains(t, string(raw), "EntryAuthors")
			require.NotContains(t, string(raw), "participants")
		})
	}
}

func TestContactVotingOptionalCapabilities(t *testing.T) {
	for _, op := range []string{"read", "set", "remove"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"valid", nil}, {"nil service", ErrCommsVoteUnavailable},
			{"nil voter", ErrCommsVoteUnavailable}, {"typed nil voter", ErrCommsVoteUnavailable},
			{"nil repository", ErrCommsVoteUnavailable}, {"typed nil repository", ErrCommsVoteUnavailable},
			{"legacy repository", ErrCommsVoteUnavailable},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				contacts := &votingContactsProbe{}
				votes := &votingServiceProbe{}
				s := NewService(contacts).WithVoterService(votes)
				switch tc.name {
				case "nil service":
					s = nil
				case "nil voter":
					s.WithVoterService(nil)
				case "typed nil voter":
					s.WithVoterService((*votingServiceProbe)(nil))
				case "nil repository":
					s.contacterRepository = nil
				case "typed nil repository":
					s.contacterRepository = (*votingContactsProbe)(nil)
				case "legacy repository":
					s.contacterRepository = &struct{ contacterRepository }{contacts}
				}
				var result *CommsVoteResult
				var err error
				req := &ChangeCommsVoteRequest{ActorID: "viewer", CommsID: "contact", Vote: voter.Up}
				switch op {
				case "read":
					result, err = s.GetCommsVotes(context.Background(), &GetCommsVotesRequest{ActorID: "viewer", CommsID: "contact"})
				case "set":
					result, err = s.SetCommsVote(context.Background(), req)
				case "remove":
					result, err = s.RemoveCommsVote(context.Background(), req)
				}
				require.ErrorIs(t, err, tc.want)
				if tc.want != nil {
					require.Nil(t, result)
					require.Zero(t, contacts.reads)
					require.Zero(t, votes.reads)
					require.Zero(t, votes.writes)
				} else {
					require.Empty(t, result.EntryAuthors)
				}
			})
		}
	}
}

func TestVotingServiceDelegationBoundaries(t *testing.T) {
	native := errors.New("native failure")
	for _, op := range []string{"set", "remove", "read"} {
		for _, tc := range []struct {
			name                                                  string
			missing, wrongChild, nilVotes, inconsistent, mismatch bool
			failure, want                                         error
		}{
			{name: "valid"},
			{name: "contradictory actor", mismatch: true, want: voter.ErrActorInvalid},
			{name: "missing contact", missing: true, want: ErrCommsNotFound},
			{name: "cross contact child", wrongChild: true, want: ErrCommsVoteUnavailable},
			{name: "missing voter", nilVotes: true, want: ErrCommsVoteUnavailable},
			{name: "native failure", failure: native, want: native},
			{name: "inconsistent summary", inconsistent: true, want: ErrCommsVoteUnavailable},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				contacts := &votingContactsProbe{missing: tc.missing, wrongChild: tc.wrongChild}
				votes := &votingServiceProbe{failure: tc.failure, inconsistent: tc.inconsistent}
				s := NewService(contacts).WithVoterService(votes)
				if tc.nilVotes {
					s.voterService = nil
				}
				ctx := context.Background()
				if tc.mismatch {
					ctx = helpers.TransitAuthenticatedWith(helpers.TransitWith(ctx, "other-actor"), true)
				}
				child := strings.Repeat("a", 64)
				req := &ChangeCommsVoteRequest{ActorID: "actor", CommsID: "contact", EntryID: child, Vote: voter.Up}
				var err error
				switch op {
				case "set":
					_, err = s.SetCommsVote(ctx, req)
				case "remove":
					_, err = s.RemoveCommsVote(ctx, req)
				case "read":
					_, err = s.GetCommsVotes(ctx, &GetCommsVotesRequest{ActorID: "actor", CommsID: "contact", EntryIDs: []string{child}})
				}
				require.ErrorIs(t, err, tc.want)
				if tc.mismatch || tc.nilVotes {
					require.Zero(t, contacts.reads)
				}
				if op == "read" || tc.missing || tc.wrongChild || tc.nilVotes || tc.mismatch {
					require.Zero(t, votes.writes)
				} else {
					require.Equal(t, 1, votes.writes)
					require.Equal(t, "actor", votes.actor)
					require.Equal(t, voter.Target{Domain: "contacter", ResourceID: "contact", ChildID: child}, votes.target)
				}
			})
		}
	}
}
