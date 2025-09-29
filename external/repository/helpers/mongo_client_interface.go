package repositoryhelpers

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// MongoClient defines the interface for MongoDB client operations
type MongoClient interface {
	GetClient(ctx context.Context) (*mongo.Client, error)
	GetDatabase(ctx context.Context, name string) (*mongo.Database, error)
	Ping(ctx context.Context) error
	Close(ctx context.Context) error
	Health(ctx context.Context) map[string]interface{}
}

// MongoClientManager defines the interface for managing MongoDB connections
type MongoClientManager interface {
	MongoClient
	Reconnect(ctx context.Context) error
	Stats() ConnectionStats
}

// ConnectionStats provides connection statistics
type ConnectionStats struct {
	ConnectionsCreated int64
	ConnectionsActive  int64
	LastConnected      time.Time
	LastError          error
	ErrorCount         int64
}

// MonitoringHook defines the interface for MongoDB operation monitoring
type MonitoringHook interface {
	OnConnect(ctx context.Context, addr string) context.Context
	OnDisconnect(ctx context.Context, addr string)
	OnError(ctx context.Context, err error, operation string)
}
