package user_test

import (
	"context"
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

// statusCommand is a trusted snapshot of the case-owned fixture account.
func statusCommand(clear bool) *user.SetAccountStatusRequest {
	c := &user.SetAccountStatusRequest{Account: user.AccountSnapshot{UserID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: "ACTIVE"}, Status: "SUSPENDED", At: time.Now().UTC().Format(time.RFC3339Nano), ClearEmailVerification: clear, PreviousEmailVerified: true, PreviousEmailVerifiedAt: "prior"}
	if clear {
		c.Status = "PROVISIONED"
	}
	return c
}

func TestAccountStatusMongo(t *testing.T) {
	for _, clear := range []bool{false, true} {
		for _, name := range []string{"current", "concurrent unrelated", "null objects", "missing objects", "legacy type revision", "legacy missing email", "stale email", "stale type", "stale revision", "stale status", "binary email", "concurrent verification", "absent", "malformed metadata", "unacknowledged", "activation"} {
			prefix := "status/"
			if clear {
				prefix = "clear/"
			}
			t.Run(prefix+name, func(t *testing.T) {
				ctx := context.Background()
				db, repo, _ := handleMongoFixture(t, false)
				c, doc := statusCommand(clear), profileDocument()
				doc["verification"] = bson.M{"email_verified": true, "email_verified_at": "prior", "phone_verified": true, "phone_verified_at": "phone-keep", "custom": "keep"}
				doc["metadata"] = bson.M{"created_at": "keep", "activated_at": "original", "last_login_at": "login", "last_fresh_login_at": "fresh", "custom_timestamps": bson.M{"custom": "keep"}}
				var want error
				switch name {
				case "null objects":
					doc["metadata"], doc["verification"] = nil, nil
					c.PreviousEmailVerified = false
					c.PreviousEmailVerifiedAt = ""
				case "missing objects":
					delete(doc, "metadata")
					delete(doc, "verification")
					c.PreviousEmailVerified = false
					c.PreviousEmailVerifiedAt = ""
				case "legacy type revision":
					delete(doc, "type")
					delete(doc, "email_revision")
					c.Account.Type = ""
					c.Account.EmailRevision = 0
				case "legacy missing email":
					delete(doc, "email")
					c.Account.Email = ""
				case "stale email":
					c.Account.Email = "changed@example.test"
					want = user.ErrStatusUpdateConflict
				case "stale type":
					c.Account.Type = "other"
					want = user.ErrStatusUpdateConflict
				case "stale revision":
					c.Account.EmailRevision++
					want = user.ErrStatusUpdateConflict
				case "stale status":
					c.Account.Status = "PROVISIONED"
					want = user.ErrStatusUpdateConflict
				case "binary email":
					require.NoError(t, db.CreateCollection(ctx, "users", options.CreateCollection().SetCollation(&options.Collation{Locale: "en", Strength: 2})))
					c.Account.Email = "OWNER@example.test"
					want = user.ErrStatusUpdateConflict
				case "concurrent verification":
					doc["verification"].(bson.M)["email_verified_at"] = "new-proof"
					if clear {
						want = user.ErrStatusUpdateConflict
					}
				case "absent":
					c.Account.UserID = "absent"
					want = user.ErrStatusUpdateConflict
				case "malformed metadata":
					doc["metadata"] = "bad"
				case "activation":
					doc["status"] = "PROVISIONED"
					c.Account.Status = "PROVISIONED"
					c.Status = "ACTIVE"
				case "unacknowledged":
					store, err := repository.NewMongoDbRepositoryFromDatabase(db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged())), nil)
					require.NoError(t, err)
					repo = user.NewRepository(store)
					want = repository.ErrUnacknowledgedMongoWrite
				}
				_, err := db.Collection("users").InsertOne(ctx, doc)
				require.NoError(t, err)
				if name == "concurrent unrelated" {
					_, err = db.Collection("users").UpdateOne(ctx, bson.M{"_id": "owner"}, bson.M{"$set": bson.M{"personal_info.first_name": "Concurrent", "roles": bson.A{"ADMIN"}, "verification.phone_verified_at": "new-phone"}})
					require.NoError(t, err)
				}
				var before, after bson.M
				require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&before))
				got, err := repo.SetAccountStatus(ctx, c)
				if name == "malformed metadata" {
					require.Error(t, err)
					require.NotErrorIs(t, err, user.ErrStatusUpdateConflict)
					require.Nil(t, got)
					return
				}
				require.Equal(t, want, err)
				if name == "unacknowledged" {
					require.Nil(t, got)
					return
				} // no rollback assertion after uncertainty
				require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&after))
				if err != nil {
					require.Nil(t, got)
					require.Equal(t, before, after)
					return
				}
				require.Equal(t, c.Status, got.Status)
				require.Equal(t, c.At, got.Metadata.StatusChangedAt)
				require.Equal(t, c.At, got.Metadata.UpdatedAt)
				for _, field := range []string{"_id", "email", "email_revision", "type", "roles", "personal_info", "handle", "oauth_identity_keys", "extensions", "version"} {
					require.Equal(t, before[field], after[field], field)
				}
				if clear {
					require.False(t, got.Verification.EmailVerified)
					require.Empty(t, got.Verification.EmailVerifiedAt)
				} else {
					require.Equal(t, before["verification"], after["verification"])
				}
				if name != "null objects" && name != "missing objects" {
					require.Equal(t, "keep", got.Metadata.CreatedAt)
					require.Equal(t, "login", got.Metadata.LastLoginAt)
					require.Equal(t, "fresh", got.Metadata.LastFreshLoginAt)
					require.Equal(t, "keep", got.Metadata.CustomTimestamps["custom"])
					require.True(t, got.Verification.PhoneVerified)
				}
				if name == "activation" {
					require.Equal(t, c.At, got.Metadata.ActivatedAt)
				}
			})
		}
	}
}

func TestAccountStatusMongoConcurrentWriters(t *testing.T) {
	for _, name := range []string{"competing transitions", "profile and status"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, profileDocument())
			require.NoError(t, err)
			c := statusCommand(false)
			start := make(chan struct{})
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i := range 2 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					if name == "profile and status" && i == 0 {
						_, errs[i] = repo.SetProfileNames(ctx, profileCommand())
						return
					}
					x := *c
					if i == 1 {
						x.Status = "DEACTIVATED"
					}
					_, errs[i] = repo.SetAccountStatus(ctx, &x)
				}()
			}
			close(start)
			wg.Wait()
			if name == "competing transitions" {
				winners := 0
				for _, err := range errs {
					if err == nil {
						winners++
					} else {
						require.ErrorIs(t, err, user.ErrStatusUpdateConflict)
					}
				}
				require.Equal(t, 1, winners)
			} else {
				require.NoError(t, errs[1])
				if errs[0] != nil {
					require.ErrorIs(t, errs[0], user.ErrProfileUpdateConflict)
				}
				v, err := repo.GetUserByID(ctx, "owner")
				require.NoError(t, err)
				if errs[0] == nil {
					require.Equal(t, "New", v.PersonalInfo.FirstName)
				}
			}
		})
	}
}
