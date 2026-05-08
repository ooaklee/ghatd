package notifier

import (
	"context"
	"fmt"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDbStore represents the datastore methods required by notifier.
type MongoDbStore interface {
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository manages notifier persistence.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	addressesCollection        *mongo.Collection
	addressesCollectionMutex   sync.Mutex
	preferencesCollection      *mongo.Collection
	preferencesCollectionMutex sync.Mutex
}

// NewRepository creates a notifier repository.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit overrides collection initialisation retry attempts.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}
	return r
}

// GetNotificationAddressesCollection returns the notification addresses collection.
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

// GetNotificationPreferencesCollection returns the notification preferences collection.
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

// UpsertAddress registers or moves a notification address to the provided user.
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

// GetActiveAddressesByUserID retrieves active addresses for a user.
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

// GetAddressesByUserID retrieves all addresses for a user.
func (r *Repository) GetAddressesByUserID(ctx context.Context, userID string) ([]NotificationAddress, error) {
	return r.findAddresses(ctx, bson.M{"user_id": userID})
}

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

// DeleteAddressByIDForUser deletes a user-owned notification address.
func (r *Repository) DeleteAddressByIDForUser(ctx context.Context, userID, addressID string) error {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return err
	}

	result, err := collection.DeleteOne(ctx, bson.M{"_id": addressID, "user_id": userID})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}
	if result.DeletedCount == 0 {
		return ErrNotificationAddressNotFound
	}

	return nil
}

// DeleteAddressesByUserID deletes all notification addresses for a user.
func (r *Repository) DeleteAddressesByUserID(ctx context.Context, userID string) error {
	collection, err := r.GetNotificationAddressesCollection(ctx)
	if err != nil {
		return err
	}

	if _, err := collection.DeleteMany(ctx, bson.M{"user_id": userID}); err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}

// GetPreferencesByUserID retrieves a user's preferences.
func (r *Repository) GetPreferencesByUserID(ctx context.Context, userID string) (*NotificationPreferences, error) {
	collection, err := r.GetNotificationPreferencesCollection(ctx)
	if err != nil {
		return nil, err
	}

	var preferences NotificationPreferences
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{"_id": userID}, &preferences, "notification_preferences", false, ErrNotificationAddressNotFound)
	if err != nil {
		return nil, err
	}

	return &preferences, nil
}

// UpsertPreferences creates or updates a user's preferences.
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

// DeletePreferencesByUserID deletes preferences for a user.
func (r *Repository) DeletePreferencesByUserID(ctx context.Context, userID string) error {
	collection, err := r.GetNotificationPreferencesCollection(ctx)
	if err != nil {
		return err
	}

	if _, err := collection.DeleteOne(ctx, bson.M{"_id": userID}); err != nil {
		return fmt.Errorf("%w: %v", ErrDatabaseError, err)
	}

	return nil
}
