package repositoryhelpers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
)

// Handler implements MongoClientManager interface
type Handler struct {
	config    *Config
	client    *mongo.Client
	mu        sync.RWMutex
	connected bool
	stats     ConnectionStats
	lastError error
}

// NewHandler creates a new MongoDB handler with configuration
func NewHandler(config *Config) (*Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return &Handler{
		config: config,
		stats: ConnectionStats{
			LastConnected: time.Time{},
		},
	}, nil
}

// NewHandlerWithOptions creates a new MongoDB handler with functional options
func NewHandlerWithOptions(connectionString, database string, opts ...ConfigOption) (*Handler, error) {
	config := DefaultConfig(connectionString, database)
	for _, opt := range opts {
		opt(config)
	}

	return NewHandler(config)
}

// GetClient returns the MongoDB client, connecting if necessary
func (h *Handler) GetClient(ctx context.Context) (*mongo.Client, error) {
	h.mu.RLock()
	if h.connected && h.client != nil {
		h.mu.RUnlock()
		return h.client, nil
	}
	h.mu.RUnlock()

	return h.connectWithLock(ctx)
}

// connectWithLock handles connection with proper locking
func (h *Handler) connectWithLock(ctx context.Context) (*mongo.Client, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check pattern
	if h.connected && h.client != nil {
		return h.client, nil
	}

	return h.connect(ctx)
}

// connect establishes connection to MongoDB
func (h *Handler) connect(ctx context.Context) (*mongo.Client, error) {
	// Notify monitoring hooks
	for _, hook := range h.config.MonitoringHooks {
		ctx = hook.OnConnect(ctx, h.config.ConnectionString)
	}

	clientOptions := h.config.BuildClientOptions()

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		h.handleError(ctx, err, "connect")
		return nil, fmt.Errorf("failed-to-connect-to-mongodb: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx) // Clean up
		h.handleError(ctx, err, "ping")
		return nil, fmt.Errorf("failed-to-ping-mongodb: %w", err)
	}

	h.client = client
	h.connected = true
	h.lastError = nil
	h.stats.ConnectionsCreated++
	h.stats.ConnectionsActive++
	h.stats.LastConnected = time.Now()

	return client, nil
}

// GetDatabase returns the specified database
func (h *Handler) GetDatabase(ctx context.Context, name string) (*mongo.Database, error) {
	client, err := h.GetClient(ctx)
	if err != nil {
		return nil, err
	}

	if name == "" {
		name = h.config.Database
	}

	return client.Database(name), nil
}

// Ping tests the connection to MongoDB
func (h *Handler) Ping(ctx context.Context) error {
	client, err := h.GetClient(ctx)
	if err != nil {
		return err
	}

	return client.Ping(ctx, nil)
}

// Close closes the MongoDB connection
func (h *Handler) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.client != nil {
		// Notify monitoring hooks
		for _, hook := range h.config.MonitoringHooks {
			hook.OnDisconnect(ctx, h.config.ConnectionString)
		}

		err := h.client.Disconnect(ctx)
		h.client = nil
		h.connected = false
		h.stats.ConnectionsActive--
		return err
	}

	return nil
}

// Reconnect closes existing connection and establishes a new one
func (h *Handler) Reconnect(ctx context.Context) error {
	if err := h.Close(ctx); err != nil {
		return fmt.Errorf("failed to close existing connection: %w", err)
	}

	_, err := h.connectWithLock(ctx)
	return err
}

// Stats returns connection statistics
func (h *Handler) Stats() ConnectionStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	return ConnectionStats{
		ConnectionsCreated: h.stats.ConnectionsCreated,
		ConnectionsActive:  h.stats.ConnectionsActive,
		LastConnected:      h.stats.LastConnected,
		LastError:          h.lastError,
		ErrorCount:         h.stats.ErrorCount,
	}
}

// Health returns health information about the MongoDB connection
func (h *Handler) Health(ctx context.Context) map[string]interface{} {
	health := map[string]interface{}{
		"connected":           h.connected,
		"database":            h.config.Database,
		"connections_created": h.stats.ConnectionsCreated,
		"connections_active":  h.stats.ConnectionsActive,
		"last_connected":      h.stats.LastConnected,
		"error_count":         h.stats.ErrorCount,
	}

	if h.lastError != nil {
		health["last_error"] = h.lastError.Error()
	}

	// Try to ping if connected
	if h.connected {
		if err := h.Ping(ctx); err != nil {
			health["ping_error"] = err.Error()
			health["healthy"] = false
		} else {
			health["healthy"] = true
		}
	} else {
		health["healthy"] = false
	}

	return health
}

// handleError processes errors and updates stats
func (h *Handler) handleError(ctx context.Context, err error, operation string) {
	h.lastError = err
	h.stats.ErrorCount++

	for _, hook := range h.config.MonitoringHooks {
		hook.OnError(ctx, err, operation)
	}
}
