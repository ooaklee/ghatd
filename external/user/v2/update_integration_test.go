package user_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// Generic updates intentionally retain broad $set semantics. These tables prove
// acknowledged post-images/protected-field preservation, not general field CAS.
func TestGenericUserUpdateMongoPostimage(t *testing.T) {
	for _, name := range []string{"current", "empty stored email", "empty submitted email", "missing stored email", "legacy revision", "stale revision", "stale email", "absent", "binary email", "marshal failure", "omitted optional field"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			doc := profileDocument()
			if name == "empty stored email" {
				doc["email"] = ""
			}
			if name == "missing stored email" {
				delete(doc, "email")
			}
			if name == "legacy revision" {
				delete(doc, "email_revision")
			}
			if name == "binary email" {
				require.NoError(t, db.CreateCollection(ctx, "users", options.CreateCollection().SetCollation(&options.Collation{Locale: "en", Strength: 2})))
			}
			_, err := db.Collection("users").InsertOne(ctx, doc)
			require.NoError(t, err)
			v, err := repo.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			v.PersonalInfo.FirstName = "New"
			// All these submitted values are ignored by this legacy repository.
			v.Handle = "forged-handle"
			v.OAuthIdentityKeys = []string{"forged-provider"}
			v.HadOAuthIdentity = true
			want := error(nil)
			switch name {
			case "missing stored email":
				want = user.ErrOAuthConnectionConflict
			case "empty submitted email":
				v.Email = ""
				want = user.ErrOAuthConnectionConflict
			case "stale revision":
				v.EmailRevision--
				want = user.ErrOAuthConnectionConflict
			case "stale email":
				v.Email = "other@example.test"
				want = user.ErrOAuthConnectionConflict
			case "absent":
				v.ID = "missing"
				want = user.ErrOAuthConnectionConflict
			case "binary email":
				v.Email = "OWNER@example.test"
				want = user.ErrOAuthConnectionConflict
			case "marshal failure":
				v.Extensions = map[string]any{"invalid": make(chan int)}
			case "omitted optional field":
				v.PersonalInfo = nil
			}
			got, err := repo.UpdateUser(ctx, v)
			if name == "marshal failure" {
				require.Error(t, err)
				require.NotEqual(t, user.ErrOAuthConnectionConflict, err)
				require.Nil(t, got)
				return
			}
			require.Equal(t, want, err)
			stored, readErr := repo.GetUserByID(ctx, "owner")
			require.NoError(t, readErr)
			if err != nil {
				require.Nil(t, got)
				require.Equal(t, "Old", stored.PersonalInfo.FirstName)
				return
			}
			require.NotSame(t, v, got)
			require.Equal(t, stored, got)
			require.Equal(t, "keep-handle", got.Handle)
			require.Equal(t, []string{"keep-provider"}, got.OAuthIdentityKeys)
			require.False(t, got.HadOAuthIdentity)
			if name == "omitted optional field" {
				require.Equal(t, "Old", got.PersonalInfo.FirstName)
			} else {
				require.Equal(t, "New", got.PersonalInfo.FirstName)
			}
			require.Equal(t, "forged-handle", v.Handle, "repository mutated the supplied snapshot")
		})
	}
}

func TestGenericUserUpdateMongoAcknowledgement(t *testing.T) {
	for _, ack := range []bool{true, false} {
		t.Run(fmt.Sprint(ack), func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, profileDocument())
			require.NoError(t, err)
			v, err := repo.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			if !ack {
				store, err := repository.NewMongoDbRepositoryFromDatabase(db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged())), nil)
				require.NoError(t, err)
				repo = user.NewRepository(store)
			}
			v.PersonalInfo.FirstName = "New"
			got, err := repo.UpdateUser(ctx, v)
			if ack {
				require.NoError(t, err)
				require.Equal(t, "New", got.PersonalInfo.FirstName)
			} else {
				require.Equal(t, repository.ErrUnacknowledgedMongoWrite, err)
				require.Nil(t, got)
			}
		})
	}
}

func TestGenericUserUpdateMongoHTTP(t *testing.T) {
	for _, name := range []string{"path selects target", "replacement preserves protected data", "stale replacement"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, s := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, profileDocument())
			require.NoError(t, err)
			if name == "path selects target" {
				h := user.NewHandler(s, validator.NewValidator())
				r := httptest.NewRequest(http.MethodPatch, "/owner", strings.NewReader(`{"id":"nonexistent","first_name":"new"}`))
				r = mux.SetURLVars(r, map[string]string{user.UserURIVariableID: "owner"})
				w := httptest.NewRecorder()
				h.UpdateUser(w, r)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Contains(t, w.Body.String(), "keep-handle")
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			} else {
				v, err := repo.GetUserByID(ctx, "owner")
				require.NoError(t, err)
				v.Handle = "forged"
				v.OAuthIdentityKeys = nil
				v.PersonalInfo.FirstName = "new"
				if name == "stale replacement" {
					_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": "owner"}, bson.M{"$inc": bson.M{"email_revision": 1}})
					require.NoError(t, err)
				}
				res, err := s.UpdateUser(ctx, &user.UpdateUserRequest{User: v})
				if name == "stale replacement" {
					require.Equal(t, user.ErrOAuthConnectionConflict, err)
					require.Nil(t, res)
					return
				}
				require.NoError(t, err)
				require.Equal(t, "keep-handle", res.User.Handle)
				require.Equal(t, []string{"keep-provider"}, res.User.OAuthIdentityKeys)
				require.Equal(t, "forged", v.Handle)
				require.Equal(t, "new", v.PersonalInfo.FirstName)
			}
			stored, err := repo.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			require.Equal(t, "New", stored.PersonalInfo.FirstName)
		})
	}
}
