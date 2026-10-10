package repositoryhelpers

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// MongoClient defines the interface for MongoDB client operations
type MongoClient interface {
	// GetClient returns the MongoDB client, connecting if necessary; the Handler
	// implementation returns a cached connected client when available.
	GetClient(ctx context.Context) (*mongo.Client, error)
	// GetDatabase returns the named database handle; the Handler implementation
	// obtains the client first and falls back to its configured database name when
	// the name is empty.
	GetDatabase(ctx context.Context, name string) (*mongo.Database, error)
	// Ping verifies connectivity to MongoDB by obtaining the client and issuing a
	// ping.
	Ping(ctx context.Context) error
	// Close disconnects the MongoDB client and marks it disconnected; the Handler
	// implementation is a no-op when no client exists.
	Close(ctx context.Context) error
	// Health returns a map describing connection health; the Handler implementation
	// includes connection statistics, the last error, and a healthy flag derived
	// from a ping when connected.
	Health(ctx context.Context) map[string]interface{}
}

// MongoClientManager defines the interface for managing MongoDB connections
type MongoClientManager interface {
	MongoClient
	// Reconnect re-establishes the MongoDB connection; the Handler implementation
	// closes any existing connection before connecting again under lock.
	Reconnect(ctx context.Context) error
	// Stats returns connection statistics such as connections created and active,
	// last connected time, last error and error count.
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
	// OnConnect notifies the hook that a MongoDB connection to addr was attempted,
	// returning ctx, possibly decorated, for downstream use. Implementations log a
	// masked address, count connections, or reset a half-open circuit breaker.
	OnConnect(ctx context.Context, addr string) context.Context
	// OnDisconnect notifies the hook of a disconnection from addr. Implementations
	// log a masked address and record disconnect counts; the circuit breaker
	// implementation currently takes no action.
	OnDisconnect(ctx context.Context, addr string)
	// OnError reports operation failure err for the named MongoDB operation.
	// Implementations log the error, increment error counters, and may open the
	// circuit breaker once maxErrors is reached.
	OnError(ctx context.Context, err error, operation string)
}
