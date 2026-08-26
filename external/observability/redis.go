package observability

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	redis "github.com/go-redis/redis/v7"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	redisInstrumentationName = "github.com/ooaklee/ghatd/external/observability/redis"
	redisBatchOperation      = "BATCH"
	unknownRedisOperation    = "unknown"
)

var redisDurationBuckets = []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10}

// RedisHookOption configures the Redis OpenTelemetry hook.
type RedisHookOption interface {
	applyRedisHook(*redisHookConfig)
}

type redisHookOptionFunc func(*redisHookConfig)

// applyRedisHook applies a functional option to the Redis hook configuration.
func (option redisHookOptionFunc) applyRedisHook(config *redisHookConfig) {
	option(config)
}

type redisHookConfig struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// WithRedisTracerProvider configures the tracer provider used by a Redis hook.
// The global OpenTelemetry tracer provider is used by default.
func WithRedisTracerProvider(provider trace.TracerProvider) RedisHookOption {
	return redisHookOptionFunc(func(config *redisHookConfig) {
		if provider != nil {
			config.tracerProvider = provider
		}
	})
}

// WithRedisMeterProvider configures the meter provider used by a Redis hook.
// The global OpenTelemetry meter provider is used by default.
func WithRedisMeterProvider(provider metric.MeterProvider) RedisHookOption {
	return redisHookOptionFunc(func(config *redisHookConfig) {
		if provider != nil {
			config.meterProvider = provider
		}
	})
}

type redisHook struct {
	tracer            trace.Tracer
	operationDuration metric.Float64Histogram
	operationErrors   metric.Int64Counter
	serverAttributes  []attribute.KeyValue
	stateKey          *redisHookStateKey
}

// The byte gives independently constructed hooks distinct, non-zero-sized
// context keys even on runtimes that coalesce pointers to zero-sized values.
type redisHookStateKey struct {
	_ byte
}

type redisOperationState struct {
	operation string
	startedAt time.Time
	span      trace.Span
	once      sync.Once
}

// NewRedisHook creates an OpenTelemetry hook compatible with go-redis v7.
//
// Telemetry contains the Redis command name, but never command arguments,
// query text, keys, or values. A pipeline is represented as one BATCH client
// operation so its cardinality is independent of its commands and size.
func NewRedisHook(options *redis.Options, hookOptions ...RedisHookOption) redis.Hook {
	config := redisHookConfig{
		tracerProvider: otel.GetTracerProvider(),
		meterProvider:  otel.GetMeterProvider(),
	}
	for _, option := range hookOptions {
		if option != nil {
			option.applyRedisHook(&config)
		}
	}

	meter := config.meterProvider.Meter(redisInstrumentationName)
	operationDuration, err := meter.Float64Histogram(
		"db.client.operation.duration",
		metric.WithDescription("Duration of Redis client operations."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(redisDurationBuckets...),
	)
	if err != nil {
		otel.Handle(err)
		operationDuration = nil
	}
	operationErrors, err := meter.Int64Counter(
		"db.client.operation.errors",
		metric.WithDescription("Number of failed Redis client operations."),
		metric.WithUnit("{error}"),
	)
	if err != nil {
		otel.Handle(err)
		operationErrors = nil
	}

	return &redisHook{
		tracer:            config.tracerProvider.Tracer(redisInstrumentationName),
		operationDuration: operationDuration,
		operationErrors:   operationErrors,
		serverAttributes:  redisServerAttributes(options),
		stateKey:          &redisHookStateKey{},
	}
}

// BeforeProcess starts telemetry for one Redis command.
func (hook *redisHook) BeforeProcess(ctx context.Context, command redis.Cmder) (context.Context, error) {
	return hook.start(ctx, redisOperationName(command)), nil
}

// AfterProcess completes telemetry for one Redis command.
func (hook *redisHook) AfterProcess(ctx context.Context, command redis.Cmder) error {
	hook.finish(ctx, redisCommandError(command))

	return nil
}

// BeforeProcessPipeline starts one low-cardinality span for a Redis pipeline.
func (hook *redisHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return hook.start(ctx, redisBatchOperation), nil
}

// AfterProcessPipeline completes telemetry for a Redis pipeline.
func (hook *redisHook) AfterProcessPipeline(ctx context.Context, commands []redis.Cmder) error {
	hook.finish(ctx, redisPipelineError(commands))

	return nil
}

// start records the operation start time and opens its client span.
func (hook *redisHook) start(ctx context.Context, operation string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}

	attributes := make([]attribute.KeyValue, 0, len(hook.serverAttributes)+2)
	attributes = append(attributes, semconv.DBSystemNameRedis, semconv.DBOperationName(operation))
	attributes = append(attributes, hook.serverAttributes...)

	spanName := "redis." + strings.ToLower(operation)
	spanCtx, span := hook.tracer.Start(
		ctx,
		spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attributes...),
	)
	state := &redisOperationState{
		operation: operation,
		startedAt: time.Now(),
		span:      span,
	}

	return context.WithValue(spanCtx, hook.stateKey, state)
}

// finish closes an operation once and records its duration and error count.
func (hook *redisHook) finish(ctx context.Context, operationErr error) {
	if ctx == nil {
		return
	}
	state, ok := ctx.Value(hook.stateKey).(*redisOperationState)
	if !ok || state == nil {
		return
	}

	state.once.Do(func() {
		attributes := make([]attribute.KeyValue, 0, len(hook.serverAttributes)+3)
		attributes = append(attributes, semconv.DBSystemNameRedis, semconv.DBOperationName(state.operation))
		attributes = append(attributes, hook.serverAttributes...)

		if isRedisOperationError(operationErr) {
			errorType := semconv.ErrorType(operationErr)
			attributes = append(attributes, errorType)
			state.span.SetAttributes(errorType)
			state.span.SetStatus(codes.Error, "Redis operation failed")

			if hook.operationErrors != nil {
				hook.operationErrors.Add(ctx, 1, metric.WithAttributes(attributes...))
			}
		}

		if hook.operationDuration != nil {
			hook.operationDuration.Record(
				ctx,
				time.Since(state.startedAt).Seconds(),
				metric.WithAttributes(attributes...),
			)
		}
		state.span.End()
	})
}

// redisOperationName returns a stable command name without arguments.
func redisOperationName(command redis.Cmder) string {
	if command == nil {
		return unknownRedisOperation
	}
	operation := strings.TrimSpace(command.Name())
	if operation == "" {
		return unknownRedisOperation
	}

	return strings.ToLower(operation)
}

// redisCommandError returns a command error when a command is present.
func redisCommandError(command redis.Cmder) error {
	if command == nil {
		return nil
	}

	return command.Err()
}

// redisPipelineError returns the first reportable pipeline error.
func redisPipelineError(commands []redis.Cmder) error {
	for _, command := range commands {
		operationErr := redisCommandError(command)
		if isRedisOperationError(operationErr) {
			return operationErr
		}
	}

	return nil
}

// isRedisOperationError excludes Redis cache misses from failure telemetry.
func isRedisOperationError(operationErr error) bool {
	return operationErr != nil && !errors.Is(operationErr, redis.Nil)
}

// redisServerAttributes derives safe server and transport attributes from options.
func redisServerAttributes(options *redis.Options) []attribute.KeyValue {
	network := "tcp"
	address := "localhost:6379"
	if options != nil {
		if options.Network != "" {
			network = strings.ToLower(options.Network)
		} else if strings.HasPrefix(options.Addr, "/") {
			network = "unix"
		}
		if options.Addr != "" {
			address = options.Addr
		}
	}

	if network == "unix" {
		return []attribute.KeyValue{
			semconv.ServerAddress(address),
			semconv.NetworkTransportUnix,
		}
	}

	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return []attribute.KeyValue{
			semconv.ServerAddress(address),
			semconv.NetworkTransportTCP,
		}
	}
	if host == "" {
		host = "localhost"
	}

	attributes := []attribute.KeyValue{
		semconv.ServerAddress(host),
		semconv.NetworkTransportTCP,
	}
	if port, err := strconv.Atoi(portText); err == nil && port > 0 && port <= 65535 {
		attributes = append(attributes, semconv.ServerPort(port))
	}

	return attributes
}
