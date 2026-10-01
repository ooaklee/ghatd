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

// DisconnectChallengeTTL is the maximum lifetime of a Settings email challenge.
const DisconnectChallengeTTL = 10 * time.Minute

// DisconnectResendCooldownTTL is the minimum interval between explicit email
// challenge requests for the same account in a store namespace.
const DisconnectResendCooldownTTL = time.Minute

// DisconnectMaxProofAttempts defines the intended failed-proof limit. Keep it
// in sync with the Redis consume script and Read checks, which enforce five attempts.
const DisconnectMaxProofAttempts = 5

// ErrDisconnectProofInvalid reports absent, expired, incorrectly bound or
// invalid proof without revealing another account's challenge state.
var ErrDisconnectProofInvalid = errors.New("DisconnectProofInvalid")

// ErrDisconnectChallengeLocked reports that the failed-proof limit has been
// reached and a new challenge is required.
var ErrDisconnectChallengeLocked = errors.New("DisconnectChallengeLocked")

// ErrDisconnectCooldown reports that the account must wait before requesting
// another verification email.
var ErrDisconnectCooldown = errors.New("DisconnectCooldown")

// disconnectIDPattern accepts the unpadded base64url encoding used for challenge IDs.
var disconnectIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// disconnectSchemePattern restricts native Settings return schemes to reverse-domain syntax.
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
	// sign-in-email challenge ("sign_in_email"). Empty means a legacy final
	// challenge. The isolated connection-verification store accepts only
	// "connect_email", which cannot remove providers or change email.
	Stage string `json:"stage,omitempty"`
	// AuthTimeMillis carries the existing-account proof time into a final
	// candidate-email challenge. Confirming the candidate must not reset it
	// to the time of candidate verification.
	AuthTimeMillis int64  `json:"auth_time_ms,omitempty"`
	Payload        []byte `json:"payload"`
	CodeHash       string `json:"code_hash"`
	TokenHash      string `json:"token_hash"`
	ExpiresAt      int64  `json:"expires_at"`
	Attempts       int    `json:"attempts"`
}

// DisconnectChallengeStore persists Settings proofs independently of login
// codes. Implementations must enforce expiry, purpose isolation, account/session/
// provider binding, single-use consumption and a bounded failed-proof count.
// Custom stores may expose ConnectionVerificationStore() DisconnectChallengeStore
// for an isolated connect_email namespace; native flows also require a reader.
type DisconnectChallengeStore interface {
	// Save inserts a new, validated challenge with expiry, never overwriting an ID.
	Save(context.Context, *DisconnectChallenge) error
	// Consume receives ID, user ID, access UUID, provider, code and token.
	// Exactly one proof is required. Binding, expiry, the attempt limit and
	// successful deletion must be checked atomically; foreign sessions must
	// neither consume proof nor exhaust another session's attempts.
	Consume(context.Context, string, string, string, string, string, string) (*DisconnectChallenge, error)
	// AcquireCooldown reserves a resend window for the user ID. False means
	// an existing reservation prevents another explicit send for now.
	AcquireCooldown(context.Context, string) (bool, error)
}

// DisconnectChallengeReader is an optional read-only store capability so hosts
// can render review screens without consuming or mutating a challenge. Public
// extension compatibility is preserved: stores without it keep working.
type DisconnectChallengeReader interface {
	// Read receives ID, user ID, access UUID and provider. It verifies binding,
	// purpose, expiry and the attempt limit without consuming or modifying proof.
	Read(context.Context, string, string, string, string) (*DisconnectChallenge, error)
}

// disconnectHash derives a domain-prefixed SHA-256 digest for stored proof and
// cooldown keys, avoiding persistence of the original proof strings.
func disconnectHash(secret string) string {
	digest := sha256.Sum256([]byte("oauth-disconnect:" + secret))
	return hex.EncodeToString(digest[:])
}

// newDisconnectSecret generates a 256-bit secret encoded as unpadded base64url.
func newDisconnectSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// validDisconnectStages lists legacy and staged disconnect purposes accepted
// by the base store; connection verification uses a separate store namespace.
var validDisconnectStages = map[string]bool{"": true, "current_email": true, "sign_in_email": true}

// NewDisconnectChallenge creates an expiring challenge and returns its plaintext
// 8-character code and link token for delivery. Only their hashes enter the
// challenge. The caller must set the purpose and return address, deliver the
// proof and save the challenge; this constructor performs no I/O.
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

// RedisDisconnectChallengeStore implements expiring Settings challenges with
// atomic proof consumption and per-account resend cooldowns. Connection verification
// uses a separate namespace and purpose through ConnectionVerificationStore.
type RedisDisconnectChallengeStore struct {
	client                 RedisTransactionClient
	prefix                 string
	connectionVerification bool
}

// ConnectionVerificationStore provides a distinct proof namespace. Even a
// direct Consume against the wrong purpose cannot spend another flow's proof.
// Hosts with custom stores may implement this optional capability themselves.
func (s *RedisDisconnectChallengeStore) ConnectionVerificationStore() DisconnectChallengeStore {
	return &RedisDisconnectChallengeStore{client: s.client, prefix: s.prefix + "verify-connect:", connectionVerification: true}
}

// validStage restricts a store instance to disconnect or connection-verification
// purposes, preventing proof reuse across the two flows.
func (s *RedisDisconnectChallengeStore) validStage(stage string) bool {
	if s.connectionVerification {
		return stage == "connect_email"
	}
	return validDisconnectStages[stage]
}

// NewRedisDisconnectChallengeStore creates the disconnect store for a host
// namespace. Use a distinct namespace for each application sharing Redis.
func NewRedisDisconnectChallengeStore(client RedisTransactionClient, namespace string) *RedisDisconnectChallengeStore {
	return &RedisDisconnectChallengeStore{client: client, prefix: "oauth:disconnect:" + namespace + ":"}
}

// contextual binds standard Redis clients to the request context, leaving
// custom RedisTransactionClient implementations responsible for context handling.
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

// Save validates the challenge's purpose, binding and remaining lifetime before
// inserting it with expiry. An existing ID is never overwritten or extended.
func (s *RedisDisconnectChallengeStore) Save(ctx context.Context, c *DisconnectChallenge) error {
	if c != nil && c.RedirectURI != "" && !ValidDisconnectRedirectURI(c.RedirectURI) {
		return ErrDisconnectProofInvalid
	}
	if s == nil || s.client == nil || c == nil || !disconnectIDPattern.MatchString(c.ID) || c.UserID == "" || c.AccessUUID == "" || (c.Provider != "google" && c.Provider != "apple") || !s.validStage(c.Stage) || len(c.CodeHash) != 64 || len(c.TokenHash) != 64 || len(c.Payload) == 0 {
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

// AcquireCooldown atomically reserves the account's resend window. It returns
// false when a reservation already exists and does not extend that reservation.
func (s *RedisDisconnectChallengeStore) AcquireCooldown(ctx context.Context, userID string) (bool, error) {
	if s == nil || s.client == nil || userID == "" {
		return false, ErrDisconnectProofInvalid
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.contextual(ctx).SetNX(s.prefix+"cooldown:"+disconnectHash(userID), 1, DisconnectResendCooldownTTL).Result()
}

// consumeDisconnectScript checks binding, the failed-attempt limit and proof
// consumption atomically. Wrong-session requests cannot consume proof or exhaust
// another user's limit. Keep the embedded limit aligned with DisconnectMaxProofAttempts.
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
	if json.Unmarshal([]byte(raw), &c) != nil || c.ID != id || c.UserID != userID || c.AccessUUID != accessUUID || c.Provider != provider || !s.validStage(c.Stage) || c.ExpiresAt <= time.Now().UnixMilli() {
		return nil, ErrDisconnectProofInvalid
	}
	if c.Attempts >= 5 {
		return nil, ErrDisconnectChallengeLocked
	}
	return &c, nil
}

// Consume verifies exactly one code or token and atomically deletes matching
// proof. Incorrect proof counts towards the limit only after account, session and
// provider binding succeeds; failed attempts preserve the original expiry.
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
	if json.Unmarshal([]byte(value), &c) != nil || c.ID != id || c.UserID != userID || c.AccessUUID != accessUUID || c.Provider != provider || !s.validStage(c.Stage) || c.ExpiresAt <= time.Now().UnixMilli() {
		return nil, ErrDisconnectProofInvalid
	}
	return &c, nil
}
