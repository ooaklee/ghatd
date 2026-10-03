package apitoken

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// fencedInventoryStub records lock-before-count ordering without claiming real
// atomicity. The Mongo matrices exercise actual transaction behavior.
type fencedInventoryStub struct {
	*inventoryRepository
	fenceErr error
	fenced   bool
}

func (s *fencedInventoryStub) FenceInventory(context.Context, string) error {
	if len(s.owners) != 0 {
		panic("count occurred before fence")
	}
	s.fenced = true
	return s.fenceErr
}

func TestFencedCountRequiresCapabilityAndSuccessfulFence(t *testing.T) {
	failure := errors.New("fence failed")
	for _, tc := range []struct {
		name     string
		missing  bool
		fenceErr error
	}{
		{"success", false, nil}, {"fence failure", false, failure}, {"old count-only adapter", true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			counter := &inventoryRepository{permanent: 2, ephemeral: 1}
			fenced := &fencedInventoryStub{inventoryRepository: counter, fenceErr: tc.fenceErr}
			var port ApitokenRespository = fenced
			if tc.missing {
				port = counter
			}
			got, err := NewService(port).CountTokenInventoryFenced(context.Background(), "owner")
			switch {
			case tc.missing:
				require.ErrorIs(t, err, ErrInventoryUnavailable)
			case tc.fenceErr != nil:
				require.ErrorIs(t, err, tc.fenceErr)
			default:
				require.NoError(t, err)
				require.True(t, fenced.fenced)
				require.Equal(t, Inventory{Permanent: 2, Ephemeral: 1}, got)
			}
			if err != nil {
				require.Zero(t, got)
				require.Empty(t, counter.owners)
			}
		})
	}
}

func TestMongoInventoryPreparation(t *testing.T) {
	for _, tc := range []struct {
		name                string
		concurrent, corrupt bool
	}{
		{"repeat preserves existing marker", false, false}, {"concurrent insert-only preparation", true, false}, {"corrupt identity cannot be overwritten", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			require.NoError(t, repo.InitializeTokenInventory(ctx))
			require.NoError(t, repo.PrepareTokenInventory(ctx, "owner"))
			tokens, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			fences := tokens.Database().Collection(InventoryFenceCollection)
			var before inventoryFence
			require.NoError(t, fences.FindOne(ctx, bson.M{"_id": inventoryOwnerID("owner")}).Decode(&before))
			if tc.corrupt {
				_, err := fences.UpdateOne(ctx, bson.M{"_id": before.ID}, bson.M{"$set": bson.M{"owner_id": "other"}})
				require.NoError(t, err)
				require.ErrorIs(t, repo.PrepareTokenInventory(ctx, "owner"), ErrInventoryUnavailable)
			} else {
				workers := 1
				if tc.concurrent {
					workers = 16
				}
				outcomes := make(chan error, workers)
				var wg sync.WaitGroup
				for i := 0; i < workers; i++ {
					wg.Add(1)
					go func() { defer wg.Done(); outcomes <- repo.PrepareTokenInventory(ctx, "new-owner") }()
				}
				wg.Wait()
				close(outcomes)
				for err := range outcomes {
					require.NoError(t, err)
				}
				require.NoError(t, repo.PrepareTokenInventory(ctx, "owner"))
				var after inventoryFence
				require.NoError(t, fences.FindOne(ctx, bson.M{"_id": before.ID}).Decode(&after))
				require.Equal(t, before, after)
			}
			count, err := tokens.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count, "preparation creates no credential")
		})
	}
}

func TestMongoInventoryFenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"prepared active transaction", "valid", nil},
		{"missing initialization", "uninitialized", ErrInventoryUnavailable},
		{"missing owner lock", "missing", ErrInventoryUnavailable},
		{"corrupt owner identity", "corrupt-owner", ErrInventoryUnavailable},
		{"empty lock marker", "empty-marker", ErrInventoryUnavailable},
		{"wrong lock marker type", "wrong-marker", ErrInventoryUnavailable},
		{"empty owner", "empty", ErrInventoryUnavailable},
		{"oversize owner", "oversize", ErrInventoryUnavailable},
		{"malformed owner", "utf8", ErrInventoryUnavailable},
		{"whitespace owner", "whitespace", ErrInventoryUnavailable},
		{"no session", "no-session", ErrInventoryUnavailable},
		{"session without transaction", "no-transaction", ErrInventoryUnavailable},
		{"foreign client", "foreign", ErrInventoryUnavailable},
		{"preparation inside transaction", "nested-prepare", ErrInventoryUnavailable},
		{"initialization inside transaction", "nested-initialize", ErrInventoryUnavailable},
		{"cancelled", "cancelled", context.Canceled},
		{"nil context", "nil", ErrInventoryUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx := isolatedTokenRepository(t)
			core := repo.Store.(*repository.MongoDbRepository)
			tokens, err := repo.GetApiTokenCollection(ctx)
			require.NoError(t, err)
			if tc.mode != "uninitialized" {
				require.NoError(t, repo.InitializeTokenInventory(ctx))
				if tc.mode != "missing" {
					require.NoError(t, repo.PrepareTokenInventory(ctx, "owner"))
				}
			}
			var corrupt bson.M
			switch tc.mode {
			case "corrupt-owner":
				corrupt = bson.M{"owner_id": "different-owner"}
			case "empty-marker":
				corrupt = bson.M{"fence": ""}
			case "wrong-marker":
				corrupt = bson.M{"fence": 12}
			}
			if corrupt != nil {
				_, err := tokens.Database().Collection(InventoryFenceCollection).UpdateOne(ctx, bson.M{"_id": inventoryOwnerID("owner")}, bson.M{"$set": corrupt})
				require.NoError(t, err)
			}
			owner := "owner"
			switch tc.mode {
			case "empty":
				owner = ""
			case "oversize":
				owner = strings.Repeat("a", 257)
			case "utf8":
				owner = string([]byte{0xff})
			case "whitespace":
				owner = "two owners"
			}
			call := func(run context.Context) error {
				if tc.mode == "nested-prepare" {
					return repo.PrepareTokenInventory(run, owner)
				}
				if tc.mode == "nested-initialize" {
					return repo.InitializeTokenInventory(run)
				}
				if tc.mode == "cancelled" {
					cancelled, cancel := context.WithCancel(run)
					cancel()
					run = cancelled
				}
				return repo.FenceInventory(run, owner)
			}
			switch tc.mode {
			case "nil":
				err = repo.FenceInventory(nil, owner)
			case "no-session":
				err = call(ctx)
			case "no-transaction":
				session, e := tokens.Database().Client().StartSession()
				require.NoError(t, e)
				defer session.EndSession(context.Background())
				err = call(mongo.NewSessionContext(ctx, session))
			case "foreign":
				other, otherCtx := isolatedTokenRepository(t)
				otherTokens, e := other.GetApiTokenCollection(otherCtx)
				require.NoError(t, e)
				err = other.Store.(*repository.MongoDbRepository).WithMongoTransaction(otherCtx, otherTokens.Database(), call)
			default:
				err = core.WithMongoTransaction(ctx, tokens.Database(), call)
			}
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
