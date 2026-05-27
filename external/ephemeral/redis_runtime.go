package ephemeral

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// RedisRuntime groups a Redis client with the GHATD ephemeral store built from it.
type RedisRuntime struct {
	Client *redis.Client
	Store  *Client
}

// NewRedisRuntimeRequest holds the Redis bootstrap inputs.
type NewRedisRuntimeRequest struct {
	Options *redis.Options
	Hooks   []redis.Hook

	MaxUnauthedRequestAllowance int64
	Component                   string
	Environment                 string

	// SkipPing skips the initial Redis ping. This is useful for tests and
	// command paths that want to construct the runtime without opening a socket.
	SkipPing bool
}

// NewRedisRuntime creates a Redis client, attaches hooks, optionally pings it,
// and builds the GHATD ephemeral store.
func NewRedisRuntime(ctx context.Context, request *NewRedisRuntimeRequest) (*RedisRuntime, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "new-redis-runtime")
	if request == nil {
		logger.Warn("redis-runtime-nil-request")
		return nil, fmt.Errorf("ephemeral/redis-runtime-nil-request")
	}
	if request.Options == nil {
		logger.Warn("redis-runtime-missing-options")
		return nil, fmt.Errorf("ephemeral/redis-runtime-missing-options")
	}

	logger.Info("redis-runtime-initialising", zap.String("addr", request.Options.Addr), zap.Int("db", request.Options.DB), zap.Int("hooks", len(request.Hooks)), zap.Bool("skip-ping", request.SkipPing))
	redisClient := redis.NewClient(request.Options)
	for _, hook := range request.Hooks {
		if hook != nil {
			redisClient.AddHook(hook)
		}
	}

	if !request.SkipPing {
		if _, err := redisClient.Ping().Result(); err != nil {
			_ = redisClient.Close()
			logger.Error("redis-runtime-ping-failed", zap.String("addr", request.Options.Addr), zap.Error(err))
			return nil, fmt.Errorf("ephemeral/redis-runtime-ping: %w", err)
		}
	}

	logger.Info("redis-runtime-ready", zap.String("addr", request.Options.Addr), zap.Int("db", request.Options.DB))
	return &RedisRuntime{
		Client: redisClient,
		Store: NewRedisStore(
			redisClient,
			request.MaxUnauthedRequestAllowance,
			request.Component,
			request.Environment,
		),
	}, nil
}

// Close closes the underlying Redis client.
func (r *RedisRuntime) Close(ctx context.Context) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "close-redis-runtime")
	if r == nil || r.Client == nil {
		logger.Debug("redis-runtime-close-skipped")
		return nil
	}

	if err := r.Client.Close(); err != nil {
		logger.Error("redis-runtime-close-failed", zap.Error(err))
		return err
	}

	logger.Info("redis-runtime-closed")
	return nil
}
