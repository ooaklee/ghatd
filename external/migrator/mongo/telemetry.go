package mongo

import (
	"errors"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelcobra"
	"github.com/spf13/cobra"
)

// telemetryResolver builds an otelcobra config plus a cleanup function for one
// command invocation, returning an error when telemetry cannot be prepared.
type telemetryResolver func(*cobra.Command) (otelcobra.Config, func(), error)

// WithTelemetry gives each up/down invocation an action-scoped runtime and a
// fresh MongoDB monitor. Resolve runs after Cobra validation and pre-hooks;
// help and new never call it. The host owns the supplied logger's lifetime.
// Name is always mongo-migrator.up/down; other config fields remain host-owned.
// Do not combine this option with another runtime owner around the command.
// A nil resolver returns an error when up/down runs. The last telemetry option
// wins. WithMongoCommandMonitor overrides the automatic monitor.
func WithTelemetry(resolve func(*cobra.Command) (otelcobra.Config, error)) CommandOption {
	return func(options *commandOptions) {
		options.telemetry = func(command *cobra.Command) (otelcobra.Config, func(), error) {
			if resolve == nil {
				return otelcobra.Config{}, nil, errors.New("migrator/telemetry-resolver-required")
			}
			config, err := resolve(command)
			return config, nil, err
		}
	}
}

// WithTelemetryFromEnvironment opts into standard host environment settings,
// loaded lazily for each up/down invocation. COMPONENT overrides defaultComponent;
// the service name appends -mongo-migrator. Scope is a static host-owned tracing
// scope (empty uses otelcobra's default). ENVIRONMENT, GIT_COMMIT, LOG_LEVEL and
// GRACEFUL_SERVER_TIMEOUT retain local/local/info/15-second defaults. Standard
// OTEL_* settings are interpreted by the shared observability runtime.
// This option owns and syncs its logger after telemetry shutdown.
func WithTelemetryFromEnvironment(defaultComponent, scope string) CommandOption {
	return func(options *commandOptions) {
		options.telemetry = func(*cobra.Command) (otelcobra.Config, func(), error) {
			return telemetryFromEnvironment(defaultComponent, scope)
		}
	}
}

// telemetryFromEnvironment loads telemetry settings with envconfig, applies
// defaults for environment, commit, timeout and log level, and requires a non-
// empty component. It returns a config whose service name is the component
// suffixed with -mongo-migrator and a cleanup that syncs the created logger.
// Errors are fixed strings so environment values are never echoed.
func telemetryFromEnvironment(defaultComponent, scope string) (otelcobra.Config, func(), error) {
	settings := struct {
		Environment           string `default:"local"`
		Component             string
		GitCommit             string `envconfig:"git_commit" default:"local"`
		GracefulServerTimeout int64  `envconfig:"graceful_server_timeout" default:"15"`
		LogLevel              string `envconfig:"log_level" default:"info"`
	}{Component: defaultComponent}
	if err := envconfig.Process("", &settings); err != nil {
		// envconfig errors can echo supplied values. Keep diagnostics data-free.
		return otelcobra.Config{}, nil, errors.New("migrator/unable-to-load-telemetry-settings")
	}
	if strings.TrimSpace(settings.Component) == "" {
		return otelcobra.Config{}, nil, errors.New("migrator/telemetry-component-required")
	}
	if settings.GracefulServerTimeout < 0 || settings.GracefulServerTimeout > int64((1<<63-1)/time.Second) {
		return otelcobra.Config{}, nil, errors.New("migrator/invalid-telemetry-shutdown-timeout")
	}
	appLogger, err := logger.NewLogger(settings.LogLevel, settings.Environment, settings.Component)
	if err != nil {
		return otelcobra.Config{}, nil, errors.New("migrator/unable-to-create-telemetry-logger")
	}
	return otelcobra.Config{
		Scope: scope,
		Runtime: observability.RuntimeConfig{
			Telemetry: observability.Config{
				ServiceName: settings.Component + "-mongo-migrator",
				Version:     settings.GitCommit, Environment: settings.Environment,
			},
			Logger: appLogger, ShutdownTimeout: time.Duration(settings.GracefulServerTimeout) * time.Second,
		},
	}, func() { _ = appLogger.Sync() }, nil
}

// runDatabaseCommand executes a database action, wrapping it in the otelcobra
// runtime named mongo-migrator.<action> when a telemetry resolver is
// configured; without telemetry it runs the action directly. It restores the
// command's original context and runs telemetry cleanup afterwards.
func (runner commandRunner) runDatabaseCommand(command *cobra.Command, args []string, action string) error {
	execute := func(command *cobra.Command, _ []string) error {
		return runner.run(command, action, func(settings Settings) error {
			return runner.runDatabaseAction(command.Context(), settings, action)
		})
	}
	if runner.telemetry == nil {
		return execute(command, args)
	}
	originalContext := command.Context()
	defer command.SetContext(originalContext)
	config, cleanup, err := runner.telemetry(command)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return err
	}
	config.Name = "mongo-migrator." + action
	return otelcobra.Run(command, args, config, execute)
}
