package repositoryhelpers

import (
	"context"
	"log"
	"strings"
	"time"
)

// LoggingHook provides basic logging for MongoDB operations
type LoggingHook struct {
	logger interface {
		Printf(format string, v ...interface{})
	}

	maskList []string
}

// NewLoggingHook creates a new logging hook
func NewLoggingHook(logger interface {
	Printf(format string, v ...interface{})
}, maskList []string) *LoggingHook {
	if logger == nil {
		logger = log.Default()
	}
	return &LoggingHook{logger: logger, maskList: maskList}
}

// OnConnect logs connection events
func (l *LoggingHook) OnConnect(ctx context.Context, addr string) context.Context {

	for _, sensitive := range l.maskList {
		addr = strings.ReplaceAll(addr, sensitive, "[MASKED]")
	}

	l.logger.Printf("MongoDB: Connecting to %s", addr)
	return ctx
}

// OnDisconnect logs disconnection events
func (l *LoggingHook) OnDisconnect(ctx context.Context, addr string) {

	for _, sensitive := range l.maskList {
		addr = strings.ReplaceAll(addr, sensitive, "[MASKED]")
	}

	l.logger.Printf("MongoDB: Disconnected from %s", addr)
}

// OnError logs error events
func (l *LoggingHook) OnError(ctx context.Context, err error, operation string) {
	l.logger.Printf("MongoDB Error during %s: %v", operation, err)
}

// MetricsHook provides metrics collection for MongoDB operations
type MetricsHook struct {
	connectCount    int64
	disconnectCount int64
	errorCount      int64
	lastConnect     time.Time
	lastDisconnect  time.Time
	lastError       time.Time
}

// NewMetricsHook creates a new metrics hook
func NewMetricsHook() *MetricsHook {
	return &MetricsHook{}
}

// OnConnect records connection metrics
func (m *MetricsHook) OnConnect(ctx context.Context, addr string) context.Context {
	m.connectCount++
	m.lastConnect = time.Now()
	return ctx
}

// OnDisconnect records disconnection metrics
func (m *MetricsHook) OnDisconnect(ctx context.Context, addr string) {
	m.disconnectCount++
	m.lastDisconnect = time.Now()
}

// OnError records error metrics
func (m *MetricsHook) OnError(ctx context.Context, err error, operation string) {
	m.errorCount++
	m.lastError = time.Now()
}

// GetMetrics returns current metrics
func (m *MetricsHook) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"connect_count":    m.connectCount,
		"disconnect_count": m.disconnectCount,
		"error_count":      m.errorCount,
		"last_connect":     m.lastConnect,
		"last_disconnect":  m.lastDisconnect,
		"last_error":       m.lastError,
	}
}

// CircuitBreakerHook provides circuit breaker functionality
type CircuitBreakerHook struct {
	maxErrors    int
	errorCount   int
	resetTimeout time.Duration
	lastError    time.Time
	state        string // "closed", "open", "half-open"
}

// NewCircuitBreakerHook creates a new circuit breaker hook
func NewCircuitBreakerHook(maxErrors int, resetTimeout time.Duration) *CircuitBreakerHook {
	return &CircuitBreakerHook{
		maxErrors:    maxErrors,
		resetTimeout: resetTimeout,
		state:        "closed",
	}
}

// OnConnect resets circuit breaker on successful connection
func (cb *CircuitBreakerHook) OnConnect(ctx context.Context, addr string) context.Context {
	if cb.state == "half-open" {
		cb.state = "closed"
		cb.errorCount = 0
	}
	return ctx
}

// OnDisconnect handles disconnection events
func (cb *CircuitBreakerHook) OnDisconnect(ctx context.Context, addr string) {
	// Could implement logic here if needed
}

// OnError handles error events and circuit breaker logic
func (cb *CircuitBreakerHook) OnError(ctx context.Context, err error, operation string) {
	cb.errorCount++
	cb.lastError = time.Now()

	if cb.errorCount >= cb.maxErrors {
		cb.state = "open"
	}
}

// ShouldAllowConnection checks if connection should be allowed
func (cb *CircuitBreakerHook) ShouldAllowConnection() bool {
	switch cb.state {
	case "closed":
		return true
	case "open":
		if time.Since(cb.lastError) > cb.resetTimeout {
			cb.state = "half-open"
			return true
		}
		return false
	case "half-open":
		return true
	default:
		return false
	}
}

// GetState returns current circuit breaker state
func (cb *CircuitBreakerHook) GetState() map[string]interface{} {
	return map[string]interface{}{
		"state":         cb.state,
		"error_count":   cb.errorCount,
		"max_errors":    cb.maxErrors,
		"last_error":    cb.lastError,
		"reset_timeout": cb.resetTimeout,
	}
}
