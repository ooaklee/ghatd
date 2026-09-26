package otelcobra

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	otellog "go.opentelemetry.io/otel/log"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRunPreservesActionResultAndExportsSafeClassifiedCompletion(t *testing.T) {
	const private = "synthetic-private-command-value"
	rejected := errors.New(private + "-rejected")
	classifier, err := observability.NewErrorClassifier(observability.ErrorRule{
		Err: rejected, Code: "APP-001", Outcome: observability.OutcomeRejected,
	})
	require.NoError(t, err)
	for _, test := range []struct {
		name    string
		err     error
		outcome string
		code    string
		status  codes.Code
	}{
		{"success", nil, "success", "", codes.Unset},
		{"rejected", fmt.Errorf(private+": %w", rejected), "rejected", "APP-001", codes.Unset},
		{"cancelled", fmt.Errorf(private+": %w", context.Canceled), "cancelled", "cancelled", codes.Unset},
		{"timeout", fmt.Errorf(private+": %w", context.DeadlineExceeded), "timeout", "timeout", codes.Error},
		{"unknown", errors.New(private), "error", "internal", codes.Error},
		{"unprintable", &unprintableCommandError{}, "error", "internal", codes.Error},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, spans, logs, local := commandFixture(t)
			config.Errors = classifier
			parent := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID: trace.TraceID{1, 2, 3}, SpanID: trace.SpanID{4, 5, 6}, TraceFlags: trace.FlagsSampled,
			})
			type contextKey struct{}
			original := context.WithValue(trace.ContextWithSpanContext(context.Background(), parent), contextKey{}, "preserved")
			command := &cobra.Command{Use: private}
			command.SetContext(original)
			arguments := []string{private}
			var commandSpanContext trace.SpanContext
			var runtime *observability.Runtime
			gotErr := Run(command, arguments, config, func(gotCommand *cobra.Command, gotArgs []string) error {
				assert.Same(t, command, gotCommand)
				assert.Same(t, &arguments[0], &gotArgs[0])
				assert.Equal(t, "preserved", gotCommand.Context().Value(contextKey{}))
				runtime = observability.RuntimeFromContext(gotCommand.Context())
				require.NotNil(t, runtime)
				require.NotNil(t, runtime.SDK())
				commandSpanContext = trace.SpanContextFromContext(gotCommand.Context())
				assert.NotEqual(t, parent.SpanID(), commandSpanContext.SpanID())
				assert.Equal(t, parent.TraceID(), commandSpanContext.TraceID())
				// Completion keeps its captured context even if application code
				// changes the command's current context while it runs.
				gotCommand.SetContext(context.Background())
				return test.err
			})
			assert.True(t, gotErr == test.err, "the exact action error must be returned")
			assert.True(t, original == command.Context())
			assert.Same(t, runtime.SDK().TracerProvider(), otel.GetTracerProvider(), "shutdown must not reset global providers")
			ended := spans.Spans()
			require.Len(t, ended, 1, "span must end before SDK shutdown flushes")
			span := ended[0]
			assert.Equal(t, config.Name, span.Name())
			assert.Equal(t, config.Scope, span.InstrumentationScope().Name)
			assert.Equal(t, parent.SpanID(), span.Parent().SpanID())
			assert.Equal(t, test.status, span.Status().Code)
			attributes := attribute.NewSet(span.Attributes()...)
			assertSpanAttribute(t, attributes, "outcome", test.outcome)
			if test.code != "" {
				assertSpanAttribute(t, attributes, "error.type", test.code)
			}
			records := logs.Records()
			require.Len(t, records, 1, "completion log must exist before logger shutdown")
			assert.Equal(t, commandSpanContext.TraceID(), records[0].TraceID())
			assert.Equal(t, commandSpanContext.SpanID(), records[0].SpanID())
			assert.Equal(t, "command completed", records[0].Body().AsString())
			logAttributes := commandLogAttributes(records[0])
			assert.Equal(t, test.outcome, logAttributes["outcome"].AsString())
			if test.code != "" {
				assert.Equal(t, test.code, logAttributes["error.type"].AsString())
				assert.Equal(t, test.outcome, logAttributes["error.category"].AsString())
			}
			require.Len(t, local.All(), 1)
			assert.NotContains(t, fmt.Sprint(local.All()[0].ContextMap()), private)
			assert.NotContains(t, local.All()[0].Message, private)
			if test.err != nil {
				assert.Equal(t, "command failed", local.All()[0].ContextMap()["error"])
			}
			assertCommandTelemetryExcludes(t, span, records[0], private, "unprintableCommandError")
		})
	}
}

func TestRunPanicFlushesCompletionAndRethrowsOriginalValue(t *testing.T) {
	for _, value := range []any{errors.New("synthetic-private-panic"), "synthetic-private-panic", map[string]string{"private": "synthetic-private-panic"}} {
		config, spans, logs, local := commandFixture(t)
		command := &cobra.Command{}
		original := context.Background()
		command.SetContext(original)
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			_ = Run(command, nil, config, func(*cobra.Command, []string) error { panic(value) })
		}()
		assert.Equal(t, value, recovered)
		if err, ok := value.(error); ok {
			assert.True(t, recovered == err)
		}
		assert.True(t, original == command.Context())
		require.Len(t, spans.Spans(), 1)
		require.Len(t, logs.Records(), 1)
		span := spans.Spans()[0]
		assert.Equal(t, codes.Error, span.Status().Code)
		assertSpanAttribute(t, attribute.NewSet(span.Attributes()...), "outcome", "panic")
		assertSpanAttribute(t, attribute.NewSet(span.Attributes()...), "error.type", "panic")
		assert.Equal(t, "panic", commandLogAttributes(logs.Records()[0])["error.type"].AsString())
		assertCommandTelemetryExcludes(t, span, logs.Records()[0], "synthetic-private-panic")
		require.Len(t, local.All(), 1)
		assert.NotContains(t, fmt.Sprint(local.All()[0].ContextMap()), "synthetic-private-panic")
	}
}

func TestRunFlushFailureDoesNotChangeCommittedActionResult(t *testing.T) {
	for _, actionErr := range []error{nil, errors.New("synthetic-private-action-error")} {
		config, spans, logs, _ := commandFixture(t)
		spans.shutdownErr = errors.New("synthetic-private-exporter-endpoint")
		command := &cobra.Command{}
		var stderr bytes.Buffer
		command.SetErr(&stderr)
		gotErr := Run(command, nil, config, func(*cobra.Command, []string) error { return actionErr })
		assert.True(t, gotErr == actionErr)
		assert.Equal(t, "OpenTelemetry shutdown failed\n", stderr.String())
		require.Len(t, spans.Spans(), 1)
		require.Len(t, logs.Records(), 1)
		assertCommandTelemetryExcludes(t, spans.Spans()[0], logs.Records()[0], "synthetic-private")
	}
}

func TestInstrumentPreservesCobraHooksValidationAndRunEPrecedence(t *testing.T) {
	config, spans, logs, _ := commandFixture(t)
	var order []string
	original := context.Background()
	command := &cobra.Command{
		Use: "process", Args: cobra.ExactArgs(1), SilenceErrors: true, SilenceUsage: true,
		PreRunE: func(command *cobra.Command, args []string) error {
			order = append(order, "pre")
			assert.Nil(t, observability.RuntimeFromContext(command.Context()))
			return nil
		},
		Run: func(*cobra.Command, []string) { t.Error("Run must not override existing RunE") },
		RunE: func(command *cobra.Command, args []string) error {
			order = append(order, "action")
			assert.Equal(t, []string{"synthetic-private-argument"}, args)
			assert.NotNil(t, observability.RuntimeFromContext(command.Context()))
			ghatdlogger.AcquireFrom(command.Context()).Info("action-progress")
			return nil
		},
		PostRunE: func(command *cobra.Command, args []string) error {
			order = append(order, "post")
			assert.Nil(t, observability.RuntimeFromContext(command.Context()))
			require.Len(t, spans.Spans(), 1, "action telemetry must be flushed before the outside post hook")
			return nil
		},
	}
	command.SetContext(original)
	require.NoError(t, Instrument(command, func(*cobra.Command) (Config, error) {
		order = append(order, "resolve")
		return config, nil
	}))
	command.SetArgs(nil)
	require.Error(t, command.Execute())
	assert.Empty(t, order, "invalid Cobra arguments do not start a runtime")
	command.SetArgs([]string{"synthetic-private-argument"})
	require.NoError(t, command.Execute())
	assert.Equal(t, []string{"pre", "resolve", "action", "post"}, order)
	assert.True(t, original == command.Context())
	assert.Len(t, logs.Records(), 2)
}

func TestInstrumentResolvesFreshRuntimeAndWrapsRunOnly(t *testing.T) {
	config, spans, _, _ := commandFixture(t)
	var runtimes []*observability.Runtime
	command := &cobra.Command{Run: func(command *cobra.Command, _ []string) {
		runtimes = append(runtimes, observability.RuntimeFromContext(command.Context()))
	}}
	assert.Nil(t, command.Context())
	var resolutions int
	require.NoError(t, Instrument(command, func(*cobra.Command) (Config, error) {
		resolutions++
		return config, nil
	}))
	for range 2 {
		require.NoError(t, command.RunE(command, nil))
		assert.Nil(t, command.Context())
	}
	assert.Equal(t, 2, resolutions)
	require.Len(t, runtimes, 2)
	assert.NotSame(t, runtimes[0], runtimes[1])
	assert.NotSame(t, runtimes[0].SDK(), runtimes[1].SDK())
	assert.Len(t, spans.Spans(), 2)
}

func TestRunAndInstrumentRejectInvalidSetupWithoutExecutingAction(t *testing.T) {
	config, _, _, _ := commandFixture(t)
	called := false
	action := func(*cobra.Command, []string) error { called = true; return nil }
	for _, badName := range []string{"", "synthetic-private-name\n", "unicode-ſ", strings.Repeat("n", 65), "1name"} {
		invalid := config
		invalid.Name = badName
		err := Run(&cobra.Command{}, nil, invalid, action)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "synthetic-private")
	}
	invalid := config
	invalid.Scope = "synthetic-private-scope\n"
	require.Error(t, Run(&cobra.Command{}, nil, invalid, action))
	require.Error(t, Run(nil, nil, config, action))
	require.Error(t, Run(&cobra.Command{}, nil, config, nil))
	resolve := func(*cobra.Command) (Config, error) { return config, nil }
	require.Error(t, Instrument(nil, resolve))
	require.Error(t, Instrument(&cobra.Command{}, resolve))
	require.Error(t, Instrument(&cobra.Command{RunE: action}, nil))
	assert.False(t, called)
	wantErr := errors.New("private resolver error")
	command := &cobra.Command{RunE: action}
	original := context.Background()
	command.SetContext(original)
	require.NoError(t, Instrument(command, func(command *cobra.Command) (Config, error) {
		command.SetContext(context.WithValue(command.Context(), struct{}{}, "temporary"))
		return Config{}, wantErr
	}))
	assert.Same(t, wantErr, command.RunE(command, nil))
	assert.True(t, original == command.Context())
	assert.False(t, called)
}

type unprintableCommandError struct{}

func (*unprintableCommandError) Error() string { panic("original error message must not be evaluated") }

var fixtureID atomic.Uint64

func commandFixture(t *testing.T) (Config, *commandSpanExporter, *commandLogExporter, *observer.ObservedLogs) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
	oldTraces, oldMetrics := otel.GetTracerProvider(), otel.GetMeterProvider()
	oldLogs, oldPropagation := otellogglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(oldTraces)
		otel.SetMeterProvider(oldMetrics)
		otellogglobal.SetLoggerProvider(oldLogs)
		otel.SetTextMapPropagator(oldPropagation)
	})
	spans, logs := &commandSpanExporter{}, &commandLogExporter{}
	name := fmt.Sprintf("otelcobra-test-%d", fixtureID.Add(1))
	autoexport.RegisterSpanExporter(name, func(context.Context) (sdktrace.SpanExporter, error) { return spans, nil })
	autoexport.RegisterLogExporter(name, func(context.Context) (sdklog.Exporter, error) { return logs, nil })
	t.Setenv("OTEL_TRACES_EXPORTER", name)
	t.Setenv("OTEL_LOGS_EXPORTER", name)
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	core, local := observer.New(zap.InfoLevel)
	return Config{
		Name: "process-orders", Scope: "example.test/service/commands",
		Runtime: observability.RuntimeConfig{
			Telemetry: observability.Config{ServiceName: "command-contract"},
			Logger:    zap.New(core), ShutdownTimeout: time.Second,
		},
	}, spans, logs, local
}

type commandSpanExporter struct {
	mu          sync.Mutex
	spans       []sdktrace.ReadOnlySpan
	shutdownErr error
}

func (exporter *commandSpanExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	exporter.spans = append(exporter.spans, spans...)
	return nil
}

func (exporter *commandSpanExporter) Shutdown(context.Context) error { return exporter.shutdownErr }

func (exporter *commandSpanExporter) Spans() []sdktrace.ReadOnlySpan {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return append([]sdktrace.ReadOnlySpan(nil), exporter.spans...)
}

type commandLogExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (exporter *commandLogExporter) Export(_ context.Context, records []sdklog.Record) error {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	for _, record := range records {
		exporter.records = append(exporter.records, record.Clone())
	}
	return nil
}

func (*commandLogExporter) Shutdown(context.Context) error   { return nil }
func (*commandLogExporter) ForceFlush(context.Context) error { return nil }

func (exporter *commandLogExporter) Records() []sdklog.Record {
	exporter.mu.Lock()
	defer exporter.mu.Unlock()
	return append([]sdklog.Record(nil), exporter.records...)
}

func commandLogAttributes(record sdklog.Record) map[string]otellog.Value {
	attributes := map[string]otellog.Value{}
	record.WalkAttributes(func(attribute otellog.KeyValue) bool {
		attributes[attribute.Key] = attribute.Value
		return true
	})
	return attributes
}

func assertSpanAttribute(t *testing.T, attributes attribute.Set, key attribute.Key, want string) {
	t.Helper()
	value, ok := attributes.Value(key)
	require.True(t, ok)
	assert.Equal(t, want, value.AsString())
}

func assertCommandTelemetryExcludes(t *testing.T, span sdktrace.ReadOnlySpan, record sdklog.Record, private ...string) {
	t.Helper()
	text := fmt.Sprint(span.Name(), span.Attributes(), span.Status(), span.Events(), record.Body(), commandLogAttributes(record))
	for _, value := range private {
		assert.NotContains(t, text, value)
	}
}
