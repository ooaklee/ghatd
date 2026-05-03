package contacter

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CommsCollection collection name for comms
const CommsCollection string = "comms"

// MongoDbStore represents the datastore to hold resource data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	// ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	// ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)
	// ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error

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

// GetCommsCollection returns collection used for comms domain
func (r *Repository) GetCommsCollection(ctx context.Context) (*mongo.Collection, error) {

	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(CommsCollection)

	return collection, nil
}

// GetTotalComms handles fetching the total count of comms in repository
func (r *Repository) GetTotalComms(ctx context.Context, req *GetTotalCommsRequest) (int64, error) {

	// Example mongo query
	// /// get comms total
	// db.getCollection("comms").countDocuments({_id : { $ne : null }})
	// /// get comms with full name x total
	// db.getCollection("comms").countDocuments({_id : { $ne : null }, full_name: "x"})
	// /// get logged in comms total
	// db.getCollection("comms").countDocuments({_id : { $ne : null }, user_logged_in: true})

	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req.FullName != "" {
		queryFilter["full_name"] = bson.M{"$regex": req.FullName, "$options": "i"}
	}

	if len(req.Emails) > 0 {

		standardisedProvidedEmails := standardisedEmails(req.Emails)

		queryFilter["email"] = bson.M{"$in": standardisedProvidedEmails}
	}

	if len(req.CommsTypes) > 0 {

		standardisedProvidedCommsTypes := standardisedProvidedCommsTypes(req.CommsTypes)

		queryFilter["type"] = bson.M{"$in": standardisedProvidedCommsTypes}
	}

	if req.MessageContains != "" {
		queryFilter["message"] = bson.M{"$regex": req.MessageContains, "$options": "i"}
	}

	if len(req.DisplayedAs) > 0 {
		queryFilter["meta.displayed_as"] = bson.M{"$in": req.DisplayedAs}
	}

	if req.CustomSubjectContains != "" {
		queryFilter["meta.custom_subject"] = bson.M{"$regex": req.CustomSubjectContains, "$options": "i"}
	}

	if req.CreatedAtFrom != "" {
		queryFilter["created_at"] = bson.M{"$gte": req.CreatedAtFrom}
	}

	if req.CreatedAtTo != "" {
		queryFilter["created_at"] = bson.M{"$lte": req.CreatedAtTo}
	}

	if req.UserLoggedIn {
		queryFilter["user_logged_in"] = true
	}

	if req.UserNotLoggedIn {
		queryFilter["user_logged_in"] = false
	}

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// GetCommsByIds handles fetching comms in repositry that match the provided Ids
func (r *Repository) GetCommsByIds(ctx context.Context, commsIds []string) ([]Comms, error) {

	var (
		result      []Comms
		queryFilter = bson.M{"_id": bson.M{"$in": commsIds}}
		findOptions = options.Find()
	)

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetCommsByNanoIds handles fetching comms in repositry that match the provided nano Ids
func (r *Repository) GetCommsByNanoIds(ctx context.Context, commsNanoIds []string) ([]Comms, error) {

	var (
		result      []Comms
		queryFilter = bson.M{"_nano_id": bson.M{"$in": commsNanoIds}}
		findOptions = options.Find()
	)

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateComms handles creating a comms in repositry
func (r *Repository) CreateComms(ctx context.Context, newComms *Comms) (*Comms, error) {

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	newComms.SetCreatedAtTimeToNow()

	// only set nano Id if not already set
	if newComms.NanoId == "" {
		newComms.GenerateNanoId()
	}

	// only set Id if not already set
	if newComms.Id == "" {
		newComms.GenerateId()
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, newComms, "comms")
	if err != nil {
		return nil, err
	}

	return newComms, nil
}

// UpdateComms handles updating a comms in repositry
func (r *Repository) UpdateComms(ctx context.Context, comms *Comms) (*Comms, error) {

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	comms.SetUpdatedAtTimeToNow()

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": comms.Id}, bson.M{"$set": comms}, "comms")
	if err != nil {
		return nil, err
	}

	return comms, nil
}

// DeleteComms handles deleting a comms in repositry
func (r *Repository) DeleteComms(ctx context.Context, commsId string) error {

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return err
	}

	err = r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": commsId}, "comms")
	if err != nil {
		return err
	}

	return nil
}

// GetComms handles fetching comms from repositry
func (r *Repository) GetComms(ctx context.Context, req *GetCommsRequest) ([]Comms, error) {

	var (
		result          []Comms
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = repository.GetPaginationSkip(int64(req.Page), paginationLimit)

	// generate query filter from request
	if req.FullName != "" {
		queryFilter = append(queryFilter, bson.E{Key: "full_name", Value: bson.M{"$regex": req.FullName, "$options": "i"}})
	}

	if len(req.FromEmails) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "email", Value: bson.M{"$in": standardisedEmails(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.FromEmails),
		)}})
	}

	if len(req.WithTypes) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "type", Value: bson.M{"$in": standardisedProvidedCommsTypesAsStrings(
			toolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTypes),
		)}})
	}

	if req.MessageContains != "" {
		queryFilter = append(queryFilter, bson.E{Key: "message", Value: bson.M{"$regex": req.MessageContains, "$options": "i"}})
	}

	if len(req.DisplayedAs) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "meta.displayed_as", Value: bson.M{"$in": req.DisplayedAs}})
	}

	if req.CustomSubjectContains != "" {
		queryFilter = append(queryFilter, bson.E{Key: "meta.custom_subject", Value: bson.M{"$regex": req.CustomSubjectContains, "$options": "i"}})
	}

	if req.CreatedAtFrom != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$gte": req.CreatedAtFrom}})
	}

	if req.CreatedAtTo != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: bson.M{"$lte": req.CreatedAtTo}})
	}

	if req.UserLoggedIn {
		queryFilter = append(queryFilter, bson.E{Key: "user_logged_in", Value: true})
	}

	if req.UserNotLoggedIn {
		queryFilter = append(queryFilter, bson.E{Key: "user_logged_in", Value: false})
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

	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
		return nil, err
	}

	return result, nil
}

///// Private helper functions

// standardisedProvidedCommsTypesAsStrings takes a slice of strings representing communication types and returns a new slice with the strings standardised to lowercase and with spaces replaced by hyphens.
// This function is a helper for standardising communication type strings.
func standardisedProvidedCommsTypesAsStrings(commsTypes []string) []string {
	standardisedCommsTypes := []string{}
	for _, commsType := range commsTypes {
		standardisedCommsTypes = append(standardisedCommsTypes, strings.ReplaceAll(
			strings.ToLower(commsType),
			" ",
			"-",
		))
	}
	return standardisedCommsTypes
}

// standardisedProvidedCommsTypes takes a slice of CommsType and returns a slice of standardised string representations.
// Each comms type is converted to lowercase and any spaces are replaced with hyphens.
func standardisedProvidedCommsTypes(commsTypes []CommsType) []string {
	standardisedCommsTypes := []string{}
	for _, commsType := range commsTypes {
		standardisedCommsTypes = append(standardisedCommsTypes, strings.ReplaceAll(
			strings.ToLower(string(commsType)),
			" ",
			"-",
		))
	}
	return standardisedCommsTypes
}

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

// GetCommsStatsCounts retrieves aggregated comms stats in a single round-trip using
// an aggregation pipeline.
func (r *Repository) GetCommsStatsCounts(ctx context.Context, req *GetCommsStatsRequest) (*CommsStats, error) {
	collection, err := r.GetCommsCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Build the base match stage from the email regex filter (if provided).
	matchStage := bson.D{}
	if req.WithEmailRegex != "" {
		matchStage = bson.D{{Key: "email", Value: bson.M{"$regex": req.WithEmailRegex}}}
	}

	// Helper function to count documents with a specific condition
	countStageForCondition := func(condition interface{}) bson.A {
		return bson.A{
			bson.D{{Key: "$match", Value: condition}},
			bson.D{{Key: "$count", Value: "count"}},
		}
	}

	pipeline := []bson.D{
		{{Key: "$match", Value: matchStage}},
		{{Key: "$facet", Value: bson.D{
			{Key: "total", Value: bson.A{
				bson.D{{Key: "$count", Value: "count"}},
			}},
			{Key: "replied_to", Value: countStageForCondition(bson.M{"admin_reply": bson.M{"$ne": ""}})},
			{Key: "reached_out", Value: countStageForCondition(bson.M{"reached_out_at": bson.M{"$exists": true, "$ne": ""}})},
			{Key: "with_admin_notes", Value: countStageForCondition(bson.M{"admin_notes": bson.M{"$ne": ""}})},
			{Key: "with_linked_comms", Value: countStageForCondition(bson.M{"linked_comms_ids": bson.M{"$exists": true, "$ne": bson.A{}}})},
			{Key: "from_logged_in_users", Value: countStageForCondition(bson.M{"user_logged_in": true})},
			{Key: "from_guests", Value: countStageForCondition(bson.M{"user_logged_in": false})},
			{Key: "most_recent", Value: bson.A{
				bson.D{{Key: "$sort", Value: bson.M{"created_at": -1}}},
				bson.D{{Key: "$limit", Value: 1}},
				bson.D{{Key: "$project", Value: bson.M{"created_at": 1}}},
			}},
			{Key: "avg_reply_time", Value: bson.A{
				bson.D{{Key: "$match", Value: bson.M{"reached_out_at": bson.M{"$exists": true, "$ne": ""}}}},
				bson.D{{Key: "$project", Value: bson.M{
					"reply_time_ms": bson.M{"$subtract": bson.A{
						bson.M{"$dateFromString": bson.M{"dateString": "$reached_out_at"}},
						bson.M{"$dateFromString": bson.M{"dateString": "$created_at"}},
					}},
				}}},
				bson.D{{Key: "$group", Value: bson.M{
					"_id":    nil,
					"avg_ms": bson.M{"$avg": "$reply_time_ms"},
				}}},
			}},
			{Key: "by_type", Value: bson.A{
				bson.D{{Key: "$group", Value: bson.M{
					"_id":   "$type",
					"count": bson.M{"$sum": 1},
				}}},
			}},
		}}},
	}

	cursor, err := r.Store.ExecuteAggregateCommand(ctx, collection, pipeline)
	if err != nil {
		return nil, err
	}

	type countDoc struct {
		Count int64 `bson:"count"`
	}

	type dateDoc struct {
		CreatedAt string `bson:"created_at"`
	}

	type replyTimeDoc struct {
		AvgMs float64 `bson:"avg_ms"`
	}

	type typeCountDoc struct {
		Type  string `bson:"_id"`
		Count int64  `bson:"count"`
	}

	type facetResult struct {
		Total             []countDoc     `bson:"total"`
		RepliedTo         []countDoc     `bson:"replied_to"`
		ReachedOut        []countDoc     `bson:"reached_out"`
		WithAdminNotes    []countDoc     `bson:"with_admin_notes"`
		WithLinkedComms   []countDoc     `bson:"with_linked_comms"`
		FromLoggedInUsers []countDoc     `bson:"from_logged_in_users"`
		FromGuests        []countDoc     `bson:"from_guests"`
		MostRecent        []dateDoc      `bson:"most_recent"`
		AvgReplyTime      []replyTimeDoc `bson:"avg_reply_time"`
		ByType            []typeCountDoc `bson:"by_type"`
	}

	var result facetResult
	if err := r.Store.MapOneInCursorToResult(ctx, cursor, &result, "comms-stats-counts"); err != nil {
		return nil, err
	}

	extractCount := func(docs []countDoc) int64 {
		if len(docs) == 0 {
			return 0
		}
		return docs[0].Count
	}

	extractDate := func(docs []dateDoc) string {
		if len(docs) == 0 {
			return ""
		}
		return docs[0].CreatedAt
	}

	extractAvgTime := func(docs []replyTimeDoc) float64 {
		if len(docs) == 0 {
			return 0
		}
		// Convert milliseconds to minutes
		return docs[0].AvgMs / (1000 * 60)
	}

	// Build type stats
	typeStats := CommsTypeStats{}
	for _, typeCount := range result.ByType {
		switch typeCount.Type {
		case "general-inquiry":
			typeStats.GeneralInquiry = typeCount.Count
		case "customer-support":
			typeStats.CustomerSupport = typeCount.Count
		case "technical-support":
			typeStats.TechnicalSupport = typeCount.Count
		case "feature-request":
			typeStats.FeatureRequest = typeCount.Count
		case "feedback":
			typeStats.Feedback = typeCount.Count
		case "feedback-companion":
			typeStats.FeedbackCompanion = typeCount.Count
		case "product-information":
			typeStats.ProductInformation = typeCount.Count
		case "press-inquiry":
			typeStats.PressInquiry = typeCount.Count
		case "partnership-opportunities":
			typeStats.PartnershipOpportunities = typeCount.Count
		case "complaints":
			typeStats.Complaints = typeCount.Count
		case "website-issues":
			typeStats.WebsiteIssues = typeCount.Count
		case "donating-supporting-us-questions":
			typeStats.DonatingSupportingUsQuestions = typeCount.Count
		case "other":
			typeStats.Other = typeCount.Count
		}
	}

	totalComms := extractCount(result.Total)
	statusStats := CommsStatusStats{
		ReachedOut:    extractCount(result.ReachedOut),
		NotReachedOut: totalComms - extractCount(result.ReachedOut),
	}

	return &CommsStats{
		Total:                   totalComms,
		RepliedTo:               extractCount(result.RepliedTo),
		ReachedOut:              extractCount(result.ReachedOut),
		WithAdminNotes:          extractCount(result.WithAdminNotes),
		WithLinkedComms:         extractCount(result.WithLinkedComms),
		FromLoggedInUsers:       extractCount(result.FromLoggedInUsers),
		FromGuests:              extractCount(result.FromGuests),
		MostRecentCommsAt:       extractDate(result.MostRecent),
		AverageReplyTimeMinutes: extractAvgTime(result.AvgReplyTime),
		ByType:                  typeStats,
		ByStatus:                statusStats,
	}, nil
}
