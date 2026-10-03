package accessmanager_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// isolatedCreationStore owns only its uniquely named test database. It uses one
// managed client for real policy fences, exact counts and credential writes.
func isolatedCreationStore(t *testing.T) (*repository.MongoDbRepository, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated Mongo replica set")
	}
	name := "ghatd_creation_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	manager, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	core := repository.NewMongoDbRepositoryWithDefaults(manager, name)
	db, err := core.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, manager.Close(cleanup))
	})
	require.NoError(t, core.EnsureMongoCollection(ctx, db, apitoken.ApiTokenCollection))
	return core, ctx
}

// interruptedCreation preserves the real transaction context while forcing an
// abort after insertion or one labeled transient failure for driver retry.
type interruptedCreation struct {
	*apitoken.Service
	fail      bool
	retryOnce bool
	attempts  int
}

func (s *interruptedCreation) CreateAPIToken(ctx context.Context, req *apitoken.CreateAPITokenRequest) (*apitoken.CreateAPITokenResponse, error) {
	response, err := s.Service.CreateAPIToken(ctx, req)
	if err != nil {
		return nil, err
	}
	s.attempts++
	if s.fail {
		return nil, errors.New("abort after insert")
	}
	if s.retryOnce && s.attempts == 1 {
		return nil, mongo.CommandError{Code: 112, Message: "test transient", Labels: []string{"TransientTransactionError"}}
	}
	return response, nil
}

func TestMongoPolicyTokenAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                      string
		workers                   int
		ttl                       int64
		fail, retry, disableGrant bool
		want                      int
	}{
		{"concurrent permanent admission", 16, 0, false, false, false, 3}, {"concurrent ephemeral admission", 16, 60, false, false, false, 3},
		{"mixed inventory shares one fence", 16, -1, false, false, false, 6},
		{"failed callback rolls back inserted secret", 1, 0, true, false, false, 0}, {"retry commits only one credential", 1, 0, false, true, false, 1}, {"disabled grant denies ADMIN", 1, 0, false, false, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, ctx := isolatedCreationStore(t)
			store, err := accesspolicy.NewMongoStore(ctx, core)
			require.NoError(t, err)
			require.NoError(t, store.Initialize(ctx))
			policy, err := accesspolicy.NewService(store, func(context.Context, string) (string, error) { return "test-admin", nil })
			require.NoError(t, err)
			_, err = policy.ReplaceGrant(ctx, accesspolicy.Grant{Subject: accesspolicy.Subject{System: "test-system", Kind: accesspolicy.UserSubject, ID: "owner"}, Enabled: !tc.disableGrant, Tokens: accesspolicy.TokenLimits{Permanent: 3, Ephemeral: 3, MinimumTTL: 60, MaximumTTL: 3600, TTLIncrement: 60}}, 0)
			require.NoError(t, err)
			tokenRepository := apitoken.NewRepository(core)
			require.NoError(t, tokenRepository.InitializeTokenInventory(ctx))
			require.NoError(t, tokenRepository.PrepareTokenInventory(ctx, "owner"))
			api := apitoken.NewService(tokenRepository)
			var port accessmanager.ApitokenService = api
			if tc.fail || tc.retry {
				port = &interruptedCreation{Service: api, fail: tc.fail, retryOnce: tc.retry}
			}
			service := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: accesspolicy.TokenPolicy{Service: policy, System: "test-system"}, ApiTokenService: port, UserService: &creationUserStub{user: &userv2.UniversalUser{ID: "owner", NanoID: "prefix", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}}}})
			type outcome struct {
				response *accessmanager.CreateUserAPITokenResponse
				err      error
			}
			outcomes := make(chan outcome, tc.workers)
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := 0; i < tc.workers; i++ {
				ttl := tc.ttl
				if ttl == -1 {
					ttl = int64(i%2) * 60
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					got, err := service.CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{UserID: "owner", Ttl: ttl})
					outcomes <- outcome{got, err}
				}()
			}
			close(start)
			wg.Wait()
			close(outcomes)
			successes := 0
			ids := map[string]bool{}
			for got := range outcomes {
				if got.err == nil {
					successes++
					require.NotNil(t, got.response)
					require.False(t, ids[got.response.UserAPIToken.ID])
					ids[got.response.UserAPIToken.ID] = true
				} else {
					require.Nil(t, got.response)
					if tc.disableGrant {
						require.ErrorIs(t, got.err, accesspolicy.ErrDenied)
					} else if !tc.fail {
						if tc.ttl == -1 {
							require.True(t, errors.Is(got.err, accessmanager.ErrPermanentAPITokenLimitReached) || errors.Is(got.err, accessmanager.ErrEphemeralAPITokenLimitReached), "unexpected admission error: %v", got.err)
						} else if tc.ttl == 0 {
							require.ErrorIs(t, got.err, accessmanager.ErrPermanentAPITokenLimitReached)
						} else {
							require.ErrorIs(t, got.err, accessmanager.ErrEphemeralAPITokenLimitReached)
						}
					}
				}
			}
			require.Equal(t, tc.want, successes)
			if intercepted, ok := port.(*interruptedCreation); ok {
				attempts := 1
				if tc.retry {
					attempts = 2
				}
				require.Equal(t, attempts, intercepted.attempts, "the real transaction boundary retries the injected transient abort exactly once")
			}
			db, err := core.GetDatabase(ctx, "")
			require.NoError(t, err)
			count, err := db.Collection(apitoken.ApiTokenCollection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, int64(tc.want), count)
			for id := range ids {
				var saved apitoken.UserAPIToken
				require.NoError(t, db.Collection(apitoken.ApiTokenCollection).FindOne(ctx, bson.M{"_id": id}).Decode(&saved))
				require.Empty(t, saved.Value)
				require.Equal(t, "owner", saved.CreatedByID)
			}
		})
	}
}

// TestMongoMultiSystemInventory verifies that independently scoped grants and
// repository instances cannot race a shared owner's count-and-insert boundary.
func TestMongoMultiSystemInventory(t *testing.T) {
	for _, tc := range []struct {
		name                                                 string
		differentOwners, differentLimits, missingPreparation bool
		want                                                 int64
	}{
		{name: "two systems share one owner limit", want: 1},
		{name: "each system checks total against its own allowance", differentLimits: true, want: 3},
		{name: "different owners have independent inventories", differentOwners: true, want: 2},
		{name: "missing preparation denies every system", missingPreparation: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, ctx := isolatedCreationStore(t)
			store, err := accesspolicy.NewMongoStore(ctx, core)
			require.NoError(t, err)
			require.NoError(t, store.Initialize(ctx))
			policy, err := accesspolicy.NewService(store, func(context.Context, string) (string, error) { return "migration-admin", nil })
			require.NoError(t, err)
			services := make([]*accessmanager.Service, 2)
			owners := []string{"owner", "owner"}
			if tc.differentOwners {
				owners[1] = "other-owner"
			}
			for i, system := range []string{"system-a", "system-b"} {
				limit := int64(1)
				if tc.differentLimits && i == 1 {
					limit = 3
				}
				_, err = policy.ReplaceGrant(ctx, accesspolicy.Grant{Subject: accesspolicy.Subject{System: system, Kind: accesspolicy.UserSubject, ID: owners[i]}, Enabled: true, Tokens: accesspolicy.TokenLimits{Permanent: limit}}, 0)
				require.NoError(t, err)
				repo := apitoken.NewRepository(core)
				require.NoError(t, repo.InitializeTokenInventory(ctx))
				if !tc.missingPreparation {
					require.NoError(t, repo.PrepareTokenInventory(ctx, owners[i]))
				}
				services[i] = accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: accesspolicy.TokenPolicy{Service: policy, System: system}, ApiTokenService: apitoken.NewService(repo), UserService: &creationUserStub{user: &userv2.UniversalUser{ID: owners[i], NanoID: owners[i], Status: userv2.AccountStatusKeyActive}}})
			}
			start := make(chan struct{})
			outcomes := make(chan error, 24)
			var wg sync.WaitGroup
			for i := 0; i < 24; i++ {
				index := i % 2
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					got, err := services[index].CreateUserAPIToken(ctx, &accessmanager.CreateUserAPITokenRequest{UserID: owners[index]})
					if err != nil && got != nil {
						outcomes <- errors.New("secret returned on failed admission")
						return
					}
					outcomes <- err
				}()
			}
			close(start)
			wg.Wait()
			close(outcomes)
			var successes int64
			for err := range outcomes {
				if err == nil {
					successes++
					continue
				}
				if tc.missingPreparation {
					require.ErrorIs(t, err, apitoken.ErrInventoryUnavailable)
				} else {
					require.ErrorIs(t, err, accessmanager.ErrPermanentAPITokenLimitReached)
				}
			}
			require.Equal(t, tc.want, successes)
			db, err := core.GetDatabase(ctx, "")
			require.NoError(t, err)
			count, err := db.Collection(apitoken.ApiTokenCollection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, tc.want, count)
		})
	}
}
