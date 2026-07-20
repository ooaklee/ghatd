package repositoryhelpers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.uber.org/zap"
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
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "get-client")

	h.mu.RLock()
	if h.connected && h.client != nil {
		h.mu.RUnlock()
		logger.Debug("mongo-client-cache-hit")
		return h.client, nil
	}
	h.mu.RUnlock()

	logger.Debug("mongo-client-cache-miss")
	return h.connectWithLock(ctx)
}

// connectWithLock handles connection with proper locking
func (h *Handler) connectWithLock(ctx context.Context) (*mongo.Client, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "connect-with-lock")
	h.mu.Lock()
	defer h.mu.Unlock()

	// Double-check pattern
	if h.connected && h.client != nil {
		logger.Debug("mongo-client-connected-while-waiting-for-lock")
		return h.client, nil
	}

	return h.connect(ctx)
}

// connect establishes connection to MongoDB
func (h *Handler) connect(ctx context.Context) (*mongo.Client, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "connect")
	logger.Debug("mongo-connect-started", zap.Int("monitoring-hooks", len(h.config.MonitoringHooks)))

	// Notify monitoring hooks
	for _, hook := range h.config.MonitoringHooks {
		ctx = hook.OnConnect(ctx, h.config.ConnectionString)
	}

	clientOptions := h.config.BuildClientOptions()

	client, err := mongo.Connect(clientOptions)
	if err != nil {
		h.handleError(ctx, err, "connect")
		logger.Error("mongo-connect-failed", zap.Error(err))
		return nil, fmt.Errorf("failed-to-connect-to-mongodb: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		client.Disconnect(ctx) // Clean up
		h.handleError(ctx, err, "ping")
		logger.Error("mongo-ping-after-connect-failed", zap.Error(err))
		return nil, fmt.Errorf("failed-to-ping-mongodb: %w", err)
	}

	h.client = client
	h.connected = true
	h.lastError = nil
	h.stats.ConnectionsCreated++
	h.stats.ConnectionsActive++
	h.stats.LastConnected = time.Now()

	logger.Info("mongo-connect-completed", zap.Int64("connections-created", h.stats.ConnectionsCreated), zap.Int64("connections-active", h.stats.ConnectionsActive))
	return client, nil
}

// GetDatabase returns the specified database
func (h *Handler) GetDatabase(ctx context.Context, name string) (*mongo.Database, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "get-database")
	client, err := h.GetClient(ctx)
	if err != nil {
		logger.Error("mongo-database-client-unavailable", zap.Error(err))
		return nil, err
	}

	if name == "" {
		name = h.config.Database
	}

	logger.Debug("mongo-database-resolved", zap.String("database", name))
	return client.Database(name), nil
}

// Ping tests the connection to MongoDB
func (h *Handler) Ping(ctx context.Context) error {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "ping")
	client, err := h.GetClient(ctx)
	if err != nil {
		logger.Error("mongo-ping-client-unavailable", zap.Error(err))
		return err
	}

	if err := client.Ping(ctx, nil); err != nil {
		logger.Error("mongo-ping-failed", zap.Error(err))
		return err
	}

	logger.Debug("mongo-ping-completed")
	return nil
}

// Close closes the MongoDB connection
func (h *Handler) Close(ctx context.Context) error {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "close")
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.client != nil {
		logger.Debug("mongo-close-started", zap.Int64("connections-active", h.stats.ConnectionsActive))
		// Notify monitoring hooks
		for _, hook := range h.config.MonitoringHooks {
			hook.OnDisconnect(ctx, h.config.ConnectionString)
		}

		err := h.client.Disconnect(ctx)
		h.client = nil
		h.connected = false
		h.stats.ConnectionsActive--
		if err != nil {
			logger.Error("mongo-close-failed", zap.Error(err))
			return err
		}
		logger.Info("mongo-close-completed", zap.Int64("connections-active", h.stats.ConnectionsActive))
		return err
	}

	logger.Debug("mongo-close-skipped-no-client")
	return nil
}

// Reconnect closes existing connection and establishes a new one
func (h *Handler) Reconnect(ctx context.Context) error {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "reconnect")
	logger.Info("mongo-reconnect-started")

	if err := h.Close(ctx); err != nil {
		logger.Error("mongo-reconnect-close-failed", zap.Error(err))
		return fmt.Errorf("failed to close existing connection: %w", err)
	}

	_, err := h.connectWithLock(ctx)
	if err != nil {
		logger.Error("mongo-reconnect-failed", zap.Error(err))
		return err
	}
	logger.Info("mongo-reconnect-completed")
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
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "health")
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
			logger.Warn("mongo-health-ping-failed", zap.Error(err))
		} else {
			health["healthy"] = true
			logger.Debug("mongo-health-ping-succeeded")
		}
	} else {
		health["healthy"] = false
		logger.Debug("mongo-health-not-connected")
	}

	return health
}

// handleError processes errors and updates stats
func (h *Handler) handleError(ctx context.Context, err error, operation string) {
	logger := logger.AcquireOperationFrom(ctx, "external/repository/helpers", "handle-error", zap.String("mongo-operation", operation))
	h.lastError = err
	h.stats.ErrorCount++
	logger.Error("mongo-operation-error-recorded", zap.Int64("error-count", h.stats.ErrorCount), zap.Error(err))

	for _, hook := range h.config.MonitoringHooks {
		hook.OnError(ctx, err, operation)
	}
}
