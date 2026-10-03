package user_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

// profileDocument includes unrelated security fields and extension data to
// detect any accidental full-snapshot rewrite by the profile capability.
func profileDocument() bson.M {
	return bson.M{"_id": "owner", "email": "owner@example.test", "type": "default", "status": "ACTIVE", "email_revision": int64(2), "version": 2, "roles": bson.A{}, "handle": "keep-handle", "oauth_identity_keys": bson.A{"keep-provider"}, "verification": bson.M{"email_verified": true}, "personal_info": bson.M{"first_name": "Old", "last_name": "Name", "full_name": "Old Name", "avatar": "keep-avatar", "phone": "keep-phone", "custom": "keep-extra"}, "metadata": bson.M{"created_at": "keep-created", "last_login_at": "keep-login"}, "extensions": bson.M{"keep": true}}
}

func profileCommand() *user.SetProfileNamesRequest {
	return &user.SetProfileNamesRequest{Account: user.UpdateProfileNamesRequest{UserID: "owner", ExpectedEmail: "owner@example.test", ExpectedRevision: 2, ExpectedType: "default", ExpectedStatus: "ACTIVE"}, Before: user.ProfileNames{FirstName: "Old", LastName: "Name", FullName: "Old Name"}, After: user.ProfileNames{FirstName: "New", LastName: "Name", FullName: "New Name"}, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
}

func TestProfileNamesMongoSnapshot(t *testing.T) {
	for _, name := range []string{"current", "unrelated changes", "null objects", "missing objects", "legacy type revision", "literal names", "stale first", "stale last", "stale full", "stale email", "stale revision", "stale type", "stale status", "absent account", "legacy full writer"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			original := profileDocument()
			r := profileCommand()
			want := error(nil)
			switch name {
			case "null objects":
				original["personal_info"], original["metadata"] = nil, nil
				r.Before = user.ProfileNames{}
			case "missing objects":
				delete(original, "personal_info")
				delete(original, "metadata")
				r.Before = user.ProfileNames{}
			case "legacy type revision":
				delete(original, "type")
				delete(original, "email_revision")
				r.Account.ExpectedType = ""
				r.Account.ExpectedRevision = 0
			case "literal names":
				r.After = user.ProfileNames{FirstName: "$email", LastName: "$type", FullName: "$status"}
			case "stale first":
				r.Before.FirstName = "stale"
				want = user.ErrProfileUpdateConflict
			case "stale last":
				r.Before.LastName = "stale"
				want = user.ErrProfileUpdateConflict
			case "stale full":
				r.Before.FullName = "stale"
				want = user.ErrProfileUpdateConflict
			case "stale email":
				r.Account.ExpectedEmail = "stale@example.test"
				want = user.ErrProfileUpdateConflict
			case "stale revision":
				r.Account.ExpectedRevision = 1
				want = user.ErrProfileUpdateConflict
			case "stale type":
				r.Account.ExpectedType = "other"
				want = user.ErrProfileUpdateConflict
			case "stale status":
				r.Account.ExpectedStatus = "SUSPENDED"
				want = user.ErrProfileUpdateConflict
			case "absent account":
				r.Account.UserID = "missing"
				want = user.ErrProfileUpdateConflict
			}
			_, err := db.Collection("users").InsertOne(ctx, original)
			require.NoError(t, err)
			if name == "unrelated changes" {
				_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": "owner"}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}, "personal_info.avatar": "new-avatar", "metadata.last_login_at": "new-login", "verification.phone_verified": true}})
				require.NoError(t, err)
			}
			if name == "legacy full writer" {
				current, err := repo.GetUserByID(ctx, "owner")
				require.NoError(t, err)
				current.PersonalInfo.FirstName = "Concurrent"
				_, err = repo.UpdateUser(ctx, current)
				require.NoError(t, err)
				want = user.ErrProfileUpdateConflict
			}
			var before, after bson.M
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&before))
			got, err := repo.SetProfileNames(ctx, r)
			require.Equal(t, want, err)
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&after))
			if want != nil {
				require.Nil(t, got)
				require.Equal(t, before, after)
				return
			}
			require.Equal(t, r.After.FirstName, got.PersonalInfo.FirstName)
			require.Equal(t, r.After.FullName, got.PersonalInfo.FullName)
			for _, field := range []string{"_id", "email", "email_revision", "status", "type", "roles", "version", "handle", "oauth_identity_keys", "verification", "extensions"} {
				require.Equal(t, before[field], after[field], field)
			}
			// BSON decodes embedded objects as bson.D; map them for precise leaf checks.
			var persisted user.UniversalUser
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&persisted))
			if name != "null objects" && name != "missing objects" {
				require.Equal(t, "keep-phone", persisted.PersonalInfo.Phone)
				require.Equal(t, "keep-created", persisted.Metadata.CreatedAt)
				avatar := "keep-avatar"
				if name == "unrelated changes" {
					avatar = "new-avatar"
					require.True(t, persisted.Verification.PhoneVerified)
				}
				require.Equal(t, avatar, persisted.PersonalInfo.Avatar)
			}
		})
	}
}

func TestProfileNamesMongoConcurrency(t *testing.T) {
	for _, writers := range []int{2, 6} {
		t.Run(fmt.Sprintf("writers=%d", writers), func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, profileDocument())
			require.NoError(t, err)
			errs := make([]error, writers)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range writers {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					r := profileCommand()
					r.After.FirstName = fmt.Sprintf("Name%d", i)
					r.After.FullName = r.After.FirstName + " Name"
					_, errs[i] = repo.SetProfileNames(ctx, r)
				}()
			}
			close(start)
			wg.Wait()
			passed := 0
			for _, err := range errs {
				if err == nil {
					passed++
				} else {
					require.Equal(t, user.ErrProfileUpdateConflict, err)
				}
			}
			require.Equal(t, 1, passed)
		})
	}
}

func TestProfileNamesMongoAcknowledgement(t *testing.T) {
	for _, ack := range []bool{true, false} {
		t.Run(fmt.Sprint(ack), func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, profileDocument())
			require.NoError(t, err)
			if !ack {
				store, err := repository.NewMongoDbRepositoryFromDatabase(db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged())), nil)
				require.NoError(t, err)
				repo = user.NewRepository(store)
			}
			got, err := repo.SetProfileNames(ctx, profileCommand())
			if ack {
				require.NoError(t, err)
				require.NotNil(t, got)
			} else {
				require.ErrorIs(t, err, repository.ErrUnacknowledgedMongoWrite)
				require.Nil(t, got)
			}
		})
	}
}

// HTTP composition uses actual manager/domain/Mongo code with fixture-published
// identity. Cryptographic verification belongs to separate middleware tests.
func TestProfileNamesMongoHTTP(t *testing.T) {
	for _, identity := range []string{"session", "api", "legacy"} {
		t.Run(identity, func(t *testing.T) {
			ctx := context.Background()
			db, _, domain := handleMongoFixture(t, false)
			doc := profileDocument()
			if identity == "legacy" {
				delete(doc, "type")
			}
			_, err := db.Collection("users").InsertOne(ctx, doc)
			require.NoError(t, err)
			ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "owner"), true)
			if identity == "api" {
				ctx = accesshelpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "owner", TokenID: "api"})
			} else {
				ctx = accesshelpers.TransitSessionWith(ctx, &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "session", UserType: "default", EmailRevision: 2})
			}
			h := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: &usermanager.Service{UserService: domain}, Validator: validator.NewValidator()})
			w := httptest.NewRecorder()
			h.UpdateUserProfile(w, httptest.NewRequest(http.MethodPatch, "/api/v1/ums/me", strings.NewReader(`{"first_name":"new","id":"foreign","email":"foreign@example.test","status":"SUSPENDED"}`)).WithContext(ctx))
			require.Equal(t, 200, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"first_name":"New"`)
			require.NotContains(t, w.Body.String(), "foreign")
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			var account user.UniversalUser
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&account))
			require.Equal(t, "New", account.PersonalInfo.FirstName)
			require.Equal(t, "owner@example.test", account.Email)
			require.Equal(t, "ACTIVE", account.Status)
			require.Equal(t, int64(2), account.EmailRevision)
		})
	}
}
