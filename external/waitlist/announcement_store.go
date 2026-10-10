package waitlist

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// SavePreview inserts immutable prepared copy; duplicate IDs are errors, not edits.
func (s *MongoStore) SavePreview(ctx context.Context, a Announcement) error {
	_, err := s.previews.InsertOne(ctx, a)
	return err
}

// Preview reads a previously saved preview; missing records remain unconfirmed.
func (s *MongoStore) Preview(ctx context.Context, id string) (Announcement, error) {
	var a Announcement
	err := s.previews.FindOne(ctx, bson.M{"_id": id}).Decode(&a)
	return a, err
}

// Started reads the singleton campaign without synthesizing a default proposal.
func (s *MongoStore) Started(ctx context.Context) (*Announcement, error) {
	var record struct {
		Announcement Announcement `bson:"announcement"`
	}
	err := s.dispatch.FindOne(ctx, bson.M{"_id": CampaignID}).Decode(&record)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record.Announcement, nil
}

// Start atomically freezes the first preview and permits only its exact ID on
// retries. A competing preview cannot replace already-authorized campaign copy.
func (s *MongoStore) Start(ctx context.Context, a Announcement) error {
	_, err := s.dispatch.UpdateOne(ctx, bson.M{"_id": CampaignID}, bson.M{"$setOnInsert": bson.M{"announcement": a}}, options.UpdateOne().SetUpsert(true))
	if err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	active, err := s.Started(ctx)
	if err != nil {
		return err
	}
	if active == nil || active.ID != a.ID {
		return ErrAnnouncementFrozen
	}
	return nil
}

// Claim persists an at-most-once dispatch claim before provider I/O. Existing
// claims, including uncertain outcomes, are never reset for an automatic resend.
func (s *MongoStore) Claim(ctx context.Context, e Entry, previewID, unsubscribeHash string) (bool, error) {
	_, err := s.deliveries.InsertOne(ctx, bson.M{"_id": e.ID, "previewID": previewID, "unsubscribeHash": unsubscribeHash, "state": "sending", "startedAt": time.Now().UTC()})
	if mongo.IsDuplicateKeyError(err) {
		return false, nil
	}
	return err == nil, err
}

// Finish records a provider outcome only for a current sending claim, preserving
// its identity and unsubscribe data. A write error does not establish rollback.
func (s *MongoStore) Finish(ctx context.Context, id, state, messageID string) error {
	_, err := s.deliveries.UpdateOne(ctx, bson.M{"_id": id, "state": "sending"}, bson.M{"$set": bson.M{"state": state, "messageID": messageID, "finishedAt": time.Now().UTC()}})
	return err
}

// DeliveryStates returns durable state independently of current subscription.
func (s *MongoStore) DeliveryStates(ctx context.Context) (map[string]string, error) {
	states := map[string]string{}
	cursor, err := s.deliveries.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1, "state": 1}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	for cursor.Next(ctx) {
		var record struct {
			ID    string `bson:"_id"`
			State string `bson:"state"`
		}
		if err := cursor.Decode(&record); err != nil {
			return nil, err
		}
		states[record.ID] = record.State
	}
	return states, cursor.Err()
}

// Unsubscribe suppresses matching delivery identities without looking up an email
// or revealing membership. It neither deletes history nor reverses consent later.
func (s *MongoStore) Unsubscribe(ctx context.Context, hash string) error {
	// No address lookup or membership disclosure. Repeated requests are harmless.
	_, err := s.deliveries.UpdateOne(ctx, bson.M{"unsubscribeHash": hash}, bson.M{"$set": bson.M{"unsubscribed": true}})
	return err
}

// deliveryMetadata is the stored per-recipient projection used for state
// summaries: delivery state, provider message ID and unsubscribe flag.
type deliveryMetadata struct {
	ID           string `bson:"_id"`
	State        string `bson:"state"`
	MessageID    string `bson:"messageID"`
	Unsubscribed bool   `bson:"unsubscribed"`
}

// deliveryMetadata loads all delivery records projected to identity, state,
// message ID and unsubscribe flag, keyed by record ID.
func (s *MongoStore) deliveryMetadata(ctx context.Context) (map[string]deliveryMetadata, error) {
	cursor, err := s.deliveries.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 1, "state": 1, "messageID": 1, "unsubscribed": 1}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	ids := map[string]deliveryMetadata{}
	for cursor.Next(ctx) {
		var record deliveryMetadata
		if err := cursor.Decode(&record); err != nil {
			return nil, err
		}
		ids[record.ID] = record
	}
	return ids, cursor.Err()
}
