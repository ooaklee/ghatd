package notifier

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDbStore describes the MongoDB operations that the notifier repository
// needs to perform.
//
// This is a subset of the full data store interface – only the read, write,
// and collection-management methods that notifier actually uses.
type MongoDbStore interface {
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository manages notifier data in MongoDB.
//
// A Repository owns two MongoDB collections:
//
//   - notification_addresses – stores each user's registered devices.
//   - notification_preferences – stores each user's notification choices.
//
// Collection access is lazy: the first time a method needs a collection,
// the repository connects to MongoDB. On transient failures, it retries
// up to collectionInitMaxAttemptsLimit times before giving up.
//
// All methods return sentinel errors from the notifier package (e.g.
// ErrDatabaseError, ErrNotificationAddressNotFound) so callers can use
// errors.Is() to identify the kind of failure.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	addressesCollection        *mongo.Collection
	addressesCollectionMutex   sync.Mutex
	preferencesCollection      *mongo.Collection
	preferencesCollectionMutex sync.Mutex
}

// NewRepository creates a notifier repository backed by the given MongoDB store.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit sets how many times the repository
// will retry collection initialisation before giving up.
//
// This is useful in environments where the database takes a moment to
// be ready after the application starts.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}
	return r
}

// GetNotificationAddressesCollection returns the notification addresses
// MongoDB collection, initialising it on first access.
func (r *Repository) GetNotificationAddressesCollection(ctx context.Context) (*mongo.Collection, error) {
	r.addressesCollectionMutex.Lock()
	defer r.addressesCollectionMutex.Unlock()

	if r.addressesCollection != nil {
		return r.addressesCollection, nil
	}

	collection, err := r.getCollection(ctx, NotificationAddressesCollection)
	if err != nil {
		return nil, err
	}
	r.addressesCollection = collection
	return r.addressesCollection, nil
}

// GetNotificationPreferencesCollection returns the notification preferences
// MongoDB collection, initialising it on first access.
func (r *Repository) GetNotificationPreferencesCollection(ctx context.Context) (*mongo.Collection, error) {
	r.preferencesCollectionMutex.Lock()
	defer r.preferencesCollectionMutex.Unlock()

	if r.preferencesCollection != nil {
		return r.preferencesCollection, nil
	}

	collection, err := r.getCollection(ctx, NotificationPreferencesCollection)
	if err != nil {
		return nil, err
	}
	r.preferencesCollection = collection
	return r.preferencesCollection, nil
}

// getCollection initialises a MongoDB collection by name with retry logic.
// On transient failures (no client yet, database not available), it retries
// up to collectionInitMaxAttemptsLimit times.
func (r *Repository) getCollection(ctx context.Context, name string) (*mongo.Collection, error) {
	var lastErr error
	collectionInitMaxAttemptsLimit := r.collectionInitMaxAttemptsLimit
	if collectionInitMaxAttemptsLimit <= 0 {
		collectionInitMaxAttemptsLimit = defaultCollectionInitMaxAttemptsLimit
	}

	for attempt := 1; attempt <= collectionInitMaxAttemptsLimit; attempt++ {
		_, err := r.Store.InitialiseClient(ctx)
		if err != nil {
			lastErr = err
			continue
		}

		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			continue
		}

		return db.Collection(name), nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, name, collectionInitMaxAttemptsLimit, lastErr)
}

// UpsertAddress saves a notification address using a find-and-upsert
// strategy keyed by (channel, address_hash).
//
// The unique index on (channel, address_hash) ensures that the same
// browser or mobile device always belongs to the latest user who
// signed in, without creating duplicate records.
//
// The update sets all the mutable fields (user ID, status, device info,
// timestamps, channel-specific payload) and uses $setOnInsert for the
// fields that should not change after the first save (ID, address_hash,
// created_at).
//
// Channel-specific payloads are exclusive: when upserting a WEBPUSH
// address, any previous FCM payload on the document is unset, and
// vice versa.
func (r *Repository) UpsertAddress(ctx context.Context, address *NotificationAddress) (*NotificationAddress, error) {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return nil, err
	}

	setFields := bson.M{
		"user_id":               address.UserID,
		"channel":               address.Channel,
		"status":                address.Status,
		"device_id":             address.DeviceID,
		"device_name":           address.DeviceName,
		"platform":              address.Platform,
		"metadata.updated_at":   address.Metadata.UpdatedAt,
		"metadata.last_seen_at": address.Metadata.LastSeenAt,
	}

	unsetFields := bson.M{}
	switch address.Channel {
	case NotificationChannelWebPush:
		setFields["webpush"] = address.WebPush
		unsetFields["fcm"] = ""
	case NotificationChannelFCM:
		setFields["fcm"] = address.FCM
		unsetFields["webpush"] = ""
	}

	update := bson.M{
		"$set": setFields,
		"$setOnInsert": bson.M{
			"_id":                 address.ID,
			"address_hash":        address.AddressHash,
			"metadata.created_at": address.Metadata.CreatedAt,
		},
	}
	if len(unsetFields) > 0 {
		update["$unset"] = unsetFields
	}

	filter := bson.M{
		"channel":      address.Channel,
		"address_hash": address.AddressHash,
	}

	var result NotificationAddress
	err = collection.FindOneAndUpdate(
		ctx,
		filter,
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return &result, nil
}

// GetActiveAddressesByUserID returns only the active addresses for a user,
// optionally filtered to specific channels.
//
// The results include the full address data (endpoints and tokens) because
// this method is used by the NotifyUser flow to actually deliver push
// messages. It is never exposed directly to clients.
func (r *Repository) GetActiveAddressesByUserID(ctx context.Context, userID string, channels ...NotificationChannel) ([]NotificationAddress, error) {
	filter := bson.M{
		"user_id": userID,
		"status":  NotificationAddressStatusActive,
	}
	if len(channels) > 0 {
		filter["channel"] = bson.M{"$in": channels}
	}

	return r.findAddresses(ctx, filter)
}

// GetAddressesByUserID returns all addresses for a user, regardless of
// status (ACTIVE and DISABLED).
//
// This is used by ListUserAddresses to give the user a complete picture
// of their registered devices. The caller must sanitise the results
// before returning them to the client.
func (r *Repository) GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error) {
	return r.findAddresses(ctx, bson.M{"user_id": userID})
}

// findAddresses runs a find query against the notification_addresses
// collection and returns the results sorted by most-recently-updated first.
func (r *Repository) findAddresses(ctx context.Context, filter bson.M) ([]NotificationAddress, error) {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return nil, err
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, options.Find().SetSort(bson.D{{Key: "metadata.updated_at", Value: -1}}))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}
	defer cursor.Close(ctx)

	var results []NotificationAddress
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &results, "notification_addresses"); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return results, nil
}

// DeleteAddressByIDForUser deletes a single notification address.
//
// The filter includes both the address ID and user ID, so a user cannot
// delete another user's addresses by guessing address IDs. If no document
// matches the filter, the method returns ErrNotificationAddressNotFound.
func (r *Repository) DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{"_id": addressID, "user_id": userID}
	var address NotificationAddress
	if err := r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &address, "notification_address", false, ErrNotificationAddressNotFound); err != nil {
		if !errors.Is(err, ErrNotificationAddressNotFound) {
			return fmt.Errorf("%w: %v", ErrDatabaseError, err)
		}
		return err
	}

	if err := r.Store.ExecuteDeleteOneCommand(ctx, collection, queryFilter, "notification_address"); err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}

// DeleteAddressesByUserID deletes every notification address belonging
// to a user. This is used during account cleanup when a user is deleted.
func (r *Repository) DeleteAddressesByUserID(ctx context.Context, userID string) error {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return err
	}

	if err := r.Store.ExecuteDeleteManyCommand(ctx, collection, bson.M{"user_id": userID}, "notification_addresses"); err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}

// DisableAddressByHash sets the status of the address with the given
// hash to DISABLED.  The address record is kept so the device can
// re-register without creating a duplicate.  If no address matches the
// hash this method is a no-op (returns nil).
func (r *Repository) DisableAddressByHash(ctx context.Context, hash string) error {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return err
	}

	now := toolbox.TimeNowUTC()
	update := bson.M{
		"$set": bson.M{
			"status":              NotificationAddressStatusDisabled,
			"metadata.updated_at": now,
		},
	}

	_, err = collection.UpdateOne(ctx, bson.M{"address_hash": hash}, update)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}

// GetPreferencesByUserID retrieves a user's notification preferences
// document, keyed by user ID.
//
// If no preferences document exists yet, the method returns
// ErrNotificationAddressNotFound. The service layer handles this by
// returning default preferences instead of an error to the caller.
func (r *Repository) GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error) {
	collection, err := r.GetNotificationPreferencesCollection(ctx)
	if err != nil {
		return nil, err
	}

	var preferences NotificationPreferences
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": userID}, &preferences, "notification_preferences", false, ErrNotificationAddressNotFound)
	if err != nil {
		if !errors.Is(err, ErrNotificationAddressNotFound) {
			return nil, fmt.Errorf("%w: %v", ErrDatabaseError, err)
		}
		return nil, err
	}

	return &preferences, nil
}

// UpsertPreferences creates or updates a user's notification preferences
// document.
//
// The document is keyed by user ID (_id = user ID) so each user can
// have at most one preferences document. The update uses $setOnInsert
// for the creation timestamp so it is only recorded once.
func (r *Repository) UpsertPreferences(ctx context.Context, preferences *NotificationPreferences) (*NotificationPreferences, error) {
	collection, err := r.GetNotificationPreferencesCollection(ctx)
	if err != nil {
		return nil, err
	}

	update := bson.M{
		"$set": bson.M{
			"enabled":             preferences.Enabled,
			"channels":            preferences.Channels,
			"metadata.updated_at": preferences.Metadata.UpdatedAt,
		},
		"$setOnInsert": bson.M{
			"_id":                 preferences.UserID,
			"metadata.created_at": preferences.Metadata.CreatedAt,
		},
	}

	var result NotificationPreferences
	err = collection.FindOneAndUpdate(
		ctx,
		bson.M{"_id": preferences.UserID},
		update,
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return &result, nil
}

// DeletePreferencesByUserID deletes a user's notification preferences
// document. This is used during account cleanup when a user is deleted.
func (r *Repository) DeletePreferencesByUserID(ctx context.Context, userID string) error {
	collection, err := r.GetNotificationPreferencesCollection(ctx)
	if err != nil {
		return err
	}

	if err := r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": userID}, "notification_preferences"); err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}
