package adminaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accesspolicy"
)

// ErrReview means the bound review expired, changed, was cancelled or was spent.
var ErrReview = errors.New("adminaccess/review-required")

// ErrProof is deliberately independent of login/OAuth codes and account existence.
var ErrProof = errors.New("adminaccess/code-invalid")

// ErrCooldown bounds email sends across tabs and sessions belonging to one actor.
var ErrCooldown = errors.New("adminaccess/code-cooldown")

// ErrUnavailable hides storage/delivery diagnostics at the HTTP reply boundary.
var ErrUnavailable = errors.New("adminaccess/unavailable")

var opaqueID = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// review is a private, immutable proposal. Redis alone owns its lifecycle state;
// neither a browser field nor a freshly-issued session establishes approval.
type review struct {
	// ID prevents a response from an older preview authorizing its replacement.
	ID string `json:"id"`
	// Target is a stored user ID, never the requesting administrator.
	Target string `json:"target"`
	// Limits and Revision freeze the exact command reviewed by the operator.
	Limits   accesspolicy.TokenLimits `json:"limits"`
	Revision int64                    `json:"revision"`
	// Expires is an absolute deadline in milliseconds, never extended by verification.
	Expires int64 `json:"expires"`
	// ObservedAt is Redis time for a clock-skew-independent browser countdown.
	ObservedAt int64 `json:"-"`
}

// RedisStore owns purpose-isolated ephemeral approvals, not session or grant
// persistence. Consume is atomic only within Redis, not with a subsequent Mongo
// policy transaction. A spent approval is never restored after an uncertain write.
type RedisStore struct {
	// client shares the host's managed connection and logging/telemetry hooks.
	client *redis.Client
	// prefix isolates deployment and purpose; the hash tag keeps script keys colocated.
	prefix string
}

// NewRedisStore requires the existing managed Redis client and a host namespace.
// It performs no network calls and does not acquire lifecycle ownership.
func NewRedisStore(client *redis.Client, namespace string) (*RedisStore, error) {
	if client == nil || namespace == "" {
		return nil, ErrUnavailable
	}
	return &RedisStore{client: client, prefix: "adminaccess:{" + digest(namespace) + "}:"}, nil
}

// randomID supplies a 256-bit opaque CSRF/review handle; none are placed in URLs.
func randomID() string {
	var b [32]byte
	// Go 1.26 crypto/rand.Read terminates on entropy failure; it never returns
	// a short read or a recoverable error. There is no zero/randomness fallback.
	_, _ = rand.Read(b[:])
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// digest domain-separates private bindings and code material before persistence.
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// Create issues a session-bound page context. Its 15-minute lifetime is not an
// elevation: each reviewed operation gets its own shorter deadline and proof.
func (s *RedisStore) Create(ctx context.Context, binding, actor string) (string, error) {
	id := randomID()
	n, err := s.client.WithContext(ctx).Eval(`if tonumber(redis.call('GET', KEYS[2]) or '0') >= 10 then return 0 end
local n = redis.call('INCR', KEYS[2]); if n == 1 then redis.call('EXPIRE', KEYS[2], 900) end
redis.call('HSET', KEYS[1], 'binding', ARGV[1]); redis.call('PEXPIRE', KEYS[1], 900000); return 1`, []string{s.prefix + id, s.prefix + "contexts:" + digest(actor)}, binding).Int64()
	if err != nil {
		return "", ErrUnavailable
	}
	if n != 1 {
		return "", ErrCooldown
	}
	return id, nil
}

// transition is the sole state mutator. Every operation first checks the exact
// initiating account/session/email binding and (except preview) review version.
// Redis TIME defines deadlines consistently across host instances.
const transition = `
if redis.call('HGET', KEYS[1], 'binding') ~= ARGV[1] then return {-1} end
local op = ARGV[2]
if op == 'check' then return {1} end
if op == 'preview' then
 local tm = redis.call('TIME'); local now = tm[1]*1000 + math.floor(tm[2]/1000)
 local expires = now + math.min(tonumber(ARGV[5]), redis.call('PTTL', KEYS[1]))
 redis.call('HSET', KEYS[1], 'review', ARGV[3], 'payload', ARGV[4], 'expires', expires, 'state', 'reviewed', 'attempts', 0)
 redis.call('HDEL', KEYS[1], 'code')
 return {1, ARGV[4], tostring(expires), tostring(now)}
end
if redis.call('HGET', KEYS[1], 'review') ~= ARGV[3] then return {-1} end
if op == 'cancel' then
 redis.call('HDEL', KEYS[1], 'review', 'payload', 'expires', 'code', 'state', 'attempts')
 return {1}
end
local tm = redis.call('TIME'); local now = tm[1]*1000 + math.floor(tm[2]/1000)
if tonumber(redis.call('HGET', KEYS[1], 'expires') or '0') <= now then return {-1} end
local state = redis.call('HGET', KEYS[1], 'state')
if op == 'read' then
 if state == 'spent' then return {-1} end
 return {1, redis.call('HGET', KEYS[1], 'payload'), redis.call('HGET', KEYS[1], 'expires'), tostring(now)}
end
if op == 'challenge' then
 if state ~= 'reviewed' then return {-1} end
 if redis.call('EXISTS', KEYS[2]) == 1 or tonumber(redis.call('GET', KEYS[3]) or '0') >= 10 then return {-3} end
 redis.call('SET', KEYS[2], '1', 'EX', 60)
 local n = redis.call('INCR', KEYS[3]); if n == 1 then redis.call('EXPIRE', KEYS[3], 3600) end
 redis.call('HSET', KEYS[1], 'state', 'challenged', 'code', ARGV[4])
 return {1}
end
if op == 'confirm' then
 if state ~= 'challenged' then return {-1} end
 local attempts = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
 if attempts > 5 then return {-1} end
 if redis.call('HGET', KEYS[1], 'code') ~= ARGV[4] then
  if attempts == 5 then redis.call('HSET', KEYS[1], 'state', 'spent'); redis.call('HDEL', KEYS[1], 'code') end
  return {-2}
 end
 redis.call('HSET', KEYS[1], 'state', 'approved'); redis.call('HDEL', KEYS[1], 'code')
 return {1}
end
if op == 'consume' then
 if state ~= 'approved' then return {-1} end
 redis.call('HSET', KEYS[1], 'state', 'spent')
 return {1, redis.call('HGET', KEYS[1], 'payload')}
end
return {-1}
`

// run validates untrusted handles before key construction and exposes only
// stable errors. Cooldown keys contain a hash of the verified actor, not email.
func (s *RedisStore) run(ctx context.Context, id, binding, operation, reviewID, value, actor string, window time.Duration) (review, error) {
	if !opaqueID.MatchString(id) || !opaqueID.MatchString(reviewID) {
		return review{}, ErrReview
	}
	output, err := s.client.WithContext(ctx).Eval(transition, []string{s.prefix + id, s.prefix + "cooldown:" + digest(actor), s.prefix + "hour:" + digest(actor)}, binding, operation, reviewID, value, window.Milliseconds()).Result()
	result, ok := output.([]interface{})
	if err != nil || !ok || len(result) == 0 {
		return review{}, ErrUnavailable
	}
	switch result[0] {
	case int64(-1):
		return review{}, ErrReview
	case int64(-2):
		return review{}, ErrProof
	case int64(-3):
		return review{}, ErrCooldown
	case int64(1):
	default:
		return review{}, ErrUnavailable
	}
	var r review
	if len(result) > 1 {
		raw, ok := result[1].(string)
		if !ok || json.Unmarshal([]byte(raw), &r) != nil {
			return review{}, ErrUnavailable
		}
	}
	if len(result) > 2 {
		raw, ok := result[2].(string)
		if !ok {
			return review{}, ErrUnavailable
		}
		r.Expires, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return review{}, ErrUnavailable
		}
	}
	if len(result) > 3 {
		raw, ok := result[3].(string)
		if !ok {
			return review{}, ErrUnavailable
		}
		r.ObservedAt, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return review{}, ErrUnavailable
		}
	}
	return r, nil
}

// codeHash salts a twelve-character proof with the unguessable, purpose-specific
// review ID. No code or proof is placed in a URL, log, cookie or HTTP response.
func codeHash(reviewID, code string) string {
	return digest("admin-token-allowance\x00" + reviewID + "\x00" + code)
}
