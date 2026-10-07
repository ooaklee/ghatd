package user_test

import (
	"fmt"
	"testing"
	"time"

	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: named datastore cases show complete bounded discovery;
// creation atomicity/profile immutability have separate owning integration cases.
func TestSignupMongoDiscoveryPaging(t *testing.T) {
	cases := []struct {
		name                  string
		lateLowerID, ackLater bool
	}{{name: "earlier_pending_prefix_does_not_hide_later_signup"}, {name: "late_lower_id_is_visible_on_next_complete_sweep", lateLowerID: true}, {name: "later_ack_does_not_consume_failed_prefix", ackLater: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, _, db, ctx := signupMongoFixture(t)
			now := time.Now().UTC()
			rows := make([]any, 0, 205)
			capture := func(id string) user.SignupAttribution {
				return user.SignupAttribution{ProgramID: "program", CustomerID: id, CreatedAt: now, CreatedAtUTC: now.Format(time.RFC3339Nano), Individual: true, State: "pending"}
			}
			for i := 0; i < 205; i++ {
				id := fmt.Sprintf("customer_%04d", i)
				rows = append(rows, bson.M{"_id": id, "email": id + "@example.test", "signup_attribution": capture(id)})
			}
			_, err := db.Collection("users").InsertMany(ctx, rows)
			require.NoError(t, err)
			first, err := service.PendingSignupAttributionsAfter(ctx, "", 200)
			require.NoError(t, err)
			require.Len(t, first, 200)
			require.Equal(t, "customer_0000", first[0].CustomerID)
			require.Equal(t, "customer_0199", first[199].CustomerID)
			second, err := service.PendingSignupAttributionsAfter(ctx, first[199].CustomerID, 200)
			require.NoError(t, err)
			require.Len(t, second, 5)
			require.Equal(t, "customer_0200", second[0].CustomerID)
			require.Equal(t, "customer_0204", second[4].CustomerID)
			end, err := service.PendingSignupAttributionsAfter(ctx, second[4].CustomerID, 200)
			require.NoError(t, err)
			require.Empty(t, end)
			if tc.lateLowerID {
				_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "customer_-late", "email": "late@example.test", "signup_attribution": capture("customer_-late")})
				require.NoError(t, err)
				nextSweep, err := service.PendingSignupAttributionsAfter(ctx, "", 200)
				require.NoError(t, err)
				require.Equal(t, "customer_-late", nextSweep[0].CustomerID)
			}
			if tc.ackLater {
				require.NoError(t, service.ConsumeSignupAttribution(ctx, "customer_0204", user.SignupConsumption{ReceiptID: "owning_decision", ActorID: "current_worker", Outcome: "no_evidence"}))
				remaining, err := service.PendingSignupAttributionsAfter(ctx, "customer_0199", 200)
				require.NoError(t, err)
				require.Len(t, remaining, 4)
			}
			// A read/discovery position never acknowledges an earlier pending signup.
			for _, id := range []string{"customer_0000", "customer_0199"} {
				original, err := service.GetSignupAttribution(ctx, id)
				require.NoError(t, err)
				require.Equal(t, "pending", original.State)
				require.Nil(t, original.Consumption)
			}
		})
	}
}
