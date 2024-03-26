package repository

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/audit"
	"go.mongodb.org/mongo-driver/bson"
)

// GetTotalAuditLogEvents total log entries token from DB that match passed arguments
func (r MongoDbRepository) GetTotalAuditLogEvents(ctx context.Context, userId string, to string, from string, domains string, actions []audit.AuditAction, targetId string, targetTypes []audit.TargetType) (int64, error) {

	// Example mongo query
	// /// get log events total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b" })
	// /// get standard viewing total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b", action: {"$in": ["VIEWING_STANDARD"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })
	// /// get advance viewing total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b", action: {"$in": ["VIEWING_ADVANCE"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })
	// /// get domain total
	// db.getCollection("audit").countDocuments({actor_id: "cdd23f83-23ab-4019-8cce-038e51cbab0b",domain: {"$in": ["contentmanager"]} , action_at: {
	// 	$gt: '2023-12-10T00:00:00.000Z',
	// 	$lt: '2023-12-10T01:31:00.000Z'
	//   } })

	auditFilter := bson.M{"actor_id": userId}

	if domains != "" {
		auditFilter["domain"] = bson.M{"$in": strings.Split(domains, ",")}
	}

	if len(actions) > 0 || actions != nil {
		auditFilter["action"] = bson.M{"$in": actions}
	}

	if targetId != "" {
		auditFilter["target_id"] = targetId
	}

	if len(targetTypes) > 0 || targetTypes != nil {
		auditFilter["target_types"] = bson.M{"$in": targetTypes}
	}

	if to != "" || from != "" {

		timeRangeFilter := bson.M{}

		if from != "" {
			timeRangeFilter["$gt"] = from
		}

		if to != "" {
			timeRangeFilter["$lt"] = to
		}

		auditFilter["action_at"] = timeRangeFilter
	}

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return 0, err
	}

	collection := r.GetAuditCollection(client)

	return ExecuteCountDocuments(ctx, collection, auditFilter)
}

// CreateAuditLogEvent creates an log event in the DB
func (r MongoDbRepository) CreateAuditLogEvent(ctx context.Context, event *audit.AuditLogEntry) error {

	// NICE_TO_HAVE: Wrap context with observability platform transaction

	client, err := r.InitialiseClient(ctx)
	if err != nil {
		return err
	}

	collection := r.GetAuditCollection(client)

	event.SetActionAtTimeToNow().GenerateNewUuid()

	_, err = collection.InsertOne(ctx, event)
	if err != nil {
		return err
	}

	return nil
}
