package oauth

import (
	"context"
	"encoding/json"
	"github.com/go-redis/redis/v7"
	"strings"
	"time"
)

// RedisTransactionClient supplies the atomic Redis operations required here.
type RedisTransactionClient interface {
	// SetNX sets the key to the value only if absent, with the given time-to-live,
	// returning whether the value was set.
	SetNX(string, interface{}, time.Duration) *redis.BoolCmd
	// Eval executes a Lua script against the given Redis keys with the supplied
	// arguments, returning the raw command result.
	Eval(string, []string, ...interface{}) *redis.Cmd
}

// RedisTransactionStore stores opaque transactions with atomic consumption.
type RedisTransactionStore struct {
	client RedisTransactionClient
	prefix string
}

// NewRedisTransactionStore scopes transactions to a host's Redis namespace.
func NewRedisTransactionStore(client RedisTransactionClient, namespace ...string) *RedisTransactionStore {
	prefix := "oauth:txn:"
	if len(namespace) > 0 {
		prefix += strings.TrimSpace(namespace[0]) + ":"
	}
	return &RedisTransactionStore{client: client, prefix: prefix}
}

// contextualClient propagates cancellation without mutating the shared client.
func (s *RedisTransactionStore) contextualClient(ctx context.Context) RedisTransactionClient {
	switch client := s.client.(type) {
	case *redis.Client:
		return client.WithContext(ctx)
	case *redis.ClusterClient:
		return client.WithContext(ctx)
	case *redis.Ring:
		return client.WithContext(ctx)
	case interface {
		WithContext(context.Context) RedisTransactionClient
	}:
		return client.WithContext(ctx)
	default:
		return s.client
	}
}

// Save persists a transaction once; random-handle collisions fail closed.
func (s *RedisTransactionStore) Save(ctx context.Context, txn *StoredTransaction) error {
	if s == nil || s.client == nil || txn == nil || txn.State == "" || txn.Nonce == "" {
		return ErrSecureTransactionInvalidState
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	deadline := time.Now().UTC().Add(TransactionTTL)
	if txn.ExpiresAt.IsZero() || txn.ExpiresAt.After(deadline) {
		txn.ExpiresAt = deadline
	}
	ttl := time.Until(txn.ExpiresAt)
	if ttl <= 0 {
		return ErrSecureTransactionExpired
	}
	payload, err := json.Marshal(txn)
	if err != nil {
		return err
	}
	inserted, err := s.contextualClient(ctx).SetNX(s.prefix+txn.State, payload, ttl).Result()
	if err != nil {
		return err
	}
	if !inserted {
		return ErrSecureTransactionInvalidState
	}
	return nil
}

// Consume atomically returns and deletes a transaction, preventing replays.
func (s *RedisTransactionStore) Consume(ctx context.Context, id string) (*StoredTransaction, error) {
	if s == nil || s.client == nil || id == "" {
		return nil, ErrSecureTransactionNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.contextualClient(ctx).Eval(`local v = redis.call('GET', KEYS[1]); if v then redis.call('DEL', KEYS[1]) end; return v`, []string{s.prefix + id}).Result()
	if err == redis.Nil {
		return nil, ErrSecureTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	raw, ok := value.(string)
	if !ok {
		return nil, ErrSecureTransactionNotFound
	}
	var txn StoredTransaction
	if err := json.Unmarshal([]byte(raw), &txn); err != nil {
		return nil, ErrSecureTransactionInvalidState
	}
	if txn.ExpiresAt.IsZero() || !time.Now().Before(txn.ExpiresAt) {
		return nil, ErrSecureTransactionExpired
	}
	return &txn, nil
}
