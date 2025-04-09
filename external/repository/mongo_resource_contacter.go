package repository

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// CommsCollection collection name for comms
const CommsCollection RepositoryCollection = "comms"

// GetCommsCollection returns collection used for comms domain
func (r MongoDbRepository) GetCommsCollection(client *mongo.Client) *mongo.Collection {
	return client.Database(r.ClientHandler.DB).Collection(string(CommsCollection))
}

// GetTotalComms handles fetching the total count of comms in repository
func (r MongoDbRepository) GetTotalComms(ctx context.Context, req *contacter.GetTotalCommsRequest) (int64, error) {

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

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return 0, err
	}

	collection := r.GetCommsCollection(client)

	total, err := ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// GetCommsByIds handles fetching comms in repositry that match the provided Ids
func (r MongoDbRepository) GetCommsByIds(ctx context.Context, commsIds []string) ([]contacter.Comms, error) {

	var (
		result      []contacter.Comms
		queryFilter = bson.M{"_id": bson.M{"$in": commsIds}}
		findOptions = options.Find()
	)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetCommsCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
		return nil, err
	}

	return result, nil
}

// GetCommsByNanoIds handles fetching comms in repositry that match the provided nano Ids
func (r MongoDbRepository) GetCommsByNanoIds(ctx context.Context, commsNanoIds []string) ([]contacter.Comms, error) {

	var (
		result      []contacter.Comms
		queryFilter = bson.M{"_nano_id": bson.M{"$in": commsNanoIds}}
		findOptions = options.Find()
	)

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetCommsCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
		return nil, err
	}

	return result, nil
}

// CreateComms handles creating a comms in repositry
func (r MongoDbRepository) CreateComms(ctx context.Context, newComms *contacter.Comms) (*contacter.Comms, error) {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetCommsCollection(client)

	newComms.SetCreatedAtTimeToNow()

	// only set nano Id if not already set
	if newComms.NanoId == "" {
		newComms.GenerateNanoId()
	}

	// only set Id if not already set
	if newComms.Id == "" {
		newComms.GenerateId()
	}

	_, err = collection.InsertOne(ctx, newComms)
	if err != nil {
		return nil, err
	}

	return newComms, nil
}

// UpdateComms handles updating a comms in repositry
func (r MongoDbRepository) UpdateComms(ctx context.Context, comms *contacter.Comms) (*contacter.Comms, error) {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetCommsCollection(client)

	comms.SetUpdatedAtTimeToNow()

	err = ExecuteUpdateOneCommand(ctx, collection, bson.M{"_id": comms.Id}, bson.M{"$set": comms}, "comms")
	if err != nil {
		return nil, err
	}

	return comms, nil
}

// DeleteComms handles deleting a comms in repositry
func (r MongoDbRepository) DeleteComms(ctx context.Context, commsId string) error {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return err
	}

	collection := r.GetCommsCollection(client)

	_, err = collection.DeleteOne(ctx, bson.M{"_id": commsId})
	if err != nil {
		return err
	}

	return nil
}

// GetComms handles fetching comms from repositry
func (r MongoDbRepository) GetComms(ctx context.Context, req *contacter.GetCommsRequest) ([]contacter.Comms, error) {

	var (
		result          []contacter.Comms
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.Limit = paginationLimit
	findOptions.Skip = GetPaginationSkip(int64(req.Page), paginationLimit)

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

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	collection := r.GetCommsCollection(client)

	c, err := ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = MapAllInCursorToResult(ctx, c, &result, "comms"); err != nil {
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

// standardisedProvidedCommsTypes takes a slice of contacter.CommsType and returns a slice of standardised string representations.
// Each comms type is converted to lowercase and any spaces are replaced with hyphens.
func standardisedProvidedCommsTypes(commsTypes []contacter.CommsType) []string {
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
