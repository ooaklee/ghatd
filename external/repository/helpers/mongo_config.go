package repositoryhelpers

import (
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Config holds all configuration for MongoDB connection
type Config struct {
	ConnectionString string
	Database         string

	// Connection Pool Settings
	MaxPoolSize     *uint64
	MinPoolSize     *uint64
	MaxIdleTime     *time.Duration
	MaxConnIdleTime *time.Duration

	// Timeout Settings
	ConnectTimeout         *time.Duration
	ServerSelectionTimeout *time.Duration
	SocketTimeout          *time.Duration

	// Retry Settings
	RetryWrites  *bool
	RetryReads   *bool
	MaxRetryTime *time.Duration

	// Read Preference
	ReadPreference *readpref.ReadPref

	// Monitoring
	MonitoringHooks []MonitoringHook

	// Custom Client Options
	CustomOptions []*options.ClientOptions
}

// DefaultConfig returns a Config with sensible defaults
func DefaultConfig(connectionString, database string) *Config {
	connectTimeout := 10 * time.Second
	serverSelectionTimeout := 5 * time.Second
	maxPoolSize := uint64(100)
	minPoolSize := uint64(0)
	maxIdleTime := 10 * time.Minute
	retryWrites := true
	retryReads := true

	return &Config{
		ConnectionString:       connectionString,
		Database:               database,
		ConnectTimeout:         &connectTimeout,
		ServerSelectionTimeout: &serverSelectionTimeout,
		MaxPoolSize:            &maxPoolSize,
		MinPoolSize:            &minPoolSize,
		MaxIdleTime:            &maxIdleTime,
		RetryWrites:            &retryWrites,
		RetryReads:             &retryReads,
		ReadPreference:         readpref.Primary(),
		MonitoringHooks:        []MonitoringHook{},
		CustomOptions:          []*options.ClientOptions{},
	}
}

// ConfigOption is a function that modifies a Config
type ConfigOption func(*Config)

// WithConnectionPool sets connection pool options
func WithConnectionPool(maxPool, minPool uint64, maxIdleTime time.Duration) ConfigOption {
	return func(c *Config) {
		c.MaxPoolSize = &maxPool
		c.MinPoolSize = &minPool
		c.MaxIdleTime = &maxIdleTime
	}
}

// WithTimeouts sets various timeout options
func WithTimeouts(connect, serverSelection, socket time.Duration) ConfigOption {
	return func(c *Config) {
		c.ConnectTimeout = &connect
		c.ServerSelectionTimeout = &serverSelection
		c.SocketTimeout = &socket
	}
}

// WithRetryPolicy sets retry options
func WithRetryPolicy(retryWrites, retryReads bool, maxRetryTime time.Duration) ConfigOption {
	return func(c *Config) {
		c.RetryWrites = &retryWrites
		c.RetryReads = &retryReads
		c.MaxRetryTime = &maxRetryTime
	}
}

// WithReadPreference sets read preference
func WithReadPreference(pref *readpref.ReadPref) ConfigOption {
	return func(c *Config) {
		c.ReadPreference = pref
	}
}

// WithMonitoring adds monitoring hooks
func WithMonitoring(hooks ...MonitoringHook) ConfigOption {
	return func(c *Config) {
		c.MonitoringHooks = append(c.MonitoringHooks, hooks...)
	}
}

// WithCustomOptions adds custom client options
func WithCustomOptions(opts ...*options.ClientOptions) ConfigOption {
	return func(c *Config) {
		c.CustomOptions = append(c.CustomOptions, opts...)
	}
}

// BuildClientOptions builds mongo.options.ClientOptions from Config
func (c *Config) BuildClientOptions() *options.ClientOptions {
	clientOpts := options.Client().ApplyURI(c.ConnectionString)

	if c.MaxPoolSize != nil {
		clientOpts.SetMaxPoolSize(*c.MaxPoolSize)
	}
	if c.MinPoolSize != nil {
		clientOpts.SetMinPoolSize(*c.MinPoolSize)
	}
	if c.MaxIdleTime != nil {
		clientOpts.SetMaxConnIdleTime(*c.MaxIdleTime)
	}
	if c.MaxConnIdleTime != nil {
		clientOpts.SetMaxConnIdleTime(*c.MaxConnIdleTime)
	}
	if c.ConnectTimeout != nil {
		clientOpts.SetConnectTimeout(*c.ConnectTimeout)
	}
	if c.ServerSelectionTimeout != nil {
		clientOpts.SetServerSelectionTimeout(*c.ServerSelectionTimeout)
	}
	if c.SocketTimeout != nil {
		clientOpts.SetSocketTimeout(*c.SocketTimeout)
	}
	if c.RetryWrites != nil {
		clientOpts.SetRetryWrites(*c.RetryWrites)
	}
	if c.RetryReads != nil {
		clientOpts.SetRetryReads(*c.RetryReads)
	}
	if c.ReadPreference != nil {
		clientOpts.SetReadPreference(c.ReadPreference)
	}

	// Apply custom options
	for _, opt := range c.CustomOptions {
		if opt.AppName != nil {
			clientOpts.SetAppName(*opt.AppName)
		}
		if opt.Auth != nil {
			clientOpts.SetAuth(*opt.Auth)
		}
		if len(opt.Compressors) > 0 {
			clientOpts.SetCompressors(opt.Compressors)
		}
		if opt.Registry != nil {
			clientOpts.SetRegistry(opt.Registry)
		}
		if opt.Monitor != nil {
			clientOpts.SetMonitor(opt.Monitor)
		}
		// Add other options as needed
	}

	return clientOpts
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.ConnectionString == "" {
		return fmt.Errorf("mongo-connection-string-cannot-be-empty")
	}
	if c.Database == "" {
		return fmt.Errorf("mongo-database-name-cannot-be-empty")
	}
	return nil
}
