package user_test

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	helpers "github.com/ooaklee/ghatd/external/repository/helpers"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/user/v2/migrations"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func signupMongoFixture(t *testing.T) (*user.Service, *user.Repository, *mongo.Database, context.Context) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI for isolated MongoDB integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	name := "signup_test_" + strings.ToLower(rand.Text())
	h, err := helpers.NewHandler(helpers.DefaultConfig(uri, name))
	require.NoError(t, err)
	store := repository.NewMongoDbRepositoryWithDefaults(h, name)
	db, err := store.GetDatabase(ctx, "")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, h.Close(cleanup))
	})
	require.NoError(t, migrations.InitUsersOAuthIndexesUp(ctx, db))
	require.NoError(t, migrations.InitUsersSignupAttributionIndexesUp(ctx, db))
	require.NoError(t, migrations.InitUsersSignupAttributionIndexesUp(ctx, db))
	r := user.NewRepository(store)
	s := user.NewService(r, nil, user.DefaultUserConfig(), &user.DefaultIDGenerator{}, &user.DefaultTimeProvider{}, &user.DefaultStringUtils{}, "")
	_, err = s.WithSignupAttribution(user.SignupAttributionConfig{ProgramID: "program", IndividualAccountTypes: []string{user.DefaultUserConfig().GetType(user.DefaultUserConfig())}})
	require.NoError(t, err)
	return s, r, db, ctx
}

func TestSignupMongoCreationAndImmutableEvidence(t *testing.T) {
	cases := []struct {
		name     string
		oauth    bool
		evidence string
	}{
		{"password signup", false, "private-signed-context"}, {"OAuth signup", true, "private-signed-context"}, {"no cookie signup", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, db, ctx := signupMongoFixture(t)
			var created *user.UniversalUser
			if tc.oauth {
				res, err := s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "subject"}, Email: "member@example.test", AttributionEvidence: tc.evidence})
				require.NoError(t, err)
				require.True(t, res.Created)
				created = res.User
			} else {
				res, err := s.CreateUser(ctx, &user.CreateUserRequest{Email: "member@example.test", FirstName: "Member", LastName: "Example", GenerateUUID: true, AttributionEvidence: tc.evidence})
				require.NoError(t, err)
				created = res.User
			}
			fact, err := s.GetSignupAttribution(ctx, created.ID)
			require.NoError(t, err)
			require.Equal(t, tc.evidence, fact.Evidence)
			require.Equal(t, created.ID, fact.CustomerID)
			require.True(t, fact.Individual)
			stamp, err := time.Parse(time.RFC3339Nano, created.Metadata.CreatedAt)
			if err != nil {
				stamp, err = time.ParseInLocation(user.DefaultTimeFormatRFC3339NanoUTC, created.Metadata.CreatedAt, time.UTC)
			}
			require.NoError(t, err)
			require.WithinDuration(t, stamp, fact.CreatedAt, time.Millisecond)
			public, err := json.Marshal(created)
			require.NoError(t, err)
			require.NotContains(t, string(public), "private-signed-context")
			require.NotContains(t, string(public), "signup_attribution")
			// A legacy broad profile write must preserve the owning creation context.
			created.SignupAttribution = &user.SignupAttribution{ProgramID: "forged", Evidence: "changed"}
			created.PersonalInfo.FirstName = "Updated"
			_, err = r.UpdateUser(ctx, created)
			require.NoError(t, err)
			unchanged, err := s.GetSignupAttribution(ctx, created.ID)
			require.NoError(t, err)
			require.Equal(t, fact, unchanged)
			pending, err := s.PendingSignupAttributions(ctx, 200)
			require.NoError(t, err)
			require.Len(t, pending, 1)
			receipt := user.SignupConsumption{ActorID: "worker", ReceiptID: "durable-referral-lock", Outcome: "attributed"}
			if tc.evidence == "" {
				receipt.Outcome = "no_evidence"
			}
			require.NoError(t, s.ConsumeSignupAttribution(ctx, created.ID, receipt))
			require.NoError(t, s.ConsumeSignupAttribution(ctx, created.ID, receipt))
			receipt.Outcome = "ineligible"
			require.ErrorIs(t, s.ConsumeSignupAttribution(ctx, created.ID, receipt), user.ErrSignupEvidenceConflict)
			pending, err = s.PendingSignupAttributions(ctx, 200)
			require.NoError(t, err)
			require.Empty(t, pending)
			stored, err := s.GetSignupAttribution(ctx, created.ID)
			require.NoError(t, err)
			require.Equal(t, "consumed", stored.State)
			require.Equal(t, tc.evidence, stored.Evidence)
			require.NotNil(t, stored.Consumption)
			require.False(t, stored.Consumption.RecordedAt.IsZero())
			// Historical accounts without creation evidence remain absent, even if the
			// current worker is configured; no retrospective evidence is invented.
			_, err = db.Collection("users").InsertOne(ctx, bson.M{"_id": "historical", "email": "old@example.test"})
			require.NoError(t, err)
			_, err = s.GetSignupAttribution(ctx, "historical")
			require.ErrorIs(t, err, user.ErrUserNotFound)
			if tc.oauth {
				res, err := s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "subject"}, Email: "member@example.test", AttributionEvidence: "replacement"})
				require.NoError(t, err)
				require.False(t, res.Created)
				again, err := s.GetSignupAttribution(ctx, res.User.ID)
				require.NoError(t, err)
				require.Equal(t, stored, again)
			}
		})
	}
}

func TestSignupMongoConcurrentOAuthCapture(t *testing.T) {
	// This single creation race verifies that each losing OAuth insertion returns
	// the winner's immutable evidence, rather than replacing it with its own.
	s, _, db, ctx := signupMongoFixture(t)
	start := make(chan struct{})
	type result struct {
		response *user.CreateOAuthUserResponse
		err      error
	}
	out := make(chan result, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			res, err := s.CreateOAuthUser(ctx, &user.CreateOAuthUserRequest{Identity: user.OAuthIdentity{Provider: "google", Issuer: "https://accounts.google.com", Subject: "same-subject"}, Email: "member@example.test", AttributionEvidence: rand.Text()})
			out <- result{res, err}
		}()
	}
	close(start)
	wg.Wait()
	close(out)
	wins := 0
	var winner *user.UniversalUser
	var all []*user.UniversalUser
	for res := range out {
		require.NoError(t, res.err)
		all = append(all, res.response.User)
		if res.response.Created {
			wins++
			winner = res.response.User
		}
	}
	count, err := db.Collection("users").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, 1, wins)
	require.NotNil(t, winner)
	for _, u := range all {
		require.Equal(t, winner.ID, u.ID)
		require.Equal(t, winner.SignupAttribution.Evidence, u.SignupAttribution.Evidence)
		require.Equal(t, winner.SignupAttribution.CreatedAtUTC, u.SignupAttribution.CreatedAtUTC)
		fact, err := s.GetSignupAttribution(ctx, u.ID)
		require.NoError(t, err)
		require.Equal(t, winner.SignupAttribution.CreatedAt, fact.CreatedAt)
	}
}
