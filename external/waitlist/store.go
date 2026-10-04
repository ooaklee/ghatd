// Package waitlist stores the prerelease audience and consent records.
package waitlist

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	// CampaignID is the single prerelease campaign identity, including UUID seeds.
	CampaignID = "prerelease-v1"
	// ConsentVersion records the signup promise; changing it is not renewed consent.
	ConsentVersion = "prerelease-v1"
)

// Entry is a persisted canonical address plus admin-only delivery projections.
// Export excludes unsubscribed addresses without erasing their original consent.
type Entry struct {
	// Sequence is a stable positive enrollment ordinal when enabled; zero means unassigned.
	Sequence int64 `bson:"sequence,omitempty"`
	// ID is the lowercase SHA-256 of Email and remains stable across retries.
	ID string `bson:"_id"`
	// Email is the validated lowercase address; never exposed by public signup.
	Email string `bson:"email"`
	// JoinedAt is the first successful enrollment time, preserved on retries.
	JoinedAt time.Time `bson:"joinedAt"`
	// ConsentVersion identifies the original prerelease communication promise.
	ConsentVersion string `bson:"consentVersion"`
	// Source identifies the trusted enrollment path, currently "landing".
	Source string `bson:"source"`
	// AnnouncementState is projected from delivery records, not stored in signup.
	AnnouncementState string `bson:"-"`
	// ProviderMessageID is an acceptance reference, not proof of delivery.
	ProviderMessageID string `bson:"-"`
}

// Store enrolls and exports the audience without sending email. Implementations
// must make Join idempotent and must not reverse an existing unsubscribe.
type Store interface {
	// Join validates and durably enrolls one address; any error means unconfirmed.
	Join(context.Context, string) error
	// Export returns only currently eligible addresses for administrative use.
	Export(context.Context) ([]Entry, error)
}

// MongoStore uses the host's managed database without owning its client lifecycle.
// Collection names and majority-write semantics are stable migration contracts.
type MongoStore struct {
	config        StoreConfig
	counters      *mongo.Collection
	sequenceReady atomic.Bool
	signups       *mongo.Collection
	previews      *mongo.Collection
	dispatch      *mongo.Collection
	deliveries    *mongo.Collection
}

// NewMongoStore binds the four existing collections on a non-nil database. It
// performs no I/O; call Initialize explicitly before serving unsubscribe traffic.
// As with the original constructor, passing a nil database is a programming error.
func NewMongoStore(db *mongo.Database) *MongoStore {
	store, err := NewMongoStoreWithConfig(db, StoreConfig{})
	if err != nil {
		panic(err)
	}
	return store
}

// NewMongoStoreWithConfig validates trusted settings without I/O. Initialize must
// succeed before sequenced enrollment; the host retains the database lifecycle.
func NewMongoStoreWithConfig(db *mongo.Database, config StoreConfig) (*MongoStore, error) {
	if db == nil {
		return nil, errors.New("waitlist requires a database")
	}
	version, err := consentVersion(config.ConsentVersion)
	if err != nil {
		return nil, err
	}
	config.ConsentVersion = version
	opts := options.Collection().SetWriteConcern(writeconcern.Majority())
	return &MongoStore{
		config:     config,
		counters:   db.Collection("waitlist_counters", opts),
		signups:    db.Collection("waitlist_signups", opts),
		previews:   db.Collection("waitlist_announcement_previews", opts),
		dispatch:   db.Collection("waitlist_announcement", opts),
		deliveries: db.Collection("waitlist_deliveries", opts),
	}, nil
}

// Initialize bounds the public unsubscribe lookup as the audience grows.
func (s *MongoStore) Initialize(ctx context.Context) error {
	_, err := s.deliveries.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "unsubscribeHash", Value: 1}},
		Options: options.Index().SetName("waitlist_unsubscribe_hash"),
	})
	if err != nil {
		return err
	}
	if s.config.SequenceEnrollment {
		return s.initializeSequence(ctx)
	}
	return nil
}

// Join records each canonical address once. Duplicate requests preserve consent
// and the original timestamp; they never reverse an unsubscribe.
func (s *MongoStore) Join(ctx context.Context, email string) error {
	address, valid := canonicalEmail(email)
	if !valid {
		return errors.New("invalid waitlist email")
	}
	digest := sha256.Sum256([]byte(address))
	entry := Entry{ID: hex.EncodeToString(digest[:]), Email: address, JoinedAt: time.Now().UTC(), ConsentVersion: s.config.ConsentVersion, Source: "landing"}
	if s.config.SequenceEnrollment {
		return s.joinSequenced(ctx, entry)
	}
	result, err := s.signups.UpdateOne(ctx, bson.M{"_id": entry.ID}, bson.M{"$setOnInsert": entry}, options.UpdateOne().SetUpsert(true))
	if mongo.IsDuplicateKeyError(err) {
		return s.signups.FindOne(ctx, bson.M{"_id": entry.ID}).Err()
	}
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged {
		return errors.New("waitlist signup was not acknowledged")
	}
	return nil
}

// Export is used only by the authenticated admin route when preparing launch
// communications. It does not send email.
func (s *MongoStore) Export(ctx context.Context) ([]Entry, error) {
	var entries []Entry
	cursor, err := s.signups.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "joinedAt", Value: 1}, {Key: "_id", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	for cursor.Next(ctx) {
		var entry Entry
		if err := cursor.Decode(&entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	deliveries, err := s.deliveryMetadata(ctx)
	if err != nil {
		return nil, err
	}
	active := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		metadata := deliveries[entry.ID]
		if !metadata.Unsubscribed {
			entry.AnnouncementState, entry.ProviderMessageID = metadata.State, metadata.MessageID
			active = append(active, entry)
		}
	}
	return active, nil
}
