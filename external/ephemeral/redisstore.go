package ephemeral

import (
	"context"
	"errors"
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
		return errors.New(ErrKeyRequestorLimitExceeded)
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
