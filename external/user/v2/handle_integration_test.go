package user_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// handleMongoFixture owns a fresh database per case; it never changes a shared
// application's collections. Cleanup remains bounded even if a test cancels.
func handleMongoFixture(t *testing.T, generate bool) (*mongo.Database, *user.Repository, *user.Service) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for real MongoDB integration")
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database("handles_" + toolbox.GenerateUuidV4())
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	store, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
	require.NoError(t, err)
	repo := user.NewRepository(store)
	config := user.DefaultUserConfig()
	config.GenerateHandle = generate
	s := user.NewService(repo, nil, config, &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "").WithHandleGenerator(func() string { return "Calm Fox" })
	return db, repo, s
}

// createHandleAccount uses the ordinary creation path, not a handcrafted model.
func createHandleAccount(t *testing.T, s *user.Service, email string) *user.UniversalUser {
	t.Helper()
	res, err := s.CreateUser(context.Background(), &user.CreateUserRequest{Email: email, FirstName: "Test", LastName: "Member", GenerateUUID: true, Status: "ACTIVE"})
	require.NoError(t, err)
	// Ordinary creation deliberately applies the config's initial status. This
	// fixture activates the account through the domain before self-service tests.
	_, err = res.User.UpdateStatus("ACTIVE")
	require.NoError(t, err)
	_, err = s.UpdateUser(context.Background(), &user.UpdateUserRequest{User: res.User})
	require.NoError(t, err)
	return res.User
}

func TestHandleCreationMongo(t *testing.T) {
	for _, kind := range []string{"ordinary", "oauth"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				name            string
				generate, index bool
			}{
				{"legacy disabled", false, false}, {"enabled missing index", true, false}, {"enabled collisions", true, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := context.Background()
					db, _, s := handleMongoFixture(t, tc.generate)
					require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
					if tc.index {
						require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
						require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
					}
					var accounts []*user.UniversalUser
					for i := 0; i < 2; i++ {
						email := fmt.Sprintf("member%d@example.test", i)
						var account *user.UniversalUser
						var err error
						if kind == "ordinary" {
							var result *user.CreateUserResponse
							result, err = s.CreateUser(ctx, &user.CreateUserRequest{Email: email, FirstName: "Test", LastName: "Member", GenerateUUID: true, Status: "ACTIVE"})
							if result != nil {
								account = result.User
							}
						} else {
							var result *user.CreateOAuthUserResponse
							result, err = s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: fmt.Sprint(i)}, Email: email})
							if result != nil {
								account = result.User
								require.True(t, result.Created)
							}
						}
						if tc.generate && !tc.index {
							require.ErrorIs(t, err, user.ErrHandleIndexesRequired)
							continue
						}
						require.NoError(t, err)
						require.NotNil(t, account)
						accounts = append(accounts, account)
						if tc.generate {
							want := "calm-fox"
							if i == 1 {
								want += "-1"
							}
							require.Equal(t, want, account.Handle)
							require.EqualValues(t, 1, account.HandleMetadata.Revision)
							require.Zero(t, account.HandleMetadata.ChangeCount)
							require.Empty(t, account.HandleMetadata.LastUserChangeAt)
						} else {
							require.Empty(t, account.Handle)
							require.Nil(t, account.HandleMetadata)
						}
					}
					count, err := db.Collection("users").CountDocuments(ctx, bson.M{})
					require.NoError(t, err)
					require.EqualValues(t, len(accounts), count)
					if kind == "oauth" && len(accounts) > 0 {
						retry, err := s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "0"}, Email: "member0@example.test"})
						require.NoError(t, err)
						require.False(t, retry.Created)
						require.Equal(t, accounts[0].ID, retry.User.ID)
						require.Equal(t, accounts[0].Handle, retry.User.Handle)
						_, err = s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "another"}, Email: "member0@example.test"})
						require.ErrorIs(t, err, user.ErrOAuthLinkRequired)
					}
				})
			}
		})
	}
}

func TestHandleLifecycleMongo(t *testing.T) {
	for _, generate := range []bool{false, true} {
		t.Run(fmt.Sprintf("generated=%t", generate), func(t *testing.T) {
			ctx := context.Background()
			db, repo, s := handleMongoFixture(t, generate)
			require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
			account := createHandleAccount(t, s, "owner@example.test")
			before, err := s.GetUserHandle(ctx, account.ID)
			require.NoError(t, err)
			revision := before.Metadata.Revision
			first, err := s.UpdateUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: account.ID, Handle: " @New_Name ", ExpectedRevision: revision})
			require.NoError(t, err)
			require.Equal(t, "new_name", first.Handle)
			require.Equal(t, revision+1, first.Metadata.Revision)
			require.EqualValues(t, 1, first.Metadata.ChangeCount)
			require.NotEmpty(t, first.Metadata.CreatedAt)
			require.Equal(t, first.Metadata.UpdatedAt, first.Metadata.LastUserChangeAt)
			if generate {
				require.Equal(t, before.Metadata.CreatedAt, first.Metadata.CreatedAt)
			}
			noop, err := s.UpdateUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: account.ID, Handle: "new_name", ExpectedRevision: first.Metadata.Revision})
			require.NoError(t, err)
			require.Equal(t, first, noop)
			_, err = s.UpdateUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: account.ID, Handle: "other-name", ExpectedRevision: revision})
			require.ErrorIs(t, err, user.ErrHandleConflict)
			// A full-profile snapshot from before the handle update must not restore it.
			account.PersonalInfo.FirstName = "Changed"
			_, err = s.UpdateUser(ctx, &user.UpdateUserRequest{User: account})
			require.NoError(t, err)
			current, err := s.GetUserHandle(ctx, account.ID)
			require.NoError(t, err)
			require.Equal(t, first, current)
			renamed, err := s.UpdateUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: account.ID, Handle: "next-name", ExpectedRevision: first.Metadata.Revision})
			require.NoError(t, err)
			require.EqualValues(t, 2, renamed.Metadata.ChangeCount)
			available, err := repo.HandleAvailable(ctx, "new_name", "")
			require.NoError(t, err)
			require.True(t, available, "old names are released, not aliases")
			available, err = repo.HandleAvailable(ctx, "next-name", account.ID)
			require.NoError(t, err)
			require.True(t, available, "current owner excluded")
			available, err = repo.HandleAvailable(ctx, "next-name", "")
			require.NoError(t, err)
			require.False(t, available)
			_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": account.ID}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
			require.NoError(t, err)
			_, err = repo.SetUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: account.ID, Handle: "blocked-name", ExpectedRevision: renamed.Metadata.Revision}, time.Now())
			require.ErrorIs(t, err, user.ErrHandleConflict)
		})
	}
}

func TestHandleConcurrentWritesMongo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		owners int
		want   error
	}{
		{"same revision", 1, user.ErrHandleConflict}, {"same candidate", 2, user.ErrHandleTaken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, s := handleMongoFixture(t, false)
			require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
			first := createHandleAccount(t, s, "first@example.test")
			ids := []string{first.ID, first.ID}
			if tc.owners == 2 {
				ids[1] = createHandleAccount(t, s, "second@example.test").ID
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i, id := range ids {
				wg.Add(1)
				go func(i int, id string) {
					defer wg.Done()
					<-start
					name := "shared-name"
					if tc.owners == 1 {
						name = fmt.Sprintf("candidate-%d", i)
					}
					_, err := repo.SetUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: id, Handle: name, ExpectedRevision: 0}, time.Now())
					results <- err
				}(i, id)
			}
			close(start)
			wg.Wait()
			close(results)
			success, denied := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else {
					require.Equal(t, tc.want, err)
					denied++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, denied)
			count, err := db.Collection("users").CountDocuments(ctx, bson.M{"handle_metadata.revision": int64(1)})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestHandleIndexReadinessMongo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{
		{"missing", false}, {"correct", true}, {"not unique", false}, {"wrong partial", false}, {"case insensitive", false}, {"duplicate data", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			if tc.name == "correct" {
				require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
			} else if tc.name == "duplicate data" {
				_, err := db.Collection("users").InsertMany(ctx, []any{bson.M{"handle": "same-name"}, bson.M{"handle": "same-name"}})
				require.NoError(t, err)
				require.Error(t, migrations.InitUsersHandleIndexesUp(ctx, db))
			} else if tc.name != "missing" {
				opts := options.Index().SetName(user.UserHandleIndexName).SetUnique(tc.name != "not unique").SetPartialFilterExpression(bson.M{"handle": bson.M{"$gt": ""}})
				if tc.name == "wrong partial" {
					opts.SetPartialFilterExpression(bson.M{"handle": bson.M{"$exists": true}})
				}
				if tc.name == "case insensitive" {
					opts.SetCollation(&options.Collation{Locale: "en", Strength: 2})
				}
				_, err := db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "handle", Value: 1}}, Options: opts})
				require.NoError(t, err)
			}
			err := repo.RequireHandleStorage(ctx)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, user.ErrHandleIndexesRequired)
			}
		})
	}
}
