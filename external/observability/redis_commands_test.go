package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	redis "github.com/go-redis/redis/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestRedisUnknownCommandNamesNeverReachTelemetry(t *testing.T) {
	const sensitive = "synthetic-sensitive-command-token"
	for _, commandName := range []string{
		sensitive,
		"custom." + sensitive,
		"get " + sensitive,
		"https://" + sensitive,
		strings.Repeat("x", 65),
		"",
	} {
		t.Run(fmt.Sprintf("name-length-%d", len(commandName)), func(t *testing.T) {
			hook, recorder, reader := newTestRedisHook(t, nil)
			command := redis.NewCmd(commandName, "private-index", "private-query-token")
			command.SetErr(errors.New("command failed with private-query-token"))
			ctx, err := hook.BeforeProcess(context.Background(), command)
			require.NoError(t, err)
			require.NoError(t, hook.AfterProcess(ctx, command))
			span := requireSingleRedisSpan(t, recorder)
			assert.Equal(t, "redis.unknown", span.Name())
			assertRedisAttributes(t, span.Attributes(), unknownRedisOperation, "localhost", 6379)
			assertTelemetryDoesNotContain(t, span, sensitive, "private-index", "private-query-token")
			metrics := collectRedisMetrics(t, reader)
			duration := requireRedisHistogram(t, metrics, "db.client.operation.duration")
			require.Len(t, duration.DataPoints, 1)
			assertRedisMetricAttributes(t, duration.DataPoints[0].Attributes, unknownRedisOperation, "localhost", 6379)
			assert.EqualValues(t, 1, redisErrorCount(t, metrics), "failed unknown commands still contribute to error metrics")
			assertMetricDataDoesNotContain(t, metrics, sensitive, "private-index", "private-query-token")
		})
	}
}

func TestRedisModuleCommandsAreExplicitSnapshotsIsolatedPerHook(t *testing.T) {
	names := []string{"FT.SEARCH", "JSON.GET"}
	registered, err := WithRedisModuleCommands(names...)
	require.NoError(t, err)
	// Neither later construction nor an already-constructed hook may read the
	// caller's mutable slice after the option has captured trusted constants.
	firstHook, firstRecorder, firstReader := newTestRedisCommandHook(t, registered)
	names[0] = "synthetic-sensitive-command-token"
	names[1] = "unregistered.command"
	secondHook, secondRecorder, secondReader := newTestRedisCommandHook(t, registered)
	unregisteredHook, unregisteredRecorder, unregisteredReader := newTestRedisCommandHook(t)
	for _, test := range []struct {
		name     string
		hook     redis.Hook
		recorder *tracetest.SpanRecorder
		reader   *sdkmetric.ManualReader
		want     string
	}{
		{name: "constructed before mutation", hook: firstHook, recorder: firstRecorder, reader: firstReader, want: "ft.search"},
		{name: "option reused after mutation", hook: secondHook, recorder: secondRecorder, reader: secondReader, want: "ft.search"},
		{name: "another hook did not opt in", hook: unregisteredHook, recorder: unregisteredRecorder, reader: unregisteredReader, want: unknownRedisOperation},
	} {
		t.Run(test.name, func(t *testing.T) {
			command := redis.NewCmd("FT.SEARCH", "private-index", "@user:private-query-token")
			ctx, err := test.hook.BeforeProcess(context.Background(), command)
			require.NoError(t, err)
			require.NoError(t, test.hook.AfterProcess(ctx, command))
			span := requireSingleRedisSpan(t, test.recorder)
			assert.Equal(t, "redis."+test.want, span.Name())
			assertRedisAttributes(t, span.Attributes(), test.want, "localhost", 6379)
			assertTelemetryDoesNotContain(t, span, "private-index", "private-query-token", "synthetic-sensitive-command-token")
			metrics := collectRedisMetrics(t, test.reader)
			duration := requireRedisHistogram(t, metrics, "db.client.operation.duration")
			require.Len(t, duration.DataPoints, 1)
			assertRedisMetricAttributes(t, duration.DataPoints[0].Attributes, test.want, "localhost", 6379)
			assertMetricDataDoesNotContain(t, metrics, "private-index", "private-query-token", "synthetic-sensitive-command-token")
		})
	}
	// The mutated caller input must not have silently registered a new name.
	command := redis.NewCmd(names[0], "private-value")
	ctx, err := secondHook.BeforeProcess(context.Background(), command)
	require.NoError(t, err)
	require.NoError(t, secondHook.AfterProcess(ctx, command))
	spans := secondRecorder.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "redis.unknown", spans[1].Name())
	assertTelemetryDoesNotContain(t, spans[1], names[0], "private-value")
}

func TestRedisModuleCommandOptionsReplaceRatherThanAccumulate(t *testing.T) {
	firstNames, secondNames := make([]string, 64), make([]string, 64)
	for index := range firstNames {
		firstNames[index] = fmt.Sprintf("first.command%d", index)
		secondNames[index] = fmt.Sprintf("second.command%d", index)
	}
	first, err := WithRedisModuleCommands(firstNames...)
	require.NoError(t, err)
	second, err := WithRedisModuleCommands(secondNames...)
	require.NoError(t, err)
	clear, err := WithRedisModuleCommands()
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		options []RedisHookOption
		want    []string
	}{
		{name: "last list replaces the preceding64 entries", options: []RedisHookOption{first, second}, want: []string{unknownRedisOperation, "second.command63", "eval"}},
		{name: "empty list clears module registration", options: []RedisHookOption{first, second, clear}, want: []string{unknownRedisOperation, unknownRedisOperation, "eval"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			hook, recorder, reader := newTestRedisCommandHook(t, test.options...)
			for _, name := range []string{"first.command0", "second.command63", "EVAL"} {
				command := redis.NewCmd(name, "private-argument")
				ctx, err := hook.BeforeProcess(context.Background(), command)
				require.NoError(t, err)
				require.NoError(t, hook.AfterProcess(ctx, command))
			}
			spans := recorder.Ended()
			require.Len(t, spans, len(test.want))
			for index, want := range test.want {
				assert.Equal(t, "redis."+want, spans[index].Name())
				assertTelemetryDoesNotContain(t, spans[index], "private-argument")
			}
			assert.EqualValues(t, 3, redisDurationCount(t, collectRedisMetrics(t, reader)))
		})
	}
}

func TestRedisModuleCommandRegistrationRejectsInvalidNamesWithoutEcho(t *testing.T) {
	const sensitive = "synthetic-sensitive-registration-input"
	for _, names := range [][]string{
		make([]string, 65), {""}, {" "}, {sensitive + "\n"},
		{"9" + sensitive}, {"." + sensitive}, {sensitive + "/path"},
		{sensitive + " query"}, {sensitive + "=value"}, {"K" + sensitive},
		{strings.Repeat("a", 65)}, {"UNKNOWN"}, {"batch"},
	} {
		option, err := WithRedisModuleCommands(names...)
		require.Error(t, err)
		assert.Nil(t, option)
		assert.NotContains(t, err.Error(), sensitive)
	}
	_, err := WithRedisModuleCommands(strings.Repeat("a", 64), "Module.Command_2-v1", "FT.SEARCH", "ft.search")
	require.NoError(t, err, "maximum-length tokens and duplicate case variants are valid")
}

func TestRedisCoreVocabularyPreservesCommandsBuiltByGoRedisV7(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "unused.example.test:6379"})
	t.Cleanup(func() { _ = client.Close() })
	pipeline := client.Pipeline()
	t.Cleanup(func() { _ = pipeline.Close() })
	// Build actual client commands without executing network operations. These
	// cover families whose names are assembled in different v7 code paths.
	commands := []redis.Cmder{
		pipeline.Get("private-key"),
		pipeline.Set("private-key", "private-value", 0),
		pipeline.Eval("private-script", []string{"private-key"}),
		pipeline.EvalSha("private-digest", []string{"private-key"}),
		pipeline.HSet("private-key", "private-field", "private-value"),
		pipeline.ZAdd("private-key", &redis.Z{Score: 1, Member: "private-member"}),
		pipeline.ZRangeByLex("private-key", &redis.ZRangeBy{Min: "-", Max: "+"}),
		pipeline.ZRevRangeByLex("private-key", &redis.ZRangeBy{Min: "-", Max: "+"}),
		pipeline.XAdd(&redis.XAddArgs{Stream: "private-key", Values: map[string]interface{}{"field": "private-value"}}),
		pipeline.XRead(&redis.XReadArgs{Streams: []string{"private-key", "0"}}),
		pipeline.XReadGroup(&redis.XReadGroupArgs{Group: "private-group", Consumer: "private-consumer", Streams: []string{"private-key", ">"}}),
		pipeline.GeoRadius("private-key", 0, 0, &redis.GeoRadiusQuery{Radius: 1, Unit: "km"}),
		pipeline.GeoRadiusByMember("private-key", "private-member", &redis.GeoRadiusQuery{Radius: 1, Unit: "km"}),
		pipeline.BitField("private-key", "GET", "i8", 0),
		pipeline.ScriptLoad("private-script"),
		pipeline.ClientGetName(),
		pipeline.ClusterInfo(),
	}
	want := []string{"get", "set", "eval", "evalsha", "hset", "zadd", "zrangebylex", "zrevrangebylex", "xadd", "xread", "xreadgroup", "georadius_ro", "georadiusbymember_ro", "bitfield", "script", "client", "cluster"}
	hook, recorder, reader := newTestRedisHook(t, nil)
	for _, command := range commands {
		ctx, err := hook.BeforeProcess(context.Background(), command)
		require.NoError(t, err)
		require.NoError(t, hook.AfterProcess(ctx, command))
	}
	spans := recorder.Ended()
	require.Len(t, spans, len(want))
	for index, operation := range want {
		assert.Equal(t, "redis."+operation, spans[index].Name())
		assertRedisAttributes(t, spans[index].Attributes(), operation, "localhost", 6379)
		assertTelemetryDoesNotContain(t, spans[index], "private-")
	}
	metrics := collectRedisMetrics(t, reader)
	assert.EqualValues(t, len(want), redisDurationCount(t, metrics))
	assertMetricDataDoesNotContain(t, metrics, "private-")
}

func TestRedisUnknownAndRegisteredCommandsKeepNilAndPipelineCategories(t *testing.T) {
	registered, err := WithRedisModuleCommands("FT.SEARCH")
	require.NoError(t, err)
	hook, recorder, reader := newTestRedisCommandHook(t, registered)
	ctx, err := hook.BeforeProcess(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, hook.AfterProcess(ctx, nil))
	commands := []redis.Cmder{nil, redis.NewCmd("FT.SEARCH", "private-query"), redis.NewCmd("synthetic-sensitive-command-token")}
	ctx, err = hook.BeforeProcessPipeline(context.Background(), commands)
	require.NoError(t, err)
	require.NoError(t, hook.AfterProcessPipeline(ctx, commands))
	spans := recorder.Ended()
	require.Len(t, spans, 2)
	assert.Equal(t, "redis.unknown", spans[0].Name())
	assert.Equal(t, "redis.batch", spans[1].Name())
	assert.Equal(t, redisBatchOperation, redisSpanAttribute(t, spans[1].Attributes(), "db.operation.name").AsString())
	assertTelemetryDoesNotContain(t, spans[1], "ft.search", "private-query", "synthetic-sensitive-command-token")
	metrics := collectRedisMetrics(t, reader)
	assert.EqualValues(t, 2, redisDurationCount(t, metrics))
	assertMetricDataDoesNotContain(t, metrics, "ft.search", "private-query", "synthetic-sensitive-command-token")
}

func newTestRedisCommandHook(t *testing.T, options ...RedisHookOption) (redis.Hook, *tracetest.SpanRecorder, *sdkmetric.ManualReader) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	reader := sdkmetric.NewManualReader()
	meter := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		require.NoError(t, tracer.Shutdown(context.Background()))
		require.NoError(t, meter.Shutdown(context.Background()))
	})
	options = append(options, WithRedisTracerProvider(tracer), WithRedisMeterProvider(meter))
	return NewRedisHook(nil, options...), recorder, reader
}
