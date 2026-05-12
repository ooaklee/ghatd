package repository

import (
	"context"
	"fmt"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
)

// MongoRuntime groups the Mongo connection handler and the core repository
// built from it. It is a small host-application bootstrap helper, not a
// service container.
type MongoRuntime struct {
	Handler        *repositoryhelpers.Handler
	CoreRepository *MongoDbRepository
}

// NewMongoRuntimeRequest holds the Mongo settings needed to create a runtime.
type NewMongoRuntimeRequest struct {
	URIConfig repositoryhelpers.MongoURIConfig
	Database  string
	Options   []repositoryhelpers.ConfigOption

	// SkipWarmup skips the initial client connection check. This is useful for
	// tests or tools that want to construct the runtime without opening a socket.
	SkipWarmup bool
}

// NewMongoRuntime creates a Mongo handler, optionally warms it up, and returns
// the matching core GHATD Mongo repository.
func NewMongoRuntime(ctx context.Context, request *NewMongoRuntimeRequest) (*MongoRuntime, error) {
	if request == nil {
		return nil, fmt.Errorf("repository/mongo-runtime-nil-request")
	}

	mongoURI, err := repositoryhelpers.GenerateMongoURI(request.URIConfig)
	if err != nil {
		return nil, fmt.Errorf("repository/mongo-runtime-uri: %w", err)
	}

	mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(
		mongoURI,
		request.Database,
		request.Options...,
	)
	if err != nil {
		return nil, fmt.Errorf("repository/mongo-runtime-handler: %w", err)
	}

	if !request.SkipWarmup {
		if ctx == nil {
			ctx = context.Background()
		}
		if _, err = mongoHandler.GetClient(ctx); err != nil {
			_ = mongoHandler.Close(ctx)
			return nil, fmt.Errorf("repository/mongo-runtime-warmup: %w", err)
		}
	}

	return &MongoRuntime{
		Handler:        mongoHandler,
		CoreRepository: NewMongoDbRepositoryWithDefaults(mongoHandler, request.Database),
	}, nil
}

// Close releases the Mongo handler connection.
func (r *MongoRuntime) Close(ctx context.Context) error {
	if r == nil || r.Handler == nil {
		return nil
	}

	return r.Handler.Close(ctx)
}
