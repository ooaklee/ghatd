package ephemeral

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// PersistentClient holds methods for a valid cache
type PersistentClient interface {
	Set(key string, value interface{}, expiration time.Duration) *redis.StatusCmd
	Get(key string) *redis.StringCmd
	Del(keys ...string) *redis.IntCmd
	Incr(key string) *redis.IntCmd
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

	combinedID := combineUUIDs(userID, tokenUUID)

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

// FetchAuth retrieves tokendata from persistent storage using combinedUUID
// TODO: Create tests
func (c *Client) FetchAuth(ctx context.Context, accessDetails TokenDetailsAccess) (string, error) {

	combinedID := combineUUIDs(accessDetails.GetUserId(), accessDetails.GetTokenAccessUuid())

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

// combineUUIDs returns a string containing a combination of <userID>:<tokenUUID>
func combineUUIDs(userID, tokenUUID string) string {
	return fmt.Sprintf("%v:%v", userID, tokenUUID)
}

// createRateLimitRequestorID returns a string containing a combination of r_<clientIP>
// used to represent unauthed requestor
func createRateLimitRequestorID(clientIp string) string {
	return fmt.Sprintf("r_%v", clientIp)
}
