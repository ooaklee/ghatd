package user_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ooaklee/ghatd/external/repository"
	helpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOAuthIdentityMongoIntegration(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for real MongoDB integration")
	}
	ctx := context.Background()
	dbName := "oauth_test_" + toolbox.GenerateUuidV4()
	handler, err := helpers.NewHandler(helpers.DefaultConfig(uri, dbName))
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close(ctx) })
	store := repository.NewMongoDbRepositoryWithDefaults(handler, dbName)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Drop(ctx) })
	repo := user.NewRepository(store)
	service := user.NewService(repo, nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	identity := user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "stable"}
	request := &user.CreateOAuthUserRequest{Identity: identity, Email: "same@example.test"}
	_, err = service.CreateOAuthUser(ctx, request)
	require.ErrorIs(t, err, user.ErrOAuthIndexesRequired)
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	var wins atomic.Int32
	var wg sync.WaitGroup
	ids := make(chan string, 24)
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := service.CreateOAuthUser(ctx, request)
			if err != nil {
				failures <- err
				return
			}
			if result.Created {
				wins.Add(1)
			}
			ids <- result.User.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Equal(t, int32(1), wins.Load())
	winner := ""
	for id := range ids {
		if winner == "" {
			winner = id
		}
		require.Equal(t, winner, id)
	}
	count, err := db.Collection("users").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	existing, err := service.GetUserByOAuthIdentity(ctx, &identity)
	require.NoError(t, err)
	require.Equal(t, "ACTIVE", existing.Status)
	require.True(t, existing.Verification.EmailVerified)
	require.Empty(t, existing.PersonalInfo.FirstName)
	require.NoError(t, existing.Validate())
	payload, err := json.Marshal(existing)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "oauth_identity")
	require.NotContains(t, string(payload), "stable")
	_, err = service.CreateUser(ctx, &user.CreateUserRequest{Email: "ordinary@example.test"})
	require.ErrorIs(t, err, user.ErrValidationFailed)
	retry, err := service.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: identity, Email: "changed@example.test"})
	require.NoError(t, err)
	require.False(t, retry.Created)
	require.Equal(t, "same@example.test", retry.User.Email)
	_, err = service.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: identity.Issuer, Subject: "different"}, Email: request.Email})
	require.ErrorIs(t, err, user.ErrOAuthLinkRequired)
	ordinary, err := service.CreateUser(ctx, &user.CreateUserRequest{Email: "member@example.test", FirstName: "Member", LastName: "Example", GenerateUUID: true, Extensions: map[string]interface{}{"membership": "keep"}, Status: "ACTIVE"})
	require.NoError(t, err)
	_, err = ordinary.User.UpdateStatus("ACTIVE")
	require.NoError(t, err)
	ordinary.User.VerifyEmail()
	_, err = service.UpdateUser(ctx, &user.UpdateUserRequest{User: ordinary.User})
	require.NoError(t, err)
	stale, err := service.GetUserByID(ctx, &user.GetUserByIDRequest{ID: ordinary.User.ID})
	require.NoError(t, err)
	apple := user.OAuthIdentity{Provider: "apple", Issuer: "https://appleid.apple.com", Subject: "apple-stable"}
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.LinkOAuthIdentity(ctx, ordinary.User.ID, &apple)
			require.NoError(t, err)
		}()
	}
	wg.Wait()
	linked, err := service.GetUserByOAuthIdentity(ctx, &apple)
	require.NoError(t, err)
	require.Len(t, linked.OAuthIdentities, 1)
	require.Equal(t, ordinary.User.ID, linked.ID)
	require.Equal(t, "Member", linked.PersonalInfo.FirstName)
	require.Equal(t, "keep", linked.Extensions["membership"])
	stale.User.PersonalInfo.LastName = "Updated"
	_, err = service.UpdateUser(ctx, &user.UpdateUserRequest{User: stale.User})
	require.NoError(t, err)
	linked, err = service.GetUserByOAuthIdentity(ctx, &apple)
	require.NoError(t, err)
	require.Len(t, linked.OAuthIdentities, 1)
	require.Equal(t, "Updated", linked.PersonalInfo.LastName)
	_, err = service.LinkOAuthIdentity(ctx, winner, &apple)
	require.ErrorIs(t, err, user.ErrOAuthIdentityConflict)
	for _, status := range []string{"PROVISIONED", "SUSPENDED", "DEACTIVATED", "LOCKED_OUT", "RECOVERY", "DELETED"} {
		t.Run(status, func(t *testing.T) {
			_, err := db.Collection("users").UpdateOne(ctx, bson.M{"_id": winner}, bson.M{"$set": bson.M{"status": status}})
			require.NoError(t, err)
			_, err = service.RecordOAuthLogin(ctx, winner, time.Now())
			require.ErrorIs(t, err, user.ErrOAuthRestricted)
			_, err = service.LinkOAuthIdentity(ctx, winner, &user.OAuthIdentity{Provider: "apple", Issuer: apple.Issuer, Subject: "restricted"})
			require.ErrorIs(t, err, user.ErrOAuthRestricted)
			var current user.UniversalUser
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": winner}).Decode(&current))
			require.Equal(t, status, current.Status)
		})
	}
	_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": winner}, bson.M{"$set": bson.M{"status": "ACTIVE", "verification.email_verified": false}})
	require.NoError(t, err)
	_, err = service.RecordOAuthLogin(ctx, winner, time.Now())
	require.ErrorIs(t, err, user.ErrOAuthRestricted)
	t.Run("migration refuses duplicates without PII", func(t *testing.T) {
		duplicateDB := db.Client().Database(dbName + "_duplicates")
		defer duplicateDB.Drop(ctx)
		_, err := duplicateDB.Collection("users").InsertMany(ctx, []interface{}{bson.M{"email": "private@example.test"}, bson.M{"email": "private@example.test"}})
		require.NoError(t, err)
		err = migrations.InitUsersOAuthIndexesUp(ctx, duplicateDB)
		require.Error(t, err)
		require.NotContains(t, fmt.Sprint(err), "private@example.test")
	})
}
