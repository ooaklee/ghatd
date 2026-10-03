package user_test

import (
	"context"
	"slices"
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

// roleCommand selects the case-owned fixture's exact state and role array.
func roleCommand() *user.SetAccountRolesRequest {
	return &user.SetAccountRolesRequest{Account: user.AccountSnapshot{UserID: "owner", Email: "owner@example.test", EmailRevision: 2, Type: "default", Status: "ACTIVE"}, PreviousRoles: []string{"USER"}, Roles: []string{"USER", "ADMIN"}, At: time.Now().UTC().Format(time.RFC3339Nano)}
}

func TestAccountRoleMongo(t *testing.T) {
	for _, name := range []string{"current", "unrelated fields", "legacy type revision email", "missing roles", "null roles", "empty roles", "nested roles", "scalar roles", "extra roles", "reordered roles", "stale email", "stale revision", "stale type", "stale status", "binary roles", "binary email", "null metadata", "missing metadata", "malformed metadata", "literal role", "remove all", "no-op", "no-op missing", "no-op null", "unacknowledged"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			c, doc := roleCommand(), profileDocument()
			doc["roles"] = bson.A{"USER"}
			doc["metadata"].(bson.M)["updated_at"] = "prior"
			var want error
			switch name {
			case "unrelated fields":
				doc["personal_info"].(bson.M)["first_name"] = "Concurrent"
				doc["verification"].(bson.M)["phone_verified_at"] = "new-phone"
			case "legacy type revision email":
				delete(doc, "type")
				delete(doc, "email_revision")
				delete(doc, "email")
				c.Account.Type = ""
				c.Account.EmailRevision = 0
				c.Account.Email = ""
			case "missing roles", "no-op missing":
				delete(doc, "roles")
				c.PreviousRoles = nil
			case "null roles", "no-op null":
				doc["roles"] = nil
				c.PreviousRoles = nil
			case "empty roles":
				doc["roles"] = bson.A{}
				c.PreviousRoles = nil
			case "nested roles":
				doc["roles"] = bson.A{bson.A{"USER"}}
				want = user.ErrRoleUpdateConflict
			case "scalar roles":
				doc["roles"] = "USER"
				want = user.ErrRoleUpdateConflict
			case "extra roles":
				doc["roles"] = bson.A{"USER", "OTHER"}
				want = user.ErrRoleUpdateConflict
			case "reordered roles":
				doc["roles"] = bson.A{"ADMIN", "USER"}
				c.PreviousRoles = []string{"USER", "ADMIN"}
				want = user.ErrRoleUpdateConflict
			case "stale email":
				c.Account.Email = "changed@example.test"
				want = user.ErrRoleUpdateConflict
			case "stale revision":
				c.Account.EmailRevision++
				want = user.ErrRoleUpdateConflict
			case "stale type":
				c.Account.Type = "other"
				want = user.ErrRoleUpdateConflict
			case "stale status":
				c.Account.Status = "SUSPENDED"
				want = user.ErrRoleUpdateConflict
			case "binary roles", "binary email":
				require.NoError(t, db.CreateCollection(ctx, "users", options.CreateCollection().SetCollation(&options.Collation{Locale: "en", Strength: 2})))
				want = user.ErrRoleUpdateConflict
				if name == "binary roles" {
					c.PreviousRoles = []string{"user"}
				} else {
					c.Account.Email = "OWNER@example.test"
				}
			case "null metadata":
				doc["metadata"] = nil
			case "missing metadata":
				delete(doc, "metadata")
			case "malformed metadata":
				doc["metadata"] = "bad"
			case "literal role":
				c.Roles = []string{"$roles", "$ADMIN"}
			case "remove all":
				c.Roles = []string{}
			case "unacknowledged":
				store, err := repository.NewMongoDbRepositoryFromDatabase(db.Client().Database(db.Name(), options.Database().SetWriteConcern(writeconcern.Unacknowledged())), nil)
				require.NoError(t, err)
				repo = user.NewRepository(store)
				want = repository.ErrUnacknowledgedMongoWrite
			}
			if name == "no-op" || name == "no-op missing" || name == "no-op null" {
				c.Roles = slices.Clone(c.PreviousRoles)
			}
			_, err := db.Collection("users").InsertOne(ctx, doc)
			require.NoError(t, err)
			var before, after bson.M
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&before))
			got, err := repo.SetAccountRoles(ctx, c)
			if name == "malformed metadata" {
				require.Error(t, err)
				require.NotErrorIs(t, err, user.ErrRoleUpdateConflict)
				return
			}
			require.Equal(t, want, err)
			if name == "unacknowledged" {
				require.Nil(t, got)
				return
			} // Uncertain writes are not rollback evidence.
			require.NoError(t, db.Collection("users").FindOne(ctx, bson.M{"_id": "owner"}).Decode(&after))
			if err != nil {
				require.Nil(t, got)
				require.Equal(t, before, after)
				return
			}
			require.True(t, slices.Equal(c.Roles, got.Roles))
			if slices.Equal(c.PreviousRoles, c.Roles) {
				require.Equal(t, before, after)
				return
			}
			require.Equal(t, c.At, got.Metadata.UpdatedAt)
			for _, field := range []string{"email", "email_revision", "type", "status", "personal_info", "verification", "oauth_identity_keys", "extensions", "handle"} {
				require.Equal(t, before[field], after[field], field)
			}
			if m, ok := before["metadata"].(bson.M); ok {
				delete(m, "updated_at")
				am := after["metadata"].(bson.M)
				delete(am, "updated_at")
				require.Equal(t, m, am)
			}
		})
	}
}

func TestAccountRoleMongoConcurrency(t *testing.T) {
	for _, mode := range []string{"competing roles", "role and profile"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			db, repo, _ := handleMongoFixture(t, false)
			doc := profileDocument()
			doc["roles"] = bson.A{"USER"}
			_, err := db.Collection("users").InsertOne(ctx, doc)
			require.NoError(t, err)
			var wg sync.WaitGroup
			start := make(chan struct{})
			results := make(chan error, 2)
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					if mode == "role and profile" && i == 1 {
						_, err := repo.SetProfileNames(ctx, profileCommand())
						results <- err
						return
					}
					c := roleCommand()
					if i == 1 {
						c.Roles = []string{"USER", "REVIEWER"}
					}
					_, err := repo.SetAccountRoles(ctx, c)
					results <- err
				}(i)
			}
			close(start)
			wg.Wait()
			close(results)
			wins := 0
			for err := range results {
				if err == nil {
					wins++
				} else {
					require.ErrorIs(t, err, user.ErrRoleUpdateConflict)
				}
			}
			if mode == "competing roles" {
				require.Equal(t, 1, wins)
			} else {
				require.Equal(t, 2, wins)
			}
			got, err := repo.GetUserByID(ctx, "owner")
			require.NoError(t, err)
			require.Len(t, got.Roles, 2)
			if mode == "role and profile" {
				require.Equal(t, "New", got.PersonalInfo.FirstName)
				require.Contains(t, got.Roles, "ADMIN")
			}
		})
	}
}
