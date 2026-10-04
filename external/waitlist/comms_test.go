package waitlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/repository"
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func commsTestDatabase(t *testing.T) (context.Context, *contacter.Repository, *MongoStore, *mongo.Collection) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("requires the isolated Mongo test service")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	dbName := fmt.Sprintf("examplehost_waitlist_comms_test_%d", time.Now().UnixNano())
	handler, err := repositoryhelpers.NewHandler(repositoryhelpers.DefaultConfig(uri, dbName))
	require.NoError(t, err)
	repo := contacter.NewRepository(repository.NewMongoDbRepositoryWithDefaults(handler, dbName))
	collection, err := repo.GetCommsCollection(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		require.NoError(t, collection.Database().Drop(cleanupCtx))
		require.NoError(t, handler.Close(cleanupCtx))
	})
	return ctx, repo, NewMongoStore(collection.Database()), collection
}

func waitlistCommsRequest(email string) *contacter.CreateCommsRequest {
	return &contacter.CreateCommsRequest{FullName: "Waitlist subscriber", Email: email, Type: CommsType, Message: "Interested in early access, please reach out to me."}
}

// Stateful exception: creation, concurrent replay, admin edits and unsubscribe
// must operate on one persisted record to prove preservation across retries.
func TestCommsWaitlistConcurrentRetriesPreserveAudienceAndAdminData(t *testing.T) {
	ctx, repo, audience, collection := commsTestDatabase(t)
	service := NewCommsService(repo, audience)
	require.Equal(t, "Waitlist", service.CommsTypes()[CommsType])
	first, err := service.CreateComms(ctx, waitlistCommsRequest(" Person@Example.invalid "))
	require.NoError(t, err)
	entries, err := audience.Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	var original contacter.Comms
	require.NoError(t, collection.FindOne(ctx, bson.M{"email": "person@example.invalid"}).Decode(&original))
	require.Equal(t, CommsType, original.Type)
	require.Equal(t, ConsentVersion, original.Meta["consent_version"])
	require.NotEmpty(t, original.NanoId)
	require.NotEmpty(t, original.CreatedAt)
	_, err = collection.UpdateOne(ctx, bson.M{"_id": original.Id}, bson.M{"$set": bson.M{"admin_notes": "Private note", "admin_reply": "Private reply"}})
	require.NoError(t, err)
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for range 16 {
		wg.Go(func() {
			receipt, err := service.CreateComms(ctx, waitlistCommsRequest("person@example.invalid"))
			if err == nil {
				raw, marshalErr := json.Marshal(receipt)
				firstRaw, _ := json.Marshal(first)
				err = marshalErr
				if err == nil && string(raw) != string(firstRaw) {
					err = errors.New("new and duplicate public receipts differ")
				}
			}
			failures <- err
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	count, err := collection.CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	var saved contacter.Comms
	require.NoError(t, collection.FindOne(ctx, bson.M{"_id": original.Id}).Decode(&saved))
	require.Equal(t, "Private note", saved.AdminNotes)
	require.Equal(t, "Private reply", saved.AdminReply)
	require.Equal(t, original.CreatedAt, saved.CreatedAt)
	require.Equal(t, original.NanoId, saved.NanoId)
	raw, err := json.Marshal(first)
	require.NoError(t, err)
	for _, private := range []string{"person@example.invalid", "Private note", "Private reply", original.Id, original.CreatedAt} {
		require.NotContains(t, string(raw), private)
	}
	after, err := audience.Export(ctx)
	require.NoError(t, err)
	require.Equal(t, entries, after)
	// Admin queries see the same saved record through the normal contact service.
	listed, err := service.GetComms(ctx, &contacter.GetCommsRequest{WithTypes: "waitlist"})
	require.NoError(t, err)
	require.Len(t, listed.Comms, 1)
	require.Equal(t, original.Id, listed.Comms[0].Id)
	// A repeated comm must not restore someone who unsubscribed or reallocate their place.
	token := tokenHash("test-unsubscribe-token")
	_, err = audience.Claim(ctx, entries[0], "test-announcement", token)
	require.NoError(t, err)
	require.NoError(t, audience.Unsubscribe(ctx, token))
	_, err = service.CreateComms(ctx, waitlistCommsRequest("person@example.invalid"))
	require.NoError(t, err)
	after, err = audience.Export(ctx)
	require.NoError(t, err)
	require.Empty(t, after)
}

// Stateful exception: the compatibility endpoint is repeated against one store
// so its audience and contact cardinality can be compared after both writes.
func TestLegacyWaitlistSignupAlsoCreatesCommunication(t *testing.T) {
	ctx, repo, audience, collection := commsTestDatabase(t)
	legacy := NewCommsSignupStore(NewCommsService(repo, audience), audience)
	require.NoError(t, legacy.Join(ctx, "legacy@example.invalid"))
	require.NoError(t, legacy.Join(ctx, "legacy@example.invalid"))
	count, err := collection.CountDocuments(ctx, bson.M{"type": CommsType})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	entries, err := legacy.Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

type unavailableAudience struct{ Store }

func (unavailableAudience) Join(context.Context, string) error {
	return errors.New("audience unavailable")
}

// Stateful exception: the second service repairs the partial write deliberately
// left by the first; resetting fixtures between phases would erase the scenario.
func TestCommsWaitlistRetryRepairsIncompleteEnrollment(t *testing.T) {
	ctx, repo, audience, collection := commsTestDatabase(t)
	failed := NewCommsService(repo, unavailableAudience{audience})
	_, err := failed.CreateComms(ctx, waitlistCommsRequest("retry@example.invalid"))
	require.ErrorContains(t, err, "audience unavailable")
	entries, err := audience.Export(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = NewCommsService(repo, audience).CreateComms(ctx, waitlistCommsRequest("retry@example.invalid"))
	require.NoError(t, err)
	count, err := collection.CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	entries, err = audience.Export(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestCommsOrdinaryContactStaysSeparateAndInvalidWaitlistIsRejected(t *testing.T) {
	ctx, repo, audience, collection := commsTestDatabase(t)
	service := NewCommsService(repo, audience)
	for _, tc := range []struct{ name, email, message string }{
		{"invalid address", "not-an-email", "Notify me"},
		{"missing copy", "person@example.invalid", "  "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := waitlistCommsRequest(tc.email)
			req.Message = tc.message
			_, err := service.CreateComms(ctx, req)
			require.ErrorIs(t, err, contacter.ErrInvalidCommsPayload)
		})
	}
	count, err := collection.CountDocuments(ctx, bson.M{})
	require.NoError(t, err)
	require.Zero(t, count)
	req := waitlistCommsRequest("contact@example.invalid")
	req.Type = contacter.CommsTypeOther
	req.Meta = map[string]interface{}{"subject": "A regular question", "displayed_as": "Other"}
	response, err := service.CreateComms(ctx, req)
	require.NoError(t, err)
	require.NotEmpty(t, response.Comms.Id)
	require.Equal(t, req.Meta, response.Comms.Meta)
	entries, err := audience.Export(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)
	count, err = collection.CountDocuments(ctx, bson.M{"type": contacter.CommsTypeOther})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
}
