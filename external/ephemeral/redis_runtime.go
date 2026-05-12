package ephemeral

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v7"
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
	if request == nil {
		return nil, fmt.Errorf("ephemeral/redis-runtime-nil-request")
	}
	if request.Options == nil {
		return nil, fmt.Errorf("ephemeral/redis-runtime-missing-options")
	}

	redisClient := redis.NewClient(request.Options)
	for _, hook := range request.Hooks {
		if hook != nil {
			redisClient.AddHook(hook)
		}
	}

	if !request.SkipPing {
		if _, err := redisClient.Ping().Result(); err != nil {
			_ = redisClient.Close()
			return nil, fmt.Errorf("ephemeral/redis-runtime-ping: %w", err)
		}
	}

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
	if r == nil || r.Client == nil {
		return nil
	}

	return r.Client.Close()
}
