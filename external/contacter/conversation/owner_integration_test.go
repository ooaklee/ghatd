package commsconversation_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/contacter/conversation"
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
	"github.com/ooaklee/ghatd/external/waitlist"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// This stateful lifecycle deliberately crosses authentication, account switching,
// exact replay and live demotion against real signed sessions, Redis and MongoDB.
// Its ordered persistence assertions cannot be isolated into independent cases.
func TestActualSignedSessionOwnerBindingBeforeConversationPersistence(t *testing.T) {
	ctx := context.Background()
	uri, redisAddr := os.Getenv("GHATD_TEST_MONGO_URI"), os.Getenv("GHATD_TEST_REDIS_ADDR")
	if uri == "" || redisAddr == "" {
		t.Skip("set GHATD_TEST_MONGO_URI and GHATD_TEST_REDIS_ADDR for signed-session integration")
	}
	namespace := "conversation_owner_" + toolbox.GenerateUuidV4()
	handler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, namespace))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, handler.Close(ctx)) })
	store := repository.NewMongoDbRepositoryWithDefaults(handler, namespace)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Drop(ctx)) })
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	users := user.NewService(user.NewRepository(store), nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	runtime, err := ephemeral.NewRedisRuntime(ctx, &ephemeral.NewRedisRuntimeRequest{Options: &redis.Options{Addr: redisAddr}, Component: namespace, Environment: "test"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, runtime.Close(ctx)) })
	signing := auth.NewService(&auth.NewServiceRequest{AccessTokenSecret: "synthetic-owner-access", RefreshTokenSecret: "synthetic-owner-refresh"})
	authority := accessmanager.NewService(&accessmanager.NewServiceRequest{UserService: users, AuthService: signing, EphemeralStore: runtime.Store})
	newAdmin := func(email string) (*user.UniversalUser, *auth.TokenDetails) {
		created, err := users.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: oauth.GoogleSecureIssuer, Subject: email}, Email: email})
		require.NoError(t, err)
		_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": created.User.ID}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}}})
		require.NoError(t, err)
		tokens, err := signing.CreateTokenWithAuthenticationTime(ctx, created.User, time.Now())
		require.NoError(t, err)
		require.NoError(t, runtime.Store.CreateAuth(ctx, created.User.ID, tokens))
		t.Cleanup(func() { require.NoError(t, runtime.Store.DeleteAllTokenExceptedSpecified(ctx, created.User.ID, nil)) })
		return created.User, tokens
	}
	owner, ownerTokens := newAdmin("owner-a@example.test")
	another, anotherTokens := newAdmin("owner-b@example.test")
	contacts := waitlist.NewCommsService(contacter.NewRepository(store), waitlist.NewMongoStore(db))
	created, err := contacts.CreateComms(ctx, &contacter.CreateCommsRequest{FullName: "Synthetic sender", Email: "sender@example.test", Type: contacter.CommsTypeGeneralInquiry, Message: "Original"})
	require.NoError(t, err)
	require.NoError(t, contacter.EnsureCommsConversationIndexes(ctx, db))
	manager := (&usermanager.Service{UserService: users, ContacterService: contacts}).WithAdministratorAuthorizer(authority)
	h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: manager, Validator: validator.NewValidator()})
	suite, err := middleware.NewSuite(&middleware.NewSuiteRequest{Service: authority, EphemeralStore: runtime.Store, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh"})
	require.NoError(t, err)
	routes := router.NewRouter(nil, nil)
	var demoteNext, demoted atomic.Bool
	require.NoError(t, routes.SetRouteAuthorizer(func(ctx context.Context, _ *http.Request, def router.RouteDefinition) error {
		if def.Operation == "usermanager.AppendCommsConversationEntry" && demoteNext.Swap(false) {
			_, err := db.Collection("users").UpdateOne(ctx, bson.M{"_id": owner.ID}, bson.M{"$set": bson.M{"roles": bson.A{}}})
			if err != nil {
				return err
			}
			demoted.Store(true)
		}
		return nil
	}))
	group := routes.NewRouteGroup("/api/v1/ums", router.AdminSession, suite.AdminOnly)
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: "usermanager.GetCommsConversation", Methods: []string{http.MethodGet, http.MethodOptions}}, h.GetCommsConversation)
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: "usermanager.AppendCommsConversationEntry", Methods: []string{http.MethodPost, http.MethodOptions}}, h.AppendCommsConversationEntry)
	group.Handle(router.RouteDefinition{Path: "/me", Operation: "usermanager.GetUserProfile", Methods: []string{http.MethodGet}}, h.GetUserProfile)
	group.Handle(router.RouteDefinition{Path: "/comms/{id}", Operation: "usermanager.UpdateComms", Methods: []string{http.MethodPut, http.MethodOptions}}, h.UpdateComms)
	before := routes.RouteInventory()
	require.NoError(t, commsconversation.RequireRoutes(routes))
	require.True(t, reflect.DeepEqual(before, routes.RouteInventory()))
	require.NoError(t, routes.ValidateRoutePolicies())
	server := httptest.NewServer(routes.GetRouter())
	t.Cleanup(server.Close)
	path := "/api/v1/ums/comms/" + created.Comms.Id + "/conversation"
	call := func(method, url, body string, tokens *auth.TokenDetails, owners []string, api bool) (int, []byte, http.Header) {
		req, err := http.NewRequest(method, server.URL+url, strings.NewReader(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		for _, value := range owners {
			req.Header.Add(commsconversation.OwnerHeader, value)
		}
		if tokens != nil {
			req.AddCookie(&http.Cookie{Name: "access", Value: tokens.AccessToken})
			req.AddCookie(&http.Cookie{Name: "refresh", Value: tokens.RefreshToken})
		}
		if api {
			req.Header.Set("X-Api-Token", "synthetic-api-only")
		}
		response, err := server.Client().Do(req)
		require.NoError(t, err)
		raw, err := io.ReadAll(response.Body)
		closeErr := response.Body.Close()
		require.NoError(t, err)
		require.NoError(t, closeErr)
		for _, cookie := range response.Cookies() {
			require.Contains(t, []int{401, 403}, response.StatusCode, "only shared authority denials may clear cookies")
			require.Empty(t, cookie.Value, "denial must not issue a session")
			require.Negative(t, cookie.MaxAge, "preserve shared cookie cleanup")
		}
		return response.StatusCode, raw, response.Header
	}
	entries := func() int64 {
		count, err := db.Collection("comms_entries").CountDocuments(ctx, bson.M{"comms_id": created.Comms.Id})
		require.NoError(t, err)
		return count
	}
	command := `{"request_id":"10000000-0000-4000-8000-000000000041","kind":"internal_note","body":"Frozen note"}`
	status, raw, _ := call(http.MethodGet, "/api/v1/ums/me", "", ownerTokens, nil, false)
	require.Equal(t, 200, status)
	var profile struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &profile))
	require.Equal(t, owner.ID, profile.Data.ID)
	status, raw, headers := call(http.MethodPost, path, command, anotherTokens, []string{owner.ID}, false)
	require.Equal(t, 412, status, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	require.Contains(t, string(raw), commsconversation.OwnerChangedCode)
	require.Equal(t, int64(0), entries())
	status, raw, _ = call(http.MethodPut, strings.TrimSuffix(path, "/conversation"), `{"reached_out":true,"linked_comms_ids":[]}`, anotherTokens, []string{owner.ID}, false)
	require.Equal(t, 412, status, string(raw))
	status, raw, _ = call(http.MethodPut, strings.TrimSuffix(path, "/conversation"), `{"reached_out":true,"linked_comms_ids":[]}`, ownerTokens, []string{owner.ID}, false)
	require.Equal(t, 200, status, string(raw))
	t.Log("preflight owner A -> actual POST cookie B + expected A: 412 and zero records")
	for _, test := range []struct {
		name   string
		owners []string
		tokens *auth.TokenDetails
		api    bool
		status int
	}{
		{"missing", nil, ownerTokens, false, 428}, {"duplicate", []string{owner.ID, owner.ID}, ownerTokens, false, 400}, {"comma", []string{owner.ID + "," + another.ID}, ownerTokens, false, 400}, {"empty", []string{""}, ownerTokens, false, 400}, {"oversize", []string{strings.Repeat("x", 129)}, ownerTokens, false, 400}, {"anonymous", []string{owner.ID}, nil, false, 401}, {"API token only", []string{owner.ID}, nil, true, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			status, raw, _ := call(http.MethodPost, path, command, test.tokens, test.owners, test.api)
			require.Equal(t, test.status, status, string(raw))
			require.Equal(t, int64(0), entries())
		})
	}
	status, raw, _ = call(http.MethodPost, path, command, ownerTokens, []string{owner.ID}, false)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, int64(1), entries())
	require.Contains(t, string(raw), `"replayed":false`)
	status, raw, _ = call(http.MethodPost, path, command, ownerTokens, []string{owner.ID}, false)
	require.Equal(t, 200, status, string(raw))
	require.Equal(t, int64(1), entries())
	require.Contains(t, string(raw), `"replayed":true`)
	status, _, _ = call(http.MethodGet, path, "", ownerTokens, nil, false)
	require.Equal(t, 200, status)
	status, _, _ = call(http.MethodGet, path, "", nil, nil, false)
	require.Equal(t, 401, status)
	status, _, _ = call(http.MethodOptions, path, "", ownerTokens, nil, false)
	require.Equal(t, 200, status)
	status, raw, _ = call(http.MethodPost, path, command, anotherTokens, []string{owner.ID}, false)
	require.Equal(t, 412, status, string(raw))
	require.Equal(t, int64(1), entries())
	demoteNext.Store(true)
	newCommand := `{"request_id":"10000000-0000-4000-8000-000000000042","kind":"internal_note","body":"Must not persist"}`
	status, raw, _ = call(http.MethodPost, path, newCommand, ownerTokens, []string{owner.ID}, false)
	require.True(t, demoted.Load(), "demotion ran after middleware and owner guard, before shared manager")
	require.Equal(t, 401, status, string(raw))
	require.Contains(t, string(raw), "AM00-010")
	require.Equal(t, int64(1), entries())
	status, raw, _ = call(http.MethodPost, path, newCommand, ownerTokens, []string{owner.ID}, false)
	require.Equal(t, 401, status, string(raw))
	require.Contains(t, string(raw), "AM00-010")
	require.Equal(t, int64(1), entries())
	t.Log("matching owner new/replay, private GET/OPTIONS, anonymous/API-token and live-demoted denial passed; only one persisted entry")
}
