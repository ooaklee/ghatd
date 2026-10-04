package waitlist

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Stateful exception: concurrent creation followed by restart/retry must preserve
// the first timestamp and complete audience; those phases share durable state.
func TestMongoWaitlistConcurrentBoundaryAndDuplicateRetries(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("requires the isolated Mongo test service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database(fmt.Sprintf("examplehost_waitlist_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	store := NewMongoStore(db)
	require.NoError(t, store.Initialize(ctx))
	require.NoError(t, store.Initialize(ctx)) // Startup remains safe on an existing audience.
	// Exercise concurrent initial creation and repeated requests for one email.
	concurrently := func(emails []string) {
		var wg sync.WaitGroup
		failures := make(chan error, len(emails))
		for _, email := range emails {
			wg.Go(func() {
				if err := store.Join(ctx, email); err != nil {
					failures <- err
				}
			})
		}
		wg.Wait()
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
	}
	concurrently([]string{"first@example.com", "first@example.com", "first@example.com"})
	initial, err := store.Export(ctx)
	require.NoError(t, err)
	require.Len(t, initial, 1)
	// Concurrent retries must create exactly one record per canonical address.
	var emails []string
	for i := 0; i < 32; i++ {
		email := fmt.Sprintf("boundary-%d@example.com", i)
		emails = append(emails, email, email, email)
	}
	emails = append(emails, "first@example.com")
	concurrently(emails)
	// A fresh repository instance verifies persistence, not an in-memory counter.
	entries, err := NewMongoStore(db).Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 33)
	seen := map[string]bool{}
	for _, entry := range entries {
		require.False(t, seen[entry.Email], "duplicate address: %s", entry.Email)
		seen[entry.Email] = true
		require.Equal(t, ConsentVersion, entry.ConsentVersion)
		require.Equal(t, "landing", entry.Source)
	}
	require.Equal(t, initial[0].JoinedAt, entries[0].JoinedAt, "duplicate signup must preserve its original timestamp")
}

// Stateful exception: freeze, claim race, result, restart and unsubscribe exercise
// one durable lifecycle rather than independent input/output cases.
func TestMongoAnnouncementClaimsAndUnsubscribePersist(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("requires the isolated Mongo test service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	require.NoError(t, err)
	db := client.Database(fmt.Sprintf("examplehost_announcement_test_%d", time.Now().UnixNano()))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, db.Drop(ctx))
		require.NoError(t, client.Disconnect(ctx))
	})
	store := NewMongoStore(db)
	require.NoError(t, store.Join(ctx, "subscriber@example.com"))
	entries, err := store.Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	a := announcementDraft()
	a.ID = "first-preview"
	a.RecipientCount = 1
	a.PreparedAt = time.Now()
	require.NoError(t, store.SavePreview(ctx, a))
	require.NoError(t, store.Start(ctx, a))
	other := a
	other.ID = "different-preview"
	require.ErrorIs(t, store.Start(ctx, other), ErrAnnouncementFrozen)
	var wg sync.WaitGroup
	claims := make(chan bool, 12)
	errs := make(chan error, 12)
	token := randomToken()
	for i := 0; i < 12; i++ {
		wg.Go(func() {
			claimed, err := store.Claim(ctx, entries[0], a.ID, tokenHash(token))
			claims <- claimed
			errs <- err
		})
	}
	wg.Wait()
	close(claims)
	close(errs)
	winners := 0
	for claimed := range claims {
		if claimed {
			winners++
		}
	}
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 1, winners)
	require.NoError(t, store.Finish(ctx, entries[0].ID, "accepted", "provider-reference"))
	fresh := NewMongoStore(db)
	active, err := fresh.Started(ctx)
	require.NoError(t, err)
	require.Equal(t, a.ID, active.ID)
	saved, err := fresh.Export(ctx)
	require.NoError(t, err)
	require.Equal(t, "accepted", saved[0].AnnouncementState)
	require.Equal(t, "provider-reference", saved[0].ProviderMessageID)
	require.NoError(t, fresh.Unsubscribe(ctx, tokenHash(token)))
	require.NoError(t, fresh.Unsubscribe(ctx, tokenHash(token)))
	require.NoError(t, fresh.Join(ctx, "subscriber@example.com"))
	saved, err = fresh.Export(ctx)
	require.NoError(t, err)
	require.Empty(t, saved, "signup retries must not reverse an unsubscribe")
	count, err := db.Collection("waitlist_signups").CountDocuments(ctx, bson.M{"_id": entries[0].ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, count, "unsubscribe must preserve the original signup")
}
