// Package otelcobra supplies telemetry lifecycle for executable Cobra actions.
// It leaves command validation, flags, help, and pre/post hooks to Cobra.
package otelcobra

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const defaultScope = "github.com/ooaklee/ghatd/external/observability/otelcobra"

// Config supplies a static command name and its invocation's runtime settings.
// Name must be a trusted constant, never command arguments, flags, or user data.
// Errors classifies both the command span and automatic completion log; nil
// selects the default cancellation, deadline, and internal classifications.
type Config struct {
	Name    string
	Scope   string
	Runtime observability.RuntimeConfig
	Errors  *observability.ErrorClassifier
}

// Action is the executable body of a command. It must return errors instead of
// calling os.Exit, log.Fatal, or zap.Fatal, which bypass deferred cleanup.
type Action func(*cobra.Command, []string) error

// Run starts a fresh runtime for one action, supplies a traced command context,
// and restores the original context afterward. The original argument slice and
// returned error are passed through unchanged. Span completion and its static
// log happen before telemetry shutdown, including during a panic.
//
// Exporter shutdown failures produce a static diagnostic on ErrOrStderr and do
// not change the action's result. Panics are classified without formatting their
// payload, then rethrown unchanged. Cobra pre/post hooks are outside this scope.
func Run(command *cobra.Command, args []string, config Config, action Action) (runErr error) {
	if command == nil || action == nil {
		return errors.New("observability: command telemetry requires a command and action")
	}
	if !validName(config.Name) {
		return errors.New("observability: command telemetry name must be a 1–64 byte ASCII identifier")
	}
	if config.Scope == "" {
		config.Scope = defaultScope
	}
	if !validScope(config.Scope) {
		return errors.New("observability: command telemetry scope must contain 1–255 valid text bytes without controls or surrounding whitespace")
	}
	originalContext := command.Context()
	defer command.SetContext(originalContext)
	// Snapshot the option slice before selecting this invocation's classifier.
	config.Runtime.LogOptions = append(append([]observability.LogOption(nil), config.Runtime.LogOptions...),
		observability.WithLogErrorClassifier(config.Errors))
	runtime, err := observability.StartRuntime(originalContext, config.Runtime)
	if err != nil {
		return err
	}
	ctx := runtime.Context()
	defer func() {
		if shutdownErr := runtime.Shutdown(ctx); shutdownErr != nil {
			// A migration or another action may already have committed its work.
			// Neither its result nor automatic output should expose exporter data.
			_, _ = fmt.Fprintln(command.ErrOrStderr(), "OpenTelemetry shutdown failed")
		}
	}()
	ctx, span := runtime.SDK().TracerProvider().Tracer(config.Scope).Start(ctx, config.Name,
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("command", config.Name)))
	// A directly deferred SDK span.End recovers and records an exception with
	// the raw panic payload. Call it indirectly to keep cleanup privacy-safe.
	defer func() { span.End() }()
	commandLogger := observability.WithTraceContext(ctx, runtime.Logger())
	ctx = ghatdlogger.TransitWith(ctx, commandLogger)
	command.SetContext(ctx)
	defer func() {
		// This recover is directly inside the deferred function. Preserve the
		// original value without inspecting or formatting it.
		recovered := recover()
		classification := observability.Classification{
			Code: "panic", Outcome: observability.OutcomePanic, Category: "panic",
		}
		if recovered == nil {
			classification = config.Errors.Classify(runErr)
		}
		complete(span, commandLogger, config.Name, classification, runErr)
		if recovered != nil {
			panic(recovered)
		}
	}()
	return action(command, args)
}

// Instrument wraps an existing executable action. Call it once per command.
// Resolve is called for
// each invocation so settings are fresh; validation/flags/help and existing
// pre/post hooks are unchanged and do not run inside the telemetry lifetime.
// An existing RunE retains Cobra's precedence over Run. Parent commands with no
// executable action are rejected instead of being made runnable accidentally.
func Instrument(command *cobra.Command, resolve func(*cobra.Command) (Config, error)) error {
	if command == nil || resolve == nil {
		return errors.New("observability: command instrumentation requires a command and configuration resolver")
	}
	originalRunE, originalRun := command.RunE, command.Run
	if originalRunE == nil && originalRun == nil {
		return errors.New("observability: command instrumentation requires an existing executable action")
	}
	command.RunE = func(command *cobra.Command, args []string) error {
		originalContext := command.Context()
		defer command.SetContext(originalContext)
		config, err := resolve(command)
		if err != nil {
			return err
		}
		return Run(command, args, config, func(command *cobra.Command, args []string) error {
			if originalRunE != nil {
				return originalRunE(command, args)
			}
			originalRun(command, args)
			return nil
		})
	}
	return nil
}

// commandLogError keeps local automatic logs as safe as OTLP logs, while
// exposing the original identity solely to the configured errors.Is registry.
type commandLogError struct{ cause error }

func (commandLogError) Error() string     { return "command failed" }
func (err commandLogError) Unwrap() error { return err.cause }

func complete(span trace.Span, logger *zap.Logger, name string, classification observability.Classification, err error) {
	span.SetAttributes(attribute.String("outcome", string(classification.Outcome)))
	fields := []zap.Field{zap.String("command", name), zap.String("outcome", string(classification.Outcome))}
	if classification.Code != "" {
		span.SetAttributes(attribute.String("error.type", classification.Code), attribute.String("error.category", classification.Category))
	}
	if classification.Outcome == observability.OutcomePanic {
		fields = append(fields, zap.String("error.type", "panic"))
	} else if err != nil {
		fields = append(fields, zap.Error(commandLogError{cause: err}))
	}
	switch classification.Outcome {
	case observability.OutcomePanic:
		span.SetStatus(codes.Error, "command panicked")
		logger.Error("command completed", fields...)
	case observability.OutcomeError:
		span.SetStatus(codes.Error, "command failed")
		logger.Error("command completed", fields...)
	case observability.OutcomeTimeout:
		span.SetStatus(codes.Error, "command timed out")
		logger.Error("command completed", fields...)
	case observability.OutcomeRejected, observability.OutcomeCancelled:
		logger.Warn("command completed", fields...)
	default:
		logger.Info("command completed", fields...)
	}
}

func validName(name string) bool {
	if len(name) == 0 || len(name) > 64 || !asciiLetter(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		char := name[index]
		if !asciiLetter(char) && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func asciiLetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

func validScope(scope string) bool {
	if len(scope) == 0 || len(scope) > 255 || !utf8.ValidString(scope) || strings.TrimSpace(scope) != scope {
		return false
	}
	for _, char := range scope {
		if unicode.IsControl(char) {
			return false
		}
	}
	return true
}
