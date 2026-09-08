// The observability example runs an HTTP service or a standalone worker with
// shared telemetry lifecycle and no external application dependencies.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelcobra"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const exampleScope = "github.com/ooaklee/ghatd/examples/observability"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := newCommand().ExecuteContext(ctx); err != nil {
		// Automatic CLI output must not echo arguments, credentials, or exporter
		// endpoint details. Deferred runtime cleanup has already completed.
		_, _ = fmt.Fprintln(os.Stderr, "observability example failed")
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	command := &cobra.Command{Use: "observability", SilenceErrors: true, SilenceUsage: true}
	var listen string
	var suppressHTTPNoise bool
	serve := &cobra.Command{
		Use: "serve", Short: "Run the local HTTP reference service", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runServer(command.Context(), listen, suppressHTTPNoise)
		},
	}
	serve.Flags().StringVar(&listen, "listen", "127.0.0.1:8080", "HTTP listen address")
	serve.Flags().BoolVar(&suppressHTTPNoise, "suppress-http-noise", false, "Suppress new health-check traces while retaining HTTP metrics and logs")
	work := &cobra.Command{
		Use: "work", Short: "Run one standalone instrumented work item", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, args []string) error {
			config, classifier, err := exampleRuntimeConfig("example-worker")
			if err != nil {
				return err
			}
			return otelcobra.Run(command, args, otelcobra.Config{
				Name: "work", Scope: exampleScope + "/commands", Runtime: config, Errors: classifier,
			}, func(command *cobra.Command, _ []string) error {
				runtime := observability.RuntimeFromContext(command.Context())
				operations, err := exampleOperations(runtime, classifier)
				if err != nil {
					return err
				}
				dependencyURL, closeDependency, err := startLocalDependency(runtime)
				if err != nil {
					return err
				}
				defer closeDependency()
				service := newService(operations, classifier, dependencyURL)
				defer service.client.CloseIdleConnections()
				traceID, err := service.process(command.Context(), false)
				if err != nil {
					return err
				}
				return json.NewEncoder(command.OutOrStdout()).Encode(workResponse{Status: "ok", TraceID: traceID})
			})
		},
	}
	command.AddCommand(serve, work)
	return command
}

func exampleRuntimeConfig(serviceName string) (observability.RuntimeConfig, *observability.ErrorClassifier, error) {
	classifier, err := observability.NewErrorClassifier(
		observability.ErrorRule{Err: errQuotaExceeded, Code: "EXAMPLE-001", Outcome: observability.OutcomeRejected},
		observability.ErrorRule{Err: errDependencyFailed, Code: "EXAMPLE-002", Outcome: observability.OutcomeError},
	)
	if err != nil {
		return observability.RuntimeConfig{}, nil, err
	}
	logConfig := zap.NewProductionConfig()
	logConfig.DisableCaller, logConfig.DisableStacktrace = true, true
	logger, err := logConfig.Build()
	if err != nil {
		return observability.RuntimeConfig{}, nil, err
	}
	return observability.RuntimeConfig{
		Telemetry: exampleIdentity(serviceName), Logger: logger,
		LogOptions:      []observability.LogOption{observability.WithLogErrorClassifier(classifier)},
		ShutdownTimeout: 5 * time.Second,
	}, classifier, nil
}

// The example supplies defaults only. Intentional standard environment identity
// values remain higher priority; GHATD itself still owns parsing and validation.
func exampleIdentity(defaultService string) observability.Config {
	config := observability.Config{}
	if strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAME")) == "" && !hasResourceIdentity("service.name") {
		config.ServiceName = defaultService
	}
	if !hasResourceIdentity("service.namespace") {
		config.Namespace = "example"
	}
	if !hasResourceIdentity("deployment.environment.name") {
		config.Environment = "local"
	}
	return config
}

func hasResourceIdentity(key string) bool {
	value := ""
	for _, pair := range strings.Split(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"), ",") {
		attributeKey, attributeValue, ok := strings.Cut(pair, "=")
		if ok && strings.TrimSpace(attributeKey) == key {
			value = attributeValue
			if decoded, err := url.PathUnescape(value); err == nil {
				value = decoded
			}
		}
	}
	return strings.TrimSpace(value) != ""
}

func exampleOperations(runtime *observability.Runtime, classifier *observability.ErrorClassifier) (*observability.Operations, error) {
	return observability.NewOperations(observability.OperationConfig{
		Scope: exampleScope + "/services", MetricPrefix: "example.service.operation",
		TracerProvider: runtime.SDK().TracerProvider(), MeterProvider: runtime.SDK().MeterProvider(), Errors: classifier,
	})
}

func runServer(ctx context.Context, listen string, suppressHTTPNoise bool) error {
	var httpOptions []observability.HTTPServerOption
	if suppressHTTPNoise {
		policy, err := observability.NewHTTPTracePolicy([]string{"/healthz"}, nil)
		if err != nil {
			return err
		}
		httpOptions = append(httpOptions, observability.WithHTTPTracePolicy(policy))
	}
	config, classifier, err := exampleRuntimeConfig("example-api")
	if err != nil {
		return err
	}
	runtime, err := observability.StartRuntime(ctx, config)
	if err != nil {
		return err
	}
	defer func() {
		if err := runtime.Shutdown(ctx); err != nil {
			runtime.Logger().Warn("telemetry shutdown failed")
		}
	}()
	operations, err := exampleOperations(runtime, classifier)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	dependencyURL := listenerURL(listener) + "/dependency"
	service := newService(operations, classifier, dependencyURL)
	defer service.client.CloseIdleConnections()
	server := &http.Server{
		Handler: newHandler(runtime, service, httpOptions...), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(runtime.Context()) },
	}
	// Also close accepted connections if Serve exits unexpectedly, before
	// dependency cleanup and telemetry shutdown run.
	defer server.Close()
	completed := make(chan error, 1)
	go func() { completed <- server.Serve(listener) }()
	runtime.Logger().Info("example server ready")
	select {
	case err := <-completed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
		if err := <-completed; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func listenerURL(listener net.Listener) string {
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "127.0.0.1"
		if ip.To4() == nil {
			host = "::1"
		}
	}
	return "http://" + net.JoinHostPort(host, port)
}
