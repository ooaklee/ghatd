package user_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// TestUserLookupMongo exercises real filtering/limits on both sides of the
// batch boundary. The shared fixture creates and cleans an isolated database.
func TestUserLookupMongo(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
	}{
		{"empty", 0}, {"single", 1}, {"full batch", 100},
		{"batch plus one", 101}, {"three batches", 201},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, _, service := handleMongoFixture(t, false)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			rows := []any{bson.M{"_id": "unrequested", "email": "hidden@example.test"}}
			ids := make([]string, tc.count)
			for i := range ids {
				ids[i] = fmt.Sprintf("user-%03d", i)
				rows = append(rows, bson.M{"_id": ids[i], "email": fmt.Sprintf("person%d@example.test", i)})
			}
			_, err := db.Collection(user.UserCollection).InsertMany(ctx, rows)
			require.NoError(t, err)
			if tc.count > 0 {
				ids = append(ids, ids[0], "deleted-user")
			}
			got, err := service.GetUsersByIDs(ctx, &user.GetUsersByIDsRequest{IDs: ids})
			require.NoError(t, err)
			require.Len(t, got.Users, tc.count)
			require.NotContains(t, got.Users, "unrequested")
			require.NotContains(t, got.Users, "deleted-user")
			for i := 0; i < tc.count; i++ {
				id := fmt.Sprintf("user-%03d", i)
				require.Equal(t, id, got.Users[id].ID)
			}
		})
	}
}
