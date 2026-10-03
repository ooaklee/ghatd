package contacter

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// conversationMongoFixture uses an explicitly supplied disposable Mongo server.
// Every case owns a unique database; cleanup never touches existing collections.
func conversationMongoFixture(t *testing.T) (*Service, *Repository, *mongo.Database, *AppendCommsEntryRequest) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to run real Mongo conversation tests")
	}
	name := "contacter_conversation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	h, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	db, err := h.GetDatabase(context.Background(), name)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, h.Close(ctx))
	})
	r := NewRepository(repository.NewMongoDbRepositoryWithDefaults(h, name))
	root, err := r.CreateComms(context.Background(), &Comms{Message: "Original", AdminReply: "Legacy reply", AdminNotes: "Legacy private note"})
	require.NoError(t, err)
	return NewService(r), r, db, &AppendCommsEntryRequest{ActorID: "admin-a", CommsID: root.Id, RequestID: uuid.NewString(), Kind: CommsEntryInternalNote, Body: "Private note"}
}

func TestConversationMongoConcurrentAppends(t *testing.T) {
	for _, tc := range []struct {
		name   string
		unique bool
		want   int
	}{{"same command retries", false, 1}, {"independent administrators", true, 16}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, db, req := conversationMongoFixture(t)
			type outcome struct {
				result *AppendCommsEntryResponse
				err    error
			}
			ch := make(chan outcome, 16)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < 16; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					r := *req
					if tc.unique {
						r.ActorID = fmt.Sprintf("admin-%d", i)
					}
					<-start
					v, err := s.AppendCommsEntry(context.Background(), &r)
					ch <- outcome{v, err}
				}(i)
			}
			close(start)
			wg.Wait()
			close(ch)
			created := 0
			ids := map[string]bool{}
			for got := range ch {
				require.NoError(t, got.err)
				ids[got.result.Entry.ID] = true
				if !got.result.Replayed {
					created++
				}
			}
			require.Len(t, ids, tc.want)
			require.Equal(t, tc.want, created)
			count, err := db.Collection(CommsEntriesCollection).CountDocuments(context.Background(), bson.M{})
			require.NoError(t, err)
			require.Equal(t, int64(tc.want), count)
		})
	}
}

func TestConversationMongoLifecycle(t *testing.T) {
	for _, name := range []string{"exact replay", "changed content", "same thread parent", "cross thread parent", "legacy update preserves history", "deleted root hides history", "email redelivery", "email different root conflicts", "email headers do not deduplicate"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s, r, db, req := conversationMongoFixture(t)
			first, err := s.AppendCommsEntry(ctx, req)
			require.NoError(t, err)
			switch name {
			case "exact replay":
				got, err := s.AppendCommsEntry(ctx, req)
				require.NoError(t, err)
				require.True(t, got.Replayed)
				require.Equal(t, first.Entry, got.Entry)
			case "changed content":
				req.Body = "Changed"
				got, err := s.AppendCommsEntry(ctx, req)
				require.ErrorIs(t, err, ErrCommsEntryConflict)
				require.Nil(t, got)
			case "same thread parent", "cross thread parent":
				req.RequestID = uuid.NewString()
				req.ParentEntryID = first.Entry.ID
				if name == "cross thread parent" {
					root, err := r.CreateComms(ctx, &Comms{Message: "Another"})
					require.NoError(t, err)
					req.CommsID = root.Id
				}
				got, err := s.AppendCommsEntry(ctx, req)
				if name == "cross thread parent" {
					require.ErrorIs(t, err, ErrCommsEntryNotFound)
					require.Nil(t, got)
				} else {
					require.NoError(t, err)
					require.Equal(t, first.Entry.ID, got.Entry.ParentEntryID)
				}
			case "legacy update preserves history":
				roots, err := r.GetCommsByIds(ctx, []string{req.CommsID})
				require.NoError(t, err)
				roots[0].AdminNotes = "Updated legacy snapshot"
				_, err = r.UpdateComms(ctx, &roots[0])
				require.NoError(t, err)
				page, err := s.ListCommsConversation(ctx, &ListCommsConversationRequest{ActorID: req.ActorID, CommsID: req.CommsID})
				require.NoError(t, err)
				require.Len(t, page.Entries, 1)
				require.Equal(t, *first.Entry, page.Entries[0])
				require.Equal(t, "Updated legacy snapshot", page.Legacy.AdminNotes)
			case "deleted root hides history":
				require.NoError(t, r.DeleteComms(ctx, req.CommsID))
				_, err = s.AppendCommsEntry(ctx, req)
				require.ErrorIs(t, err, ErrCommsNotFound)
				_, err = s.ListCommsConversation(ctx, &ListCommsConversationRequest{ActorID: req.ActorID, CommsID: req.CommsID})
				require.ErrorIs(t, err, ErrCommsNotFound)
				count, err := db.Collection(CommsEntriesCollection).CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.Equal(t, int64(1), count)
			default:
				mail := &ImportCommsEmailRequest{Entry: CommsEntry{ActorID: "mail-worker", CommsID: req.CommsID, Kind: CommsEntryEmailInbound, Body: "Email body", Email: &CommsEntryEmailMetadata{Provider: "test-provider", Mailbox: "support", ProviderMessageID: "m-1", From: "sender@example.test", To: []string{"support@example.test"}, MessageID: "<untrusted@example.test>", OccurredAt: time.Now()}}}
				original, err := s.ImportCommsEmail(ctx, mail)
				require.NoError(t, err)
				mail.Entry.ActorID = "next-worker"
				if name == "email different root conflicts" {
					root, err := r.CreateComms(ctx, &Comms{Message: "Other root"})
					require.NoError(t, err)
					mail.Entry.CommsID = root.Id
				}
				if name == "email headers do not deduplicate" {
					mail.Entry.Email.ProviderMessageID = "m-2"
				}
				got, err := s.ImportCommsEmail(ctx, mail)
				if name == "email different root conflicts" {
					require.ErrorIs(t, err, ErrCommsEntryConflict)
					require.Nil(t, got)
				} else {
					require.NoError(t, err)
					if name == "email redelivery" {
						require.True(t, got.Replayed)
						require.Equal(t, original.Entry, got.Entry)
					} else {
						require.False(t, got.Replayed)
						require.NotEqual(t, original.Entry.ID, got.Entry.ID)
					}
				}
			}
		})
	}
}

func TestConversationMongoKeyset(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprintf("index=%t", indexed), func(t *testing.T) {
			ctx := context.Background()
			s, r, db, req := conversationMongoFixture(t)
			if indexed {
				require.NoError(t, EnsureCommsConversationIndexes(ctx, db))
				require.NoError(t, EnsureCommsConversationIndexes(ctx, db))
			}
			fixed := time.UnixMilli(1700000000000).UTC()
			for n := 0; n < 31; n++ {
				v := &CommsEntry{ID: entryID("admin", req.CommsID, req.ActorID, uuid.NewString()), CommsID: req.CommsID, ActorID: req.ActorID, Kind: CommsEntryReply, Body: "Recorded reply", RecordedAt: fixed}
				_, replay, err := r.InsertCommsEntry(ctx, v)
				require.NoError(t, err)
				require.False(t, replay)
			}
			seen := map[string]bool{}
			cursor := ""
			for n := 0; n < 20; n++ {
				page, err := s.ListCommsConversation(ctx, &ListCommsConversationRequest{ActorID: req.ActorID, CommsID: req.CommsID, Limit: 3, Cursor: cursor})
				require.NoError(t, err)
				require.LessOrEqual(t, len(page.Entries), 3)
				for _, v := range page.Entries {
					require.False(t, seen[v.ID])
					seen[v.ID] = true
				}
				cursor = page.NextCursor
				if cursor == "" {
					break
				}
			}
			require.Empty(t, cursor)
			require.Len(t, seen, 31)
		})
	}
}
