package ephemeral

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/stretchr/testify/require"
)

// fakePersistentClient implements the Redis commands used by these store tests.
type fakePersistentClient struct {
	values   map[string]string
	contexts []context.Context
}

// newFakePersistentClient creates an in-memory Redis-like client for store tests.
func newFakePersistentClient() *fakePersistentClient {
	return &fakePersistentClient{values: map[string]string{}}
}

// WithContext records the caller context and returns the fake client.
func (f *fakePersistentClient) WithContext(ctx context.Context) PersistentClient {
	f.contexts = append(f.contexts, ctx)
	return f
}

// Set stores a string representation of the supplied value.
func (f *fakePersistentClient) Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	f.values[key] = fmt.Sprint(value)
	return redis.NewStatusResult("OK", nil)
}

// SetNX stores a value only when the key does not already exist.
func (f *fakePersistentClient) SetNX(key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	if _, ok := f.values[key]; ok {
		return redis.NewBoolResult(false, nil)
	}

	f.values[key] = fmt.Sprint(value)
	return redis.NewBoolResult(true, nil)
}

// Get returns the value associated with a key or redis.Nil when it is absent.
func (f *fakePersistentClient) Get(key string) *redis.StringCmd {
	value, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}

	return redis.NewStringResult(value, nil)
}

// Del removes the supplied keys and reports how many existed.
func (f *fakePersistentClient) Del(keys ...string) *redis.IntCmd {
	var deleted int64
	for _, key := range keys {
		if _, ok := f.values[key]; ok {
			delete(f.values, key)
			deleted++
		}
	}

	return redis.NewIntResult(deleted, nil)
}

// Incr returns a successful increment result for store tests.
func (f *fakePersistentClient) Incr(key string) *redis.IntCmd {
	return redis.NewIntResult(1, nil)
}

// Scan returns an empty completed scan for store tests.
func (f *fakePersistentClient) Scan(cursor uint64, match string, count int64) *redis.ScanCmd {
	return redis.NewScanCmdResult([]string{}, 0, nil)
}

// TestRedisStorePropagatesContextToEveryCommandType verifies every Redis command receives its caller context.
func TestRedisStorePropagatesContextToEveryCommandType(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-context")
	client := newFakePersistentClient()
	store := NewRedisStore(client, 10, "Astr", "local")

	require.NoError(t, store.StoreToken(ctx, "token", "user", time.Minute))
	_, err := store.AcquireRefreshTokenRotationLock(ctx, "user", "refresh", time.Minute)
	require.NoError(t, err)
	_, err = store.GetRefreshTokenRotationResult(ctx, "missing-user", "missing-refresh")
	require.NoError(t, err)
	_, err = store.ReleaseRefreshTokenRotationLock(ctx, "user", "refresh")
	require.NoError(t, err)
	require.NoError(t, store.DeleteAllTokenExceptedSpecified(ctx, "user", nil))
	require.NoError(t, store.incrementAndUpdateRequestCountEntry(ctx, "request-count"))

	require.Len(t, client.contexts, 6)
	for _, commandContext := range client.contexts {
		require.Equal(t, "request-context", commandContext.Value(contextKey{}))
	}
}

// TestRedisStoreClonesGoRedisClientWithContext verifies contextual clients do not mutate the shared client.
func TestRedisStoreClonesGoRedisClientWithContext(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })
	store := NewRedisStore(redisClient, 10, "Astr", "local")
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request-context")

	contextualClient, ok := store.clientForContext(ctx).(*redis.Client)
	require.True(t, ok)
	require.NotSame(t, redisClient, contextualClient)
	require.Equal(t, "request-context", contextualClient.Context().Value(contextKey{}))
	require.Nil(t, redisClient.Context().Value(contextKey{}))
}

// TestRefreshTokenRotationResultStore verifies refresh rotation replay payload persistence.
func TestRefreshTokenRotationResultStore(t *testing.T) {
	t.Parallel()

	store := NewRedisStore(newFakePersistentClient(), 10, "Astr", "local")
	ctx := context.Background()

	got, err := store.GetRefreshTokenRotationResult(ctx, "user-1", "old-refresh")
	require.NoError(t, err)
	require.Nil(t, got)

	result := &RefreshTokenRotationResult{
		AccessToken:           "access-token",
		RefreshToken:          "refresh-token",
		AccessTokenExpiresAt:  100,
		RefreshTokenExpiresAt: 200,
	}

	require.NoError(t, store.StoreRefreshTokenRotationResult(ctx, "user-1", "old-refresh", result, time.Second))

	got, err = store.GetRefreshTokenRotationResult(ctx, "user-1", "old-refresh")
	require.NoError(t, err)
	require.Equal(t, result, got)

	require.Error(t, store.StoreRefreshTokenRotationResult(ctx, "user-1", "old-refresh", nil, time.Second))
}

// TestRefreshTokenRotationLock verifies one caller can hold a refresh rotation lock at a time.
func TestRefreshTokenRotationLock(t *testing.T) {
	t.Parallel()

	store := NewRedisStore(newFakePersistentClient(), 10, "Astr", "local")
	ctx := context.Background()

	acquired, err := store.AcquireRefreshTokenRotationLock(ctx, "user-1", "old-refresh", time.Second)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = store.AcquireRefreshTokenRotationLock(ctx, "user-1", "old-refresh", time.Second)
	require.NoError(t, err)
	require.False(t, acquired)

	deleted, err := store.ReleaseRefreshTokenRotationLock(ctx, "user-1", "old-refresh")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	acquired, err = store.AcquireRefreshTokenRotationLock(ctx, "user-1", "old-refresh", time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
}

// TestLoginEmailCooldown verifies login-email cooldown keys suppress duplicate sends per context.
func TestLoginEmailCooldown(t *testing.T) {
	t.Parallel()

	store := NewRedisStore(newFakePersistentClient(), 10, "Astr", "local")
	ctx := context.Background()

	acquired, err := store.AcquireLoginEmailCooldown(ctx, "user-1", false, "/app", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	acquired, err = store.AcquireLoginEmailCooldown(ctx, "user-1", false, "/app", time.Minute)
	require.NoError(t, err)
	require.False(t, acquired)

	acquired, err = store.AcquireLoginEmailCooldown(ctx, "user-1", false, "/different", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)

	deleted, err := store.ReleaseLoginEmailCooldown(ctx, "user-1", false, "/app")
	require.NoError(t, err)
	require.EqualValues(t, 1, deleted)

	acquired, err = store.AcquireLoginEmailCooldown(ctx, "user-1", false, "/app", time.Minute)
	require.NoError(t, err)
	require.True(t, acquired)
}
