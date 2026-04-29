package group

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// GroupCollection collection name for groups
const GroupCollection string = "groups"

// MongoDbStore represents the datastore to hold group data
type MongoDbStore interface {
	ExecuteCountDocuments(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.CountOptions) (int64, error)
	ExecuteDeleteOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error)
	ExecuteInsertOneCommand(ctx context.Context, collection *mongo.Collection, document interface{}, resultObjectName string) (*mongo.InsertOneResult, error)
	ExecuteUpdateOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteDeleteManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, targetObjectName string) error
	ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter interface{}, result interface{}, resultObjectName string, logError bool, onFailureErr error) error
	ExecuteAggregateCommand(ctx context.Context, collection *mongo.Collection, mongoPipeline []bson.D) (*mongo.Cursor, error)
	ExecuteReplaceOneCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, replacementObject interface{}, resultObjectName string) error
	ExecuteUpdateManyCommand(ctx context.Context, collection *mongo.Collection, filter interface{}, updateFilter interface{}, resultObjectName string) error
	ExecuteInsertManyCommand(ctx context.Context, collection *mongo.Collection, documents []interface{}, resultObjectName string) (*mongo.InsertManyResult, error)

	GetDatabase(ctx context.Context, dbName string) (*mongo.Database, error)
	InitialiseClient(ctx context.Context) (*mongo.Client, error)
	MapAllInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
	MapOneInCursorToResult(ctx context.Context, cursor *mongo.Cursor, result interface{}, resultObjectName string) error
}

// Repository handles group data persistence
type Repository struct {
	Store MongoDbStore
}

// NewRepository creates a new group repository
func NewRepository(store MongoDbStore) *Repository {
	return &Repository{
		Store: store,
	}
}

// GetGroupCollection returns collection used for groups domain
func (r *Repository) GetGroupCollection(ctx context.Context) (*mongo.Collection, error) {
	_, err := r.Store.InitialiseClient(ctx)
	if err != nil {
		return nil, err
	}

	db, err := r.Store.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	collection := db.Collection(GroupCollection)

	return collection, nil
}

// CreateGroup creates a new group in the repository
func (r *Repository) CreateGroup(ctx context.Context, group *UniversalGroup) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	_, err = r.Store.ExecuteInsertOneCommand(ctx, collection, group, "group")
	if err != nil {
		return nil, err
	}

	return group, nil
}

// GetGroupByID retrieves a group by ID
func (r *Repository) GetGroupByID(ctx context.Context, id string) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_id": id,
	}

	var result UniversalGroup
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "group", true, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetGroupByNanoID retrieves a group by nano ID
func (r *Repository) GetGroupByNanoID(ctx context.Context, nanoID string) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_nano_id": nanoID,
	}

	var result UniversalGroup
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "group", true, errors.New(ErrKeyResourceNotFound))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetGroupByName retrieves a group by name and optional type
func (r *Repository) GetGroupByName(ctx context.Context, name, groupType string, logError bool) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"name": normaliseGroupName(name),
	}

	if groupType != "" {
		queryFilter["type"] = groupType
	}

	var result UniversalGroup
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "group", logError, errors.New(ErrKeyUnableToFindGroupWithName))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// GetGroupByNameAndParent retrieves a group by name under a specific parent.
func (r *Repository) GetGroupByNameAndParent(ctx context.Context, name, parentGroupID string, logError bool) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"name":            normaliseGroupName(name),
		"parent_group_id": parentGroupID,
	}

	var result UniversalGroup
	err = r.Store.ExecuteFindOneCommandDecodeResult(ctx, collection, queryFilter, &result, "group", logError, errors.New(ErrKeyUnableToFindGroupWithName))
	if err != nil {
		return nil, err
	}

	return &result, nil
}

// UpdateGroup updates an existing group
func (r *Repository) UpdateGroup(ctx context.Context, group *UniversalGroup) (*UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"_id": group.ID,
	}

	update := bson.M{
		"$set": group,
	}

	if len(group.Members) == 0 {
		update["$unset"] = bson.M{
			"members": "",
		}
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "group")
	if err != nil {
		return nil, err
	}

	return group, nil
}

// DeleteGroupByID deletes a group by ID
func (r *Repository) DeleteGroupByID(ctx context.Context, id string) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id": id,
	}

	err = r.Store.ExecuteDeleteOneCommand(ctx, collection, queryFilter, "group")
	return err
}

// SoftDeleteGroup soft deletes a group by setting deleted timestamp
func (r *Repository) SoftDeleteGroup(ctx context.Context, id, deletedByID string, deletedAt string) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id": id,
	}

	update := bson.M{
		"$set": bson.M{
			"metadata.deleted_at":    deletedAt,
			"metadata.deleted_by_id": deletedByID,
			"status":                 GroupStatusArchived,
		},
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "group")
	return err
}

// GetGroups retrieves groups with filters and pagination
func (r *Repository) GetGroups(ctx context.Context, req *GetGroupsRequest) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Build query filter
	queryFilter := r.buildGroupQueryFilter(req)

	// Build sort options
	sortOptions := r.buildSortOptions(req.OrderBy)

	// Calculate skip
	skip := int64((req.Page - 1) * req.PageSize)
	limit := int64(req.PageSize)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetTotalGroups retrieves the total count of groups matching filters
func (r *Repository) GetTotalGroups(ctx context.Context, req *GetGroupsRequest) (int64, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return 0, err
	}

	queryFilter := r.buildGroupQueryFilter(req)

	count, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// GetGroupsByType retrieves groups by type with pagination
func (r *Repository) GetGroupsByType(ctx context.Context, groupType string, page, pageSize int, order string) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"type": groupType,
	}

	sortOptions := r.buildSortOptions(order)
	skip := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsByStatus retrieves groups by status with pagination
func (r *Repository) GetGroupsByStatus(ctx context.Context, status string, page, pageSize int, order string) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"status": status,
	}

	sortOptions := r.buildSortOptions(order)
	skip := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	options := options.Find().
		SetSort(sortOptions).
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsByReferencedUserID retrieves groups where user is either owner or a member.
func (r *Repository) GetGroupsByReferencedUserID(ctx context.Context, userID string) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"$or": []bson.M{
			{"owner_id": userID},
			{
				"members": bson.M{
					"$elemMatch": bson.M{
						"id": userID,
					},
				},
			},
		},
		"metadata.deleted_at": bson.M{"$exists": false},
	}

	options := options.Find().SetSort(bson.D{{Key: "metadata.created_at", Value: -1}})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsAwaitingAnswerForInvitationsByMemberID retrieves groups where the
// provided member ID has a pending invitation.
func (r *Repository) GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, memberID string) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"members": bson.M{
			"$elemMatch": bson.M{
				"id":   memberID,
				"type": MemberTypeUser,
				"$or": []bson.M{
					{"invitation_state": MemberInvitationStateInvited},
					{"invited_at": bson.M{"$exists": true, "$ne": ""}},
				},
			},
		},
		"metadata.deleted_at": bson.M{"$exists": false},
	}

	options := options.Find().SetSort(bson.D{{Key: "metadata.created_at", Value: -1}})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsByMemberID retrieves groups that contain a specific member
func (r *Repository) GetGroupsByMemberID(ctx context.Context, memberID string, memberType string, page, pageSize int) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"members": bson.M{
			"$elemMatch": bson.M{
				"id": memberID,
			},
		},
	}

	if memberType != "" {
		queryFilter["members"].(bson.M)["$elemMatch"].(bson.M)["type"] = memberType
	}

	skip := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	options := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "metadata.created_at", Value: -1}})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsByLeaderID retrieves groups where the user is the owner.
func (r *Repository) GetGroupsByLeaderID(ctx context.Context, leaderID string, page, pageSize int) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"$or": []bson.M{
			{"owner_id": leaderID},
		},
	}

	skip := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	options := options.Find().
		SetSkip(skip).
		SetLimit(limit).
		SetSort(bson.D{{Key: "metadata.created_at", Value: -1}})

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// GetGroupsByLineageAncestor retrieves all groups that descend from the provided ancestor group ID.
func (r *Repository) GetGroupsByLineageAncestor(ctx context.Context, ancestorGroupID string) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"lineage":             ancestorGroupID,
		"metadata.deleted_at": bson.M{"$exists": false},
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// SearchGroupsByExtension searches groups by extension field
func (r *Repository) SearchGroupsByExtension(ctx context.Context, key string, value interface{}, page, pageSize int) ([]UniversalGroup, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"extensions." + key: value,
	}

	skip := int64((page - 1) * pageSize)
	limit := int64(pageSize)

	options := options.Find().
		SetSkip(skip).
		SetLimit(limit)

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options)
	if err != nil {
		return nil, err
	}

	var results []UniversalGroup
	err = r.Store.MapAllInCursorToResult(ctx, cursor, &results, "group")
	if err != nil {
		return nil, err
	}

	return results, nil
}

// HasGroupDependents checks if any active group references the target group.
// A dependency means either:
// 1) parent_group_id equals the target ID, or
// 2) members contains a GROUP member whose id equals the target ID.
func (r *Repository) HasGroupDependents(ctx context.Context, groupID string) (bool, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return false, err
	}

	queryFilter := bson.M{
		"$or": []bson.M{
			{"parent_group_id": groupID},
			{
				"members": bson.M{
					"$elemMatch": bson.M{
						"id":   groupID,
						"type": MemberTypeGroup,
					},
				},
			},
		},
		"metadata.deleted_at": bson.M{"$exists": false},
	}

	count, err := r.Store.ExecuteCountDocuments(ctx, collection, queryFilter)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// AddMemberToGroup adds a member to a group
func (r *Repository) AddMemberToGroup(ctx context.Context, groupID string, member Member) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id": groupID,
	}

	update := bson.M{
		"$push": bson.M{
			"members": member,
		},
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "group")
	return err
}

// RemoveMemberFromGroup removes a member from a group
func (r *Repository) RemoveMemberFromGroup(ctx context.Context, groupID, memberID string) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id":        groupID,
		"members.id": memberID,
	}

	update := bson.M{
		"$pull": bson.M{
			"members": bson.M{
				"id": memberID,
			},
		},
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "group")
	return err
}

// ClearOwnerFromGroup clears owner_id for a group when it currently matches the provided owner ID.
func (r *Repository) ClearOwnerFromGroup(ctx context.Context, groupID, ownerID string) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id":      groupID,
		"owner_id": ownerID,
	}

	update := bson.M{
		"$set": bson.M{
			"owner_id": "",
		},
	}

	err = r.Store.ExecuteUpdateOneCommand(ctx, collection, queryFilter, update, "group")
	return err
}

// GetGroupIDsWithInvalidMembers returns IDs of groups containing members with empty or null IDs.
func (r *Repository) GetGroupIDsWithInvalidMembers(ctx context.Context) ([]string, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	queryFilter := bson.M{
		"members": bson.M{
			"$elemMatch": bson.M{
				"$or": bson.A{
					bson.M{"id": ""},
					bson.M{"id": nil},
				},
			},
		},
	}

	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, queryFilter, options.Find().SetProjection(bson.M{"_id": 1}))
	if err != nil {
		return nil, err
	}

	type groupIDResult struct {
		ID string `bson:"_id"`
	}

	results := []groupIDResult{}
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &results, "groups"); err != nil {
		return nil, err
	}

	groupIDs := make([]string, 0, len(results))
	for _, result := range results {
		groupIDs = append(groupIDs, result.ID)
	}

	return groupIDs, nil
}

// RepairInvalidMembers removes members with empty or null IDs from all affected groups.
func (r *Repository) RepairInvalidMembers(ctx context.Context) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"members": bson.M{
			"$elemMatch": bson.M{
				"$or": bson.A{
					bson.M{"id": ""},
					bson.M{"id": nil},
				},
			},
		},
	}

	update := bson.M{
		"$pull": bson.M{
			"members": bson.M{
				"id": bson.M{
					"$in": bson.A{"", nil},
				},
			},
		},
	}

	return r.Store.ExecuteUpdateManyCommand(ctx, collection, queryFilter, update, "groups")
}

// BulkUpdateGroupsStatus updates status for multiple groups
func (r *Repository) BulkUpdateGroupsStatus(ctx context.Context, groupIDs []string, status string) error {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return err
	}

	queryFilter := bson.M{
		"_id": bson.M{"$in": groupIDs},
	}

	update := bson.M{
		"$set": bson.M{
			"status": status,
		},
	}

	err = r.Store.ExecuteUpdateManyCommand(ctx, collection, queryFilter, update, "groups")
	return err
}

// Helper methods

// buildGroupQueryFilter builds a query filter for group searches
func (r *Repository) buildGroupQueryFilter(req *GetGroupsRequest) bson.M {
	queryFilter := bson.M{}

	// Type filters
	if len(req.Types) > 0 {
		for i, t := range req.Types {
			req.Types[i] = strings.ToUpper(strings.TrimSpace(t))
		}
		queryFilter["type"] = bson.M{"$in": req.Types}
	}

	// Status filters
	if len(req.Statuses) > 0 {
		queryFilter["status"] = bson.M{"$in": req.Statuses}
	}

	// Member filters
	if req.MemberID != "" {
		memberFilter := bson.M{
			"members": bson.M{
				"$elemMatch": bson.M{
					"id": req.MemberID,
				},
			},
		}
		if req.MemberType != "" {
			memberFilter["members"].(bson.M)["$elemMatch"].(bson.M)["type"] = req.MemberType
		}
		for key, value := range memberFilter {
			queryFilter[key] = value
		}
	}

	// Members with specific IDs and types
	if len(req.MembersWithIDs) > 0 {
		memberConditions := []bson.M{}
		for memberType, ids := range req.MembersWithIDs {
			for _, id := range ids {
				memberConditions = append(memberConditions, bson.M{
					"members": bson.M{
						"$elemMatch": bson.M{
							"id":   id,
							"type": memberType,
						},
					},
				})
			}
		}
		if len(memberConditions) > 0 {
			queryFilter["$and"] = memberConditions
		}
	}

	// Owner filter
	if req.OwnerID != "" {
		queryFilter["owner_id"] = req.OwnerID
	}

	// Settings filters
	if req.Visibility != "" {
		queryFilter["settings.visibility"] = req.Visibility
	}

	// Name search (case-insensitive regex)
	if req.NameSearch != "" {
		queryFilter["name"] = bson.M{
			"$regex":   req.NameSearch,
			"$options": "i",
		}
	}

	// Extension filters
	for key, value := range req.ExtensionFilters {
		queryFilter["extensions."+key] = value
	}

	// Exclude soft-deleted groups by default
	if queryFilter["metadata.deleted_at"] == nil {
		queryFilter["metadata.deleted_at"] = bson.M{"$exists": false}
	}

	return queryFilter
}

// buildSortOptions builds sort options based on order string
func (r *Repository) buildSortOptions(order string) bson.D {
	switch order {
	case GetGroupOrderCreatedAtDesc:
		return bson.D{{Key: "metadata.created_at", Value: -1}}
	case GetGroupOrderCreatedAtAsc:
		return bson.D{{Key: "metadata.created_at", Value: 1}}
	case GetGroupOrderUpdatedAtDesc:
		return bson.D{{Key: "metadata.updated_at", Value: -1}}
	case GetGroupOrderUpdatedAtAsc:
		return bson.D{{Key: "metadata.updated_at", Value: 1}}
	case GetGroupOrderNameAsc:
		return bson.D{{Key: "name", Value: 1}}
	case GetGroupOrderNameDesc:
		return bson.D{{Key: "name", Value: -1}}
	case GetGroupOrderMemberCountDesc:
		return bson.D{{Key: "members", Value: -1}}
	case GetGroupOrderMemberCountAsc:
		return bson.D{{Key: "members", Value: 1}}
	default:
		return bson.D{{Key: "metadata.created_at", Value: -1}}
	}
}

// normaliseGroupName standardises group name
func normaliseGroupName(name string) string {
	return strings.TrimSpace(name)
}

// GetGroupsStatsCounts retrieves aggregated group statistics using a $facet aggregation.
func (r *Repository) GetGroupsStatsCounts(ctx context.Context) (*AllGroupsStats, error) {
	collection, err := r.GetGroupCollection(ctx)
	if err != nil {
		return nil, err
	}

	// Exclude soft-deleted groups
	baseMatch := bson.D{{Key: "$match", Value: bson.M{"metadata.deleted_at": bson.M{"$exists": false}}}}

	countPipeline := func(filter bson.M) bson.A {
		return bson.A{
			bson.D{{Key: "$match", Value: filter}},
			bson.D{{Key: "$count", Value: "count"}},
		}
	}

	pipeline := []bson.D{
		baseMatch,
		{{Key: "$facet", Value: bson.D{
			// Total
			{Key: "total", Value: bson.A{
				bson.D{{Key: "$count", Value: "count"}},
			}},
			// By status
			{Key: "status_active", Value: countPipeline(bson.M{"status": GroupStatusActive})},
			{Key: "status_inactive", Value: countPipeline(bson.M{"status": GroupStatusInactive})},
			{Key: "status_archived", Value: countPipeline(bson.M{"status": GroupStatusArchived})},
			{Key: "status_suspended", Value: countPipeline(bson.M{"status": GroupStatusSuspended})},
			{Key: "status_provisioned", Value: countPipeline(bson.M{"status": GroupStatusProvisioned})},
			// By type
			{Key: "by_type", Value: bson.A{
				bson.D{{Key: "$group", Value: bson.M{"_id": "$type", "count": bson.M{"$sum": 1}}}},
			}},
			// By visibility
			{Key: "visibility_public", Value: countPipeline(bson.M{"settings.visibility": VisibilityPublic})},
			{Key: "visibility_private", Value: countPipeline(bson.M{"settings.visibility": VisibilityPrivate})},
			{Key: "visibility_internal", Value: countPipeline(bson.M{"settings.visibility": VisibilityInternal})},
			// Integrations
			{Key: "with_slack", Value: countPipeline(bson.M{"integrations.slack": bson.M{"$exists": true, "$ne": nil}})},
			{Key: "with_custom", Value: countPipeline(bson.M{"integrations.custom": bson.M{"$exists": true, "$ne": nil, "$gt": bson.M{}}})},
			// Ownership
			{Key: "with_owner", Value: countPipeline(bson.M{"owner_id": bson.M{"$exists": true, "$ne": ""}})},
			// Total members (sum of members array sizes)
			{Key: "member_totals", Value: bson.A{
				bson.D{{Key: "$project", Value: bson.M{"member_count": bson.M{"$size": bson.M{"$ifNull": bson.A{"$members", bson.A{}}}}}}},
				bson.D{{Key: "$group", Value: bson.M{"_id": nil, "total": bson.M{"$sum": "$member_count"}}}},
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
	type typeCountDoc struct {
		Type  string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	type memberTotalDoc struct {
		Total int64 `bson:"total"`
	}
	type facetResult struct {
		Total              []countDoc       `bson:"total"`
		StatusActive       []countDoc       `bson:"status_active"`
		StatusInactive     []countDoc       `bson:"status_inactive"`
		StatusArchived     []countDoc       `bson:"status_archived"`
		StatusSuspended    []countDoc       `bson:"status_suspended"`
		StatusProvisioned  []countDoc       `bson:"status_provisioned"`
		ByType             []typeCountDoc   `bson:"by_type"`
		VisibilityPublic   []countDoc       `bson:"visibility_public"`
		VisibilityPrivate  []countDoc       `bson:"visibility_private"`
		VisibilityInternal []countDoc       `bson:"visibility_internal"`
		WithSlack          []countDoc       `bson:"with_slack"`
		WithCustom         []countDoc       `bson:"with_custom"`
		WithOwner          []countDoc       `bson:"with_owner"`
		MemberTotals       []memberTotalDoc `bson:"member_totals"`
	}

	var result facetResult
	if err := r.Store.MapOneInCursorToResult(ctx, cursor, &result, "groups-stats-counts"); err != nil {
		return nil, err
	}

	extractCount := func(docs []countDoc) int64 {
		if len(docs) == 0 {
			return 0
		}
		return docs[0].Count
	}

	var totalMembers int64
	if len(result.MemberTotals) > 0 {
		totalMembers = result.MemberTotals[0].Total
	}

	byType := make(GroupsByTypeStats, len(result.ByType))
	for _, tc := range result.ByType {
		byType[tc.Type] = tc.Count
	}

	withSlack := extractCount(result.WithSlack)
	withCustom := extractCount(result.WithCustom)

	withOwner := extractCount(result.WithOwner)

	return &AllGroupsStats{
		Total:        extractCount(result.Total),
		TotalMembers: totalMembers,
		ByStatus: GroupsByStatusStats{
			GroupStatusActive:      extractCount(result.StatusActive),
			GroupStatusInactive:    extractCount(result.StatusInactive),
			GroupStatusArchived:    extractCount(result.StatusArchived),
			GroupStatusSuspended:   extractCount(result.StatusSuspended),
			GroupStatusProvisioned: extractCount(result.StatusProvisioned),
		},
		ByType: byType,
		ByVisibility: GroupsByVisibilityStats{
			VisibilityPublic:   extractCount(result.VisibilityPublic),
			VisibilityPrivate:  extractCount(result.VisibilityPrivate),
			VisibilityInternal: extractCount(result.VisibilityInternal),
		},
		Integrations: GroupIntegrationStats{
			WithSlack:          withSlack,
			WithCustom:         withCustom,
			WithAnyIntegration: withSlack + withCustom,
		},
		Leadership: GroupLeadershipStats{
			WithOwner: withOwner,
			WithAny:   withOwner,
		},
	}, nil
}
