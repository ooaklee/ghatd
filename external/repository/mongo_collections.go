package repository

import (
	"go.mongodb.org/mongo-driver/mongo"
)

// GetAuditCollection returns collection used for audit domain
func (r MongoDbRepository) GetAuditCollection(client *mongo.Client) *mongo.Collection {
	return client.Database(r.ClientHandler.DB).Collection(string(AuditCollection))
}

// GetApiTokenCollection returns collection used for api token domain
func (r MongoDbRepository) GetApiTokenCollection(client *mongo.Client) *mongo.Collection {
	return client.Database(r.ClientHandler.DB).Collection(string(ApiTokenCollection))
}

// GetUserCollection returns collection used for user domain
func (r MongoDbRepository) GetUserCollection(client *mongo.Client) *mongo.Collection {
	return client.Database(r.ClientHandler.DB).Collection(string(UserCollection))
}
