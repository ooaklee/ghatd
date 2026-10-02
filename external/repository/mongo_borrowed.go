package repository

import (
	"context"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// NewMongoDbRepositoryFromDatabase adapts an already managed host database
// without creating, reconnecting or disconnecting its client. This supports
// integrations that receive *mongo.Database instead of a GHATD runtime. The
// original owner must keep the client alive and close it after all consumers.
// A nil logger selects metadata-only Zap logging; pass NoOp explicitly to mute.
func NewMongoDbRepositoryFromDatabase(db *mongo.Database, log RepositoryLogger) (*MongoDbRepository, error) {
	if db == nil {
		return nil, ErrInvalidMongoOperation
	}
	if log == nil {
		log = NewZapRepositoryLogger()
	}
	return NewMongoDbRepository(&borrowedMongoDatabase{database: db}, log, db.Name()), nil
}

// borrowedMongoDatabase implements the manager contract without lifecycle
// ownership. Its configuration is immutable and restricted to one database.
type borrowedMongoDatabase struct {
	database *mongo.Database // database is owned and closed by the host.
}

// GetClient returns the existing client without connecting another pool.
func (b *borrowedMongoDatabase) GetClient(ctx context.Context) (*mongo.Client, error) {
	if ctx == nil {
		return nil, ErrInvalidMongoOperation
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return b.database.Client(), nil
}

// GetDatabase rejects attempts to widen the bound database namespace.
func (b *borrowedMongoDatabase) GetDatabase(ctx context.Context, name string) (*mongo.Database, error) {
	if ctx == nil {
		return nil, ErrInvalidMongoOperation
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if name != "" && name != b.database.Name() {
		return nil, ErrInvalidMongoOperation
	}
	return b.database, nil
}

// Ping checks primary availability without changing client ownership.
func (b *borrowedMongoDatabase) Ping(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidMongoOperation
	}
	return b.database.Client().Ping(ctx, readpref.Primary())
}

// Close deliberately leaves lifecycle management with the original owner.
func (*borrowedMongoDatabase) Close(context.Context) error { return nil }

// Reconnect cannot replace a client shared with other domain repositories.
func (*borrowedMongoDatabase) Reconnect(context.Context) error { return ErrInvalidMongoOperation }

// Health exposes only availability, not server or credential details.
func (b *borrowedMongoDatabase) Health(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"healthy": b.Ping(ctx) == nil, "borrowed": true}
}

// Stats has no owned connection counters; zero values are not pool telemetry.
func (*borrowedMongoDatabase) Stats() repositoryhelpers.ConnectionStats {
	return repositoryhelpers.ConnectionStats{}
}
