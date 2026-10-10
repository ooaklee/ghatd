package post

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/toolbox"
	internalToolbox "github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// PostCollection collection name for posts
const PostCollection string = "posts"

const defaultCollectionInitMaxAttemptsLimit = 3

// MongoDbStore delegates commands to GHATD's shared Mongo helpers. Mutations
// require real command receipts so missing matches are not reported as success.
// Custom stores must preserve driver error identity and transaction contexts.
type MongoDbStore interface {
	// ExecuteCountDocuments delegates a count command against the given Mongo
	// collection and filter, returning the matching document count. Part of the
	// MongoDbStore contract, which requires delegation to shared Mongo helpers.
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	// ExecuteDeleteOneCommandResult deletes a single document matching the filter
	// in the given collection and returns the DeleteResult receipt. Per the
	// MongoDbStore contract, mutations require real command receipts so missing
	// matches are not reported as success.
	ExecuteDeleteOneCommandResult(ctx context.Context, collection *mongo.Collection, filter any, opts ...options.Lister[options.DeleteOneOptions]) (*mongo.DeleteResult, error)
	// ExecuteFindCommand runs a find query on the given collection with the
	// supplied filter and options, returning a Mongo cursor for result iteration.
	// Part of the MongoDbStore contract delegating to shared Mongo helpers.
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	// ExecuteInsertOneCommand inserts the given document into the collection and
	// returns the InsertOneResult receipt, using resultObjectName for diagnostics.
	// Per the MongoDbStore contract, driver error identity and transaction contexts
	// must be preserved.
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	// ExecuteUpdateOneCommandResult updates a single document matching the filter
	// in the given collection and returns the UpdateResult receipt. Part of the
	// MongoDbStore contract requiring genuine command receipts for mutations.
	ExecuteUpdateOneCommandResult(ctx context.Context, collection *mongo.Collection, filter, update any, opts ...options.Lister[options.UpdateOneOptions]) (*mongo.UpdateResult, error)
	// ExecuteDeleteManyCommand deletes all documents matching the filter in the
	// given collection, using targetObjectName for diagnostics. Per the
	// MongoDbStore contract, driver error identity and transaction contexts must be
	// preserved.
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	// ExecuteFindOneCommandDecodeResult finds a single matching document in the
	// collection and decodes it into result, using resultObjectName for
	// diagnostics, logError to control logging and onFailureErr as the error
	// returned on failure. Part of the MongoDbStore contract.
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	// ExecuteAggregateCommand runs the supplied Mongo aggregation pipeline against
	// the given collection and returns a cursor over the results. Part of the
	// MongoDbStore contract delegating to shared Mongo helpers.
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	// ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	// ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	// ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)

	// GetDatabase returns the Mongo database handle for the given database name.
	// Part of the MongoDbStore contract; custom stores must preserve driver error
	// identity and transaction contexts.
	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	// InitialiseClient creates and returns the Mongo client used by the store. Part
	// of the MongoDbStore contract delegating client setup to GHATD's shared Mongo
	// helpers.
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	// MapAllInCursorToResult decodes every document remaining in the cursor into
	// result, using resultObjectName for diagnostics, and returns an error if
	// decoding fails. Part of the MongoDbStore contract.
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	// MapOneInCursorToResult decodes a single document from the cursor into result,
	// using resultObjectName for diagnostics. Part of the MongoDbStore contract; it
	// is used for single-item query results.
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository represents the datastore to hold resource data
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository initiates new instance of repository
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit overrides the number of collection initialisation attempts.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}

	return r
}

// GetPostCollection returns collection used for posts domain
func (r *Repository) GetPostCollection(ctx context.Context) (*mongo.Collection, error) {
	if ctx == nil {
		return nil, ErrPostBadRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || nilPostDependency(r.Store) {
		return nil, ErrPostUnavailable
	}
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if r.collection != nil {
		return r.collection, nil
	}

	var lastErr error
	collectionInitMaxAttemptsLimit := r.collectionInitMaxAttemptsLimit
	if collectionInitMaxAttemptsLimit <= 0 {
		collectionInitMaxAttemptsLimit = defaultCollectionInitMaxAttemptsLimit
	}
	for attempt := 1; attempt <= collectionInitMaxAttemptsLimit; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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

		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if db == nil {
			return nil, ErrPostUnavailable
		}
		r.collection = db.Collection(PostCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("unable to initialise %s collection after %d attempts: %w", PostCollection, collectionInitMaxAttemptsLimit, lastErr)
}

// CreateRawPosts inserts a caller-supplied identity without mutating its snapshot.
func (r *Repository) CreateRawPosts(ctx context.Context, newPost *Post) (*Post, error) {
	if newPost == nil {
		return nil, ErrPostBadRequest
	}
	newPost = copyPost(newPost)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	if newPost.CreatedAt == "" {
		newPost.SetCreatedAtTimeToNow()
	}

	// verify the id is preset
	if newPost.Id == "" {
		return nil, ErrIdIsRequired
	}

	// verify the nano id is preset
	if newPost.NanoId == "" {
		return nil, ErrNanoIdIsRequired
	}

	// verify url friendly id is preset
	if newPost.UrlFriendlyId == "" {
		return nil, ErrUrlFriendlyIdIsRequired
	}

	result, err := r.Store.ExecuteInsertOneCommand(ctx, collection, newPost, "post")
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Acknowledged || result.InsertedID != newPost.Id {
		return nil, ErrPostUnavailable
	}

	return newPost, nil
}

// GetPostById selects a stored identity; only authoritative absence becomes
// ErrResourceNotFound. Operational and decode errors retain their original cause.
func (r *Repository) GetPostById(ctx context.Context, postId string) (*Post, error) {
	return r.getPost(ctx, "_id", postId, ErrIdIsRequired)
}

// GetPostByNanoId handles fetching a post in repositry that match the provided nano Id
func (r *Repository) GetPostByNanoId(ctx context.Context, postNanoId string) (*Post, error) {
	return r.getPost(ctx, "_nano_id", postNanoId, ErrNanoIdIsRequired)
}

// GetPostByUrlFriendlyId handles fetching a post in repositry that match the provided url friendly id
func (r *Repository) GetPostByUrlFriendlyId(ctx context.Context, postUrlFriendlyId string) (*Post, error) {
	return r.getPost(ctx, "_url_friendly_id", postUrlFriendlyId, ErrUrlFriendlyIdIsRequired)
}

// getPost centralizes selector validation and the absence contract for all
// single-item reads. Passing nil onFailureErr keeps store diagnostics native.
func (r *Repository) getPost(ctx context.Context, key, value string, missingValue error) (*Post, error) {
	if strings.TrimSpace(value) == "" {
		return nil, missingValue
	}
	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}
	var foundPost Post
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, bson.M{key: value}, &foundPost, "post", true, nil)
	if err != nil {
		if postAbsent(err) {
			return nil, ErrResourceNotFound
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actual := foundPost.Id
	if key == "_nano_id" {
		actual = foundPost.NanoId
	}
	if key == "_url_friendly_id" {
		actual = foundPost.UrlFriendlyId
	}
	if foundPost.Id == "" || actual != value {
		return nil, ErrPostUnavailable
	}
	return &foundPost, nil
}

// GetPostsByIds handles fetching posts in repositry that match the provided Ids
func (r *Repository) GetPostsByIds(ctx context.Context, postIds []string) ([]Post, error) {

	var (
		post        []Post
		queryFilter = bson.M{"_id": bson.M{"$in": postIds}}
		findOptions = options.Find()
	)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &post, "posts"); err != nil {
		return nil, err
	}

	return post, nil
}

// GetPostsByNanoIds handles fetching posts in repositry that match the provided nano Ids
func (r *Repository) GetPostsByNanoIds(ctx context.Context, postNanoIds []string) ([]Post, error) {

	var (
		post        []Post
		queryFilter = bson.M{"_nano_id": bson.M{"$in": postNanoIds}}
		findOptions = options.Find()
	)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &post, "posts"); err != nil {
		return nil, err
	}

	return post, nil
}

// GetPostsByUrlFriendlyIds handles fetching posts in repositry that match the provided url friendly Ids
func (r *Repository) GetPostsByUrlFriendlyIds(ctx context.Context, postUrlFriendlyIds []string) ([]Post, error) {

	var (
		post        []Post
		queryFilter = bson.M{"_url_friendly_id": bson.M{"$in": postUrlFriendlyIds}}
		findOptions = options.Find()
	)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &post, "posts"); err != nil {
		return nil, err
	}

	return post, nil
}

// CreatePost assigns missing identities to a copy and inserts it once. Insert
// errors may have uncertain outcomes; this repository never retries the write.
func (r *Repository) CreatePost(ctx context.Context, newPost *Post) (*Post, error) {
	if newPost == nil {
		return nil, ErrPostBadRequest
	}
	newPost = copyPost(newPost)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	newPost.SetCreatedAtTimeToNow()

	// only set Id if not already set
	if newPost.Id == "" {
		newPost.GenerateId()
	}

	// only set nano id if not already set
	if newPost.NanoId == "" {
		newPost.GenerateNanoId()
	}

	// verify url friendly id is preset
	if newPost.UrlFriendlyId == "" {
		return nil, ErrUrlFriendlyIdIsRequired
	}

	result, err := r.Store.ExecuteInsertOneCommand(ctx, collection, newPost, "post")
	if err != nil {
		return nil, err
	}
	if result == nil || !result.Acknowledged || result.InsertedID != newPost.Id {
		return nil, ErrPostUnavailable
	}

	return newPost, nil
}

// UpdatePost writes a private snapshot and requires one matched document. A
// matched no-op succeeds; missing matches and malformed receipts are distinct.
func (r *Repository) UpdatePost(ctx context.Context, post *Post) (*Post, error) {
	if post == nil {
		return nil, ErrPostBadRequest
	}
	if strings.TrimSpace(post.Id) == "" {
		return nil, ErrIdIsRequired
	}
	post = copyPost(post)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	post.SetUpdatedAtTimeToNow()

	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": post.Id}, bson.M{"$set": post})
	if err := postUpdateOutcome(result, err); err != nil {
		return nil, err
	}

	return post, nil
}

// DeletePost distinguishes a missing match from successful hard deletion. It
// preserves native errors and does not retry uncertain write outcomes.
func (r *Repository) DeletePost(ctx context.Context, postId string) error {
	if strings.TrimSpace(postId) == "" {
		return ErrIdIsRequired
	}

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return err
	}

	result, err := r.Store.ExecuteDeleteOneCommandResult(ctx, collection, bson.M{"_id": postId})
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.DeletedCount < 0 || result.DeletedCount > 1 {
		return ErrPostUnavailable
	}
	if result.DeletedCount == 0 {
		return ErrResourceNotFound
	}

	return nil
}

// SoftDeletePost records deletion on a copy and requires a matched document.
func (r *Repository) SoftDeletePost(ctx context.Context, post *Post, userId string) error {
	if post == nil {
		return ErrPostBadRequest
	}
	if strings.TrimSpace(post.Id) == "" {
		return ErrIdIsRequired
	}
	if strings.TrimSpace(userId) == "" {
		return ErrUserIdMustBeProvided
	}
	post = copyPost(post)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return err
	}

	post.SetDeletedAtTimeToNow()
	post.DeletedByUserId = userId

	result, err := r.Store.ExecuteUpdateOneCommandResult(ctx, collection, bson.M{"_id": post.Id}, bson.M{"$set": post})
	return postUpdateOutcome(result, err)
}

// postUpdateOutcome accepts a matched no-op but never a missing match, upsert or
// malformed receipt. Cancellation after a successful write does not erase it.
func postUpdateOutcome(result *mongo.UpdateResult, err error) error {
	if err != nil {
		return err
	}
	if result == nil || !result.Acknowledged || result.UpsertedID != nil || result.UpsertedCount != 0 || result.MatchedCount < 0 || result.MatchedCount > 1 || result.ModifiedCount < 0 || result.ModifiedCount > result.MatchedCount {
		return ErrPostUnavailable
	}
	if result.MatchedCount == 0 {
		return ErrResourceNotFound
	}
	return nil
}

// GetTotalPosts handles fetching the total count of posts in repository
func (r *Repository) GetTotalPosts(ctx context.Context, req *GetTotalPostsRequest) (int64, error) {

	// Example mongo query
	// /// get posts total
	// db.getCollection("posts").countDocuments({_id : { $ne : null }})
	// /// get posts where title is "Hello World"
	// db.getCollection("posts").find({"title": {$eq: "Hello World"}})

	queryFilter := bson.M{"_id": bson.M{"$exists": true}}

	if req.Title != "" {
		regexQueryStringFmtString := ".*(%s).*"

		regexQueryString := fmt.Sprintf(regexQueryStringFmtString,
			strings.ReplaceAll(toolbox.StringRemoveMultiSpace(
				strings.TrimSpace(req.Title),
			), " ", "|"),
		)

		queryFilter["title"] = bson.M{"$regex": bson.Regex{Pattern: regexQueryString, Options: "i"}}
	}

	if req.PublishedWithAlias {
		queryFilter["published_as"] = bson.M{"$exists": true}
	}
	if req.PublishedWithoutAlias {
		queryFilter["published_as"] = bson.M{"$exists": false}
	}

	if len(req.CreatedByIds) > 0 {
		queryFilter["created_by_user_id"] = bson.M{"$in": req.CreatedByIds}
	}

	if len(req.UrlFriendlyIds) > 0 {
		queryFilter["_url_friendly_id"] = bson.M{"$in": req.UrlFriendlyIds}
	}

	if len(req.PostTypes) > 0 {

		standardisedProvidedPostTypes := internalToolbox.StandardisedTypesAsStrings(req.PostTypes)

		queryFilter["type"] = bson.M{"$in": standardisedProvidedPostTypes}
	}

	if len(req.Tags) > 0 || len(req.WithoutTags) > 0 {
		tagFilter := bson.M{}

		if len(req.Tags) > 0 {
			tagFilter["$in"] = req.Tags
		}

		if len(req.WithoutTags) > 0 {
			tagFilter["$nin"] = req.WithoutTags
		}

		queryFilter["tags"] = tagFilter
	}

	if len(req.PostTextFormats) > 0 {
		standardisedProvidedPostTextFormats := internalToolbox.StandardisedTypesAsStrings(req.PostTextFormats)

		queryFilter["text_format"] = bson.M{"$in": standardisedProvidedPostTextFormats}
	}

	if req.TextContains != "" {
		regexQueryStringFmtString := ".*(%s).*"

		regexQueryString := fmt.Sprintf(regexQueryStringFmtString,
			strings.ReplaceAll(toolbox.StringRemoveMultiSpace(
				strings.TrimSpace(req.TextContains),
			), " ", "|"),
		)

		queryFilter["text"] = bson.M{"$regex": bson.Regex{Pattern: regexQueryString, Options: "i"}}
	}

	if len(req.PostHeaderImageTypes) > 0 {
		standardisedProvidedPostHeaderImageTypes := internalToolbox.StandardisedTypesAsStrings(req.PostHeaderImageTypes)

		queryFilter["header_image_type"] = bson.M{"$in": standardisedProvidedPostHeaderImageTypes}
	}

	if len(req.PublishedAs) > 0 {
		queryFilter["published_as"] = bson.M{"$in": req.PublishedAs}
	}

	createdAtFilter := bson.M{}
	if req.CreatedAtFrom != "" {
		createdAtFilter["$gte"] = req.CreatedAtFrom
	}
	if req.CreatedAtTo != "" {
		createdAtFilter["$lte"] = req.CreatedAtTo
	}
	if len(createdAtFilter) > 0 {
		queryFilter["created_at"] = createdAtFilter
	}

	publishedAtFilter := bson.M{}
	if req.PublishedAtFrom != "" {
		publishedAtFilter["$gte"] = req.PublishedAtFrom
	}
	if req.PublishedAtTo != "" {
		publishedAtFilter["$lte"] = req.PublishedAtTo
	}
	if len(publishedAtFilter) > 0 {
		queryFilter["published_at"] = publishedAtFilter
	}

	deletedAtFilter := bson.M{}
	if req.DeletedAtFrom != "" {
		deletedAtFilter["$gte"] = req.DeletedAtFrom
	}
	if req.DeletedAtTo != "" {
		deletedAtFilter["$lte"] = req.DeletedAtTo
	}
	if len(deletedAtFilter) > 0 {
		queryFilter["deleted_at"] = deletedAtFilter
	}

	if req.IsDeleted {
		queryFilter["deleted_at"] = bson.M{"$exists": true, "$ne": ""}
	}
	if req.IsNotDeleted {
		queryFilter["$or"] = []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": ""},
		}
	}

	if req.IsPublished {
		// the date exists, is not empty, and cannot be after now
		currentTime := toolbox.TimeNowUTC()
		queryFilter["published_at"] = bson.M{"$exists": true, "$ne": "", "$lte": currentTime}
	}
	if req.IsNotPublished {
		// the date does not exist, is empty, or is after now
		currentTime := toolbox.TimeNowUTC()
		queryFilter["$or"] = []bson.M{
			{"published_at": bson.M{"$exists": false}},
			{"published_at": ""},
			{"published_at": bson.M{"$gt": currentTime}},
		}
	}

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return 0, err
	}

	total, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return total, nil
}

// GetPosts handles fetching posts from repositry
func (r *Repository) GetPosts(ctx context.Context, req *GetPostsRequest) ([]Post, error) {

	var (
		post            []Post
		queryFilter     bson.D = bson.D{}
		requestFilter   bson.D = bson.D{}
		paginationLimit *int64 = repository.GetPaginationLimit(int64(req.PerPage))
	)

	findOptions := options.Find()

	findOptions.SetLimit(*paginationLimit)
	findOptions.SetSkip(*repository.GetPaginationSkip(int64(req.Page), paginationLimit))

	// generate query filter from request
	if req.Title != "" {
		regexQueryStringFmtString := ".*(%s).*"

		regexQueryString := fmt.Sprintf(regexQueryStringFmtString,
			strings.ReplaceAll(toolbox.StringRemoveMultiSpace(
				strings.TrimSpace(req.Title),
			), " ", "|"),
		)

		queryFilter = append(queryFilter, bson.E{Key: "title", Value: bson.M{"$regex": bson.Regex{Pattern: regexQueryString, Options: "i"}}})
	}

	if req.PublishedWithAlias {
		queryFilter = append(queryFilter, bson.E{Key: "published_as", Value: bson.M{"$exists": true}})
	}
	if req.PublishedWithoutAlias {
		queryFilter = append(queryFilter, bson.E{Key: "published_as", Value: bson.M{"$exists": false}})
	}

	if req.CreatedByIds != "" {
		queryFilter = append(queryFilter, bson.E{Key: "created_by_user_id", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.CreatedByIds)}})
	}

	if req.UrlFriendlyIds != "" {
		queryFilter = append(queryFilter, bson.E{Key: "_url_friendly_id", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.UrlFriendlyIds)}})
	}

	if req.WithTypes != "" {
		queryFilter = append(queryFilter, bson.E{Key: "type", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTypes)}})
	}

	if req.WithTags != "" || req.WithoutTags != "" {
		tagFilter := bson.M{}

		withTags := internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTags)
		if len(withTags) > 0 {
			tagFilter["$in"] = withTags
		}

		withoutTags := internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithoutTags)
		if len(withoutTags) > 0 {
			tagFilter["$nin"] = withoutTags
		}

		if len(tagFilter) > 0 {
			queryFilter = append(queryFilter, bson.E{Key: "tags", Value: tagFilter})
		}
	}

	if req.WithTextFormats != "" {
		queryFilter = append(queryFilter, bson.E{Key: "text_format", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithTextFormats)}})
	}

	if req.TextContains != "" {
		regexQueryStringFmtString := ".*(%s).*"

		regexQueryString := fmt.Sprintf(regexQueryStringFmtString,
			strings.ReplaceAll(toolbox.StringRemoveMultiSpace(
				strings.TrimSpace(req.TextContains),
			), " ", "|"),
		)

		queryFilter = append(queryFilter, bson.E{Key: "text", Value: bson.M{"$regex": bson.Regex{Pattern: regexQueryString, Options: "i"}}})
	}

	if req.WithHeaderImageType != "" {
		queryFilter = append(queryFilter, bson.E{Key: "header_image_type", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.WithHeaderImageType)}})
	}

	if req.PublishedAs != "" {
		queryFilter = append(queryFilter, bson.E{Key: "published_as", Value: bson.M{"$in": internalToolbox.SplitCommaSeparatedStringAndRemoveEmptyStrings(req.PublishedAs)}})
	}

	createdAtFilter := bson.M{}
	if req.CreatedAtFrom != "" {
		createdAtFilter["$gte"] = req.CreatedAtFrom
	}
	if req.CreatedAtTo != "" {
		createdAtFilter["$lte"] = req.CreatedAtTo
	}
	if len(createdAtFilter) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "created_at", Value: createdAtFilter})
	}

	publishedAtFilter := bson.M{}
	if req.PublishedAtFrom != "" {
		publishedAtFilter["$gte"] = req.PublishedAtFrom
	}
	if req.PublishedAtTo != "" {
		publishedAtFilter["$lte"] = req.PublishedAtTo
	}
	if len(publishedAtFilter) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "published_at", Value: publishedAtFilter})
	}

	deletedAtFilter := bson.M{}
	if req.DeletedAtFrom != "" {
		deletedAtFilter["$gte"] = req.DeletedAtFrom
	}
	if req.DeletedAtTo != "" {
		deletedAtFilter["$lte"] = req.DeletedAtTo
	}
	if len(deletedAtFilter) > 0 {
		queryFilter = append(queryFilter, bson.E{Key: "deleted_at", Value: deletedAtFilter})
	}

	if req.IsDeleted {
		queryFilter = append(queryFilter, bson.E{Key: "deleted_at", Value: bson.M{"$exists": true, "$ne": ""}})
	}
	if req.IsNotDeleted {
		queryFilter = append(queryFilter, bson.E{Key: "$or", Value: []bson.M{
			{"deleted_at": bson.M{"$exists": false}},
			{"deleted_at": ""},
		}})
	}

	if req.IsPublished {
		// the date exists, is not empty, and cannot be after now
		currentTime := toolbox.TimeNowUTC()
		queryFilter = append(queryFilter, bson.E{Key: "published_at", Value: bson.M{"$exists": true, "$ne": "", "$lte": currentTime}})
	}
	if req.IsNotPublished {
		// the date does not exist, is empty, or is after now
		currentTime := toolbox.TimeNowUTC()
		queryFilter = append(queryFilter, bson.E{Key: "$or", Value: []bson.M{
			{"published_at": bson.M{"$exists": false}},
			{"published_at": ""},
			{"published_at": bson.M{"$gt": currentTime}},
		}})
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
	case "deleted_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "deleted_at", Value: 1})
	case "deleted_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "deleted_at", Value: -1})
	case "published_at_asc":
		requestFilter = append(requestFilter, bson.E{Key: "published_at", Value: 1})
	case "published_at_desc":
		requestFilter = append(requestFilter, bson.E{Key: "published_at", Value: -1})
	default:
		requestFilter = append(requestFilter, bson.E{Key: "created_at", Value: -1})
	}

	// Sort by request field
	findOptions.SetSort(requestFilter)

	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}

	// If EnforceUniqueType is true, use aggregation pipeline
	if req.EnforceUniqueType {
		// Build aggregation pipeline
		pipeline := []bson.D{}

		// Add match stage if there are filters
		if len(queryFilter) > 0 {
			pipeline = append(pipeline, bson.D{{Key: "$match", Value: queryFilter}})
		}

		// Sort documents
		pipeline = append(pipeline, bson.D{{Key: "$sort", Value: requestFilter}})

		// Group by type and take first document of each type
		pipeline = append(pipeline, bson.D{
			{Key: "$group", Value: bson.D{
				{Key: "_id", Value: "$type"},
				{Key: "doc", Value: bson.D{{Key: "$first", Value: "$$ROOT"}}},
			}},
		})

		// Replace root with the document
		pipeline = append(pipeline, bson.D{{Key: "$replaceRoot", Value: bson.D{{Key: "newRoot", Value: "$doc"}}}})

		// Sort again after grouping
		pipeline = append(pipeline, bson.D{{Key: "$sort", Value: requestFilter}})

		// Apply limit
		if paginationLimit != nil {
			pipeline = append(pipeline, bson.D{{Key: "$limit", Value: *paginationLimit}})
		}

		c, err := r.Store.ExecuteAggregateCommand(ctx, collection, pipeline)
		if err != nil {
			return nil, err
		}

		if err = r.Store.MapAllInCursorToResult(ctx, c, &post, "posts"); err != nil {
			return nil, err
		}

		return post, nil
	}

	// Standard find operation
	c, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, findOptions)
	if err != nil {
		return nil, err
	}

	if err = r.Store.MapAllInCursorToResult(ctx, c, &post, "posts"); err != nil {
		return nil, err
	}

	return post, nil
}
