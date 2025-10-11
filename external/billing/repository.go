package billing

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// BillingEventsCollection collection name for billing events
const BillingEventsCollection string = "billing_events"

// BillingSubscriptionCollection collection name for billing subscriptions
const BillingSubscriptionCollection string = "billing_subscriptions"

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold resource data
type Repository struct {
	Store MongoDbStore
}

// NewRepository initiates new instance of repository
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store: store,
	}
}

// GetBillingEventsCollection returns collection used for billing events domain
func (r *Repository) GetBillingEventsCollection(ctx context.Context) (*mongo.Collection, error) {

	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(BillingEventsCollection)

	return collection, nil
}

// GetBillingSubscriptionsCollection returns a collection used for the subscriptions domain
func (r *Repository) GetBillingSubscriptionsCollection(ctx context.Context) (*mongo.Collection, error) {

	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(BillingSubscriptionCollection)

	return collection, nil
}

// GetTotalSubscriptions handles fetching the total count of subscriptions in repository
func (r *Repository) GetTotalSubscriptions(ctx context.Context, req *GetTotalSubscriptionsRequest) (int64, error) {

	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req.IntegratorName != "" {
		queryFilter["integrator"] = req.IntegratorName
	}

	if req.IntegratorSubscriptionID != "" {
		queryFilter["integrator_subscription_id"] = req.IntegratorSubscriptionID
	}

	if req.IntegratorCustomerID != "" {
		queryFilter["integrator_customer_id"] = req.IntegratorCustomerID
	}

	if len(req.UserIDs) > 0 {
		queryFilter["user_id"] = bson.M{"$in": req.UserIDs}
	}

	if len(req.Emails) > 0 {
		standardisedProvidedEmails := standardisedEmails(req.Emails)
		queryFilter["email"] = bson.M{"$in": standardisedProvidedEmails}
	}

	if len(req.Statuses) > 0 {
		queryFilter["status"] = bson.M{"$in": req.Statuses}
	}

	if req.PlanNameContains != "" {
		queryFilter["plan_name"] = bson.M{"$regex": req.PlanNameContains, "$options": "i"}
	}

	if len(req.Currency) > 0 {
		queryFilter["currency"] = bson.M{"$in": req.Currency}
	}

	if len(req.BillingInterval) > 0 {
		queryFilter["billing_interval"] = bson.M{"$in": req.BillingInterval}
	}

	if req.CreatedAtFrom != "" {
		queryFilter["created_at"] = bson.M{"$gte": req.CreatedAtFrom}
	}

	if req.CreatedAtTo != "" {
		if _, exists := queryFilter["created_at"]; exists {
			queryFilter["created_at"].(bson.M)["$lte"] = req.CreatedAtTo
		} else {
			queryFilter["created_at"] = bson.M{"$lte": req.CreatedAtTo}
		}
	}

	if req.NextBillingDateFrom != "" {
		queryFilter["next_billing_date"] = bson.M{"$gte": req.NextBillingDateFrom}
	}

	if req.NextBillingDateTo != "" {
		if _, exists := queryFilter["next_billing_date"]; exists {
			queryFilter["next_billing_date"].(bson.M)["$lte"] = req.NextBillingDateTo
		} else {
			queryFilter["next_billing_date"] = bson.M{"$lte": req.NextBillingDateTo}
		}
	}

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// GetSubscriptions handles fetching subscriptions from repository
func (r *Repository) GetSubscriptions(ctx context.Context, req *GetSubscriptionsRequest) ([]Subscription, error) {

	var (
		result          []Subscription
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = repository.GetPaginationSkip(int64(req.Page), paginationLimit)

	// generate query filter from request
	if req.IntegratorName != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator", Value: req.IntegratorName})
	}

	if req.IntegratorSubscriptionID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator_subscription_id", Value: req.IntegratorSubscriptionID})
	}

	if req.IntegratorCustomerID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator_customer_id", Value: req.IntegratorCustomerID})
	}

	if len(req.ForUserIDs) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "user_id", Value: bson.M{"$in": req.ForUserIDs}})
	}

	if len(req.ForEmails) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "email", Value: bson.M{"$in": standardisedEmails(req.ForEmails)}})
	}

	if len(req.Statuses) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "status", Value: bson.M{"$in": req.Statuses}})
	}

	if req.PlanNameContains != "" {
		queryFilter = append(queryFilter, bson.E{Key: "plan_name", Value: bson.M{"$regex": req.PlanNameContains, "$options": "i"}})
	}

	if len(req.Currency) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "currency", Value: bson.M{"$in": req.Currency}})
	}

	if len(req.BillingInterval) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "billing_interval", Value: bson.M{"$in": req.BillingInterval}})
	}

	if req.NextBillingDateFrom != "" {
		queryFilter = append(queryFilter, bson.E{Key: "next_billing_date", Value: bson.M{"$gte": req.NextBillingDateFrom}})
	}

	if req.NextBillingDateTo != "" {
		queryFilter = append(queryFilter, bson.E{Key: "next_billing_date", Value: bson.M{"$lte": req.NextBillingDateTo}})
	}

	if req.CreatedAtFrom != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$gte": req.CreatedAtFrom}})
	}

	if req.CreatedAtTo != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$lte": req.CreatedAtTo}})
	}

	// generate sort filter from request
	switch req.Order {
	case "created_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: 1})
	case "created_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	case "updated_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: 1})
	case "updated_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: -1})
	default:
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	}

	// Sort by request field
	findOptions.SetSort(requestFilter)

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "subscriptions"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateSubscription handles creating a subscription in repository
func (r *Repository) CreateSubscription(ctx context.Context, newSubscription *Subscription) (*Subscription, error) {

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, newSubscription, "subscription")
	if err != nil {
		return nil, err
	}

	return newSubscription, nil
}

// GetSubscriptionByID handles fetching a subscription by ID from repository
func (r *Repository) GetSubscriptionByID(ctx context.Context, subscriptionID string) (*Subscription, error) {

	var (
		result      Subscription
		queryFilter = bson.M{"_id": subscriptionID}
	)

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "subscription", true, errors.New(ErrKeyBillingSubscriptionNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetSubscriptionByIntegratorID handles fetching a subscription by integrator subscription ID from repository
func (r *Repository) GetSubscriptionByIntegratorID(ctx context.Context, integratorName, integratorSubscriptionID string) (*Subscription, error) {

	var (
		result      Subscription
		queryFilter = bson.M{"integrator": integratorName, "integrator_subscription_id": integratorSubscriptionID}
	)

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "subscription", true, errors.New(ErrKeyBillingSubscriptionNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateSubscription updates an existing subscription
func (r *Repository) UpdateSubscription(ctx context.Context, subscription *Subscription) (*Subscription, error) {

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": subscription.ID}, bson.M{"$set": subscription}, "subscription")
	if err != nil {
		return nil, err
	}

	return subscription, nil
}

// DeleteSubscription handles deleting a subscription from repository
func (r *Repository) DeleteSubscription(ctx context.Context, subscriptionID string) error {

	collection, err := r.GetBillingSubscriptionsCollection(ctx)
	if err != nil {
		return err
	}

	err = r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": subscriptionID}, "subscription")
	if err != nil {
		return err
	}

	return nil
}

// GetTotalBillingEvents handles fetching the total count of billing events in repository
func (r *Repository) GetTotalBillingEvents(ctx context.Context, req *GetTotalBillingEventsRequest) (int64, error) {

	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req.IntegratorName != "" {
		queryFilter["integrator"] = req.IntegratorName
	}

	if req.IntegratorUserID != "" {
		queryFilter["integrator_customer_id"] = req.IntegratorUserID
	}

	if req.IntegratorSubscriptionID != "" {
		queryFilter["integrator_subscription_id"] = req.IntegratorSubscriptionID
	}

	if len(req.UserIDs) > 0 {
		queryFilter["user_id"] = bson.M{"$in": req.UserIDs}
	}

	if len(req.EventTypes) > 0 {
		queryFilter["event_type"] = bson.M{"$in": req.EventTypes}
	}

	if req.PlanNameContains != "" {
		queryFilter["plan_name"] = bson.M{"$regex": req.PlanNameContains, "$options": "i"}
	}

	if len(req.Currency) > 0 {
		queryFilter["currency"] = bson.M{"$in": req.Currency}
	}

	if len(req.Statuses) > 0 {
		queryFilter["status"] = bson.M{"$in": req.Statuses}
	}

	if req.CreatedAtFrom != "" {
		queryFilter["created_at"] = bson.M{"$gte": req.CreatedAtFrom}
	}

	if req.CreatedAtTo != "" {
		if _, exists := queryFilter["created_at"]; exists {
			queryFilter["created_at"].(bson.M)["$lte"] = req.CreatedAtTo
		} else {
			queryFilter["created_at"] = bson.M{"$lte": req.CreatedAtTo}
		}
	}

	if req.EventTimeFrom != "" {
		queryFilter["provider_event_time"] = bson.M{"$gte": req.EventTimeFrom}
	}

	if req.EventTimeTo != "" {
		if _, exists := queryFilter["provider_event_time"]; exists {
			queryFilter["provider_event_time"].(bson.M)["$lte"] = req.EventTimeTo
		} else {
			queryFilter["provider_event_time"] = bson.M{"$lte": req.EventTimeTo}
		}
	}

	collection, err := r.GetBillingEventsCollection(ctx)
	if err != nil {
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// GetBillingEvents handles fetching billing events from repository
func (r *Repository) GetBillingEvents(ctx context.Context, req *GetBillingEventsRequest) ([]BillingEvent, error) {

	var (
		result          []BillingEvent
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = repository.GetPaginationSkip(int64(req.Page), paginationLimit)

	// generate query filter from request
	if req.IntegratorName != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator", Value: req.IntegratorName})
	}

	if req.IntegratorUserID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator_customer_id", Value: req.IntegratorUserID})
	}

	if req.IntegratorSubscriptionID != "" {
		queryFilter = append(queryFilter, bson.E{Key: "integrator_subscription_id", Value: req.IntegratorSubscriptionID})
	}

	if len(req.ForUserIDs) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "user_id", Value: bson.M{"$in": req.ForUserIDs}})
	}

	if len(req.EventTypes) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "event_type", Value: bson.M{"$in": req.EventTypes}})
	}

	if req.PlanNameContains != "" {
		queryFilter = append(queryFilter, bson.E{Key: "plan_name", Value: bson.M{"$regex": req.PlanNameContains, "$options": "i"}})
	}

	if len(req.Currency) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "currency", Value: bson.M{"$in": req.Currency}})
	}

	if len(req.Statuses) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "status", Value: bson.M{"$in": req.Statuses}})
	}

	if req.EventTimeFrom != "" {
		queryFilter = append(queryFilter, bson.E{Key: "provider_event_time", Value: bson.M{"$gte": req.EventTimeFrom}})
	}

	if req.EventTimeTo != "" {
		queryFilter = append(queryFilter, bson.E{Key: "provider_event_time", Value: bson.M{"$lte": req.EventTimeTo}})
	}

	if req.CreatedAtFrom != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$gte": req.CreatedAtFrom}})
	}

	if req.CreatedAtTo != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$lte": req.CreatedAtTo}})
	}

	// generate sort filter from request
	switch req.Order {
	case "created_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: 1})
	case "created_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	case "updated_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: 1})
	case "updated_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "updated_at", Value: -1})
	default:
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	}

	// Sort by request field
	findOptions.SetSort(requestFilter)

	collection, err := r.GetBillingEventsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "billing_events"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateBillingEvent handles creating a billing event in repository
func (r *Repository) CreateBillingEvent(ctx context.Context, newEvent *BillingEvent) (*BillingEvent, error) {

	collection, err := r.GetBillingEventsCollection(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, newEvent, "billing_event")
	if err != nil {
		return nil, err
	}

	return newEvent, nil
}

// GetBillingEventByID handles fetching a billing event by ID from repository
func (r *Repository) GetBillingEventByID(ctx context.Context, eventID string) (*BillingEvent, error) {

	var (
		result      BillingEvent
		queryFilter = bson.M{"_id": eventID}
	)

	collection, err := r.GetBillingEventsCollection(ctx)
	if err != nil {
		return nil, err
	}

	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "billing_event", true, errors.New(ErrKeyBillingEventNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetFirstSuccessfulBillingEventWithPlanNameBySubscriptionId retrieves the first successful billing event
// that has a plan name for a given subscription ID and integrator
func (r *Repository) GetFirstSuccessfulBillingEventWithPlanNameBySubscriptionId(ctx context.Context, integrator string, subscriptionID string) (*BillingEvent, error) {

	var result []BillingEvent

	// Query for events matching the subscription and integrator with successful status and plan name
	queryFilter := bson.D{
		{Key: "integrator_subscription_id", Value: subscriptionID},
		{Key: "integrator", Value: integrator},
		{Key: "status", Value: StatusActive},
		{Key: "plan_name", Value: bson.M{"$ne": ""}},
	}

	// Sort by created_at ascending to get the first one
	sortFilter := bson.D{{Key: "created_at", Value: 1}}

	findOptions := options.Find()
	findOptions.SetSort(sortFilter)
	findOptions.SetLimit(1)

	collection, err := r.GetBillingEventsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "billing_event"); err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return nil, errors.New(ErrKeyBillingEventNotFound)
	}

	return &result[0], nil
}

///// Private helper functions

// standardisedEmails takes a slice of email strings and returns a new slice with the emails standardised to lowercase.
// Any empty email strings are skipped.
func standardisedEmails(emails []string) []string {
	standardisedEmails := []string{}
	for _, email := range emails {
		if email == "" {
			continue
		}
		standardisedEmails = append(standardisedEmails, toolbox.StringStandardisedToLower(email))
	}
	return standardisedEmails
}
