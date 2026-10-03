package usermanager_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// TestHandleHTTPMongoJourney composes the production context publisher, handler,
// manager, lower domain and shared Mongo adapter. Credential verification itself
// remains covered by the access-manager suite, not simulated by a bearer string.
func TestHandleHTTPMongoJourney(t *testing.T) {
	for _, generated := range []bool{false, true} {
		t.Run(fmt.Sprintf("generated=%t", generated), func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI for real MongoDB integration")
			}
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database("handle_http_" + toolbox.GenerateUuidV4())
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Drop(ctx))
				require.NoError(t, client.Disconnect(ctx))
			})
			ctx := context.Background()
			require.NoError(t, migrations.InitUsersHandleIndexesUp(ctx, db))
			store, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
			require.NoError(t, err)
			cfg := user.DefaultUserConfig()
			cfg.GenerateHandle = generated
			domain := user.NewService(user.NewRepository(store), nil, cfg, &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
			created, err := domain.CreateUser(ctx, &user.CreateUserRequest{Email: "member@example.test", FirstName: "Test", LastName: "Member", GenerateUUID: true})
			require.NoError(t, err)
			_, err = created.User.UpdateStatus("ACTIVE")
			require.NoError(t, err)
			_, err = domain.UpdateUser(ctx, &user.UpdateUserRequest{User: created.User})
			require.NoError(t, err)
			ctx, err = middleware.ContextWithAuthentication(ctx, &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: created.User.ID, User: created.User, Token: &auth.TokenAccessDetails{UserID: created.User.ID, UserType: created.User.Type, TokenUse: auth.TokenUseAccess, AccessUUID: "verified-session"}})
			require.NoError(t, err)
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: &usermanager.Service{UserService: domain}})
			request := func(method, body, tag string) *http.Request {
				r := httptest.NewRequest(method, "/api/v1/ums/me/handle?user_id=other", strings.NewReader(body)).WithContext(ctx)
				r.Header.Set("Content-Type", "application/json")
				if tag != "" {
					r.Header.Set("If-Match", tag)
				}
				return r
			}
			read := httptest.NewRecorder()
			h.GetMyHandle(read, request(http.MethodGet, "", ""))
			require.Equal(t, 200, read.Code, read.Body.String())
			initialTag := read.Header().Get("ETag")
			if generated {
				require.Equal(t, `"1"`, initialTag)
			} else {
				require.Equal(t, `"0"`, initialTag)
			}
			write := httptest.NewRecorder()
			h.UpdateMyHandle(write, request(http.MethodPatch, `{"handle":"@Chosen_Name"}`, initialTag))
			require.Equal(t, 200, write.Code, write.Body.String())
			var envelope struct {
				Data user.UserHandle `json:"data"`
			}
			require.NoError(t, json.Unmarshal(write.Body.Bytes(), &envelope))
			require.Equal(t, "chosen_name", envelope.Data.Handle)
			stored, err := domain.GetUserHandle(context.Background(), created.User.ID)
			require.NoError(t, err)
			require.Equal(t, *stored, envelope.Data)
			stale := httptest.NewRecorder()
			h.UpdateMyHandle(stale, request(http.MethodPatch, `{"handle":"next-name"}`, initialTag))
			require.Equal(t, 412, stale.Code, stale.Body.String())
			require.Contains(t, stale.Body.String(), "USV2-029")
			for _, invalid := range []string{`{"handle":"bad space"}`, `{"handle":"next-name","ActorID":"other"}`} {
				rec := httptest.NewRecorder()
				h.UpdateMyHandle(rec, request(http.MethodPatch, invalid, write.Header().Get("ETag")))
				require.Equal(t, 400, rec.Code, rec.Body.String())
			}
			final, err := domain.GetUserHandle(context.Background(), created.User.ID)
			require.NoError(t, err)
			require.Equal(t, stored, final)
		})
	}
}
