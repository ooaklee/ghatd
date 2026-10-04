package usermanager_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/ghatd/external/voter"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Crosses the real shared signed-cookie middleware, live account authority and
// native conversation handler. Votes never replace or modify contact history.
// Stateful exception: later remove/concurrency/demotion phases assert the same
// history; independent input validation remains table-driven within the lifecycle.
func TestPrivateConversationVotingWithActualSessionAndPersistence(t *testing.T) {
	uri, redisAddr := os.Getenv("GHATD_TEST_MONGO_URI"), os.Getenv("GHATD_TEST_REDIS_ADDR")
	if uri == "" || redisAddr == "" {
		t.Skip("set GHATD_TEST_MONGO_URI and GHATD_TEST_REDIS_ADDR for signed-session integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	namespace := "conversation_votes_" + toolbox.GenerateUuidV4()
	handler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, namespace))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handler.Close(context.Background())) })
	store := repository.NewMongoDbRepositoryWithDefaults(handler, namespace)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Drop(context.Background())) })
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	users := user.NewService(user.NewRepository(store), nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	runtime, err := ephemeral.NewRedisRuntime(ctx, &ephemeral.NewRedisRuntimeRequest{Options: &redis.Options{Addr: redisAddr}, Component: namespace, Environment: "test"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close(context.Background())) })
	signing := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "synthetic-votes-access", RefreshTokenSecret: "synthetic-votes-refresh"})
	authority := accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: users, AuthService: signing, EphemeralStore: runtime.Store})
	newAdmin := func(email, name string) (*user.UniversalUser, *auth.TokenDetails) {
		created, err := users.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: email}, Email: email})
		require.NoError(t, err)
		_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": created.User.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}, "personal_info.full_name": name}})
		require.NoError(t, err)
		tokens, err := signing.CreateTokenWithAuthenticationTime(ctx, created.User, time.Now())
		require.NoError(t, err)
		require.NoError(t, runtime.Store.CreateAuth(ctx, created.User.ID, tokens))
		t.Cleanup(func() {
			require.NoError(t, runtime.Store.DeleteAllTokenExceptedSpecified(context.Background(), created.User.ID, nil))
		})
		return created.User, tokens
	}
	owner, ownerTokens := newAdmin("voter-a@example.test", "First Voter")
	another, anotherTokens := newAdmin("voter-b@example.test", "Second Voter")
	contacts := contacter.NewRepository(store)
	service := contacter.NewService(contacts)
	created, err := service.CreateComms(ctx, &contacter.CreateCommsRequest{FullName: "Synthetic sender", Email: "sender@example.test", Type: contacter.CommsTypeGeneralInquiry, Message: "Original"})
	require.NoError(t, err)
	other, err := service.CreateComms(ctx, &contacter.CreateCommsRequest{FullName: "Other sender", Email: "other@example.test", Type: contacter.CommsTypeGeneralInquiry, Message: "Other"})
	require.NoError(t, err)
	require.NoError(t, contacter.EnsureCommsConversationIndexes(ctx, db))
	require.NoError(t, voter.EnsureIndexes(ctx, db))
	manager := (&usermanager.Service{UserService: users, ContacterService: service}).WithAdministratorAuthorizer(authority).WithCommsVotingService(service.WithVoterService(voter.NewService(voter.NewRepository(store))))
	native := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: manager, Validator: validator.NewValidator()})
	suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{Service: authority, EphemeralStore: runtime.Store, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
	require.NoError(t, err)
	routes := router.NewRouter(nil, nil)
	var demoteNext atomic.Bool
	require.NoError(t, routes.SetRouteAuthorizer(func(ctx context.Context, _ *http.Request, def router.RouteDefinition) error {
		if (def.Operation == "commsconversation.SetVote" || def.Operation == "commsconversation.RemoveVote") && demoteNext.Swap(false) {
			_, err := db.Collection("users").UpdateOne(ctx, bson.M{"_id": owner.ID}, bson.M{"$set": bson.M{"roles": bson.A{}}})
			return err
		}
		return nil
	}))
	usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
		Router: routes, Handler: native, EnableCommsVoting: true,
		AdminOnlyMiddleware:                suite.AdminOnly,
		ActiveValidApiTokenOrJWTMiddleware: suite.ActiveValidApiTokenOrJWT,
		ValidApiTokenOrJWTMiddleware:       suite.ActiveValidApiTokenOrAuthenticated,
		RateLimitOrActiveMiddleware:        suite.RateLimitOrActive,
	})
	require.NoError(t, usermanager.RequireCommsConversationRoutes(routes))
	require.NoError(t, routes.ValidateRoutePolicies())
	for _, def := range routes.RouteInventory() {
		if strings.HasPrefix(def.Operation, "commsconversation.") {
			require.Equal(t, router.AdminSession, def.Access)
		}
	}
	server := httptest.NewServer(routes.GetRouter())
	t.Cleanup(server.Close)
	call := func(method, path, body string, tokens *auth.TokenDetails, expected string) (int, []byte, http.Header, error) {
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			return 0, nil, nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		if expected != "" {
			req.Header.Set(usermanager.CommsOwnerHeader, expected)
		}
		if tokens != nil {
			req.AddCookie(&http.Cookie{Name: "access", Value: tokens.AccessToken})
			req.AddCookie(&http.Cookie{Name: "refresh", Value: tokens.RefreshToken})
		} else {
			req.Header.Set("X-Api-Token", "synthetic-api-only")
		}
		response, err := server.Client().Do(req)
		if err != nil {
			return 0, nil, nil, err
		}
		raw, err := io.ReadAll(response.Body)
		if closeErr := response.Body.Close(); err == nil {
			err = closeErr
		}
		return response.StatusCode, raw, response.Header, err
	}
	root := "/api/v1/ums/comms/" + created.Comms.Id
	status, raw, _, err := call(http.MethodPost, root+"/conversation", `{"request_id":"20000000-0000-4000-8000-000000000001","kind":"internal_note","body":"Immutable context"}`, ownerTokens, owner.ID)
	require.NoError(t, err)
	require.Equal(t, 200, status, string(raw))
	var receipt struct {
		Data struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &receipt))
	require.Len(t, receipt.Data.Entry.ID, 64)
	entryID := receipt.Data.Entry.ID
	entryPath := root + "/conversation/" + entryID + "/vote"
	readPath := root + "/conversation/votes?entry_id=" + entryID
	check := func(method, path, body string, tokens *auth.TokenDetails, expected string, want int) usermanager.CommsVotePage {
		t.Helper()
		status, raw, headers, err := call(method, path, body, tokens, expected)
		require.NoError(t, err)
		require.Equal(t, want, status, string(raw))
		if want == 200 {
			require.Equal(t, "no-store", headers.Get("Cache-Control"))
			var response struct {
				Data usermanager.CommsVotePage `json:"data"`
			}
			require.NoError(t, json.Unmarshal(raw, &response))
			return response.Data
		}
		return usermanager.CommsVotePage{}
	}
	t.Run("authoritative zero counts and bounded participants", func(t *testing.T) {
		page := check(http.MethodGet, readPath, "", ownerTokens, "", 200)
		require.Equal(t, owner.ID, page.ViewerActorID)
		require.Len(t, page.ByEntry, 2)
		require.Nil(t, page.ByEntry[entryID].ViewerVote)
		require.Zero(t, page.ByEntry[entryID].Up)
		require.Len(t, page.Participants, 1)
		require.Equal(t, owner.ID, page.Participants[0].ID)
		require.NotContains(t, fmt.Sprint(page), "voter-a@example.test")
	})
	t.Run("set switch repeat and remove for entry and original", func(t *testing.T) {
		for _, target := range []struct{ path, id string }{{entryPath, entryID}, {root + "/vote", ""}} {
			up := check(http.MethodPost, target.path, `{"vote":1}`, ownerTokens, owner.ID, 200).ByEntry[target.id]
			require.Equal(t, 1, up.Up)
			require.Equal(t, 1, *up.ViewerVote)
			repeat := check(http.MethodPost, target.path, `{"vote":1}`, ownerTokens, owner.ID, 200).ByEntry[target.id]
			require.Equal(t, 1, repeat.Up)
			down := check(http.MethodPost, target.path, `{"vote":0}`, ownerTokens, owner.ID, 200).ByEntry[target.id]
			require.Zero(t, down.Up)
			require.Equal(t, 1, down.Down)
			require.Equal(t, 0, *down.ViewerVote)
			removed := check(http.MethodDelete, target.path, "", ownerTokens, owner.ID, 200).ByEntry[target.id]
			require.Nil(t, removed.ViewerVote)
			require.Zero(t, removed.Up)
			require.Zero(t, removed.Down)
			check(http.MethodDelete, target.path, "", ownerTokens, owner.ID, 200)
		}
	})
	t.Run("strict input owner binding and contact membership", func(t *testing.T) {
		for _, test := range []struct {
			name, method, path, body, expected string
			tokens                             *auth.TokenDetails
			status                             int
		}{
			{"anonymous API token read", http.MethodGet, readPath, "", "", nil, 401},
			{"anonymous API token write", http.MethodPost, entryPath, `{"vote":1}`, owner.ID, nil, 401},
			{"missing owner", http.MethodPost, entryPath, `{"vote":1}`, "", ownerTokens, 428},
			{"wrong owner set", http.MethodPost, entryPath, `{"vote":1}`, owner.ID, anotherTokens, 412},
			{"wrong owner remove", http.MethodDelete, entryPath, "", owner.ID, anotherTokens, 412},
			{"forged actor", http.MethodPost, entryPath, `{"vote":1,"actor_id":"forged"}`, owner.ID, ownerTokens, 400},
			{"missing vote", http.MethodPost, entryPath, `{}`, owner.ID, ownerTokens, 400},
			{"invalid vote", http.MethodPost, entryPath, `{"vote":2}`, owner.ID, ownerTokens, 400},
			{"trailing JSON", http.MethodPost, entryPath, `{"vote":1}{}`, owner.ID, ownerTokens, 400},
			{"invalid entry", http.MethodPost, root + "/conversation/bad/vote", `{"vote":1}`, owner.ID, ownerTokens, 400},
			{"cross contact entry", http.MethodPost, "/api/v1/ums/comms/" + other.Comms.Id + "/conversation/" + entryID + "/vote", `{"vote":1}`, owner.ID, ownerTokens, 404},
			{"duplicate batch", http.MethodGet, readPath + "&entry_id=" + entryID, "", "", ownerTokens, 400},
			{"oversized batch", http.MethodGet, root + "/conversation/votes?" + strings.Repeat("entry_id="+entryID+"&", 101), "", "", ownerTokens, 400},
		} {
			t.Run(test.name, func(t *testing.T) { check(test.method, test.path, test.body, test.tokens, test.expected, test.status) })
		}
		count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{})
		require.NoError(t, err)
		require.Zero(t, count)
	})
	t.Run("full bounded page uses native entry identities and actual participants", func(t *testing.T) {
		ids := []string{entryID}
		docs := []any{}
		for index := 1; index < 100; index++ {
			id := fmt.Sprintf("%064x", index)
			ids = append(ids, id)
			docs = append(docs, bson.M{"_id": id, "comms_id": created.Comms.Id, "actor_id": another.ID, "body": "Synthetic bounded page"})
		}
		_, err := db.Collection(contacter.CommsEntriesCollection).InsertMany(ctx, docs)
		require.NoError(t, err)
		page := check(http.MethodGet, root+"/conversation/votes?entry_id="+strings.Join(ids, "&entry_id="), "", ownerTokens, "", 200)
		require.Len(t, page.ByEntry, 101)
		require.Len(t, page.Participants, 2)
		for _, id := range ids {
			require.Zero(t, page.ByEntry[id].Up)
			require.Nil(t, page.ByEntry[id].ViewerVote)
		}
		_, err = db.Collection(contacter.CommsEntriesCollection).DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids[1:]}})
		require.NoError(t, err)
	})
	t.Run("concurrent same actor stays unique and distinct actors stay independent", func(t *testing.T) {
		var wg sync.WaitGroup
		failures := make(chan string, 32)
		for index := 0; index < 32; index++ {
			tokens, expected := ownerTokens, owner.ID
			if index%2 == 1 {
				tokens, expected = anotherTokens, another.ID
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, raw, _, err := call(http.MethodPost, entryPath, `{"vote":1}`, tokens, expected)
				if err != nil || status != 200 {
					failures <- fmt.Sprintf("status=%d error=%v body=%s", status, err, raw)
				}
			}()
		}
		wg.Wait()
		close(failures)
		for failure := range failures {
			t.Error(failure)
		}
		count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{"domain": "contacter", "resource_id": created.Comms.Id, "child_id": entryID})
		require.NoError(t, err)
		require.Equal(t, int64(2), count)
		page := check(http.MethodGet, readPath, "", ownerTokens, "", 200)
		require.Equal(t, 2, page.ByEntry[entryID].Up)
		require.Equal(t, 1, *page.ByEntry[entryID].ViewerVote)
		remaining := check(http.MethodDelete, entryPath, "", ownerTokens, owner.ID, 200).ByEntry[entryID]
		require.Equal(t, 1, remaining.Up)
		require.Nil(t, remaining.ViewerVote)
		otherView := check(http.MethodGet, readPath, "", anotherTokens, "", 200)
		require.Equal(t, 1, *otherView.ByEntry[entryID].ViewerVote)
	})
	t.Run("missing stored vote stays unknown instead of becoming downvote", func(t *testing.T) {
		_, err := db.Collection(voter.Collection).InsertOne(ctx, bson.M{"_id": "malformed", "scope": "", "domain": "contacter", "resource_id": created.Comms.Id, "child_id": "", "actor_id": "malformed-actor"})
		require.NoError(t, err)
		check(http.MethodGet, readPath, "", ownerTokens, "", 503)
		_, err = db.Collection(voter.Collection).DeleteOne(ctx, bson.M{"_id": "malformed"})
		require.NoError(t, err)
	})
	t.Run("live demotion between admission and handler cannot vote", func(t *testing.T) {
		for _, method := range []string{http.MethodPost, http.MethodDelete} {
			_, err := db.Collection("users").UpdateOne(ctx, bson.M{"_id": owner.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
			require.NoError(t, err)
			demoteNext.Store(true)
			check(method, entryPath, `{"vote":1}`, ownerTokens, owner.ID, 401)
			require.False(t, demoteNext.Load())
			count, err := db.Collection(voter.Collection).CountDocuments(ctx, bson.M{"actor_id": owner.ID})
			require.NoError(t, err)
			require.Zero(t, count)
		}
		var original bson.M
		require.NoError(t, db.Collection("comms").FindOne(ctx, bson.M{"_id": created.Comms.Id}).Decode(&original))
		require.Equal(t, "Original", original["message"])
		count, err := db.Collection(contacter.CommsEntriesCollection).CountDocuments(ctx, bson.M{})
		require.NoError(t, err)
		require.Equal(t, int64(1), count)
	})
}
