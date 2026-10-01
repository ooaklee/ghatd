package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/url"
	"regexp"
	"time"

	"github.com/go-redis/redis/v7"
)

const DisconnectChallengeTTL = 10 * time.Minute
const DisconnectResendCooldownTTL = time.Minute
const DisconnectMaxProofAttempts = 5

var ErrDisconnectProofInvalid = errors.New("DisconnectProofInvalid")
var ErrDisconnectChallengeLocked = errors.New("DisconnectChallengeLocked")
var ErrDisconnectCooldown = errors.New("DisconnectCooldown")
var disconnectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var disconnectSchemePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z0-9-]+)+$`)

// ValidDisconnectRedirectURI validates an exact private-scheme Settings return
// address. Register its scheme separately from the OAuth browser callback.
func ValidDisconnectRedirectURI(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 512 && disconnectSchemePattern.MatchString(u.Scheme) && u.Host == "" && u.User == nil && u.Opaque == "" && u.Path == "/oauth/disconnect" && u.RawPath == "" && u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && raw == u.Scheme+":/oauth/disconnect"
}

// DisconnectIDPattern matches the high-entropy challenge identifiers hosts may
// validate before touching the store.
func DisconnectIDPattern() *regexp.Regexp { return disconnectIDPattern }

// DisconnectChallenge stores only hashed proof and a private account snapshot.
// Its namespace and purpose are independent of login codes and tokens.
type DisconnectChallenge struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	AccessUUID string `json:"access_uuid"`
	Provider   string `json:"provider"`
	// Empty identifies web proof. Native proof retains its exact allowlisted
	// Settings return address through all stages and cannot cross transports.
	RedirectURI string `json:"redirect_uri,omitempty"`
	// Stage distinguishes a current-email approval challenge ("current_email"),
	// which grants only the right to issue a candidate challenge, from a final
	// sign-in-email challenge ("sign_in_email"). Empty means a legacy
	// single-stage final challenge.
	Stage string `json:"stage,omitempty"`
	// AuthTimeMillis is the account-freshness timestamp the completion session
	// will carry once this stage's proof is consumed (or, for first stages, once
	// the follow-up final proof is consumed).
	AuthTimeMillis int64  `json:"auth_time_ms,omitempty"`
	Payload        []byte `json:"payload"`
	CodeHash       string `json:"code_hash"`
	TokenHash      string `json:"token_hash"`
	ExpiresAt      int64  `json:"expires_at"`
	Attempts       int    `json:"attempts"`
}

type DisconnectChallengeStore interface {
	Save(context.Context, *DisconnectChallenge) error
	Consume(context.Context, string, string, string, string, string, string) (*DisconnectChallenge, error)
	AcquireCooldown(context.Context, string) (bool, error)
}

// DisconnectChallengeReader is an optional read-only store capability so hosts
// can render review screens without consuming or mutating a challenge. Public
// extension compatibility is preserved: stores without it keep working.
type DisconnectChallengeReader interface {
	Read(context.Context, string, string, string, string) (*DisconnectChallenge, error)
}

func disconnectHash(secret string) string {
	digest := sha256.Sum256([]byte("oauth-disconnect:" + secret))
	return hex.EncodeToString(digest[:])
}
func newDisconnectSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

var validDisconnectStages = map[string]bool{"": true, "current_email": true, "sign_in_email": true}

func NewDisconnectChallenge(userID, accessUUID, provider string, payload []byte) (*DisconnectChallenge, string, string, error) {
	id, err := newDisconnectSecret()
	if err != nil {
		return nil, "", "", err
	}
	token, err := newDisconnectSecret()
	if err != nil {
		return nil, "", "", err
	}
	code := make([]byte, 8)
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	for i := range code {
		n, e := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if e != nil {
			return nil, "", "", e
		}
		code[i] = alphabet[n.Int64()]
	}
	c := &DisconnectChallenge{ID: id, UserID: userID, AccessUUID: accessUUID, Provider: provider, Payload: payload, CodeHash: disconnectHash(string(code)), TokenHash: disconnectHash(token), ExpiresAt: time.Now().Add(DisconnectChallengeTTL).UnixMilli()}
	return c, string(code), token, nil
}

type RedisDisconnectChallengeStore struct {
	client RedisTransactionClient
	prefix string
}

func NewRedisDisconnectChallengeStore(client RedisTransactionClient, namespace string) *RedisDisconnectChallengeStore {
	return &RedisDisconnectChallengeStore{client: client, prefix: "oauth:disconnect:" + namespace + ":"}
}
func (s *RedisDisconnectChallengeStore) contextual(ctx context.Context) RedisTransactionClient {
	switch c := s.client.(type) {
	case *redis.Client:
		return c.WithContext(ctx)
	case *redis.ClusterClient:
		return c.WithContext(ctx)
	case *redis.Ring:
		return c.WithContext(ctx)
	default:
		return s.client
	}
}
func (s *RedisDisconnectChallengeStore) Save(ctx context.Context, c *DisconnectChallenge) error {
	if c != nil && c.RedirectURI != "" && !ValidDisconnectRedirectURI(c.RedirectURI) {
		return ErrDisconnectProofInvalid
	}
	if s == nil || s.client == nil || c == nil || !disconnectIDPattern.MatchString(c.ID) || c.UserID == "" || c.AccessUUID == "" || (c.Provider != "google" && c.Provider != "apple") || !validDisconnectStages[c.Stage] || len(c.CodeHash) != 64 || len(c.TokenHash) != 64 || len(c.Payload) == 0 {
		return ErrDisconnectProofInvalid
	}
	ttl := time.Until(time.UnixMilli(c.ExpiresAt))
	if ttl <= 0 || ttl > DisconnectChallengeTTL {
		return ErrDisconnectProofInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	ok, err := s.contextual(ctx).SetNX(s.prefix+c.ID, raw, ttl).Result()
	if err != nil {
		return err
	}
	if !ok {
		return ErrDisconnectProofInvalid
	}
	return nil
}
func (s *RedisDisconnectChallengeStore) AcquireCooldown(ctx context.Context, userID string) (bool, error) {
	if s == nil || s.client == nil || userID == "" {
		return false, ErrDisconnectProofInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.contextual(ctx).SetNX(s.prefix+"cooldown:"+disconnectHash(userID), 1, DisconnectResendCooldownTTL).Result()
}

// Binding, failed-attempt limit and successful consume are one atomic operation.
// Wrong-session requests cannot consume a proof or exhaust another user's limit.
const consumeDisconnectScript = `
local value=redis.call('GET',KEYS[1])
if not value then return 0 end
local c=cjson.decode(value)
if c.user_id~=ARGV[1] or c.access_uuid~=ARGV[2] or c.provider~=ARGV[3] then return 0 end
if c.expires_at<=tonumber(ARGV[6]) then redis.call('DEL',KEYS[1]); return 0 end
if c.attempts>=5 then return 2 end
local expected=c.code_hash
if ARGV[4]=='token' then expected=c.token_hash end
-- Compare all digest bytes, without an early mismatch return.
local diff=0
for i=1,64 do if string.byte(expected,i)~=string.byte(ARGV[5],i) then diff=diff+1 end end
if diff==0 then redis.call('DEL',KEYS[1]);return value end
c.attempts=c.attempts+1
local ttl=redis.call('PTTL',KEYS[1])
if ttl>0 then redis.call('SET',KEYS[1],cjson.encode(c),'PX',ttl) end
if c.attempts>=5 then return 2 end
return 0
`

// Read returns the challenge when it exists and matches owner, session and
// provider. It never mutates state: expired challenges are reported as absent
// and no attempt counters are touched.
func (s *RedisDisconnectChallengeStore) Read(ctx context.Context, id, userID, accessUUID, provider string) (*DisconnectChallenge, error) {
	if s == nil || s.client == nil || !disconnectIDPattern.MatchString(id) || userID == "" || accessUUID == "" || (provider != "google" && provider != "apple") {
		return nil, ErrDisconnectProofInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := s.contextual(ctx).Eval(`return redis.call('GET', KEYS[1])`, []string{s.prefix + id}).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrDisconnectProofInvalid
	}
	if err != nil {
		return nil, err
	}
	raw, ok := value.(string)
	if !ok {
		return nil, ErrDisconnectProofInvalid
	}
	var c DisconnectChallenge
	if json.Unmarshal([]byte(raw), &c) != nil || c.ID != id || c.UserID != userID || c.AccessUUID != accessUUID || c.Provider != provider || !validDisconnectStages[c.Stage] || c.ExpiresAt <= time.Now().UnixMilli() {
		return nil, ErrDisconnectProofInvalid
	}
	if c.Attempts >= 5 {
		return nil, ErrDisconnectChallengeLocked
	}
	return &c, nil
}

func (s *RedisDisconnectChallengeStore) Consume(ctx context.Context, id, userID, accessUUID, provider, code, token string) (*DisconnectChallenge, error) {
	if s == nil || s.client == nil || !disconnectIDPattern.MatchString(id) || (code == "") == (token == "") {
		return nil, ErrDisconnectProofInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	kind, proof := "code", code
	if token != "" {
		kind, proof = "token", token
	}
	raw, err := s.contextual(ctx).Eval(consumeDisconnectScript, []string{s.prefix + id}, userID, accessUUID, provider, kind, disconnectHash(proof), time.Now().UnixMilli()).Result()
	if err != nil {
		return nil, err
	}
	if n, ok := raw.(int64); ok {
		if n == 2 {
			return nil, ErrDisconnectChallengeLocked
		}
		return nil, ErrDisconnectProofInvalid
	}
	value, ok := raw.(string)
	if !ok {
		return nil, ErrDisconnectProofInvalid
	}
	var c DisconnectChallenge
	if json.Unmarshal([]byte(value), &c) != nil || c.ID != id || c.UserID != userID || c.AccessUUID != accessUUID || c.Provider != provider || !validDisconnectStages[c.Stage] || c.ExpiresAt <= time.Now().UnixMilli() {
		return nil, ErrDisconnectProofInvalid
	}
	return &c, nil
}
