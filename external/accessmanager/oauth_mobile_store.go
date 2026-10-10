package accessmanager

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/oauth"
)

const (
	// mobileStartTTLCap bounds browser-start ticket lifetime in Redis.
	mobileStartTTLCap = 120 * time.Second
	// mobileGrantTTLCap bounds native exchange-grant lifetime in Redis.
	mobileGrantTTLCap = 60 * time.Second
)

// mobileTicketPattern bounds ticket/code identifiers to 43-char base64url
// strings (32 random bytes), preventing arbitrary Redis key injection.
var mobileTicketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// mobileGrantEnvelope is the JSON value stored for an exchange grant.
type mobileGrantEnvelope struct {
	Challenge   string `json:"challenge"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	Payload     []byte `json:"payload"` // base64-encoded by encoding/json
}

// MobileOAuthStore is the one-use storage contract for the mobile oauth
// browser handoff: an atomic start ticket plus an atomic, proof-checked
// exchange grant.
type MobileOAuthStore interface {
	// SaveStart persists the start-ticket payload once under the ticket key with
	// the given TTL, supporting the one-use browser handoff contract.
	SaveStart(ctx context.Context, ticket string, payload []byte, ttl time.Duration) error
	// ConsumeStart atomically returns and deletes the start payload for the ticket
	// so it cannot be replayed.
	ConsumeStart(ctx context.Context, ticket string) ([]byte, error)
	// SaveGrant persists the exchange grant once, binding the PKCE challenge,
	// redirect URI, state and payload under the code with the given TTL.
	SaveGrant(ctx context.Context, code string, challenge string, redirectURI string, state string, payload []byte, ttl time.Duration) error
	// ConsumeGrant atomically returns and deletes the grant payload only when
	// challenge, redirect URI and state match; mismatch leaves the grant stored.
	ConsumeGrant(ctx context.Context, code string, challenge string, redirectURI string, state string) ([]byte, error)
}

// RedisMobileOAuthStore is the Redis-backed MobileOAuthStore. All saves use
// SET NX with a TTL; all consumes are atomic so every artifact is usable
// exactly once.
type RedisMobileOAuthStore struct {
	client oauth.RedisTransactionClient
	prefix string
}

// NewRedisMobileOAuthStore scopes mobile oauth artifacts under
// oauth:mobile:<namespace>:. A nil client fails closed on every operation.
func NewRedisMobileOAuthStore(client oauth.RedisTransactionClient, namespace string) MobileOAuthStore {
	if client == nil {
		return &RedisMobileOAuthStore{prefix: "oauth:mobile:"}
	}
	return &RedisMobileOAuthStore{
		client: client,
		prefix: "oauth:mobile:" + namespace + ":",
	}
}

// contextualClient propagates cancellation without mutating the shared
// client, mirroring the oauth transaction store helper.
func (s *RedisMobileOAuthStore) contextualClient(ctx context.Context) oauth.RedisTransactionClient {
	switch client := s.client.(type) {
	case *redis.Client:
		return client.WithContext(ctx)
	case *redis.ClusterClient:
		return client.WithContext(ctx)
	case *redis.Ring:
		return client.WithContext(ctx)
	case interface {
		WithContext(context.Context) oauth.RedisTransactionClient
	}:
		return client.WithContext(ctx)
	default:
		return s.client
	}
}

// startKey hashes the opaque ticket into the store's browser-start namespace.
func (s *RedisMobileOAuthStore) startKey(id string) string { return s.prefix + "start:" + id }

// grantKey hashes the opaque code into the store's native-exchange namespace.
func (s *RedisMobileOAuthStore) grantKey(id string) string { return s.prefix + "grant:" + id }

// validMobileID reports whether an opaque handle is a 43-char base64url
// string, bounding the Redis keyspace.
func validMobileID(id string) bool {
	return mobileTicketPattern.MatchString(id)
}

// cappedTTL clamps a requested TTL to the store's upper bound and rejects
// nonpositive values.
func cappedTTL(ttl, cap time.Duration) (time.Duration, error) {
	if ttl <= 0 {
		return 0, errors.New("mobile oauth ttl must be positive")
	}
	if ttl > cap {
		return cap, nil
	}
	return ttl, nil
}

// SaveStart persists the start-ticket payload once under SET NX semantics.
func (s *RedisMobileOAuthStore) SaveStart(ctx context.Context, ticket string, payload []byte, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return oauth.ErrSecureTransactionInvalidState
	}
	if !validMobileID(ticket) {
		return oauth.ErrSecureTransactionInvalidState
	}
	ttl, err := cappedTTL(ttl, mobileStartTTLCap)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	inserted, err := s.contextualClient(ctx).SetNX(s.startKey(ticket), payload, ttl).Result()
	if err != nil {
		return err
	}
	if !inserted {
		return oauth.ErrSecureTransactionInvalidState
	}
	return nil
}

// ConsumeStart atomically returns and deletes the start payload so the
// ticket can never be replayed.
func (s *RedisMobileOAuthStore) ConsumeStart(ctx context.Context, ticket string) ([]byte, error) {
	if s == nil || s.client == nil || !validMobileID(ticket) {
		return nil, oauth.ErrSecureTransactionNotFound
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.contextualClient(ctx).Eval(
		`local v = redis.call('GET', KEYS[1]); if v then redis.call('DEL', KEYS[1]) end; return v`,
		[]string{s.startKey(ticket)},
	).Result()
	if err == redis.Nil {
		return nil, oauth.ErrSecureTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	raw, ok := value.(string)
	if !ok {
		return nil, oauth.ErrSecureTransactionNotFound
	}
	return []byte(raw), nil
}

// SaveGrant persists the exchange grant once; the value is a JSON envelope
// binding the expected PKCE challenge, redirect URI and state.
func (s *RedisMobileOAuthStore) SaveGrant(ctx context.Context, code string, challenge string, redirectURI string, state string, payload []byte, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return oauth.ErrSecureTransactionInvalidState
	}
	if !validMobileID(code) || !validMobileID(challenge) || !validMobileID(state) || redirectURI == "" {
		return oauth.ErrSecureTransactionInvalidState
	}
	ttl, err := cappedTTL(ttl, mobileGrantTTLCap)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	value, err := json.Marshal(mobileGrantEnvelope{
		Challenge:   challenge,
		RedirectURI: redirectURI,
		State:       state,
		Payload:     payload,
	})
	if err != nil {
		return err
	}
	inserted, err := s.contextualClient(ctx).SetNX(s.grantKey(code), value, ttl).Result()
	if err != nil {
		return err
	}
	if !inserted {
		return oauth.ErrSecureTransactionInvalidState
	}
	return nil
}

// mobileConsumeGrantScript deletes the grant only when every stored proof
// matches; a wrong proof leaves the grant in place for its rightful owner.
// Returns nil when the grant is missing, 0 on proof mismatch, otherwise the
// envelope value (which is consumed atomically).
var mobileConsumeGrantScript = `
local v = redis.call('GET', KEYS[1])
if not v then return nil end
local e = cjson.decode(v)
if e.challenge == ARGV[1] and e.redirect_uri == ARGV[2] and e.state == ARGV[3] then
	redis.call('DEL', KEYS[1])
	return v
end
return 0
`

// ConsumeGrant atomically returns and deletes the grant payload only when
// challenge, redirect URI and state all match. Any mismatch returns
// ErrSecureTransactionInvalidState and leaves the grant untouched so the
// rightful owner can still redeem it.
func (s *RedisMobileOAuthStore) ConsumeGrant(ctx context.Context, code string, challenge string, redirectURI string, state string) ([]byte, error) {
	if s == nil || s.client == nil || !validMobileID(code) {
		return nil, oauth.ErrSecureTransactionInvalidState
	}
	if challenge == "" || redirectURI == "" || state == "" {
		return nil, oauth.ErrSecureTransactionInvalidState
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.contextualClient(ctx).Eval(mobileConsumeGrantScript, []string{s.grantKey(code)},
		challenge, redirectURI, state,
	).Result()
	if err == redis.Nil {
		return nil, oauth.ErrSecureTransactionNotFound
	}
	if err != nil {
		return nil, err
	}
	switch matched := value.(type) {
	case int64:
		// Script found the grant but the proofs did not match; the grant
		// remains stored for its rightful owner. Return a non-leaking
		// invalid-state error.
		if matched == 0 {
			return nil, oauth.ErrSecureTransactionInvalidState
		}
	case string:
		var envelope mobileGrantEnvelope
		if err := json.Unmarshal([]byte(matched), &envelope); err != nil {
			// Corrupt or tampered value: consume it and fail closed.
			_, _ = s.contextualClient(ctx).Eval(`return redis.call('DEL', KEYS[1])`, []string{s.grantKey(code)}).Result()
			return nil, oauth.ErrSecureTransactionInvalidState
		}
		return envelope.Payload, nil
	}
	return nil, oauth.ErrSecureTransactionNotFound
}
