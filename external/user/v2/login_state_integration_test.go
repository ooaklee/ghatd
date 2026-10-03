package user_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

func loginCommand(activate bool) *user.SetLoginStateRequest {
	status := user.AccountStatusKeyActive
	if activate {
		status = user.AccountStatusKeyProvisioned
	}
	return &user.SetLoginStateRequest{Account: user.AccountSnapshot{UserID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: status}, At: time.Now().UTC().Format(time.RFC3339Nano)}
}
func loginWrite(ctx context.Context, r user.LoginStateRepository, c *user.SetLoginStateRequest, activate bool) (*user.UniversalUser, error) {
	if activate {
		return r.SetVerifiedEmailActivation(ctx, c)
	}
	return r.SetFreshLogin(ctx, c)
}

func TestLoginStateMongoSnapshot(t *testing.T) {
	for _, activate := range []bool{false, true} {
		for _, name := range []string{"current", "unrelated changes", "null objects", "missing objects", "legacy type revision", "stale email", "stale revision", "stale type", "stale status", "absent", "binary email", "malformed metadata", "unacknowledged"} {
			t.Run(fmt.Sprintf("activate=%t/%s", activate, name), func(t *testing.T) {
				ctx := context.Background()
				db, repo, _ := handleMongoFixture(t, false)
				doc := profileDocument()
				c := loginCommand(activate)
				doc["status"] = c.Account.Status
				doc["verification"] = bson.M{"email_verified": false, "phone_verified": true, "phone_verified_at": "keep-phone", "custom": "keep"}
				doc["metadata"] = bson.M{"updated_at": "keep-profile-update", "created_at": "keep-created", "last_login_at": "legacy-time", "custom_timestamps": bson.M{"keep": "value"}, "other": "keep"}
				want := error(nil)
				switch name {
				case "null objects":
					doc["metadata"], doc["verification"] = nil, nil
				case "missing objects":
					delete(doc, "metadata")
					delete(doc, "verification")
				case "legacy type revision":
					delete(doc, "type")
					delete(doc, "email_revision")
					c.Account.Type = ""
					c.Account.EmailRevision = 0
				case "stale email":
					c.Account.Email = "other@example.test"
					want = user.ErrLoginStateConflict
				case "stale revision":
					c.Account.EmailRevision++
					want = user.ErrLoginStateConflict
				case "stale type":
					c.Account.Type = "other"
					want = user.ErrLoginStateConflict
				case "stale status":
					doc["status"] = user.AccountStatusKeySuspended
					want = user.ErrLoginStateConflict
				case "absent":
					c.Account.UserID = "absent"
					want = user.ErrLoginStateConflict
				case "binary email":
					require.NoError(t, db.CreateCollection(ctx, "users", options.CreateCollection().SetCollation(&options.Collation{Locale: "en", Strength: 2})))
					c.Account.Email = "OWNER@example.test"
					want = user.ErrLoginStateConflict
				case "malformed metadata":
					doc["metadata"] = "bad"
				case "unacknowledged":
					store, err := repository.NewMongoDbRepositoryFromDatabase(db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged())), nil)
					require.NoError(t, err)
					repo = user.NewRepository(store)
					want = repository.ErrUnacknowledgedMongoWrite
				}
				_, err := db.Collection("users").InsertOne(ctx, doc)
				require.NoError(t, err)
				if name == "unrelated changes" {
					_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": "owner"}, bson.M{"$set": bson.M{"roles": bson.A{"ADMIN"}, "personal_info.first_name": "Concurrent", "metadata.updated_at": "new-profile-update", "verification.phone_verified_at": "new-phone"}})
					require.NoError(t, err)
				}
				var before, after bson.M
				require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&before))
				got, err := loginWrite(ctx, repo, c, activate)
				if name == "malformed metadata" {
					require.Error(t, err)
					require.NotEqual(t, user.ErrLoginStateConflict, err)
					require.Nil(t, got)
					return
				}
				require.Equal(t, want, err)
				if name == "unacknowledged" {
					require.Nil(t, got)
					return
				} // uncertain write is not a rollback assertion
				require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&after))
				if err != nil {
					require.Nil(t, got)
					require.Equal(t, before, after)
					return
				}
				require.Equal(t, c.At, got.Metadata.LastLoginAt)
				require.Equal(t, c.At, got.Metadata.LastFreshLoginAt)
				for _, field := range []string{"_id", "email", "email_revision", "type", "roles", "personal_info", "handle", "oauth_identity_keys", "extensions", "version"} {
					require.Equal(t, before[field], after[field], field)
				}
				if activate {
					require.Equal(t, user.AccountStatusKeyActive, got.Status)
					require.True(t, got.Verification.EmailVerified)
					require.Equal(t, c.At, got.Verification.EmailVerifiedAt)
					require.Equal(t, c.At, got.Metadata.ActivatedAt)
				} else {
					require.Equal(t, before["verification"], after["verification"])
					if name != "null objects" && name != "missing objects" {
						stamp := "keep-profile-update"
						if name == "unrelated changes" {
							stamp = "new-profile-update"
						}
						require.Equal(t, stamp, got.Metadata.UpdatedAt)
					}
				}
				if name != "null objects" && name != "missing objects" {
					require.Equal(t, "keep-created", got.Metadata.CreatedAt)
					require.Equal(t, "value", got.Metadata.CustomTimestamps["keep"])
					if activate {
						require.True(t, got.Verification.PhoneVerified)
					}
				}
			})
		}
	}
}

func TestLoginStateMongoConcurrency(t *testing.T) {
	for _, mode := range []string{"competing activation", "independent logins", "profile and login"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			db, r, _ := handleMongoFixture(t, false)
			activate := mode == "competing activation"
			doc := profileDocument()
			c := loginCommand(activate)
			doc["status"] = c.Account.Status
			_, err := db.Collection("users").InsertOne(ctx, doc)
			require.NoError(t, err)
			n := 6
			if mode == "profile and login" {
				n = 2
			}
			errs := make([]error, n)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if mode == "profile and login" && i == 0 {
						_, errs[i] = r.SetProfileNames(ctx, profileCommand())
						return
					}
					input := *c
					_, errs[i] = loginWrite(ctx, r, &input, activate)
				}()
			}
			close(start)
			wg.Wait()
			winners := 0
			for _, err := range errs {
				if err == nil {
					winners++
				} else {
					require.Equal(t, user.ErrLoginStateConflict, err)
				}
			}
			if activate {
				require.Equal(t, 1, winners)
			} else {
				require.Equal(t, n, winners)
			}
			stored, err := r.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			require.Equal(t, c.At, stored.Metadata.LastLoginAt)
			if mode == "profile and login" {
				require.Equal(t, "New", stored.PersonalInfo.FirstName)
				require.NotEmpty(t, stored.Metadata.UpdatedAt)
			}
		})
	}
}
