package vision

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
)

const (
	defaultCollectionInitMaxAttemptsLimit = 3
	defaultVisionPageSize                 = 25
	maxVisionPageSize                     = 100
)

// MongoDbStore represents the datastore methods needed by vision.
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.CountOptions]) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...options.Lister[options.FindOptions]) (*mongo.Cursor, error)
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles vision data persistence.
type Repository struct {
	Store                          MongoDbStore
	collectionInitMaxAttemptsLimit int

	collection      *mongo.Collection
	collectionMutex sync.Mutex
}

// NewRepository creates a new vision repository.
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store:                          store,
		collectionInitMaxAttemptsLimit: defaultCollectionInitMaxAttemptsLimit,
	}
}

// WithCollectionInitMaxAttemptsLimit overrides collection initialisation retries.
func (r *Repository) WithCollectionInitMaxAttemptsLimit(limit int) *Repository {
	if limit > 0 {
		r.collectionInitMaxAttemptsLimit = limit
	}
	return r
}

// GetVisionCollection returns the collection used for vision records.
func (r *Repository) GetVisionCollection(ctx context.Context) (*mongo.Collection, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/vision", "get-vision-collection")
	r.collectionMutex.Lock()
	defer r.collectionMutex.Unlock()

	if r.collection != nil {
		return r.collection, nil
	}

	limit := r.collectionInitMaxAttemptsLimit
	if limit <= 0 {
		limit = defaultCollectionInitMaxAttemptsLimit
	}

	var lastErr error
	for attempt := 1; attempt <= limit; attempt++ {
		if _, err := r.Store.InitialiseClient(ctx); err != nil {
			lastErr = err
			logger.Warn("vision-collection-client-init-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		db, err := r.Store.GetDatabase(ctx, "")
		if err != nil {
			lastErr = err
			logger.Warn("vision-collection-database-resolve-failed", zap.Int("attempt", attempt), zap.Error(err))
			continue
		}

		r.collection = db.Collection(VisionCollection)
		return r.collection, nil
	}

	return nil, fmt.Errorf("%w: unable to initialise %s collection after %d attempts: %w", ErrVisionDatabaseError, VisionCollection, limit, lastErr)
}

// CreateVision creates a vision record.
func (r *Repository) CreateVision(ctx context.Context, vision *Vision) (*Vision, error) {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return nil, err
	}

	if _, err = r.Store.ExecuteInsertOneCommand(ctx, collection, vision, "vision"); err != nil {
		return nil, err
	}
	return vision, nil
}

// GetVisionByNanoID retrieves a vision by its public NanoID.
func (r *Repository) GetVisionByNanoID(ctx context.Context, nanoID string) (*Vision, error) {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return nil, err
	}

	var result Vision
	if err = r.Store.ExecuteFindOneCommandDecodeResult(
		ctx,
		collection,
		bson.M{"_nano_id": strings.TrimSpace(nanoID)},
		&result,
		"vision",
		true,
		ErrVisionResourceNotFound,
	); err != nil {
		return nil, err
	}

	normaliseStoredVision(&result)
	return &result, nil
}

// GetVisions retrieves a filtered page. Comments are omitted from list rows;
// callers can use GetVisionByNanoID for the full discussion.
func (r *Repository) GetVisions(ctx context.Context, req *GetVisionsRequest) ([]Vision, error) {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return nil, err
	}

	findOptions := options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}, {Key: "title", Value: 1}}).
		SetProjection(bson.M{"comments": 0})

	page, pageSize := normalisePagination(req)
	if pageSize > 0 {
		findOptions.SetLimit(pageSize)
		findOptions.SetSkip((page - 1) * pageSize)
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, buildVisionListFilter(req), findOptions)
	if err != nil {
		return nil, err
	}

	var result []Vision
	if err = r.Store.MapAllInCursorToResult(ctx, cursor, &result, "visions"); err != nil {
		return nil, err
	}
	for i := range result {
		normaliseStoredVision(&result[i])
	}
	return result, nil
}

// GetTotalVisions counts visions matching a filter.
func (r *Repository) GetTotalVisions(ctx context.Context, req *GetVisionsRequest) (int64, error) {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return 0, err
	}
	return r.Store.ExecuteCountDocuments(ctx, collection, buildVisionListFilter(req))
}

// UpdateVision updates mutable descriptive fields without replacing votes,
// comments, or roadmap status.
func (r *Repository) UpdateVision(ctx context.Context, vision *Vision) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{"_id": vision.ID},
		bson.M{"$set": bson.M{
			"title":              vision.Title,
			"description":        vision.Description,
			"metadata":           vision.Metadata,
			"updated_at":         vision.UpdatedAt,
			"updated_by_user_id": vision.UpdatedByUserID,
		}},
		"vision",
	)
}

// UpdateVisionStatus stores a validated roadmap transition.
func (r *Repository) UpdateVisionStatus(
	ctx context.Context,
	id string,
	status VisionStatus,
	updatedByUserID string,
	updatedAt string,
) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{
			"status":             status,
			"updated_at":         updatedAt,
			"updated_by_user_id": updatedByUserID,
		}},
		"vision",
	)
}

// SetVisionVote atomically moves a user into the selected vote bucket.
func (r *Repository) SetVisionVote(
	ctx context.Context,
	id string,
	userID string,
	vote VisionVote,
	updatedAt string,
) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	targetBucket := "voters.1"
	otherBucket := "voters.0"
	if vote == VisionVoteDownvote {
		targetBucket, otherBucket = otherBucket, targetBucket
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{"_id": id},
		bson.M{
			"$addToSet": bson.M{targetBucket: userID},
			"$pull":     bson.M{otherBucket: userID},
			"$set": bson.M{
				"updated_at":         updatedAt,
				"updated_by_user_id": userID,
			},
		},
		"vision",
	)
}

// RemoveVisionVote atomically removes a user from both vote buckets.
func (r *Repository) RemoveVisionVote(ctx context.Context, id, userID, updatedAt string) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{"_id": id},
		bson.M{
			"$pull": bson.M{
				"voters.0": userID,
				"voters.1": userID,
			},
			"$set": bson.M{
				"updated_at":         updatedAt,
				"updated_by_user_id": userID,
			},
		},
		"vision",
	)
}

// AddVisionComment atomically appends a comment.
func (r *Repository) AddVisionComment(ctx context.Context, id string, comment *VisionComment) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{"_id": id},
		bson.M{
			"$push": bson.M{"comments": comment},
			"$set": bson.M{
				"updated_at":         comment.CreatedAt,
				"updated_by_user_id": comment.UserID,
			},
		},
		"vision",
	)
}

// SetVisionCommentVote atomically moves a user between a comment's vote buckets.
func (r *Repository) SetVisionCommentVote(
	ctx context.Context,
	id, commentID, userID string,
	vote VisionVote,
	updatedAt string,
) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	otherVote := VisionVoteUpvote
	if vote == VisionVoteUpvote {
		otherVote = VisionVoteDownvote
	}
	targetPath := fmt.Sprintf("comments.$.voters.%d", vote)
	otherPath := fmt.Sprintf("comments.$.voters.%d", otherVote)

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{
			"_id":         strings.TrimSpace(id),
			"comments.id": strings.TrimSpace(commentID),
		},
		bson.M{
			"$addToSet": bson.M{targetPath: strings.TrimSpace(userID)},
			"$pull":     bson.M{otherPath: strings.TrimSpace(userID)},
			"$set": bson.M{
				"updated_at":         updatedAt,
				"updated_by_user_id": strings.TrimSpace(userID),
			},
		},
		"vision comment",
	)
}

// RemoveVisionCommentVote removes a user from both vote buckets atomically.
func (r *Repository) RemoveVisionCommentVote(
	ctx context.Context,
	id, commentID, userID, updatedAt string,
) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}

	return r.Store.ExecuteUpdateOneCommand(
		ctx,
		collection,
		bson.M{
			"_id":         strings.TrimSpace(id),
			"comments.id": strings.TrimSpace(commentID),
		},
		bson.M{
			"$pull": bson.M{
				"comments.$.voters.0": strings.TrimSpace(userID),
				"comments.$.voters.1": strings.TrimSpace(userID),
			},
			"$set": bson.M{
				"updated_at":         updatedAt,
				"updated_by_user_id": strings.TrimSpace(userID),
			},
		},
		"vision comment",
	)
}

// DeleteVisionByID deletes a vision by ID.
func (r *Repository) DeleteVisionByID(ctx context.Context, id string) error {
	collection, err := r.GetVisionCollection(ctx)
	if err != nil {
		return err
	}
	return r.Store.ExecuteDeleteOneCommand(ctx, collection, bson.M{"_id": strings.TrimSpace(id)}, "vision")
}

// buildVisionListFilter constructs a Mongo filter from the request's query params.
func buildVisionListFilter(req *GetVisionsRequest) bson.M {
	filter := bson.M{"_id": bson.M{"$exists": true}}
	if req == nil {
		return filter
	}

	if visionType := normaliseVisionType(req.Type); visionType != "" {
		filter["type"] = visionType
	}
	if status := normaliseVisionStatus(req.Status); status != "" {
		filter["status"] = status
	} else if req.RoadmapOnly {
		filter["status"] = bson.M{"$exists": true, "$ne": ""}
	}
	if query := strings.TrimSpace(req.Query); query != "" {
		regex := bson.M{"$regex": regexp.QuoteMeta(query), "$options": "i"}
		filter["$or"] = []bson.M{
			{"title": regex},
			{"description": regex},
		}
	}

	return filter
}

// normalisePagination applies the default and maximum list pagination limits.
func normalisePagination(req *GetVisionsRequest) (int64, int64) {
	if req == nil {
		return 1, defaultVisionPageSize
	}

	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = defaultVisionPageSize
	}
	if pageSize > maxVisionPageSize {
		pageSize = maxVisionPageSize
	}
	return page, pageSize
}

// normaliseStoredVision restores empty collections omitted by older documents.
func normaliseStoredVision(vision *Vision) {
	normaliseVoteBuckets(&vision.Voters)
	if vision.Comments == nil {
		vision.Comments = []VisionComment{}
	}
	for i := range vision.Comments {
		normaliseVoteBuckets(&vision.Comments[i].Voters)
	}
}

// normaliseVoteBuckets ensures both supported vote buckets are initialised.
func normaliseVoteBuckets(voters *map[VisionVote][]string) {
	if *voters == nil {
		*voters = newVisionVoteBuckets()
		return
	}
	if (*voters)[VisionVoteDownvote] == nil {
		(*voters)[VisionVoteDownvote] = []string{}
	}
	if (*voters)[VisionVoteUpvote] == nil {
		(*voters)[VisionVoteUpvote] = []string{}
	}
}
