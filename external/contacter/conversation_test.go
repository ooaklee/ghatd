package contacter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// conversationProbe traps legacy calls and supplies isolated domain receipts.
// Atomicity and duplicate-key reconciliation are verified against Mongo separately.
type conversationProbe struct {
	contacterRepository
	root    Comms
	entries map[string]*CommsEntry
	fault   string
	err     error
	writes  int
	cancel  context.CancelFunc
}

func (p *conversationProbe) GetCommsByIds(context.Context, []string) ([]Comms, error) {
	if p.fault == "read error" {
		return nil, p.err
	}
	if p.fault == "root absent" {
		return nil, nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	return []Comms{p.root}, nil
}
func (p *conversationProbe) FindCommsEntry(_ context.Context, c, id string) (*CommsEntry, error) {
	v := p.entries[id]
	if v == nil || v.CommsID != c {
		return nil, ErrCommsEntryNotFound
	}
	return copyCommsEntry(v), nil
}
func (p *conversationProbe) InsertCommsEntry(_ context.Context, v *CommsEntry) (*CommsEntry, bool, error) {
	p.writes++
	if p.fault == "write error" {
		return nil, false, p.err
	}
	if p.fault == "nil receipt" {
		return nil, false, nil
	}
	if old := p.entries[v.ID]; old != nil {
		if !sameCommsEntryContent(old, v) {
			return nil, false, ErrCommsEntryConflict
		}
		return copyCommsEntry(old), true, nil
	}
	if p.fault == "wrong receipt" {
		v.CommsID = "other"
	}
	p.entries[v.ID] = copyCommsEntry(v)
	return copyCommsEntry(v), false, nil
}
func (p *conversationProbe) QueryCommsEntries(_ context.Context, c string, after *CommsEntry, limit int) ([]CommsEntry, error) {
	if p.fault == "query error" {
		return nil, p.err
	}
	rows := []CommsEntry{}
	for _, v := range p.entries {
		if v.CommsID == c && (after == nil || entryBefore(v, after)) {
			rows = append(rows, *copyCommsEntry(v))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return entryBefore(&rows[j], &rows[i]) })
	if len(rows) > limit+1 {
		rows = rows[:limit+1]
	}
	if p.fault == "wrong page" && len(rows) > 0 {
		rows[0].CommsID = "other"
	}
	return rows, nil
}
func conversationFixture() (*Service, *conversationProbe, *AppendCommsEntryRequest) {
	p := &conversationProbe{root: Comms{Id: "contact", Message: "Original", AdminNotes: "Legacy private note"}, entries: map[string]*CommsEntry{}}
	return NewService(p), p, &AppendCommsEntryRequest{ActorID: "admin-a", CommsID: "contact", RequestID: uuid.NewString(), Kind: CommsEntryInternalNote, Body: "A private note"}
}

func TestConversationAppendBoundaries(t *testing.T) {
	native := errors.New("private-storage-failure")
	for _, tc := range []struct {
		name   string
		want   error
		writes int
	}{
		{"note", nil, 1}, {"reply", nil, 1}, {"published actor", nil, 1},
		{"contradictory cached user", ErrCommsActorRequired, 0}, {"typed nil cached user", ErrCommsActorRequired, 0}, {"non RFC request id", ErrCommsEntryInvalid, 0},
		{"nil request", ErrCommsEntryInvalid, 0}, {"nil service", ErrCommsConversationUnavailable, 0}, {"nil store", ErrCommsConversationUnavailable, 0}, {"typed nil store", ErrCommsConversationUnavailable, 0},
		{"nil context", ErrCommsActorRequired, 0}, {"canceled", context.Canceled, 0}, {"cancel read", context.Canceled, 0},
		{"blank actor", ErrCommsEntryInvalid, 0}, {"forged actor", ErrCommsActorRequired, 0}, {"anonymous actor", ErrCommsActorRequired, 0},
		{"blank body", ErrCommsEntryInvalid, 0}, {"large body", ErrCommsEntryInvalid, 0}, {"invalid utf8", ErrCommsEntryInvalid, 0}, {"email via append", ErrCommsEntryInvalid, 0}, {"bad request id", ErrCommsEntryInvalid, 0},
		{"root absent", ErrCommsNotFound, 0}, {"wrong root", ErrCommsConversationUnavailable, 0}, {"read error", native, 0}, {"write error", native, 1}, {"nil receipt", ErrCommsConversationUnavailable, 1}, {"wrong receipt", ErrCommsConversationUnavailable, 1},
		{"missing parent", ErrCommsEntryNotFound, 0}, {"cross parent", ErrCommsEntryNotFound, 0}, {"bad parent", ErrCommsEntryInvalid, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, r := conversationFixture()
			p.fault = tc.name
			p.err = native
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc.name {
			case "reply":
				r.Kind = CommsEntryReply
			case "published actor":
				ctx = helpers.TransitAuthenticatedWith(helpers.TransitWith(ctx, r.ActorID), true)
			case "contradictory cached user", "typed nil cached user":
				ctx = helpers.TransitAuthenticatedWith(helpers.TransitWith(ctx, r.ActorID), true)
				var u *user.UniversalUser
				if tc.name == "contradictory cached user" {
					u = &user.UniversalUser{ID: "other"}
				}
				ctx = context.WithValue(ctx, helpers.RequestorUserKey, u)
			case "non RFC request id":
				r.RequestID = "123e4567-e89b-42d3-0456-426614174000"
			case "nil request":
				r = nil
			case "nil service":
				s = nil
			case "nil store":
				s = NewService(nil)
			case "typed nil store":
				s = NewService((*conversationProbe)(nil))
			case "nil context":
				ctx = nil
			case "canceled":
				cancel()
			case "cancel read":
				p.cancel = cancel
			case "blank actor":
				r.ActorID = " "
			case "forged actor":
				ctx = helpers.TransitAuthenticatedWith(helpers.TransitWith(ctx, "other"), true)
			case "anonymous actor":
				ctx = helpers.TransitWith(ctx, r.ActorID)
			case "blank body":
				r.Body = " \n "
			case "large body":
				r.Body = strings.Repeat("x", MaxCommsEntryBodyBytes+1)
			case "invalid utf8":
				r.Body = string([]byte{255})
			case "email via append":
				r.Kind = CommsEntryEmailInbound
			case "bad request id":
				r.RequestID = "same-key"
			case "wrong root":
				p.root.Id = "other"
			case "missing parent":
				r.ParentEntryID = entryID("missing")
			case "cross parent":
				r.ParentEntryID = entryID("other")
				p.entries[r.ParentEntryID] = &CommsEntry{ID: r.ParentEntryID, CommsID: "other"}
			case "bad parent":
				r.ParentEntryID = "not-an-id"
			}
			got, err := s.AppendCommsEntry(ctx, r)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.writes, p.writes)
			if tc.want != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, r.Body, got.Entry.Body)
				require.Equal(t, r.ActorID, got.Entry.ActorID)
				require.False(t, got.Replayed)
			}
		})
	}
}

func TestConversationReplayAndCollaboration(t *testing.T) {
	for _, name := range []string{"exact replay", "different body", "other admin", "new reply", "reply to note"} {
		t.Run(name, func(t *testing.T) {
			s, p, r := conversationFixture()
			first, err := s.AppendCommsEntry(context.Background(), r)
			require.NoError(t, err)
			switch name {
			case "different body":
				r.Body = "Changed"
			case "other admin":
				r.ActorID = "admin-b"
			case "new reply":
				r.RequestID = uuid.NewString()
				r.Kind = CommsEntryReply
				r.Body = "Latest reply"
			case "reply to note":
				r.RequestID = uuid.NewString()
				r.ParentEntryID = first.Entry.ID
			}
			second, err := s.AppendCommsEntry(context.Background(), r)
			if name == "different body" {
				require.ErrorIs(t, err, ErrCommsEntryConflict)
				require.Len(t, p.entries, 1)
				return
			}
			require.NoError(t, err)
			if name == "exact replay" {
				require.True(t, second.Replayed)
				require.Equal(t, first.Entry, second.Entry)
				require.Len(t, p.entries, 1)
			} else {
				require.False(t, second.Replayed)
				require.Len(t, p.entries, 2)
			}
			require.Equal(t, "Legacy private note", p.root.AdminNotes)
			require.Empty(t, p.root.AdminReply)
		})
	}
}

func emailImportFixture() *ImportCommsEmailRequest {
	return &ImportCommsEmailRequest{Entry: CommsEntry{CommsID: "contact", ActorID: "ingest-a", Kind: CommsEntryEmailInbound, Body: "Email text", Email: &CommsEntryEmailMetadata{Provider: "provider", Mailbox: "support", ProviderMessageID: "provider-id", MessageID: "<header@example.test>", From: "sender@example.test", To: []string{"support@example.test"}, References: []string{"<previous@example.test>"}, OccurredAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}}}
}

func TestConversationEmailImports(t *testing.T) {
	for _, tc := range []struct {
		name  string
		want  error
		count int
	}{
		{"exact replay", nil, 1}, {"other ingestion actor", nil, 1}, {"new provider message same RFC header", nil, 2}, {"new mailbox", nil, 2},
		{"changed RFC header", ErrCommsEntryConflict, 1}, {"wrong conversation", ErrCommsEntryConflict, 1}, {"different body", ErrCommsEntryConflict, 1},
		{"header injection", ErrCommsEntryInvalid, 1}, {"missing provider id", ErrCommsEntryInvalid, 1}, {"note with email", ErrCommsEntryInvalid, 1}, {"oversized metadata", ErrCommsEntryInvalid, 1}, {"missing recipients", ErrCommsEntryInvalid, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, _ := conversationFixture()
			r := emailImportFixture()
			first, err := s.ImportCommsEmail(context.Background(), r)
			require.NoError(t, err)
			switch tc.name {
			case "other ingestion actor":
				r.Entry.ActorID = "ingest-b"
			case "new provider message same RFC header":
				r.Entry.Email.ProviderMessageID = "provider-2"
			case "new mailbox":
				r.Entry.Email.Mailbox = "other"
			case "changed RFC header":
				r.Entry.Email.MessageID = "<spoofed@example.test>"
			case "wrong conversation":
				r.Entry.CommsID = "other"
				p.root.Id = "other"
			case "different body":
				r.Entry.Body = "changed"
			case "header injection":
				r.Entry.Email.Subject = "Subject\r\nBcc:private@example.test"
			case "missing provider id":
				r.Entry.Email.ProviderMessageID = ""
			case "note with email":
				r.Entry.Kind = CommsEntryInternalNote
			case "oversized metadata":
				r.Entry.Email.References = make([]string, 65)
			case "missing recipients":
				r.Entry.Email.To = nil
			}
			result, err := s.ImportCommsEmail(context.Background(), r)
			require.ErrorIs(t, err, tc.want)
			require.Len(t, p.entries, tc.count)
			if tc.want != nil {
				require.Nil(t, result)
				return
			}
			if tc.count == 1 {
				require.True(t, result.Replayed)
				require.Equal(t, first.Entry, result.Entry)
			}
			r.Entry.Email.To[0] = "mutated"
			require.Equal(t, "support@example.test", p.entries[first.Entry.ID].Email.To[0])
		})
	}
}

func TestConversationPagination(t *testing.T) {
	for _, limit := range []int{1, 2, 25, 100} {
		t.Run(fmt.Sprintf("limit=%d", limit), func(t *testing.T) {
			s, p, r := conversationFixture()
			fixed := time.UnixMilli(1700000000000).UTC()
			for n := 0; n < 27; n++ {
				r.RequestID = uuid.NewString()
				created, err := s.AppendCommsEntry(context.Background(), r)
				require.NoError(t, err)
				p.entries[created.Entry.ID].RecordedAt = fixed
			}
			seen := map[string]bool{}
			cursor := ""
			for n := 0; n < 30; n++ {
				page, err := s.ListCommsConversation(context.Background(), &ListCommsConversationRequest{ActorID: r.ActorID, CommsID: r.CommsID, Limit: limit, Cursor: cursor})
				require.NoError(t, err)
				require.LessOrEqual(t, len(page.Entries), limit)
				require.Equal(t, "Original", page.Legacy.Message)
				for _, v := range page.Entries {
					require.False(t, seen[v.ID])
					seen[v.ID] = true
				}
				cursor = page.NextCursor
				if cursor == "" {
					break
				}
			}
			require.Len(t, seen, 27)
			require.Empty(t, cursor)
		})
	}
}

func TestConversationReadFailures(t *testing.T) {
	native := errors.New("private-query-error")
	for _, tc := range []struct {
		name string
		want error
	}{{"empty", nil}, {"bad cursor", ErrCommsEntryInvalid}, {"wrong contact cursor", ErrCommsEntryInvalid}, {"negative limit", ErrCommsEntryInvalid}, {"large limit", ErrCommsEntryInvalid}, {"root absent", ErrCommsNotFound}, {"wrong page", ErrCommsConversationUnavailable}, {"query error", native}} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, r := conversationFixture()
			created, err := s.AppendCommsEntry(context.Background(), r)
			require.NoError(t, err)
			p.fault = tc.name
			p.err = native
			q := &ListCommsConversationRequest{ActorID: r.ActorID, CommsID: r.CommsID}
			switch tc.name {
			case "empty":
				p.entries = map[string]*CommsEntry{}
			case "bad cursor":
				q.Cursor = "not json"
			case "wrong contact cursor":
				v := *created.Entry
				v.CommsID = "other"
				q.Cursor = encodeConversationCursor(v)
			case "negative limit":
				q.Limit = -1
			case "large limit":
				q.Limit = 101
			}
			result, err := s.ListCommsConversation(context.Background(), q)
			require.ErrorIs(t, err, tc.want)
			if err != nil {
				require.Nil(t, result)
			} else {
				require.Empty(t, result.Entries)
			}
		})
	}
}
