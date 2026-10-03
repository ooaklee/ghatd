package user_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

// TestEmailLookupMongo verifies real missing-versus-decode errors, normalized
// lookup and detached hydration through the shared repository helper.
func TestEmailLookupMongo(t *testing.T) {
	for _, strict := range []bool{true, false} {
		for _, name := range []string{"stored", "absent", "decode failure", "negative revision", "canceled"} {
			t.Run(fmt.Sprintf("strict=%t/%s", strict, name), func(t *testing.T) {
				db, _, s := handleMongoFixture(t, false)
				core, logs := observer.New(zap.DebugLevel)
				ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
				defer cancel()
				doc := bson.M{"_id": "owner", "email": "owner@example.test", "status": "ACTIVE", "roles": bson.A{"USER"}}
				if name == "decode failure" {
					doc["email_revision"] = "private-database-value"
				}
				if name == "negative revision" {
					doc["email_revision"] = -1
				}
				if name != "absent" {
					_, err := db.Collection("users").InsertOne(ctx, doc)
					require.NoError(t, err)
				}
				if name == "canceled" {
					cancel()
				}
				req := &user.GetUserByEmailRequest{Email: " Owner@Example.Test "}
				var result *user.GetUserByEmailResponse
				var err error
				if strict {
					result, err = s.GetUserByEmail(ctx, req)
				} else {
					result, err = s.FindUserByEmail(ctx, req)
				}
				switch name {
				case "stored":
					require.NoError(t, err)
					require.Equal(t, "owner", result.User.ID)
					require.Equal(t, "owner@example.test", result.User.Email)
				case "absent":
					require.ErrorIs(t, err, user.ErrUserNotFound)
					require.Nil(t, result)
				case "decode failure":
					require.Error(t, err)
					require.NotErrorIs(t, err, user.ErrUserNotFound)
					require.NotErrorIs(t, err, user.ErrDatabaseError)
					require.Nil(t, result)
				case "negative revision":
					require.ErrorIs(t, err, user.ErrDatabaseError)
					require.Nil(t, result)
				case "canceled":
					require.ErrorIs(t, err, context.Canceled)
					require.Nil(t, result)
				}
				for _, entry := range logs.All() {
					require.NotContains(t, fmt.Sprint(entry.Message, entry.ContextMap()), "private-database-value")
				}
			})
		}
	}
}
