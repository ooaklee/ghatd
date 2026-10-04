package voter_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/voter"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func isolatedVoter(t *testing.T) (*voter.Service, *mongo.Database, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for isolated real-Mongo contract tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	name := "voter_test_" + toolbox.GenerateUuidV4()
	h, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, h.Close(cleanup))
	})
	store := repository.NewMongoDbRepositoryWithDefaults(h, name)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
	})
	require.NoError(t, voter.EnsureIndexes(ctx, db))
	require.NoError(t, voter.EnsureIndexes(ctx, db))
	return voter.NewService(voter.NewRepository(store)), db, ctx
}

func TestMongoVotingLifecycleAndIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, actor string
		target      voter.Target
		other       voter.Target
	}{
		{"domain isolation", "actor", voter.Target{Domain: "vision", ResourceID: "same"}, voter.Target{Domain: "contacter", ResourceID: "same"}},
		{"child isolation", "actor", voter.Target{Domain: "contacter", ResourceID: "same", ChildID: "child"}, voter.Target{Domain: "contacter", ResourceID: "same"}},
		{"scoped literal actor", "$actor", voter.Target{Scope: "tenant", Domain: "vision", ResourceID: "same"}, voter.Target{Domain: "vision", ResourceID: "same"}},
		{"resource isolation", "actor", voter.Target{Domain: "vision", ResourceID: "same"}, voter.Target{Domain: "vision", ResourceID: "other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, ctx := isolatedVoter(t)
			other := tc.other
			require.NoError(t, s.SetVote(ctx, &voter.SetVoteRequest{ActorID: tc.actor, Target: other, Vote: voter.Up}))
			// Each table case deliberately follows the same vote through its lifecycle.
			for _, step := range []struct {
				name     string
				vote     voter.Value
				remove   bool
				up, down int
			}{
				{"set", voter.Up, false, 1, 0}, {"repeat", voter.Up, false, 1, 0}, {"switch", voter.Down, false, 0, 1}, {"remove", voter.Down, true, 0, 0}, {"repeat remove", voter.Down, true, 0, 0},
			} {
				t.Run(step.name, func(t *testing.T) {
					var err error
					if step.remove {
						err = s.RemoveVote(ctx, &voter.RemoveVoteRequest{ActorID: tc.actor, Target: tc.target})
					} else {
						err = s.SetVote(ctx, &voter.SetVoteRequest{ActorID: tc.actor, Target: tc.target, Vote: step.vote})
					}
					require.NoError(t, err)
					rows, err := s.GetSummaries(ctx, &voter.GetSummariesRequest{ActorID: tc.actor, Targets: []voter.Target{tc.target, other}})
					require.NoError(t, err)
					require.Equal(t, step.up, rows[tc.target].Up)
					require.Equal(t, step.down, rows[tc.target].Down)
					require.Equal(t, 1, rows[other].Up)
					if step.remove {
						require.Nil(t, rows[tc.target].ViewerVote)
					} else {
						require.Equal(t, step.vote, *rows[tc.target].ViewerVote)
					}
					public, err := s.GetSummaries(ctx, &voter.GetSummariesRequest{Targets: []voter.Target{tc.target}})
					require.NoError(t, err)
					require.Nil(t, public[tc.target].ViewerVote)
					require.Equal(t, step.up, public[tc.target].Up)
				})
			}
			count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestMongoConcurrentActorVotes(t *testing.T) {
	for _, actors := range []int{1, 4} {
		t.Run(fmt.Sprintf("%d actors", actors), func(t *testing.T) {
			s, db, ctx := isolatedVoter(t)
			target := voter.Target{Domain: "vision", ResourceID: "shared"}
			var wg sync.WaitGroup
			failures := make(chan error, 32)
			for i := 0; i < 32; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					failures <- s.SetVote(ctx, &voter.SetVoteRequest{ActorID: fmt.Sprintf("actor-%d", i%actors), Target: target, Vote: voter.Up})
				}(i)
			}
			wg.Wait()
			close(failures)
			for err := range failures {
				require.NoError(t, err)
			}
			rows, err := s.GetSummaries(ctx, &voter.GetSummariesRequest{ActorID: "actor-0", Targets: []voter.Target{target}})
			require.NoError(t, err)
			require.Equal(t, actors, rows[target].Up)
			count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.EqualValues(t, actors, count)
			var stored bson.M
			require.NoError(t, db.Collection(voter.Collection).FindOne(ctx, bson.M{"actor_id": "actor-0"}).Decode(&stored))
			stored["_id"] = "duplicate-natural-key"
			_, err = db.Collection(voter.Collection).InsertOne(ctx, stored)
			require.True(t, mongo.IsDuplicateKeyError(err))
		})
	}
}

func TestMongoMalformedVotesFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   any
		present bool
	}{
		{"missing", nil, false}, {"null", nil, true}, {"invalid", 2, true}, {"string", "1", true}, {"fraction", 0.5, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, ctx := isolatedVoter(t)
			target := voter.Target{Domain: "vision", ResourceID: "item"}
			doc := bson.M{"_id": "malformed", "scope": "", "domain": "vision", "resource_id": "item", "child_id": "", "actor_id": "actor"}
			if tc.present {
				doc["vote"] = tc.value
			}
			_, err := db.Collection(voter.Collection).InsertOne(ctx, doc)
			require.NoError(t, err)
			rows, err := s.GetSummaries(ctx, &voter.GetSummariesRequest{ActorID: "actor", Targets: []voter.Target{target}})
			require.ErrorIs(t, err, voter.ErrUnavailable)
			require.Nil(t, rows)
		})
	}
}
