package contacter

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// conversationStoreFault isolates driver receipts from service-level adapters.
// The collection handle is never contacted; unexpected broad methods panic.
type conversationStoreFault struct {
	MongoDbStore
	receipt           *mongo.InsertOneResult
	writeErr, readErr error
	existing          *CommsEntry
	reads, writes     int
}

func (p *conversationStoreFault) ExecuteInsertOneCommand(context.Context, *mongo.Collection, interface{}, string) (*mongo.InsertOneResult, error) {
	p.writes++
	return p.receipt, p.writeErr
}
func (p *conversationStoreFault) ExecuteFindOneCommandDecodeResult(_ context.Context, _ *mongo.Collection, _ interface{}, out interface{}, _ string, _ bool, _ error) error {
	p.reads++
	if p.readErr != nil {
		return p.readErr
	}
	if p.existing != nil {
		*out.(*CommsEntry) = *copyCommsEntry(p.existing)
	}
	return nil
}

func TestConversationRepositoryAcknowledgements(t *testing.T) {
	duplicate := mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000, Message: "duplicate"}}}
	native := errors.New("private-driver-diagnostic")
	for _, tc := range []struct {
		name   string
		reads  int
		replay bool
	}{
		{"acknowledged", 0, false}, {"nil receipt", 0, false}, {"unacknowledged", 0, false}, {"wrong inserted id", 0, false}, {"native write", 0, false}, {"exact duplicate", 1, true}, {"changed duplicate", 1, false}, {"duplicate missing", 1, false}, {"duplicate read failure", 1, false}, {"wrapped duplicate", 0, false}, {"mixed failure", 0, false}, {"write concern", 0, false}, {"labeled failure", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &CommsEntry{ID: entryID("test"), CommsID: "contact", ActorID: "admin", Kind: CommsEntryInternalNote, Body: "Private body", RecordedAt: time.UnixMilli(1700000000000).UTC()}
			p := &conversationStoreFault{receipt: &mongo.InsertOneResult{Acknowledged: true, InsertedID: v.ID}, existing: copyCommsEntry(v)}
			var want error
			switch tc.name {
			case "nil receipt":
				p.receipt = nil
				want = repository.ErrUnacknowledgedMongoWrite
			case "unacknowledged":
				p.receipt.Acknowledged = false
				want = repository.ErrUnacknowledgedMongoWrite
			case "wrong inserted id":
				p.receipt.InsertedID = "other"
				want = repository.ErrUnacknowledgedMongoWrite
			case "native write":
				p.writeErr = native
				want = native
			case "exact duplicate":
				p.writeErr = duplicate
			case "changed duplicate":
				p.writeErr = duplicate
				p.existing.Body = "Changed"
				want = ErrCommsEntryConflict
			case "duplicate missing":
				p.writeErr = duplicate
				p.readErr = ErrCommsEntryNotFound
				want = duplicate
			case "duplicate read failure":
				p.writeErr = duplicate
				p.readErr = native
				want = native
			case "wrapped duplicate":
				p.writeErr = fmt.Errorf("outer: %w", duplicate)
				want = p.writeErr
			case "mixed failure":
				p.writeErr = mongo.WriteException{WriteErrors: mongo.WriteErrors{{Code: 11000}, {Code: 121}}}
				want = p.writeErr
			case "write concern":
				p.writeErr = mongo.WriteException{WriteErrors: duplicate.WriteErrors, WriteConcernError: &mongo.WriteConcernError{Code: 64}}
				want = p.writeErr
			case "labeled failure":
				p.writeErr = mongo.WriteException{WriteErrors: duplicate.WriteErrors, Labels: []string{"RetryableWriteError"}}
				want = p.writeErr
			}
			client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			r := NewRepository(p)
			r.collection = client.Database("never_connected").Collection(CommsCollection)
			got, replay, err := r.InsertCommsEntry(context.Background(), v)
			require.Equal(t, want, err)
			require.Equal(t, tc.reads, p.reads)
			require.Equal(t, 1, p.writes)
			require.Equal(t, tc.replay, replay)
			if want != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, v, got)
			}
		})
	}
}

func TestConversationEmailSemanticEquality(t *testing.T) {
	for _, name := range []string{"nil references", "empty references", "different references", "different provider identity", "new ingestion actor"} {
		t.Run(name, func(t *testing.T) {
			v := &CommsEntry{ID: entryID("email", "provider", "mailbox", "message"), CommsID: "contact", ActorID: "worker1", Kind: CommsEntryEmailInbound, Body: "Email", RecordedAt: time.Now(), Email: &CommsEntryEmailMetadata{Provider: "provider", Mailbox: "mailbox", ProviderMessageID: "message", From: "from@example.test", To: []string{"to@example.test"}, OccurredAt: time.UnixMilli(1700000000000).UTC()}}
			other := copyCommsEntry(v)
			other.RecordedAt = time.Now().Add(time.Hour)
			want := true
			switch name {
			case "empty references":
				other.Email.References = []string{}
			case "different references":
				other.Email.References = []string{"<different@example.test>"}
				want = false
			case "different provider identity":
				other.Email.ProviderMessageID = "other"
				want = false
			case "new ingestion actor":
				other.ActorID = "worker2"
			}
			require.Equal(t, want, sameCommsEntryContent(v, other))
		})
	}
}
