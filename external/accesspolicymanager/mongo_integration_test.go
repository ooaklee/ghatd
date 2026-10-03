package accesspolicymanager

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
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// policyUserAdapter delegates every issuance-time identity read to the real user
// repository with the original transaction context; unrelated methods fail loudly.
type policyUserAdapter struct {
	accessmanager.UserService
	repository *userv2.Repository
}

func (a *policyUserAdapter) GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	u, err := a.repository.GetUserByID(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	return &userv2.GetUserByIDResponse{User: u}, nil
}

// policyIssuanceSession models trusted session publication for the selected
// owner's separate issuance command. These policy tests do not verify credentials.
func policyIssuanceSession(ctx context.Context) context.Context {
	ctx = accesshelpers.TransitWith(ctx, "selected")
	ctx = accesshelpers.TransitAuthenticatedWith(ctx, true)
	return accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "selected", AccessUUID: "test-session", TokenUse: auth.TokenUseAccess, IsAuthorized: true})
}

// managerMongoFixture creates a unique case-owned database on an explicit test
// server. Policy, inventory and user repositories borrow the same managed client.
func managerMongoFixture(t *testing.T) (context.Context, *repository.MongoDbRepository, *Service, *accessmanager.Service) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated Mongo replica set")
	}
	name := "ghatd_management_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	manager, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
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
	store, err := accesspolicy.NewMongoStore(ctx, core)
	require.NoError(t, err)
	require.NoError(t, store.Initialize(ctx))
	tokens := apitoken.NewRepository(core)
	require.NoError(t, tokens.InitializeTokenInventory(ctx))
	users := userv2.NewRepository(core)
	_, err = users.CreateUser(ctx, &userv2.UniversalUser{ID: "selected", NanoID: "selected-prefix", Status: userv2.AccountStatusKeyActive, Roles: []string{"ADMIN"}})
	require.NoError(t, err)
	service, err := NewService(Config{System: "service", Store: store, Users: users, Inventory: tokens, Authorize: func(context.Context, string) (string, error) { return "test-operator", nil }})
	require.NoError(t, err)
	policy, err := accesspolicy.NewService(store, nil)
	require.NoError(t, err)
	issuer := accessmanager.NewService(&accessmanager.NewServiceRequest{TokenPolicy: accesspolicy.TokenPolicy{Service: policy, System: "service"}, ApiTokenService: apitoken.NewService(tokens), UserService: &policyUserAdapter{repository: users}})
	return ctx, core, service, issuer
}

func TestMongoExplicitProvisioning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		apply     bool
		target    string
		allowance int64
		want      error
	}{
		{"preview cannot enable administrator issuance", false, "selected", 1, nil},
		{"explicit provision enables one slot", true, "selected", 1, nil},
		{"zero is not unlimited", true, "selected", 0, nil},
		{"missing stored target", true, "missing", 1, ErrUserNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, core, manager, issuer := managerMongoFixture(t)
			db, err := core.GetDatabase(ctx, "")
			require.NoError(t, err)
			_, err = issuer.CreateUserAPIToken(policyIssuanceSession(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
			require.ErrorIs(t, err, accesspolicy.ErrDenied)
			limits := accesspolicy.TokenLimits{Permanent: tc.allowance}
			preview, err := manager.Preview(ctx, tc.target, limits)
			require.ErrorIs(t, err, tc.want)
			count, err := db.Collection(apitoken.InventoryFenceCollection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count)
			if tc.want == nil {
				require.Nil(t, preview.Before)
				require.Empty(t, preview.After.Permissions)
			}
			if tc.apply {
				grant, err := manager.Apply(ctx, tc.target, 0, limits)
				require.ErrorIs(t, err, tc.want)
				if tc.want == nil {
					require.Equal(t, int64(1), grant.Revision)
					require.Empty(t, grant.Scopes)
				}
			}
			got, err := issuer.CreateUserAPIToken(policyIssuanceSession(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
			if tc.apply && tc.want == nil && tc.allowance > 0 {
				require.NoError(t, err)
				require.NotEmpty(t, got.UserAPIToken.Value)
				got, err = issuer.CreateUserAPIToken(policyIssuanceSession(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
				require.ErrorIs(t, err, accessmanager.ErrPermanentAPITokenLimitReached)
				require.Nil(t, got)
			} else {
				require.Error(t, err)
				require.Nil(t, got)
			}
			count, err = db.Collection(apitoken.InventoryFenceCollection).CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			wantCount := int64(0)
			if tc.apply && tc.want == nil {
				wantCount = 1
			}
			require.Equal(t, wantCount, count)
		})
	}
}

func TestMongoManagementConcurrency(t *testing.T) {
	for _, variant := range []string{"competing create-only provisioning", "limit reduction alongside issuance"} {
		t.Run(variant, func(t *testing.T) {
			ctx, _, manager, issuer := managerMongoFixture(t)
			workers := 8
			start := make(chan struct{})
			results := make(chan error, workers)
			var wg sync.WaitGroup
			if variant == "limit reduction alongside issuance" {
				_, err := manager.Apply(ctx, "selected", 0, accesspolicy.TokenLimits{Permanent: 1})
				require.NoError(t, err)
			}
			for i := 0; i < workers; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if variant == "competing create-only provisioning" {
						_, err := manager.Apply(ctx, "selected", 0, accesspolicy.TokenLimits{Permanent: 1})
						results <- err
						return
					}
					if i == 0 {
						_, err := manager.Apply(ctx, "selected", 1, accesspolicy.TokenLimits{})
						results <- err
						return
					}
					_, err := issuer.CreateUserAPIToken(policyIssuanceSession(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
					results <- err
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
					continue
				}
				if variant == "competing create-only provisioning" {
					require.ErrorIs(t, err, accesspolicy.ErrConflict)
				} else {
					require.True(t, errors.Is(err, accessmanager.ErrPermanentAPITokenLimitReached), "unexpected error: %v", err)
				}
			}
			if variant == "competing create-only provisioning" {
				require.Equal(t, 1, success)
			} else {
				require.GreaterOrEqual(t, success, 1)
				require.LessOrEqual(t, success, 2)
				preview, err := manager.Preview(ctx, "selected", accesspolicy.TokenLimits{})
				require.NoError(t, err)
				require.Equal(t, int64(2), preview.Before.Revision)
				got, err := issuer.CreateUserAPIToken(policyIssuanceSession(ctx), &accessmanager.CreateUserAPITokenRequest{ActorID: "selected", UserID: "selected"})
				require.ErrorIs(t, err, accessmanager.ErrPermanentAPITokenLimitReached)
				require.Nil(t, got)
			}
		})
	}
}
