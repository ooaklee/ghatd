package reminder

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore describes the MongoDB helper operations the reminder repository uses.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, update interface{}, resultObjectName string) error
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository manages reminder declarations and execution records in MongoDB.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection           *mongo.Collection
	collectionMutex      sync.Mutex
	executionsCollection *mongo.Collection
	executionsMutex      sync.Mutex
}

var _ ReminderRepository = (*Repository)(nil)

// NewRepository returns a reminder repository backed by the provided MongoDB store.
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

// GetReminderCollection returns the reminder declarations collection, initialising it lazily.
func (r *Repository) GetReminderCollection(ctx context.Context) (*mongo.Collection, error) {
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		return r.collection, nil
	}

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

		r.collection = db.Collection(ReminderCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, ReminderCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// GetReminderExecutionsCollection returns the execution tracking collection, initialising it lazily.
func (r *Repository) GetReminderExecutionsCollection(ctx context.Context) (*mongo.Collection, error) {
	r.executionsMutex.Lock()
	defer r.executionsMutex.Unlock()

	if r.executionsCollection != nil {
		return r.executionsCollection, nil
	}

	collection, err := r.getCollection(ctx, ReminderExecutionsCollection)
	if err != nil {
		return nil, err
	}

	r.executionsCollection = collection
	return r.executionsCollection, nil
}

func (r *Repository) getCollection(ctx context.Context, collectionName string) (*mongo.Collection, error) {
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

		return db.Collection(collectionName), nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrDatabaseError, collectionName, collectionInitMaxAttemptsLimit, lastErr)
}

// CreateReminder persists a new reminder declaration, generating missing IDs and timestamps.
func (r *Repository) CreateReminder(ctx context.Context, reminder *Reminder) (*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	if reminder.Id == "" {
		reminder.GenerateId()
	}
	if reminder.NanoId == "" {
		reminder.GenerateNanoId()
	}
	if reminder.CreatedAt == "" {
		reminder.SetCreatedAtTimeToNow()
	}
	if reminder.Status == "" {
		reminder.Status = ReminderStatusActive
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, reminder, "reminder")
	if err != nil {
		return nil, err
	}

	return reminder, nil
}

// GetReminderByID retrieves one reminder declaration by its platform ID.
func (r *Repository) GetReminderByID(ctx context.Context, id string) (*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	filter := bson.M{"_id": id}

	var result Reminder
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, filter, &result, "reminder", false, ErrResourceNotFound)
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// ReminderFilter captures Mongo filters shared by reminder list and count queries.
type ReminderFilter struct {
	UserID     string
	UserIDs    []string
	Status     string
	TargetType string
	TargetId   string
	IDs        []string
	DueBefore  string
}

// ReminderExecutionFilter captures Mongo filters for execution tracking queries.
type ReminderExecutionFilter struct {
	ReminderId string
	UserID     string
	Status     ReminderExecutionStatus
	TargetType string
	TargetId   string
}

func buildReminderListFilter(req *ReminderFilter) bson.M {
	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req == nil {
		return queryFilter
	}

	userIDs := normaliseReminderUserIDs(req.UserID, req.UserIDs)
	if len(userIDs) == 1 {
		queryFilter["user_id"] = userIDs[0]
	} else if len(userIDs) > 1 {
		queryFilter["user_id"] = bson.M{"$in": userIDs}
	}

	if req.Status != "" {
		queryFilter["status"] = req.Status
	}

	if req.TargetType != "" {
		queryFilter["target_type"] = req.TargetType
	}

	if req.TargetId != "" {
		queryFilter["target_id"] = req.TargetId
	}

	if len(req.IDs) > 0 {
		queryFilter["_id"] = bson.M{"$in": req.IDs}
	}

	if req.DueBefore != "" {
		queryFilter["next_due_at"] = bson.M{"$lte": req.DueBefore}
	}

	return queryFilter
}

func buildReminderExecutionFilter(req *ReminderExecutionFilter) bson.M {
	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req == nil {
		return queryFilter
	}

	if req.ReminderId != "" {
		queryFilter["reminder_id"] = req.ReminderId
	}
	if req.UserID != "" {
		queryFilter["user_id"] = req.UserID
	}
	if req.Status != "" {
		queryFilter["status"] = req.Status
	}
	if req.TargetType != "" {
		queryFilter["target_type"] = req.TargetType
	}
	if req.TargetId != "" {
		queryFilter["target_id"] = req.TargetId
	}

	return queryFilter
}

func buildReminderPaginationOptions(page, perPage int) *options.FindOptionsBuilder {
	if page <= 0 {
		page = 1
	}
	if perPage <= 0 {
		perPage = 25
	}

	return options.Find().
		SetSkip(int64((page - 1) * perPage)).
		SetLimit(int64(perPage)).
		SetSort(bson.D{{Key: "target_time", Value: 1}, {Key: "created_at", Value: -1}})
}

// ListReminders returns reminder declarations matching the supplied filters.
func (r *Repository) ListReminders(ctx context.Context, userID string, status string, targetType string, targetId string, page, perPage int) ([]*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	filter := buildReminderListFilter(&ReminderFilter{
		UserID:     userID,
		Status:     status,
		TargetType: targetType,
		TargetId:   targetId,
	})

	findOptions := buildReminderPaginationOptions(page, perPage)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, findOptions)
	if err != nil {
		return nil, err
	}

	var reminders []*Reminder
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &reminders, "reminders"); err != nil {
		return nil, err
	}

	return reminders, nil
}

// GetRemindersForTargetTypeByUserID returns a user's reminders for a target type and optional target ID.
func (r *Repository) GetRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*Reminder, error) {
	return r.listReminders(ctx, &ReminderFilter{
		UserID:     userID,
		TargetType: targetType,
		TargetId:   targetId,
	}, page, perPage)
}

// GetActiveRemindersForTargetTypeByUserID returns active reminders for a user's target type.
func (r *Repository) GetActiveRemindersForTargetTypeByUserID(ctx context.Context, userID string, targetType string, targetId string, page, perPage int) ([]*Reminder, error) {
	return r.listReminders(ctx, &ReminderFilter{
		UserID:     userID,
		Status:     string(ReminderStatusActive),
		TargetType: targetType,
		TargetId:   targetId,
	}, page, perPage)
}

// UpdateReminderByID replaces one reminder declaration by ID.
func (r *Repository) UpdateReminderByID(ctx context.Context, reminder *Reminder) (*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	reminder.SetUpdatedAtTimeToNow()

	filter := bson.M{"_id": reminder.Id}
	update := bson.M{"$set": reminder}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, filter, update, "reminder")
	if err != nil {
		return nil, err
	}

	return reminder, nil
}

// PatchReminder applies a partial update to one reminder declaration by ID.
func (r *Repository) PatchReminder(ctx context.Context, id string, update map[string]interface{}) error {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return err
	}

	update["updated_at"] = toolboxTimeNow()

	filter := bson.M{"_id": id}
	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, filter, bson.M{"$set": update}, "reminder")
	if err != nil {
		return err
	}

	return nil
}

func toolboxTimeNow() string {
	return toolbox.TimeNowUTC()
}

// DeleteReminderByID removes one reminder declaration by ID.
func (r *Repository) DeleteReminderByID(ctx context.Context, id string) error {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": id}
	err = r.Store.ExecuteDeleteOneCommand(ctx, collection, filter, "reminder")
	if err != nil {
		return err
	}

	return nil
}

func (r *Repository) listReminders(ctx context.Context, filter *ReminderFilter, page, perPage int) ([]*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	findOptions := buildReminderPaginationOptions(page, perPage)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildReminderListFilter(filter), findOptions)
	if err != nil {
		return nil, err
	}

	var reminders []*Reminder
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &reminders, "reminders"); err != nil {
		return nil, err
	}

	return reminders, nil
}

// CountReminders counts reminder declarations matching the provided filter.
func (r *Repository) CountReminders(ctx context.Context, filter *ReminderFilter) (int64, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return 0, err
	}

	queryFilter := buildReminderListFilter(filter)
	count, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetDueReminders returns reminders whose next due time is due on or before the supplied UTC timestamp.
func (r *Repository) GetDueReminders(ctx context.Context, filter *ReminderFilter, limit int64) ([]*Reminder, error) {
	collection, err := r.GetReminderCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := buildReminderListFilter(filter)

	findOptions := options.Find().
		SetSort(bson.D{{Key: "next_due_at", Value: 1}, {Key: "target_time", Value: 1}})

	if limit > 0 {
		findOptions.SetLimit(limit)
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	var reminders []*Reminder
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &reminders, "reminders"); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no-documents-found") {
			return []*Reminder{}, nil
		}
		return nil, err
	}

	return reminders, nil
}

// CreateReminderExecution persists one scheduler or notification attempt record.
func (r *Repository) CreateReminderExecution(ctx context.Context, execution *ReminderExecution) (*ReminderExecution, error) {
	collection, err := r.GetReminderExecutionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	if execution.Id == "" {
		execution.GenerateId()
	}
	if execution.NanoId == "" {
		execution.GenerateNanoId()
	}
	if execution.CreatedAt == "" {
		execution.SetCreatedAtTimeToNow()
	}
	if execution.Status == "" {
		execution.Status = ReminderExecutionStatusPending
	}
	if execution.Attempt <= 0 {
		execution.Attempt = 1
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, execution, "reminder_execution")
	if err != nil {
		return nil, err
	}

	return execution, nil
}

// ListReminderExecutions returns execution tracking records for the provided filters.
func (r *Repository) ListReminderExecutions(ctx context.Context, filter *ReminderExecutionFilter, page, perPage int) ([]*ReminderExecution, error) {
	collection, err := r.GetReminderExecutionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	findOptions := buildReminderPaginationOptions(page, perPage).
		SetSort(bson.D{{Key: "scheduled_for", Value: -1}, {Key: "created_at", Value: -1}})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildReminderExecutionFilter(filter), findOptions)
	if err != nil {
		return nil, err
	}

	var executions []*ReminderExecution
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &executions, "reminder_executions"); err != nil {
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "no-documents-found") {
			return []*ReminderExecution{}, nil
		}
		return nil, err
	}

	return executions, nil
}
