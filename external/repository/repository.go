package repository

import (
	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
)

// MongoDbRepository acts as the medium to connect to communicate with underlying
// DB
type MongoDbRepository struct {
	clientHandler *repositoryhelpers.Handler
}

// NewMongoDbRepository creates mongo repository
func NewMongoDbRepository(handler *repositoryhelpers.Handler) *MongoDbRepository {
	return &MongoDbRepository{
		clientHandler: handler,
	}
}
