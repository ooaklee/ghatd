package ephemeral

import (
	"context"
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
	Get(key string) *redis.StringCmd
	Del(keys ...string) *redis.IntCmd
	Incr(key string) *redis.IntCmd
	Scan(cursor uint64, match string, count int64) *redis.ScanCmd
}

const (
	// prefixTemplate is how the cache key prefix should be shaped, <appComponent>-<appEnvironment>_
	prefixTemplate string = "%s-%s_"
)

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

	combinedID := toolbox.CombinedUuidFormat(userID, tokenUUID)

	completeKey := c.keyPrefix + combinedID

	return c.client.Set(completeKey, userID, ttl).Err()
}

// CreateAuth saves token metadata to persistent storage
// TODO: Create tests
func (c *Client) CreateAuth(ctx context.Context, userID string, tokenDetails TokenDetailsAuth) error {

	// Store access token meta
	if err := c.StoreToken(ctx, tokenDetails.GetTokenAccessUuid(), userID, tokenDetails.GetTokenAccessTimeToLive()); err != nil {
		return err
	}

	// Store refresh token meta
	if err := c.StoreToken(ctx, tokenDetails.GetTokenRefreshUuid(), userID, tokenDetails.GetTokenRefreshTimeToLive()); err != nil {
		return err
	}

	return nil
}

// DeleteAllTokenExceptedSpecified deletes all keys except the ones specified
//
// Note, the exemptionKey should be in the format <userId>:<tokenUuid>
func (c *Client) DeleteAllTokenExceptedSpecified(ctx context.Context, userId string, exemptionTokenIds []string) error {
	logger := logger.AcquireFrom(ctx)

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

	combinedID := toolbox.CombinedUuidFormat(accessDetails.GetUserId(), accessDetails.GetTokenAccessUuid())

	completeKey := c.keyPrefix + combinedID

	return c.client.Get(completeKey).Result()
}

// DeleteAuth deletes metadata with matching combinedUUID key
// from persistent storage
// TODO: Create tests
func (c *Client) DeleteAuth(ctx context.Context, combinedUUID string) (int64, error) {

	completeKey := c.keyPrefix + combinedUUID

	deleted, err := c.client.Del(completeKey).Result()
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// AddRequestCountEntry saves client making call and the number of request
// TODO: Create tests
func (c *Client) AddRequestCountEntry(ctx context.Context, clientIp string) error {

	requestorID := createRateLimitRequestorID(clientIp)

	completeKey := c.keyPrefix + requestorID

	// See if entry exists
	_, err := c.fetchRequestCountEntry(ctx, completeKey)
	if err != nil && err == redis.Nil {
		return c.initiateRequestCountEntry(ctx, completeKey)
	}

	if err != nil {
		return err
	}

	err = c.countRequestCountEntry(ctx, completeKey, c.maxUnauthedRequestAllowance)
	if err != nil {
		return err
	}

	return c.incrementAndUpdateRequestCountEntry(ctx, completeKey)
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
	completeKey := c.keyPrefix + "code:" + code

	_, err := c.client.Get(completeKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// StoreCode saves a verification or login code to persistent storage with the given TTL.
func (c *Client) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	completeKey := c.keyPrefix + "code:" + code

	return c.client.Set(completeKey, 1, ttl).Err()
}

// StoreCodeMapping saves a code→token mapping to persistent storage with the given TTL.
func (c *Client) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	completeKey := c.keyPrefix + "codetoken:" + code

	return c.client.Set(completeKey, token, ttl).Err()
}

// GetCodeMapping retrieves the token associated with the given code.
// Returns an error if the mapping does not exist.
func (c *Client) GetCodeMapping(ctx context.Context, code string) (string, error) {
	completeKey := c.keyPrefix + "codetoken:" + code

	return c.client.Get(completeKey).Result()
}

// TrackHardenedAttempt increments rate-limit counters for the given IP and optional code,
// checking against the maximum allowed attempts within the configured time window.
// It returns an error if the limit has been exceeded for either the IP or the code.
func (c *Client) TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
	ipKey := c.keyPrefix + "hrl_ip:" + ip
	if err := c.incrementAndCheckHardened(ctx, ipKey, maxAttempts, window); err != nil {
		return err
	}

	if code != "" {
		codeKey := c.keyPrefix + "hrl_code:" + code
		if err := c.incrementAndCheckHardened(ctx, codeKey, maxAttempts, window); err != nil {
			return err
		}
	}

	return nil
}

// BlockIP stores a temporary block entry for the given IP address with the specified duration.
func (c *Client) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	blockKey := c.keyPrefix + "hrl_block:" + ip

	return c.client.Set(blockKey, "1", duration).Err()
}

// IsIPBlocked checks whether the given IP address is currently under a temporary block.
func (c *Client) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	blockKey := c.keyPrefix + "hrl_block:" + ip

	_, err := c.client.Get(blockKey).Result()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	return true, nil
}

// incrementAndCheckHardened increments a rate-limit counter key, sets the TTL on first
// increment, and returns an error if the counter exceeds maxAttempts.
func (c *Client) incrementAndCheckHardened(ctx context.Context, key string, maxAttempts int, window time.Duration) error {
	val, err := c.client.Incr(key).Result()
	if err != nil {
		return err
	}

	if val == 1 {
		_ = c.client.Set(key, val, window)
	}

	if int(val) > maxAttempts {
		return ErrHardenedRateLimitExceeded
	}

	return nil
}
