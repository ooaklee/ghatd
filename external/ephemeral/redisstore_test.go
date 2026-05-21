package ephemeral

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/stretchr/testify/require"
)

// fakePersistentClient implements the Redis commands used by these store tests.
type fakePersistentClient struct {
	values map[string]string
}

// newFakePersistentClient creates an in-memory Redis-like client for store tests.
func newFakePersistentClient() *fakePersistentClient {
	return &fakePersistentClient{values: map[string]string{}}
}

func (f *fakePersistentClient) Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	f.values[key] = value.(string)
	return redis.NewStatusResult("OK", nil)
}

func (f *fakePersistentClient) SetNX(key string, value interface{}, expiration time.Duration) *redis.BoolCmd {
	if _, ok := f.values[key]; ok {
		return redis.NewBoolResult(false, nil)
	}

	f.values[key] = value.(string)
	return redis.NewBoolResult(true, nil)
}

func (f *fakePersistentClient) Get(key string) *redis.StringCmd {
	value, ok := f.values[key]
	if !ok {
		return redis.NewStringResult("", redis.Nil)
	}

	return redis.NewStringResult(value, nil)
}

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

func (f *fakePersistentClient) Incr(key string) *redis.IntCmd {
	return redis.NewIntResult(1, nil)
}

func (f *fakePersistentClient) Scan(cursor uint64, match string, count int64) *redis.ScanCmd {
	return redis.NewScanCmdResult([]string{}, 0, nil)
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
