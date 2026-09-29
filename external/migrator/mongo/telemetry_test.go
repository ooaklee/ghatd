package mongo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelcobra"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	migrate "github.com/xakep666/mongo-migrate"
	"go.mongodb.org/mongo-driver/v2/event"
	mongodb "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// Exercise the real command tree twice. A monitor created before the first
// invocation would keep its old provider and lose the second set of DB spans.
func TestTelemetryCreatesFreshMonitorAndFlushesAfterDisconnect(t *testing.T) {
	config, exporters := migrationTelemetryFixture(t)
	settings := validTestSettings(t.TempDir())
	client := &telemetryMongoClient{}
	var monitors []*event.CommandMonitor
	var actionContexts []context.Context
	configured := commandOptionsWithDefaults()
	WithTelemetry(func(*cobra.Command) (otelcobra.Config, error) { return config, nil })(&configured)
	runner := commandRunner{
		loadSettings: func() (*Settings, error) { return &settings, nil }, telemetry: configured.telemetry,
		dependencies: commandDependencies{
			registeredMigrations: func() []migrate.Migration { return []migrate.Migration{{Version: 1}} },
			connect: func(options *options.ClientOptions) (mongoClient, error) {
				require.NotNil(t, options.Monitor)
				client.monitor = options.Monitor
				monitors = append(monitors, options.Monitor)
				return client, nil
			},
			newMigrationRunner: func(*mongodb.Database, []migrate.Migration, string) migrationRunner {
				return callbackMigration{run: func(ctx context.Context) error {
					require.NotNil(t, observability.RuntimeFromContext(ctx))
					actionContexts = append(actionContexts, ctx)
					return nil
				}}
			},
		},
	}
	command := newCommand(runner)
	command.SetOut(io.Discard)
	command.SetErr(io.Discard)
	base := context.WithValue(context.Background(), migrationContextKey{}, "retained")
	for i, action := range []string{"up", "down"} {
		child, _, err := command.Find([]string{action})
		require.NoError(t, err)
		child.PreRunE = func(cmd *cobra.Command, _ []string) error {
			assert.Nil(t, observability.RuntimeFromContext(cmd.Context()))
			return nil
		}
		child.PostRunE = func(cmd *cobra.Command, _ []string) error {
			assert.Nil(t, observability.RuntimeFromContext(cmd.Context()))
			assert.True(t, (*exporters)[i].closed.Load(), "flush finishes before the post-hook")
			return nil
		}
		client.disconnect = func(ctx context.Context) error {
			assert.False(t, (*exporters)[i].closed.Load())
			assert.Equal(t, "retained", ctx.Value(migrationContextKey{}))
			assert.Equal(t, trace.SpanContextFromContext(actionContexts[i]), trace.SpanContextFromContext(ctx))
			assert.NoError(t, ctx.Err())
			_, hasDeadline := ctx.Deadline()
			assert.True(t, hasDeadline)
			return nil
		}
		command.SetArgs([]string{action})
		require.NoError(t, command.ExecuteContext(base))
		assert.True(t, base == child.Context())
		spans := (*exporters)[i].snapshot()
		require.Len(t, spans, 2)
		assert.Equal(t, "mongodb.ping", spans[0].Name())
		assert.Equal(t, "mongo-migrator."+action, spans[1].Name())
		assert.Equal(t, config.Scope, spans[1].InstrumentationScope().Name)
		assert.Equal(t, spans[1].SpanContext(), spans[0].Parent())
		service, ok := spans[0].Resource().Set().Value("service.name")
		require.True(t, ok)
		assert.Equal(t, config.Runtime.Telemetry.ServiceName, service.AsString())
	}
	assert.NotSame(t, monitors[0], monitors[1])
	assert.NotSame(t, observability.RuntimeFromContext(actionContexts[0]), observability.RuntimeFromContext(actionContexts[1]))
}

// Cancellation, action/ping errors and panics must all disconnect with a fresh
// budget before the enclosing command span/exporter is closed.
func TestTelemetryDatabaseFailuresCleanUpBeforeFlush(t *testing.T) {
	for _, mode := range []string{"cancel", "action", "ping", "panic"} {
		t.Run(mode, func(t *testing.T) {
			config, exporters := migrationTelemetryFixture(t)
			settings := validTestSettings(t.TempDir())
			base, cancel := context.WithCancel(context.WithValue(context.Background(), migrationContextKey{}, "retained"))
			defer cancel()
			failure, cleanupFailure := errors.New("private operation error"), errors.New("private cleanup error")
			client := &telemetryMongoClient{}
			disconnected := false
			client.disconnect = func(ctx context.Context) error {
				disconnected = true
				assert.False(t, (*exporters)[0].closed.Load())
				assert.NoError(t, ctx.Err())
				assert.Equal(t, "retained", ctx.Value(migrationContextKey{}))
				return cleanupFailure
			}
			if mode == "ping" {
				client.pingErr = failure
			}
			configured := commandOptionsWithDefaults()
			WithTelemetry(func(*cobra.Command) (otelcobra.Config, error) { return config, nil })(&configured)
			runner := commandRunner{
				loadSettings: func() (*Settings, error) { return &settings, nil }, telemetry: configured.telemetry,
				dependencies: commandDependencies{
					registeredMigrations: func() []migrate.Migration { return []migrate.Migration{{Version: 1}} },
					connect:              func(*options.ClientOptions) (mongoClient, error) { return client, nil },
					newMigrationRunner: func(*mongodb.Database, []migrate.Migration, string) migrationRunner {
						return callbackMigration{run: func(ctx context.Context) error {
							switch mode {
							case "cancel":
								cancel()
								return ctx.Err()
							case "panic":
								panic(failure)
							default:
								return failure
							}
						}}
					},
				},
			}
			command := newCommand(runner)
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs([]string{"up"})
			if mode == "panic" {
				assert.PanicsWithValue(t, failure, func() { _ = command.ExecuteContext(base) })
			} else {
				err := command.ExecuteContext(base)
				assert.ErrorIs(t, err, cleanupFailure)
				if mode == "cancel" {
					assert.ErrorIs(t, err, context.Canceled)
				} else {
					assert.ErrorIs(t, err, failure)
				}
			}
			assert.True(t, disconnected)
			assert.True(t, (*exporters)[0].closed.Load())
			spans := (*exporters)[0].snapshot()
			require.Len(t, spans, 1)
			assert.NotContains(t, fmt.Sprint(spans[0].Attributes(), spans[0].Status(), spans[0].Events()), "private")
		})
	}
}

func TestTelemetryBypassesHelpNewAndInvalidArguments(t *testing.T) {
	directory := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(directory, "template.go"), []byte("package migrations\n"), 0o600))
	for _, args := range [][]string{{"up", "--help"}, {"down", "--help"}, {"new", "offline"}, {"up", "invalid"}, {"down", "--invalid"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			command := NewCommand(WithSettings(validTestSettings(directory)), WithTelemetry(func(*cobra.Command) (otelcobra.Config, error) {
				t.Fatal("this command must not resolve telemetry")
				return otelcobra.Config{}, nil
			}))
			command.SetOut(io.Discard)
			command.SetErr(io.Discard)
			command.SetArgs(args)
			err := command.Execute()
			if strings.Contains(args[1], "invalid") {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestExplicitMonitorOverridesTelemetryRegardlessOfOptionOrder(t *testing.T) {
	config, _ := migrationTelemetryFixture(t)
	for _, monitor := range []*event.CommandMonitor{nil, {}} {
		for _, reverse := range []bool{false, true} {
			telemetry := WithTelemetry(func(*cobra.Command) (otelcobra.Config, error) { return config, nil })
			opts := commandOptionsWithDefaults()
			for _, option := range []CommandOption{telemetry, WithMongoCommandMonitor(monitor)} {
				option(&opts)
			}
			if reverse {
				telemetry(&opts)
			}
			settings := validTestSettings(t.TempDir())
			runner := commandRunner{
				loadSettings: func() (*Settings, error) { return &settings, nil }, telemetry: opts.telemetry,
				commandMonitor: opts.commandMonitor, monitorSet: opts.monitorSet,
				dependencies: commandDependencies{
					registeredMigrations: func() []migrate.Migration { return []migrate.Migration{{Version: 1}} },
					connect: func(options *options.ClientOptions) (mongoClient, error) {
						assert.Same(t, monitor, options.Monitor)
						return &fakeMongoClient{}, nil
					},
					newMigrationRunner: func(*mongodb.Database, []migrate.Migration, string) migrationRunner { return &fakeMigrationRunner{} },
				},
			}
			command := newCommand(runner)
			command.SetOut(io.Discard)
			command.SetArgs([]string{"up"})
			require.NoError(t, command.Execute())
		}
	}
}

func TestTelemetryEnvironmentDefaultsOverridesAndSafeErrors(t *testing.T) {
	for _, key := range []string{"ENVIRONMENT", "COMPONENT", "GIT_COMMIT", "GRACEFUL_SERVER_TIMEOUT", "LOG_LEVEL"} {
		t.Setenv(key, "")
		require.NoError(t, os.Unsetenv(key))
	}
	opts := commandOptionsWithDefaults()
	WithTelemetryFromEnvironment("example", "example.test/host/migrator")(&opts)
	config, cleanup, err := opts.telemetry(&cobra.Command{})
	require.NoError(t, err)
	cleanup()
	assert.Equal(t, "example-mongo-migrator", config.Runtime.Telemetry.ServiceName)
	assert.Equal(t, "example.test/host/migrator", config.Scope)
	assert.Equal(t, "local", config.Runtime.Telemetry.Environment)
	assert.Equal(t, "local", config.Runtime.Telemetry.Version)
	assert.Equal(t, 15*time.Second, config.Runtime.ShutdownTimeout)
	// Change settings after the option is built: resolution must remain lazy.
	t.Setenv("COMPONENT", "renamed")
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("GIT_COMMIT", "revision")
	t.Setenv("GRACEFUL_SERVER_TIMEOUT", "3")
	config, cleanup, err = opts.telemetry(&cobra.Command{})
	require.NoError(t, err)
	cleanup()
	assert.Equal(t, "renamed-mongo-migrator", config.Runtime.Telemetry.ServiceName)
	assert.Equal(t, "production", config.Runtime.Telemetry.Environment)
	assert.Equal(t, "revision", config.Runtime.Telemetry.Version)
	assert.Equal(t, 3*time.Second, config.Runtime.ShutdownTimeout)
	for _, value := range []string{"private-invalid-value", "-1", "9223372036854775807"} {
		t.Setenv("GRACEFUL_SERVER_TIMEOUT", value)
		_, cleanup, err := opts.telemetry(&cobra.Command{})
		require.Error(t, err)
		assert.NotContains(t, err.Error(), value)
		assert.Nil(t, cleanup)
	}
	t.Setenv("GRACEFUL_SERVER_TIMEOUT", "0")
	t.Setenv("COMPONENT", " ")
	_, _, err = opts.telemetry(&cobra.Command{})
	require.ErrorContains(t, err, "component-required")
}

func TestTelemetryResolutionFailureRestoresContextAndSkipsDatabaseSettings(t *testing.T) {
	want := errors.New("host configuration failed")
	for _, resolve := range []func(*cobra.Command) (otelcobra.Config, error){nil, func(cmd *cobra.Command) (otelcobra.Config, error) {
		cmd.SetContext(context.WithValue(cmd.Context(), migrationContextKey{}, "temporary"))
		return otelcobra.Config{}, want
	}} {
		opts := commandOptionsWithDefaults()
		WithTelemetry(resolve)(&opts)
		command := newCommand(commandRunner{
			telemetry: opts.telemetry,
			loadSettings: func() (*Settings, error) {
				t.Fatal("failed telemetry setup must stop before database settings")
				return nil, nil
			},
		})
		command.SetArgs([]string{"up"})
		base := context.Background()
		err := command.ExecuteContext(base)
		if resolve == nil {
			require.EqualError(t, err, "migrator/telemetry-resolver-required")
		} else {
			assert.Same(t, want, err)
		}
		child, _, findErr := command.Find([]string{"up"})
		require.NoError(t, findErr)
		assert.True(t, base == child.Context())
	}
}

type migrationContextKey struct{}
type callbackMigration struct{ run func(context.Context) error }

func (runner callbackMigration) Up(ctx context.Context, _ int) error   { return runner.run(ctx) }
func (runner callbackMigration) Down(ctx context.Context, _ int) error { return runner.run(ctx) }

type telemetryMongoClient struct {
	fakeMongoClient
	monitor    *event.CommandMonitor
	disconnect func(context.Context) error
}

func (client *telemetryMongoClient) Ping(ctx context.Context, _ *readpref.ReadPref) error {
	if client.monitor != nil {
		client.monitor.Started(ctx, &event.CommandStartedEvent{CommandName: "ping", DatabaseName: "example", RequestID: 1, ConnectionID: "localhost:27017"})
		client.monitor.Succeeded(ctx, &event.CommandSucceededEvent{CommandFinishedEvent: event.CommandFinishedEvent{CommandName: "ping", DatabaseName: "example", RequestID: 1, ConnectionID: "localhost:27017"}})
	}
	return client.pingErr
}
func (client *telemetryMongoClient) Disconnect(ctx context.Context) error {
	return client.disconnect(ctx)
}

var migrationExporterID atomic.Uint64

func migrationTelemetryFixture(t *testing.T) (otelcobra.Config, *[]*migrationSpanExporter) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	tracer, meter, logs, propagator := otel.GetTracerProvider(), otel.GetMeterProvider(), global.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(tracer)
		otel.SetMeterProvider(meter)
		global.SetLoggerProvider(logs)
		otel.SetTextMapPropagator(propagator)
	})
	var exporters []*migrationSpanExporter
	name := fmt.Sprintf("migrator-test-%d", migrationExporterID.Add(1))
	autoexport.RegisterSpanExporter(name, func(context.Context) (sdktrace.SpanExporter, error) {
		exporter := &migrationSpanExporter{}
		exporters = append(exporters, exporter)
		return exporter, nil
	})
	t.Setenv("OTEL_TRACES_EXPORTER", name)
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	return otelcobra.Config{Scope: "example.test/migrator", Runtime: observability.RuntimeConfig{
		Telemetry: observability.Config{ServiceName: "example-mongo-migrator"}, Logger: zap.NewNop(),
	}}, &exporters
}

type migrationSpanExporter struct {
	mu     sync.Mutex
	spans  []sdktrace.ReadOnlySpan
	closed atomic.Bool
}

func (exporter *migrationSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	exporter.spans = append(exporter.spans, spans...)
	return nil
}
func (exporter *migrationSpanExporter) Shutdown(context.Context) error {
	exporter.closed.Store(true)
	return nil
}
func (exporter *migrationSpanExporter) snapshot() []sdktrace.ReadOnlySpan {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), exporter.spans...)
}
