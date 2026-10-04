package voter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type storeProbe struct {
	client                *mongo.Client
	initCalls, writeCalls int
	initErr, writeErr     error
	update                *mongo.UpdateResult
	deleted               *mongo.DeleteResult
	documents             []any
	cursorNil             bool
}

func (p *storeProbe) InitialiseClient(context.Context) (*mongo.Client, error) {
	p.initCalls++
	return p.client, p.initErr
}
func (p *storeProbe) GetDatabase(context.Context, string) (*mongo.Database, error) {
	return p.client.Database("voter_probe"), nil
}
func (p *storeProbe) ExecuteUpdateOneCommandResult(context.Context, *mongo.Collection, any, any, ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error) {
	p.writeCalls++
	return p.update, p.writeErr
}
func (p *storeProbe) ExecuteDeleteOneCommandResult(context.Context, *mongo.Collection, any, ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error) {
	p.writeCalls++
	return p.deleted, p.writeErr
}
func (p *storeProbe) ExecuteAggregateCommand(context.Context, *mongo.Collection, []bson.D) (*mongo.Cursor, error) {
	if p.cursorNil {
		return nil, nil
	}
	return mongo.NewCursorFromDocuments(p.documents, nil, nil)
}

func probeRepository(t *testing.T, p *storeProbe) *Repository {
	t.Helper()
	client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
	p.client = client
	return NewRepository(p)
}

func TestRepositoryMutationReceipts(t *testing.T) {
	target := Target{Domain: "vision", ResourceID: "item"}
	id := voteID("actor", target)
	for _, tc := range []struct {
		name    string
		update  *mongo.UpdateResult
		deleted *mongo.DeleteResult
		remove  bool
		want    error
	}{
		{name: "insert", update: &mongo.UpdateResult{Acknowledged: true, UpsertedCount: 1, UpsertedID: id}},
		{name: "update", update: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 1}},
		{name: "matched noop", update: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1}},
		{name: "nil", want: ErrUnavailable},
		{name: "unacknowledged", update: &mongo.UpdateResult{MatchedCount: 1}, want: ErrUnavailable},
		{name: "wrong insertion", update: &mongo.UpdateResult{Acknowledged: true, UpsertedCount: 1, UpsertedID: "wrong"}, want: ErrUnavailable},
		{name: "missing match", update: &mongo.UpdateResult{Acknowledged: true}, want: ErrUnavailable},
		{name: "inconsistent", update: &mongo.UpdateResult{Acknowledged: true, MatchedCount: 1, ModifiedCount: 2}, want: ErrUnavailable},
		{name: "remove", remove: true, deleted: &mongo.DeleteResult{Acknowledged: true, DeletedCount: 1}},
		{name: "already absent", remove: true, deleted: &mongo.DeleteResult{Acknowledged: true}},
		{name: "nil delete", remove: true, want: ErrUnavailable},
		{name: "unacknowledged delete", remove: true, deleted: &mongo.DeleteResult{DeletedCount: 1}, want: ErrUnavailable},
		{name: "invalid delete", remove: true, deleted: &mongo.DeleteResult{Acknowledged: true, DeletedCount: 2}, want: ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &storeProbe{update: tc.update, deleted: tc.deleted}
			r := probeRepository(t, p)
			var err error
			if tc.remove {
				err = r.RemoveVote(context.Background(), "actor", target)
			} else {
				err = r.SetVote(context.Background(), "actor", target, Up)
			}
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, 1, p.writeCalls)
		})
	}
}

func TestRepositoryNativeFailuresAndSetup(t *testing.T) {
	failure := errors.New("synthetic native error")
	for _, tc := range []struct {
		name   string
		setup  bool
		remove bool
	}{{"set uncertainty", false, false}, {"remove uncertainty", false, true}, {"bounded setup", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			p := &storeProbe{writeErr: failure}
			r := probeRepository(t, p)
			if tc.setup {
				p.initErr = failure
			}
			var err error
			if tc.remove {
				err = r.RemoveVote(context.Background(), "actor", Target{Domain: "vision", ResourceID: "id"})
			} else {
				err = r.SetVote(context.Background(), "actor", Target{Domain: "vision", ResourceID: "id"}, Up)
			}
			require.ErrorIs(t, err, failure)
			if tc.setup {
				require.Equal(t, 3, p.initCalls)
				require.Zero(t, p.writeCalls)
			} else {
				require.Equal(t, 1, p.writeCalls)
			}
		})
	}
}

func TestRepositorySummaryValidation(t *testing.T) {
	target := Target{Domain: "vision", ResourceID: "item"}
	for _, tc := range []struct {
		name      string
		docs      []any
		nilCursor bool
		want      error
	}{
		{name: "zero", docs: []any{}},
		{name: "up", docs: []any{bson.M{"_id": target, "up": 1, "down": 0, "own_up": 1}}},
		{name: "malformed stored vote", docs: []any{bson.M{"_id": target, "invalid": 1}}, want: ErrUnavailable},
		{name: "own exceeds total", docs: []any{bson.M{"_id": target, "own_up": 1}}, want: ErrUnavailable},
		{name: "duplicate own", docs: []any{bson.M{"_id": target, "up": 2, "own_up": 2}}, want: ErrUnavailable},
		{name: "other domain", docs: []any{bson.M{"_id": Target{Domain: "contacter", ResourceID: "item"}}}, want: ErrUnavailable},
		{name: "duplicate result", docs: []any{bson.M{"_id": target}, bson.M{"_id": target}}, want: ErrUnavailable},
		{name: "decode error", docs: []any{bson.M{"_id": target, "up": "bad"}}, want: errors.New("decode")},
		{name: "nil cursor", nilCursor: true, want: ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &storeProbe{documents: tc.docs, cursorNil: tc.nilCursor}
			got, err := probeRepository(t, p).GetSummaries(context.Background(), "actor", []Target{target})
			if tc.name == "decode error" {
				require.Error(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			if tc.want != nil {
				require.Nil(t, got)
			} else {
				require.Len(t, got, 1)
			}
		})
	}
}

func TestVoteIdentitySeparation(t *testing.T) {
	base := Target{Domain: "vision", ResourceID: "same"}
	original := voteID("actor", base)
	for _, tc := range []struct {
		name, actor string
		target      Target
	}{
		{"actor", "other", base},
		{"domain", "actor", Target{Domain: "contacter", ResourceID: "same"}},
		{"child", "actor", Target{Domain: "vision", ResourceID: "same", ChildID: "child"}},
		{"scope", "actor", Target{Scope: "tenant", Domain: "vision", ResourceID: "same"}},
		{"delimiter safe", "actor", Target{Domain: "vision:same", ResourceID: "x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NotEqual(t, original, voteID(tc.actor, tc.target))
			require.Len(t, voteID(tc.actor, tc.target), 64)
		})
	}
}
