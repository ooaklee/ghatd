package ephemeral

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// PersistentClient holds methods for a valid cache
type PersistentClient interface {
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	// SetNX stores a value only when the key is not already present.
	SetNX(key string, value interface{}, expiration time.Duration) *redis.BoolCmd
	Get(key string) *redis.StringCmd
	Del(keys ...string) *redis.IntCmd
	Incr(key string) *redis.IntCmd
	Scan(cursor uint64, match string, count int64) *redis.ScanCmd
}

const (
	// prefixTemplate is how the cache key prefix should be shaped, <appComponent>-<appEnvironment>_
	prefixTemplate string = "%s-%s_"
)

// refreshTokenRotationKey returns the cache key for a completed refresh rotation result.
func refreshTokenRotationKey(userID, refreshTokenUUID string) string {
	return fmt.Sprintf("refresh-rotation:%s:%s", userID, refreshTokenUUID)
}

// refreshTokenRotationLockKey returns the cache key for the in-flight refresh rotation lock.
func refreshTokenRotationLockKey(userID, refreshTokenUUID string) string {
	return fmt.Sprintf("refresh-rotation-lock:%s:%s", userID, refreshTokenUUID)
}

// loginEmailCooldownKey returns a non-reversible login-email cooldown key for a user/context.
func loginEmailCooldownKey(userID string, isDashboardRequest bool, requestURL string) string {
	// requestURLHash keeps the request context out of the raw Redis key while still scoping cooldowns.
	requestURLHash := sha256.Sum256([]byte(requestURL))

	return fmt.Sprintf("login-email-cooldown:%s:%t:%x", userID, isDashboardRequest, requestURLHash)
}

// Client communicates with the persistent storage
type Client struct {
	client                      PersistentClient
	maxUnauthedRequestAllowance int64
	keyPrefix                   string
}

// NewRedisStore creates redis based emphemeral store
func NewRedisStore(client PersistentClient, maxUnauthedRequestAllowance int64, appComponent, appEnvironment string) *Client {

	// Create prefix that matches, <appComponent>-<appEnvironment>_
	keyPrefix := fmt.Sprintf(prefixTemplate, toolbox.StringStandardisedToLower(
		toolbox.StringConvertToSnakeCase(
			appComponent)),
		toolbox.StringStandardisedToLower(toolbox.StringConvertToSnakeCase(
			appEnvironment)))

	return &Client{
		client:                      client,
		maxUnauthedRequestAllowance: maxUnauthedRequestAllowance,
		keyPrefix:                   keyPrefix,
	}
}

// StoreToken saves token and user uuid to persistent storage
// Creates entry in Store using the combinedUUID as a key.
// TODO: Create tests
func (c *Client) StoreToken(ctx context.Context, tokenUUID string, userID string, ttl time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "store-token")

	combinedID := toolbox.CombinedUuidFormat(userID, tokenUUID)

	completeKey := c.keyPrefix + combinedID

	if err := c.client.Set(completeKey, userID, ttl).Err(); err != nil {
		logger.Error("ephemeral-token-store-failed", zap.String("user-id", userID), zap.Duration("ttl", ttl), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-token-stored", zap.String("user-id", userID), zap.Duration("ttl", ttl))
	return nil
}

// CreateAuth saves token metadata to persistent storage
// TODO: Create tests
func (c *Client) CreateAuth(ctx context.Context, userID string, tokenDetails TokenDetailsAuth) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "create-auth")
	logger.Debug("ephemeral-auth-create-started", zap.String("user-id", userID))

	// Store access token meta
	if err := c.StoreToken(ctx, tokenDetails.GetTokenAccessUuid(), userID, tokenDetails.GetTokenAccessTimeToLive()); err != nil {
		logger.Error("ephemeral-auth-create-access-token-store-failed", zap.String("user-id", userID), zap.Error(err))
		return err
	}

	// Store refresh token meta
	if err := c.StoreToken(ctx, tokenDetails.GetTokenRefreshUuid(), userID, tokenDetails.GetTokenRefreshTimeToLive()); err != nil {
		logger.Error("ephemeral-auth-create-refresh-token-store-failed", zap.String("user-id", userID), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-auth-created", zap.String("user-id", userID))
	return nil
}

// AcquireRefreshTokenRotationLock tries to claim the rotation for a refresh token.
func (c *Client) AcquireRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "acquire-refresh-token-rotation-lock")
	completeKey := c.keyPrefix + refreshTokenRotationLockKey(userID, refreshTokenUUID)

	acquired, err := c.client.SetNX(completeKey, "1", ttl).Result()
	if err != nil {
		logger.Error("ephemeral-refresh-rotation-lock-acquire-failed", zap.String("user-id", userID), zap.Duration("ttl", ttl), zap.Error(err))
		return false, err
	}

	logger.Debug("ephemeral-refresh-rotation-lock-acquire-completed", zap.String("user-id", userID), zap.Bool("acquired", acquired), zap.Duration("ttl", ttl))
	return acquired, nil
}

// ReleaseRefreshTokenRotationLock releases a previously acquired refresh rotation lock.
func (c *Client) ReleaseRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "release-refresh-token-rotation-lock")
	completeKey := c.keyPrefix + refreshTokenRotationLockKey(userID, refreshTokenUUID)

	deleted, err := c.client.Del(completeKey).Result()
	if err != nil {
		logger.Error("ephemeral-refresh-rotation-lock-release-failed", zap.String("user-id", userID), zap.Error(err))
		return 0, err
	}

	logger.Debug("ephemeral-refresh-rotation-lock-released", zap.String("user-id", userID), zap.Int64("deleted", deleted))
	return deleted, nil
}

// StoreRefreshTokenRotationResult stores the newly issued token pair for brief replay.
func (c *Client) StoreRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string, result *RefreshTokenRotationResult, ttl time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "store-refresh-token-rotation-result")
	if result == nil {
		logger.Warn("ephemeral-refresh-rotation-result-store-nil-result", zap.String("user-id", userID))
		return fmt.Errorf("nil refresh token rotation result")
	}

	payload, err := json.Marshal(result)
	if err != nil {
		logger.Error("ephemeral-refresh-rotation-result-marshal-failed", zap.String("user-id", userID), zap.Error(err))
		return err
	}

	completeKey := c.keyPrefix + refreshTokenRotationKey(userID, refreshTokenUUID)
	if err := c.client.Set(completeKey, string(payload), ttl).Err(); err != nil {
		logger.Error("ephemeral-refresh-rotation-result-store-failed", zap.String("user-id", userID), zap.Duration("ttl", ttl), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-refresh-rotation-result-stored", zap.String("user-id", userID), zap.Duration("ttl", ttl))
	return nil
}

// GetRefreshTokenRotationResult retrieves a recently rotated token pair.
func (c *Client) GetRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string) (*RefreshTokenRotationResult, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "get-refresh-token-rotation-result")
	completeKey := c.keyPrefix + refreshTokenRotationKey(userID, refreshTokenUUID)

	raw, err := c.client.Get(completeKey).Result()
	if err == redis.Nil {
		logger.Debug("ephemeral-refresh-rotation-result-not-found", zap.String("user-id", userID))
		return nil, nil
	}
	if err != nil {
		logger.Error("ephemeral-refresh-rotation-result-fetch-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, err
	}

	var result RefreshTokenRotationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		logger.Error("ephemeral-refresh-rotation-result-unmarshal-failed", zap.String("user-id", userID), zap.Error(err))
		return nil, err
	}

	logger.Debug("ephemeral-refresh-rotation-result-found", zap.String("user-id", userID))
	return &result, nil
}

// AcquireLoginEmailCooldown tries to claim the login-email cooldown window.
func (c *Client) AcquireLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "acquire-login-email-cooldown")
	completeKey := c.keyPrefix + loginEmailCooldownKey(userID, isDashboardRequest, requestURL)

	acquired, err := c.client.SetNX(completeKey, "1", ttl).Result()
	if err != nil {
		logger.Error("ephemeral-login-email-cooldown-acquire-failed", zap.String("user-id", userID), zap.Bool("dashboard-request", isDashboardRequest), zap.Duration("ttl", ttl), zap.Error(err))
		return false, err
	}

	logger.Debug("ephemeral-login-email-cooldown-acquire-completed", zap.String("user-id", userID), zap.Bool("dashboard-request", isDashboardRequest), zap.Bool("acquired", acquired), zap.Duration("ttl", ttl))
	return acquired, nil
}

// ReleaseLoginEmailCooldown releases a login-email cooldown claim.
func (c *Client) ReleaseLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "release-login-email-cooldown")
	completeKey := c.keyPrefix + loginEmailCooldownKey(userID, isDashboardRequest, requestURL)

	deleted, err := c.client.Del(completeKey).Result()
	if err != nil {
		logger.Error("ephemeral-login-email-cooldown-release-failed", zap.String("user-id", userID), zap.Bool("dashboard-request", isDashboardRequest), zap.Error(err))
		return 0, err
	}

	logger.Debug("ephemeral-login-email-cooldown-released", zap.String("user-id", userID), zap.Bool("dashboard-request", isDashboardRequest), zap.Int64("deleted", deleted))
	return deleted, nil
}

// DeleteAllTokenExceptedSpecified deletes all keys except the ones specified
//
// Note, the exemptionKey should be in the format <userId>:<tokenUuid>
func (c *Client) DeleteAllTokenExceptedSpecified(ctx context.Context, userId string, exemptionTokenIds []string) error {
	logger := logger.AcquirePackageFrom(ctx, "external/ephemeral")

	var cursor uint64
	var completeExemptionTokenIds []string
	var foundTokenIds []string
	var authTokenPrefix string = c.keyPrefix + userId + ":*"

	for _, key := range exemptionTokenIds {
		completeExemptionTokenIds = append(completeExemptionTokenIds, c.keyPrefix+key)
	}

	for {
		var keys []string
		var err error
		keys, cursor, err = c.client.Scan(cursor, authTokenPrefix, 0).Result()
		if err != nil {
			logger.Error("unable-to-find-tokens-matching-prefix", zap.String("search-prefix", authTokenPrefix), zap.Error(err))
			return err
		}

		foundTokenIds = append(foundTokenIds, keys...)

		if cursor == 0 { // no more keys
			break
		}
	}

	// Remove keys form the found keys that is in the exemption list
	for _, exemptionKey := range completeExemptionTokenIds {
		for i, key := range foundTokenIds {
			if key == exemptionKey {
				logger.Info("protecting-current-token-from-token-removal-list", zap.String("token-id", key), zap.String("user-id", userId))
				foundTokenIds = append(foundTokenIds[:i], foundTokenIds[i+1:]...)
				break
			}
		}
	}

	// Delete remaining keys
	if len(foundTokenIds) > 0 {
		_, err := c.client.Del(foundTokenIds...).Result()
		if err != nil {
			logger.Error("error-while-wiping-other-user-tokens", zap.Strings("exemption-token-ids", completeExemptionTokenIds), zap.Strings("found-token-ids", foundTokenIds), zap.String("user-id", userId), zap.Error(err))
			return err
		}

		logger.Info("user-tokens-wiped", zap.Strings("exemption-token-ids", completeExemptionTokenIds), zap.Strings("found-token-ids", foundTokenIds), zap.String("user-id", userId))
		return nil
	}

	logger.Info("no-other-token-detected", zap.Strings("exemption-token-ids", completeExemptionTokenIds), zap.String("user-id", userId))
	return nil
}

// FetchAuth retrieves tokendata from persistent storage using combinedUUID
// TODO: Create tests
func (c *Client) FetchAuth(ctx context.Context, accessDetails TokenDetailsAccess) (string, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "fetch-auth")

	combinedID := toolbox.CombinedUuidFormat(accessDetails.GetUserId(), accessDetails.GetTokenAccessUuid())

	completeKey := c.keyPrefix + combinedID

	userID := accessDetails.GetUserId()
	userIDFromToken, err := c.client.Get(completeKey).Result()
	if err != nil {
		logger.Error("ephemeral-auth-fetch-failed", zap.String("user-id", userID), zap.Error(err))
		return "", err
	}

	logger.Debug("ephemeral-auth-fetched", zap.String("user-id", userID))
	return userIDFromToken, nil
}

// DeleteAuth deletes metadata with matching combinedUUID key
// from persistent storage
// TODO: Create tests
func (c *Client) DeleteAuth(ctx context.Context, combinedUUID string) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "delete-auth")

	completeKey := c.keyPrefix + combinedUUID

	deleted, err := c.client.Del(completeKey).Result()
	if err != nil {
		logger.Error("ephemeral-auth-delete-failed", zap.Error(err))
		return 0, err
	}
	logger.Debug("ephemeral-auth-deleted", zap.Int64("deleted", deleted))
	return deleted, nil
}

// AddRequestCountEntry saves client making call and the number of request
// TODO: Create tests
func (c *Client) AddRequestCountEntry(ctx context.Context, clientIp string) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "add-request-count-entry")

	requestorID := createRateLimitRequestorID(clientIp)

	completeKey := c.keyPrefix + requestorID

	// See if entry exists
	_, err := c.fetchRequestCountEntry(ctx, completeKey)
	if err != nil && err == redis.Nil {
		logger.Debug("ephemeral-request-count-entry-missing-initialising", zap.String("clientip", clientIp))
		return c.initiateRequestCountEntry(ctx, completeKey)
	}

	if err != nil {
		logger.Error("ephemeral-request-count-entry-fetch-failed", zap.String("clientip", clientIp), zap.Error(err))
		return err
	}

	err = c.countRequestCountEntry(ctx, completeKey, c.maxUnauthedRequestAllowance)
	if err != nil {
		logger.Warn("ephemeral-request-count-entry-limit-check-failed", zap.String("clientip", clientIp), zap.Int64("limit", c.maxUnauthedRequestAllowance), zap.Error(err))
		return err
	}

	if err := c.incrementAndUpdateRequestCountEntry(ctx, completeKey); err != nil {
		logger.Error("ephemeral-request-count-entry-increment-failed", zap.String("clientip", clientIp), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-request-count-entry-incremented", zap.String("clientip", clientIp))
	return nil
}

// countRequestCountEntry checks to see how many requests have been made and returns error if limit exceeded
func (c *Client) countRequestCountEntry(ctx context.Context, requestorID string, requestLimit int64) error {
	// Get current request count
	count := c.client.Get(requestorID).Val()
	i, _ := strconv.ParseInt(count, 10, 64)

	if i >= requestLimit {
		// Return rate limit error
		return ErrRequestorLimitExceeded
	}

	return nil
}

// incrementAndUpdateRequestCountEntry updates an entry with incremented value in empheral store
func (c *Client) incrementAndUpdateRequestCountEntry(ctx context.Context, requestorID string) error {

	_, err := c.client.Incr(requestorID).Result()
	return err
}

// initiateRequestCountEntry creates an entry in empheral store with TTL of 30 minutes
func (c *Client) initiateRequestCountEntry(ctx context.Context, requestorID string) error {
	var defaultTTL time.Duration = time.Minute * 30

	expiryUTC := time.Unix(time.Now().Add(defaultTTL).Unix(), 0)
	now := time.Now()

	return c.client.Set(requestorID, 1, expiryUTC.Sub(now)).Err()
}

// fetchRequestCountEntry retrieves unauth request entry from persistent storage using requestor ID
// TODO: Create tests
func (c *Client) fetchRequestCountEntry(ctx context.Context, requestorID string) (string, error) {

	return c.client.Get(requestorID).Result()
}

// createRateLimitRequestorID returns a string containing a combination of r_<clientIP>
// used to represent unauthed requestor
func createRateLimitRequestorID(clientIp string) string {
	return fmt.Sprintf("r_%v", clientIp)
}

// CodeExists checks whether a verification or login code already exists in persistent storage.
// It returns true if the code is found, false if the code does not exist.
func (c *Client) CodeExists(ctx context.Context, code string) (bool, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "code-exists")
	completeKey := c.keyPrefix + "code:" + code

	_, err := c.client.Get(completeKey).Result()
	if err == redis.Nil {
		logger.Debug("ephemeral-code-not-found")
		return false, nil
	}
	if err != nil {
		logger.Error("ephemeral-code-exists-check-failed", zap.Error(err))
		return false, err
	}

	logger.Debug("ephemeral-code-found")
	return true, nil
}

// StoreCode saves a verification or login code to persistent storage with the given TTL.
func (c *Client) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "store-code")
	completeKey := c.keyPrefix + "code:" + code

	if err := c.client.Set(completeKey, 1, ttl).Err(); err != nil {
		logger.Error("ephemeral-code-store-failed", zap.Duration("ttl", ttl), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-code-stored", zap.Duration("ttl", ttl))
	return nil
}

// StoreCodeMapping saves a code→token mapping to persistent storage with the given TTL.
func (c *Client) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "store-code-mapping")
	completeKey := c.keyPrefix + "codetoken:" + code

	if err := c.client.Set(completeKey, token, ttl).Err(); err != nil {
		logger.Error("ephemeral-code-mapping-store-failed", zap.Duration("ttl", ttl), zap.Error(err))
		return err
	}

	logger.Debug("ephemeral-code-mapping-stored", zap.Duration("ttl", ttl))
	return nil
}

// GetCodeMapping retrieves the token associated with the given code.
// Returns an error if the mapping does not exist.
func (c *Client) GetCodeMapping(ctx context.Context, code string) (string, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "get-code-mapping")
	completeKey := c.keyPrefix + "codetoken:" + code

	token, err := c.client.Get(completeKey).Result()
	if err != nil {
		logger.Error("ephemeral-code-mapping-fetch-failed", zap.Error(err))
		return "", err
	}

	logger.Debug("ephemeral-code-mapping-fetched")
	return token, nil
}

// TrackHardenedAttempt increments rate-limit counters for the given IP and optional code,
// checking against the maximum allowed attempts within the configured time window.
// It returns an error if the limit has been exceeded for either the IP or the code.
func (c *Client) TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "track-hardened-attempt")
	ipKey := c.keyPrefix + "hrl_ip:" + ip
	if err := c.incrementAndCheckHardened(ctx, ipKey, maxAttempts, window); err != nil {
		logger.Warn("ephemeral-hardened-attempt-ip-check-failed", zap.String("clientip", ip), zap.Int("max-attempts", maxAttempts), zap.Duration("window", window), zap.Error(err))
		return err
	}

	if code != "" {
		codeKey := c.keyPrefix + "hrl_code:" + code
		if err := c.incrementAndCheckHardened(ctx, codeKey, maxAttempts, window); err != nil {
			logger.Warn("ephemeral-hardened-attempt-code-check-failed", zap.String("clientip", ip), zap.Bool("has-code", true), zap.Int("max-attempts", maxAttempts), zap.Duration("window", window), zap.Error(err))
			return err
		}
	}

	logger.Debug("ephemeral-hardened-attempt-tracked", zap.String("clientip", ip), zap.Bool("has-code", code != ""), zap.Int("max-attempts", maxAttempts), zap.Duration("window", window))
	return nil
}

// BlockIP stores a temporary block entry for the given IP address with the specified duration.
func (c *Client) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "block-ip")
	blockKey := c.keyPrefix + "hrl_block:" + ip

	if err := c.client.Set(blockKey, "1", duration).Err(); err != nil {
		logger.Error("ephemeral-ip-block-store-failed", zap.String("clientip", ip), zap.Duration("duration", duration), zap.Error(err))
		return err
	}

	logger.Warn("ephemeral-ip-blocked", zap.String("clientip", ip), zap.Duration("duration", duration))
	return nil
}

// IsIPBlocked checks whether the given IP address is currently under a temporary block.
func (c *Client) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "is-ip-blocked")
	blockKey := c.keyPrefix + "hrl_block:" + ip

	_, err := c.client.Get(blockKey).Result()
	if err == redis.Nil {
		logger.Debug("ephemeral-ip-not-blocked", zap.String("clientip", ip))
		return false, nil
	}
	if err != nil {
		logger.Error("ephemeral-ip-block-check-failed", zap.String("clientip", ip), zap.Error(err))
		return false, err
	}

	logger.Debug("ephemeral-ip-blocked-entry-found", zap.String("clientip", ip))
	return true, nil
}

// incrementAndCheckHardened increments a rate-limit counter key, sets the TTL on first
// increment, and returns an error if the counter exceeds maxAttempts.
func (c *Client) incrementAndCheckHardened(ctx context.Context, key string, maxAttempts int, window time.Duration) error {
	logger := logger.AcquireOperationFrom(ctx, "external/ephemeral", "increment-and-check-hardened")
	val, err := c.client.Incr(key).Result()
	if err != nil {
		logger.Error("ephemeral-hardened-counter-increment-failed", zap.Error(err))
		return err
	}

	if val == 1 {
		_ = c.client.Set(key, val, window)
	}

	if int(val) > maxAttempts {
		logger.Warn("ephemeral-hardened-counter-limit-exceeded", zap.Int64("attempts", val), zap.Int("max-attempts", maxAttempts), zap.Duration("window", window))
		return ErrHardenedRateLimitExceeded
	}

	return nil
}
