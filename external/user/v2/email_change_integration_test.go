package user_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

func TestEmailChangeMongoWriteAcknowledgement(t *testing.T) {
	for _, acknowledged := range []bool{true, false} {
		t.Run(fmt.Sprintf("acknowledged=%t", acknowledged), func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			require.NoError(t, migrations.InitUsersIndexesUp(db))
			_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "owner", "email": "old@example.test", "status": "ACTIVE"})
			require.NoError(t, err)
			if !acknowledged {
				unack := db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged()))
				store, err := repository.NewMongoDbRepositoryFromDatabase(unack, nil)
				require.NoError(t, err)
				repo = user.NewRepository(store)
			}
			result, err := repo.ChangeUserEmail(ctx, &user.ChangeUserEmailRequest{UserID: "owner", Email: "new@example.test", ExpectedEmail: "old@example.test", ExpectedStatus: "ACTIVE"}, "PROVISIONED", time.Now())
			if acknowledged {
				require.NoError(t, err)
				require.Equal(t, int64(1), result.EmailRevision)
			} else {
				require.Error(t, err)
				require.Nil(t, result)
				require.NotEqual(t, user.ErrEmailChangeConflict, err)
				// Unknown/unacknowledged outcome is never evidence of rollback.
			}
		})
	}
}

// Each case owns a database. The shared fixture is not a production connection.
func TestEmailChangeMongoSnapshot(t *testing.T) {
	for _, variant := range []string{"current", "missing revision", "null revision", "legacy type", "null type", "null optional objects", "literal mailbox", "stale revision", "stale email", "stale status", "stale type", "duplicate email"} {
		t.Run(variant, func(t *testing.T) {
			ctx := context.Background()
			db, repo, service := handleMongoFixture(t, false)
			require.NoError(t, migrations.InitUsersIndexesUp(db))
			original := bson.M{"_id": "owner", "email": "old@example.test", "status": "ACTIVE", "type": "default", "email_revision": int64(3), "roles": bson.A{"USER"}, "handle": "keep-handle", "oauth_identity_keys": bson.A{"keep-provider"}, "verification": bson.M{"email_verified": true, "email_verified_at": "old", "phone_verified": true}, "metadata": bson.M{"created_at": "created", "activated_at": "activated"}}
			req := &user.ChangeUserEmailRequest{UserID: "owner", Email: "new@example.test", ExpectedEmail: "old@example.test", ExpectedStatus: "ACTIVE", ExpectedType: "default", ExpectedRevision: 3}
			want := error(nil)
			switch variant {
			case "missing revision":
				delete(original, "email_revision")
				req.ExpectedRevision = 0
			case "null revision":
				original["email_revision"] = nil
				req.ExpectedRevision = 0
			case "legacy type":
				delete(original, "type")
			case "null type":
				original["type"] = nil
			case "null optional objects":
				original["verification"], original["metadata"] = nil, nil
			case "literal mailbox":
				req.Email = "$field@example.test"
			case "stale revision":
				req.ExpectedRevision = 2
				want = user.ErrEmailChangeConflict
			case "stale email":
				req.ExpectedEmail = "stale@example.test"
				want = user.ErrEmailChangeConflict
			case "stale status":
				req.ExpectedStatus = "PROVISIONED"
				want = user.ErrEmailChangeConflict
			case "stale type":
				req.ExpectedType = "web_app"
				want = user.ErrEmailChangeConflict
			case "duplicate email":
				_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "other", "email": req.Email})
				require.NoError(t, err)
				want = user.ErrEmailAlreadyExists
			}
			_, err := db.Collection("users").InsertOne(ctx, original)
			require.NoError(t, err)
			var before bson.M
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&before))
			result, err := service.ChangeUserEmail(ctx, req)
			require.Equal(t, want, err)
			var after bson.M
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&after))
			if want != nil {
				require.Nil(t, result)
				require.Equal(t, before, after)
				return
			}
			require.Equal(t, req.Email, result.Email)
			require.Equal(t, req.ExpectedRevision+1, result.EmailRevision)
			require.Equal(t, "PROVISIONED", result.Status)
			require.False(t, result.Verification.EmailVerified)
			require.Empty(t, result.Verification.EmailVerifiedAt)
			require.Equal(t, "keep-handle", result.Handle)
			require.Equal(t, []string{"keep-provider"}, result.OAuthIdentityKeys)
			if variant != "null optional objects" {
				require.True(t, result.Verification.PhoneVerified)
				require.Equal(t, "created", result.Metadata.CreatedAt)
				require.Equal(t, "activated", result.Metadata.ActivatedAt)
			}
			require.NotEmpty(t, result.Metadata.UpdatedAt)
			stale := *result
			stale.Email = "old@example.test"
			stale.EmailRevision = req.ExpectedRevision
			_, err = repo.UpdateUser(ctx, &stale)
			require.Error(t, err)
			stale.EmailRevision = result.EmailRevision
			_, err = repo.UpdateUser(ctx, &stale)
			require.Error(t, err, "even a current revision cannot bypass the email command")
			current, err := repo.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			require.Equal(t, req.Email, current.Email)
		})
	}
}

func TestEmailChangeMongoConcurrency(t *testing.T) {
	for _, owners := range []int{1, 2} {
		t.Run(fmt.Sprintf("owners=%d", owners), func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			require.NoError(t, migrations.InitUsersIndexesUp(db))
			for i := 0; i < owners; i++ {
				_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": fmt.Sprint(i), "email": fmt.Sprintf("old%d@example.test", i), "status": "ACTIVE"})
				require.NoError(t, err)
			}
			start := make(chan struct{})
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					id := i % owners
					email := "shared@example.test"
					if owners == 1 {
						email = fmt.Sprintf("new%d@example.test", i)
					}
					_, err := repo.ChangeUserEmail(ctx, &user.ChangeUserEmailRequest{UserID: fmt.Sprint(id), ExpectedEmail: fmt.Sprintf("old%d@example.test", id), ExpectedStatus: "ACTIVE", Email: email}, "PROVISIONED", time.Now())
					results <- err
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			wins := 0
			want := user.ErrEmailChangeConflict
			if owners == 2 {
				want = user.ErrEmailAlreadyExists
			}
			for err := range results {
				if err == nil {
					wins++
				} else {
					require.Equal(t, want, err)
				}
			}
			require.Equal(t, 1, wins)
		})
	}
}

func TestEmailChangeMongoIndexReadiness(t *testing.T) {
	for _, kind := range []string{"missing", "nonunique", "partial", "sparse", "compound", "collation", "hidden", "double key", "valid"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			_, err := db.Collection("users").InsertOne(ctx, bson.M{"_id": "owner", "email": "old@example.test", "status": "ACTIVE"})
			require.NoError(t, err)
			if kind != "missing" {
				keys := bson.D{{Key: "email", Value: 1}}
				opts := options.Index().SetName("idx_users_email").SetUnique(kind != "nonunique")
				switch kind {
				case "hidden":
					opts.SetHidden(true)
				case "double key":
					keys[0].Value = float64(1)
				case "partial":
					opts.SetPartialFilterExpression(bson.M{"status": "ACTIVE"})
				case "sparse":
					opts.SetSparse(true)
				case "compound":
					keys = append(keys, bson.E{Key: "status", Value: 1})
				case "collation":
					opts.SetCollation(&options.Collation{Locale: "en", Strength: 2})
				}
				_, err = db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: keys, Options: opts})
				require.NoError(t, err)
			}
			_, err = repo.ChangeUserEmail(ctx, &user.ChangeUserEmailRequest{UserID: "owner", Email: "new@example.test", ExpectedEmail: "old@example.test", ExpectedStatus: "ACTIVE"}, "PROVISIONED", time.Now())
			if kind == "valid" || kind == "hidden" || kind == "double key" {
				require.NoError(t, err)
				_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "duplicate", "email": "new@example.test"})
				require.True(t, mongo.IsDuplicateKeyError(err), "constraint still enforces uniqueness")
			} else {
				require.Equal(t, user.ErrEmailIndexesRequired, err)
			}
		})
	}
}

func TestEmailChangeMongoExactCollation(t *testing.T) {
	for _, field := range []string{"exact", "id", "email", "status", "type"} {
		t.Run(field, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			require.NoError(t, db.CreateCollection(ctx, "users", options.CreateCollection().SetCollation(&options.Collation{Locale: "en", Strength: 2})))
			_, err := db.Collection("users").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetName("idx_users_email").SetUnique(true).SetCollation(&options.Collation{Locale: "simple"})})
			require.NoError(t, err)
			_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "owner", "email": "old@example.test", "status": "ACTIVE", "type": "default"})
			require.NoError(t, err)
			req := &user.ChangeUserEmailRequest{UserID: "owner", Email: "new@example.test", ExpectedEmail: "old@example.test", ExpectedStatus: "ACTIVE", ExpectedType: "default"}
			switch field {
			case "id":
				req.UserID = "OWNER"
			case "email":
				req.ExpectedEmail = "OLD@example.test"
			case "status":
				req.ExpectedStatus = "active"
			case "type":
				req.ExpectedType = "DEFAULT"
			}
			result, err := repo.ChangeUserEmail(ctx, req, "PROVISIONED", time.Now())
			if field == "exact" {
				require.NoError(t, err)
				require.Equal(t, int64(1), result.EmailRevision)
			} else {
				require.Equal(t, user.ErrEmailChangeConflict, err)
				current, err := repo.GetUserByID(ctx, "owner")
				require.NoError(t, err)
				require.Equal(t, "old@example.test", current.Email)
				require.Zero(t, current.EmailRevision)
			}
		})
	}
}
