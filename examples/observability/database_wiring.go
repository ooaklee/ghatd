package main

import (
	redis "github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/observability"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
)

// mongoOptionsWithTelemetry demonstrates compile-checked MongoDB v2 wiring.
// Supply application connection settings and use these options with mongo.Connect.
// This example does not connect to a database or claim a database integration test.
func mongoOptionsWithTelemetry(runtime *observability.Runtime) *options.ClientOptions {
	return options.Client().SetMonitor(observability.NewMongoCommandMonitor(
		otelmongo.WithTracerProvider(runtime.SDK().TracerProvider()),
	))
}

// redisClientWithTelemetry demonstrates compile-checked Redis v7 wiring.
// Commands must receive the caller's context. Close the client before shutting
// down the runtime. The runnable reference needs no Redis server.
func redisClientWithTelemetry(runtime *observability.Runtime, options *redis.Options) *redis.Client {
	client := redis.NewClient(options)
	client.AddHook(observability.NewRedisHook(options,
		observability.WithRedisTracerProvider(runtime.SDK().TracerProvider()),
		observability.WithRedisMeterProvider(runtime.SDK().MeterProvider()),
	))
	return client
}
