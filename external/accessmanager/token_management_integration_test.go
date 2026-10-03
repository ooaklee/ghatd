package accessmanager_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestTokenManagementLiveHTTP composes real routes, cookie cryptography, Redis
// session admission, manager authority and Mongo token operations. Each case
// owns a disposable account/database; no identity publisher bypasses admission.
func TestTokenManagementLiveHTTP(t *testing.T) {
	for _, operation := range []string{"create", "list", "threshold", "delete", "activate", "revoke"} {
		for _, identity := range []string{"owner", "anonymous", "api only", "other owner", "suspended", "revoked session", "stale revision"} {
			t.Run(operation+"/"+identity, func(t *testing.T) {
				f := newConnectionFixture(t)
				core, err := repository.NewMongoDbRepositoryFromDatabase(f.db, nil)
				require.NoError(t, err)
				repo := apitoken.NewRepository(core)
				require.NoError(t, repo.InitializeTokenInventory(f.ctx))
				api := apitoken.NewService(repo)
				f.service.ApitokenService = api
				target, credentials := f.account.ID, f.tokens
				want := http.StatusAccepted
				if operation == "create" {
					want = http.StatusCreated
				} else if operation == "list" || operation == "threshold" {
					want = http.StatusOK
				}
				if identity == "other owner" {
					other, _ := f.newAccount(t, "other@example.test", "other-subject")
					target, want = other.ID, http.StatusForbidden
				}
				var token apitoken.UserAPIToken
				if operation != "create" {
					created, err := api.CreateAPIToken(f.ctx, &apitoken.CreateAPITokenRequest{UserID: target, UserNanoId: "test-prefix"})
					require.NoError(t, err)
					token = created.APIToken
				}
				if operation == "activate" {
					require.NoError(t, api.RevokeAPIToken(f.ctx, &apitoken.RevokeAPITokenRequest{UserID: target, ID: token.ID}))
				}
				switch identity {
				case "anonymous", "api only":
					credentials = nil
					want = http.StatusUnauthorized
				case "suspended":
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"status": "SUSPENDED"}})
					require.NoError(t, err)
					want = http.StatusUnauthorized
				case "revoked session":
					_, err = f.ephemeral.DeleteAuth(f.ctx, f.account.ID+":"+f.tokens.AccessUUID)
					require.NoError(t, err)
					credentials.RefreshToken = ""
					want = http.StatusUnauthorized
				case "stale revision":
					_, err = f.db.Collection("users").UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$inc": bson.M{"email_revision": 1}})
					require.NoError(t, err)
					want = http.StatusUnauthorized
				}
				path, method := "/api/v1/ams/users/"+target+"/tokens", http.MethodGet
				switch operation {
				case "create":
					method = http.MethodPost
				case "threshold":
					path += "/thresholds"
				case "delete":
					path += "/" + token.ID
					method = http.MethodDelete
				case "activate", "revoke":
					path += "/" + token.ID + "/" + operation
					method = http.MethodPut
				}
				req := httptest.NewRequest(method, "https://app.example"+path+"?meta=true&UserID=forged", strings.NewReader(`{"ttl":0,"ActorID":"forged","UserID":"forged"}`))
				req.Header.Set("Content-Type", "application/json")
				if credentials != nil {
					req.AddCookie(&http.Cookie{Name: "access", Value: credentials.AccessToken})
					if credentials.RefreshToken != "" {
						req.AddCookie(&http.Cookie{Name: "refresh", Value: credentials.RefreshToken})
					}
				}
				if identity == "api only" {
					req.Header.Set("X-Api-Token", "test-prefix."+token.Value)
				}
				before, err := f.db.Collection(apitoken.ApiTokenCollection).CountDocuments(f.ctx, bson.M{})
				require.NoError(t, err)
				var beforeToken bson.M
				if token.ID != "" {
					require.NoError(t, f.db.Collection(apitoken.ApiTokenCollection).FindOne(f.ctx, bson.M{"_id": token.ID}).Decode(&beforeToken))
				}
				w := httptest.NewRecorder()
				f.router.ServeHTTP(w, req)
				require.Equal(t, want, w.Code, w.Body.String())
				after, err := f.db.Collection(apitoken.ApiTokenCollection).CountDocuments(f.ctx, bson.M{})
				require.NoError(t, err)
				if want >= 400 {
					require.Equal(t, before, after)
					require.NotContains(t, w.Body.String(), `"data"`)
					if token.ID != "" {
						var afterToken bson.M
						require.NoError(t, f.db.Collection(apitoken.ApiTokenCollection).FindOne(f.ctx, bson.M{"_id": token.ID}).Decode(&afterToken))
						require.Equal(t, beforeToken, afterToken, "denied request must not change the credential")
					}
					return
				}
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				switch operation {
				case "create":
					var body struct {
						Data apitoken.UserAPIToken `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					require.Equal(t, target, body.Data.CreatedByID)
					require.NotEmpty(t, body.Data.Value)
					require.Equal(t, "ACTIVE", body.Data.Status)
					require.Equal(t, before+1, after)
				case "list":
					var body struct {
						Data []apitoken.UserAPIToken `json:"data"`
					}
					require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
					require.Len(t, body.Data, 1)
					require.Equal(t, target, body.Data[0].CreatedByID)
					require.Empty(t, body.Data[0].Value)
					require.NotContains(t, w.Body.String(), token.Value)
				case "delete":
					require.Equal(t, before-1, after)
				case "activate", "revoke":
					var saved apitoken.UserAPIToken
					require.NoError(t, f.db.Collection(apitoken.ApiTokenCollection).FindOne(f.ctx, bson.M{"_id": token.ID}).Decode(&saved))
					expected := "ACTIVE"
					if operation == "revoke" {
						expected = "REVOKED"
					}
					require.Equal(t, expected, saved.Status)
				}
			})
		}
	}
}
