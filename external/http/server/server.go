// Package server provides helpers for running HTTP servers with graceful shutdown.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// StartServerWithRequest contains the configuration and optional hooks used to start an HTTP server.
type StartServerWithRequest struct {
	// Host is the interface or hostname used when Addr is not provided.
	Host string
	// Port is the TCP port used when Addr is not provided.
	Port string
	// Addr is the full listen address and takes precedence over Host and Port.
	Addr string
	// Handler processes incoming HTTP requests.
	Handler http.Handler
	// GracefulShutdownTimeout bounds how long shutdown may wait for in-flight requests.
	GracefulShutdownTimeout time.Duration
	// ReadHeaderTimeout limits how long the server waits to read request headers.
	ReadHeaderTimeout time.Duration
	// Log receives lifecycle messages from the server helper.
	Log func(level, message string)
	// Signals supplies an existing signal channel for shutdown notifications.
	Signals <-chan os.Signal
	// NotifySignals configures which OS signals trigger shutdown when Signals is nil.
	NotifySignals []os.Signal
	// ListenAndServe starts the server and may be overridden for tests or custom listeners.
	ListenAndServe func(*http.Server) error
	// Shutdown gracefully stops the server and may be overridden for tests or custom shutdown logic.
	Shutdown func(*http.Server, context.Context) error
	// Context cancels server execution when done.
	Context context.Context
}

// ResolveAddr returns addr when present, otherwise it combines host and port.
func ResolveAddr(host, port, addr string) string {
	if addr != "" {
		return addr
	}
	return fmt.Sprintf("%s:%s", host, port)
}

// StartServerWith starts an HTTP server and blocks until startup fails or shutdown is requested.
func StartServerWith(req *StartServerWithRequest) error {
	if req == nil {
		return ErrNilRequest
	}

	if req.Handler == nil {
		return ErrMissingHandler
	}

	resolvedAddr := ResolveAddr(req.Host, req.Port, req.Addr)
	if resolvedAddr == ":" || resolvedAddr == "" || strings.HasSuffix(resolvedAddr, ":") {
		return ErrMissingAddressData
	}

	if req.GracefulShutdownTimeout <= 0 {
		return ErrNonPositiveTimeout
	}

	ctx := req.Context
	if ctx == nil {
		ctx = context.Background()
	}
	logger := logger.AcquireOperationFrom(ctx, "external/http/server", "start-server")
	logger.Debug("server-start-requested", zap.String("addr", resolvedAddr))

	srv := &http.Server{
		Addr:              resolvedAddr,
		Handler:           req.Handler,
		ReadHeaderTimeout: req.ReadHeaderTimeout,
	}

	signals := req.Signals
	if signals == nil {
		notifySignals := req.NotifySignals
		if notifySignals == nil {
			notifySignals = []os.Signal{os.Interrupt, syscall.SIGINT, syscall.SIGTERM}
		}
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, notifySignals...)
		defer signal.Stop(sigCh)
		signals = sigCh
	}

	listenAndServe := req.ListenAndServe
	if listenAndServe == nil {
		listenAndServe = func(s *http.Server) error {
			err := s.ListenAndServe()
			if err != nil && errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	}

	shutdown := req.Shutdown
	if shutdown == nil {
		shutdown = func(s *http.Server, ctx context.Context) error {
			return s.Shutdown(ctx)
		}
	}

	emitServerEvent := req.Log
	if emitServerEvent == nil {
		emitServerEvent = func(level, message string) {}
	}

	serverErrCh := make(chan error, 1)
	go func() {
		emitServerEvent("info", fmt.Sprintf("server listening on %s", resolvedAddr))
		logger.Info("server-listening", zap.String("addr", resolvedAddr))
		if err := listenAndServe(srv); err != nil {
			logger.Error("server-listen-failed", zap.String("addr", resolvedAddr), zap.Error(err))
			serverErrCh <- err
			return
		}
		serverErrCh <- nil
	}()

	select {
	case err := <-serverErrCh:
		if err != nil {
			return fmt.Errorf("%w: %v", ErrStartupFailure, err)
		}
		return nil
	case sig := <-signals:
		emitServerEvent("info", fmt.Sprintf("received signal %s, shutting down gracefully", sig))
		logger.Info("server-shutdown-signal-received", zap.String("signal", sig.String()))
	case <-ctx.Done():
		emitServerEvent("info", "context done, shutting down gracefully")
		logger.Info("server-context-done", zap.Error(ctx.Err()))
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), req.GracefulShutdownTimeout)
	defer cancel()

	if err := shutdown(srv, shutdownCtx); err != nil {
		logger.Error("server-shutdown-failed", zap.Error(err))
		return fmt.Errorf("%w: %v", ErrShutdownFailure, err)
	}

	logger.Info("server-shutdown-completed")
	return nil
}
