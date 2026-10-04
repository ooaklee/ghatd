package waitlist

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestStoreAndContactConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, version string
		valid         bool
	}{
		{"default", "", true}, {"custom", "example-consent-v2", true}, {"spaces", "bad version", false}, {"control", "bad\nversion", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, repo, _, collection := commsTestDatabase(t)
			store, err := NewMongoStoreWithConfig(collection.Database(), StoreConfig{ConsentVersion: tc.version, SequenceEnrollment: true})
			if !tc.valid {
				require.Error(t, err)
				_, err = NewCommsServiceWithConfig(repo, &fakeStore{}, CommsConfig{ConsentVersion: tc.version})
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NoError(t, store.Initialize(ctx))
			service, err := NewCommsServiceWithConfig(repo, store, CommsConfig{ConsentVersion: tc.version})
			require.NoError(t, err)
			require.NoError(t, NewCommsSignupStore(service, store).Join(ctx, " PERSON@example.com "))
			entries, err := store.Export(ctx)
			require.NoError(t, err)
			require.Len(t, entries, 1)
			version := tc.version
			if version == "" {
				version = ConsentVersion
			}
			require.Equal(t, version, entries[0].ConsentVersion)
			require.EqualValues(t, 1, entries[0].Sequence)
			var comm struct {
				Meta map[string]any `bson:"meta"`
			}
			require.NoError(t, collection.FindOne(ctx, bson.M{"email": "person@example.com"}).Decode(&comm))
			require.Equal(t, version, comm.Meta["consent_version"])
		})
	}
}

// Concurrent duplicates, rollback, restart and unsubscribe share one durable
// lifecycle; isolation between phases would lose the no-reallocation assertion.
func TestSequenceConcurrentEnrollmentAndSuppression(t *testing.T) {
	ctx, _, _, contacts := commsTestDatabase(t)
	store, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
	require.NoError(t, err)
	require.ErrorIs(t, store.Join(ctx, "before-init@example.com"), ErrSequenceState)
	require.NoError(t, store.Initialize(ctx))
	require.NoError(t, store.Initialize(ctx))
	var wg sync.WaitGroup
	errs := make(chan error, 48)
	for i := 0; i < 16; i++ {
		for range 3 {
			wg.Go(func() { errs <- store.Join(ctx, fmt.Sprintf("member-%d@example.com", i)) })
		}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	entries, err := store.Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 16)
	seen := map[int64]bool{}
	for _, e := range entries {
		require.Positive(t, e.Sequence)
		require.LessOrEqual(t, e.Sequence, int64(16))
		require.False(t, seen[e.Sequence])
		seen[e.Sequence] = true
	}
	original := entries[0]
	_, err = store.Claim(ctx, original, "preview", "token-hash")
	require.NoError(t, err)
	require.NoError(t, store.Unsubscribe(ctx, "token-hash"))
	require.NoError(t, store.Join(ctx, original.Email))
	require.NoError(t, store.Join(ctx, "later@example.com"))
	var persisted Entry
	require.NoError(t, store.signups.FindOne(ctx, bson.M{"_id": original.ID}).Decode(&persisted))
	require.Equal(t, original.Sequence, persisted.Sequence)
	require.Equal(t, original.JoinedAt.UnixMilli(), persisted.JoinedAt.UnixMilli())
	var later Entry
	require.NoError(t, store.signups.FindOne(ctx, bson.M{"email": "later@example.com"}).Decode(&later))
	require.EqualValues(t, 17, later.Sequence)
	// Force an insert failure after counter increment. The transaction must restore it.
	_, err = store.signups.Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "source", Value: 1}}, Options: options.Index().SetName("test_reject_new_source").SetUnique(true).SetPartialFilterExpression(bson.M{"email": bson.M{"$in": bson.A{"later@example.com", "will-rollback@example.com"}}})})
	require.NoError(t, err)
	require.Error(t, store.Join(ctx, "will-rollback@example.com"))
	err = store.signups.Indexes().DropOne(ctx, "test_reject_new_source")
	require.NoError(t, err)
	require.NoError(t, store.Join(ctx, "after-rollback@example.com"))
	require.NoError(t, store.signups.FindOne(ctx, bson.M{"email": "after-rollback@example.com"}).Decode(&later))
	require.EqualValues(t, 18, later.Sequence)
	fresh, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
	require.NoError(t, err)
	require.NoError(t, fresh.Initialize(ctx))
	count, err := contacts.Database().Collection("waitlist_campaigns").CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestSequenceStateRequiresExplicitReview(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counter any
		remove  bool
	}{
		{"missing", nil, true}, {"behind", int64(0), false}, {"ahead", int64(2), false}, {"malformed", "broken", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _, contacts := commsTestDatabase(t)
			store, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
			require.NoError(t, err)
			require.NoError(t, store.Initialize(ctx))
			require.NoError(t, store.Join(ctx, "original@example.com"))
			if tc.remove {
				_, err = store.counters.DeleteOne(ctx, bson.M{"_id": CampaignID})
			} else {
				_, err = store.counters.UpdateOne(ctx, bson.M{"_id": CampaignID}, bson.M{"$set": bson.M{"value": tc.counter}})
			}
			require.NoError(t, err)
			fresh, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
			require.NoError(t, err)
			require.Error(t, fresh.Initialize(ctx))
			require.Error(t, store.Join(ctx, "new@example.com"))
			count, err := store.signups.CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestSequenceRejectsEmptyMalformedCounter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		counter bson.M
	}{
		{"missing value", bson.M{"_id": CampaignID}},
		{"null value", bson.M{"_id": CampaignID, "value": nil}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _, _, contacts := commsTestDatabase(t)
			store, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
			require.NoError(t, err)
			_, err = store.counters.InsertOne(ctx, tc.counter)
			require.NoError(t, err)
			require.ErrorIs(t, store.Initialize(ctx), ErrSequenceState)
			require.ErrorIs(t, store.Join(ctx, "new@example.com"), ErrSequenceState)
		})
	}
}

// Independent instances reproduce simultaneous startup against one database.
func TestSequenceConcurrentInitialization(t *testing.T) {
	ctx, _, _, contacts := commsTestDatabase(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		store, err := NewMongoStoreWithConfig(contacts.Database(), StoreConfig{SequenceEnrollment: true})
		require.NoError(t, err)
		wg.Go(func() { errs <- store.Initialize(ctx) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
