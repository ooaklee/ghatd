package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestStartServerWith(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	tests := []struct {
		name       string
		reqFactory func() *StartServerWithRequest
		wantErr    error
	}{
		{
			name: "good: resolved addr from Host and Port, shutdown via signal",
			reqFactory: func() *StartServerWithRequest {
				sigCh := make(chan os.Signal, 1)
				go func() { sigCh <- syscall.SIGTERM }()
				listenCh := make(chan struct{})
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 100 * time.Millisecond,
					Signals:                 sigCh,
					ListenAndServe: func(s *http.Server) error {
						<-listenCh
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						close(listenCh)
						return nil
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "good: resolved addr from explicit Addr, shutdown via signal",
			reqFactory: func() *StartServerWithRequest {
				sigCh := make(chan os.Signal, 1)
				go func() { sigCh <- syscall.SIGINT }()
				listenCh := make(chan struct{})
				return &StartServerWithRequest{
					Addr:                    "127.0.0.1:3000",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 100 * time.Millisecond,
					Signals:                 sigCh,
					ListenAndServe: func(s *http.Server) error {
						<-listenCh
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						close(listenCh)
						return nil
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "good: shutdown via context cancellation",
			reqFactory: func() *StartServerWithRequest {
				ctx, cancel := context.WithCancel(context.Background())
				listenStarted := make(chan struct{})
				listenCh := make(chan struct{})
				go func() {
					<-listenStarted
					cancel()
				}()
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "9090",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 100 * time.Millisecond,
					Context:                 ctx,
					ListenAndServe: func(s *http.Server) error {
						close(listenStarted)
						<-listenCh
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						close(listenCh)
						return nil
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "good: custom Log and ReadHeaderTimeout, shutdown via signal",
			reqFactory: func() *StartServerWithRequest {
				sigCh := make(chan os.Signal, 1)
				go func() { sigCh <- syscall.SIGTERM }()
				listenCh := make(chan struct{})
				logged := make(chan string, 2)
				return &StartServerWithRequest{
					Host:                    "0.0.0.0",
					Port:                    "8443",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 50 * time.Millisecond,
					ReadHeaderTimeout:       5 * time.Second,
					Log: func(level, message string) {
						logged <- level
					},
					Signals: sigCh,
					ListenAndServe: func(s *http.Server) error {
						<-listenCh
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						close(listenCh)
						return nil
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "good: ListenAndServe returns nil before shutdown signal",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "5050",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 100 * time.Millisecond,
					ListenAndServe: func(s *http.Server) error {
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						return errors.New("shutdown should not be called")
					},
				}
			},
			wantErr: nil,
		},
		{
			name: "bad: nil request",
			reqFactory: func() *StartServerWithRequest {
				return nil
			},
			wantErr: ErrNilRequest,
		},
		{
			name: "bad: missing handler",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					GracefulShutdownTimeout: time.Second,
				}
			},
			wantErr: ErrMissingHandler,
		},
		{
			name: "bad: missing address data - all empty",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: time.Second,
				}
			},
			wantErr: ErrMissingAddressData,
		},
		{
			name: "bad: missing address data - only colon separator",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "",
					Port:                    "",
					Addr:                    ":",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: time.Second,
				}
			},
			wantErr: ErrMissingAddressData,
		},
		{
			name: "bad: missing address data - host without port",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: time.Second,
				}
			},
			wantErr: ErrMissingAddressData,
		},
		{
			name: "bad: non-positive timeout - zero",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: 0,
				}
			},
			wantErr: ErrNonPositiveTimeout,
		},
		{
			name: "bad: non-positive timeout - negative",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: -time.Second,
				}
			},
			wantErr: ErrNonPositiveTimeout,
		},
		{
			name: "bad: ListenAndServe returns startup error",
			reqFactory: func() *StartServerWithRequest {
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: time.Second,
					ListenAndServe: func(s *http.Server) error {
						return errors.New("port already in use")
					},
				}
			},
			wantErr: ErrStartupFailure,
		},
		{
			name: "bad: Shutdown returns error",
			reqFactory: func() *StartServerWithRequest {
				sigCh := make(chan os.Signal, 1)
				go func() { sigCh <- syscall.SIGTERM }()
				listenCh := make(chan struct{})
				return &StartServerWithRequest{
					Host:                    "localhost",
					Port:                    "8080",
					Handler:                 dummyHandler,
					GracefulShutdownTimeout: time.Second,
					Signals:                 sigCh,
					ListenAndServe: func(s *http.Server) error {
						<-listenCh
						return nil
					},
					Shutdown: func(s *http.Server, ctx context.Context) error {
						close(listenCh)
						return errors.New("database drain failed")
					},
				}
			},
			wantErr: ErrShutdownFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.reqFactory()
			err := StartServerWith(req)

			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("expected no error, got: %v", err)
				}
			} else if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.wantErr)
			} else if !errors.Is(err, tt.wantErr) && !strings.Contains(err.Error(), tt.wantErr.Error()) {
				t.Errorf("expected error containing %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestResolveAddr(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		addr string
		want string
	}{
		{
			name: "explicit addr takes precedence",
			host: "localhost",
			port: "8080",
			addr: "0.0.0.0:9090",
			want: "0.0.0.0:9090",
		},
		{
			name: "host and port combined",
			host: "localhost",
			port: "8080",
			addr: "",
			want: "localhost:8080",
		},
		{
			name: "empty host with port",
			host: "",
			port: "8080",
			addr: "",
			want: ":8080",
		},
		{
			name: "all empty",
			host: "",
			port: "",
			addr: "",
			want: ":",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveAddr(tt.host, tt.port, tt.addr)
			if got != tt.want {
				t.Errorf("ResolveAddr(%q, %q, %q) = %q, want %q",
					tt.host, tt.port, tt.addr, got, tt.want)
			}
		})
	}
}

func TestStartServerWith_ConfiguredServerFields(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("GOOD - ReadHeaderTimeout set on server", func(t *testing.T) {
		var capturedSrv *http.Server
		doneCh := make(chan struct{})
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "8080",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: time.Second,
			ReadHeaderTimeout:       10 * time.Second,
			ListenAndServe: func(s *http.Server) error {
				capturedSrv = s
				close(doneCh)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		<-doneCh
		if capturedSrv.ReadHeaderTimeout != 10*time.Second {
			t.Fatalf("expected ReadHeaderTimeout %v, got %v", 10*time.Second, capturedSrv.ReadHeaderTimeout)
		}
		if capturedSrv.Addr != "localhost:8080" {
			t.Fatalf("expected Addr %q, got %q", "localhost:8080", capturedSrv.Addr)
		}
		if capturedSrv.Handler == nil {
			t.Fatalf("expected Handler to be set")
		}
	})
}

func TestStartServerWith_LogMessages(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("GOOD - server listening log message", func(t *testing.T) {
		messages := make(chan string, 1)
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "5050",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: 100 * time.Millisecond,
			Log: func(level, message string) {
				messages <- fmt.Sprintf("%s: %s", level, message)
			},
			ListenAndServe: func(s *http.Server) error {
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		msgs := readLogMessages(t, messages, 1)
		if !hasLogMessage(msgs, "server listening on") {
			t.Fatalf("expected 'server listening on' message, got %v", msgs)
		}
		if !strings.HasPrefix(msgs[0], "info: ") {
			t.Fatalf("expected log level 'info', got %q", msgs[0])
		}
	})

	t.Run("GOOD - shutdown signal log message", func(t *testing.T) {
		sigCh := make(chan os.Signal, 1)
		messages := make(chan string, 2)
		listenStarted := make(chan struct{})
		listenCh := make(chan struct{})
		go func() {
			<-listenStarted
			sigCh <- syscall.SIGTERM
		}()
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "6060",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: 100 * time.Millisecond,
			Signals:                 sigCh,
			Log: func(level, message string) {
				messages <- fmt.Sprintf("%s: %s", level, message)
			},
			ListenAndServe: func(s *http.Server) error {
				close(listenStarted)
				<-listenCh
				return nil
			},
			Shutdown: func(s *http.Server, ctx context.Context) error {
				close(listenCh)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		msgs := readLogMessages(t, messages, 2)
		if !hasLogMessage(msgs, "server listening on") {
			t.Fatalf("expected listening log message, got %v", msgs)
		}
		if !hasLogMessage(msgs, "shutting down gracefully") {
			t.Fatalf("expected shutdown signal log message, got %v", msgs)
		}
	})

	t.Run("GOOD - context cancellation log message", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		listenStarted := make(chan struct{})
		listenCh := make(chan struct{})
		messages := make(chan string, 2)
		go func() {
			<-listenStarted
			cancel()
		}()
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "7070",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: 100 * time.Millisecond,
			Context:                 ctx,
			Log: func(level, message string) {
				messages <- fmt.Sprintf("%s: %s", level, message)
			},
			ListenAndServe: func(s *http.Server) error {
				close(listenStarted)
				<-listenCh
				return nil
			},
			Shutdown: func(s *http.Server, ctx context.Context) error {
				close(listenCh)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		msgs := readLogMessages(t, messages, 2)
		if !hasLogMessage(msgs, "server listening on") {
			t.Fatalf("expected listening log message, got %v", msgs)
		}
		if !hasLogMessage(msgs, "context done") {
			t.Fatalf("expected 'context done' log message, got %v", msgs)
		}
	})
}

func TestStartServerWith_ErrorWrapping(t *testing.T) {
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	t.Run("BAD - startup error wraps ErrStartupFailure with original error", func(t *testing.T) {
		origErr := errors.New("port already in use")
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "8080",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: time.Second,
			ListenAndServe: func(s *http.Server) error {
				return origErr
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrStartupFailure) {
			t.Fatalf("expected error to wrap ErrStartupFailure, got %v", err)
		}
		if !strings.Contains(err.Error(), origErr.Error()) {
			t.Fatalf("expected wrapped error to contain %q, got %v", origErr, err)
		}
	})

	t.Run("BAD - shutdown error wraps ErrShutdownFailure with original error", func(t *testing.T) {
		sigCh := make(chan os.Signal, 1)
		go func() { sigCh <- syscall.SIGTERM }()
		listenCh := make(chan struct{})
		shutdownErr := errors.New("database drain failed")
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "8080",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: time.Second,
			Signals:                 sigCh,
			ListenAndServe: func(s *http.Server) error {
				<-listenCh
				return nil
			},
			Shutdown: func(s *http.Server, ctx context.Context) error {
				close(listenCh)
				return shutdownErr
			},
		})
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrShutdownFailure) {
			t.Fatalf("expected error to wrap ErrShutdownFailure, got %v", err)
		}
		if !strings.Contains(err.Error(), shutdownErr.Error()) {
			t.Fatalf("expected wrapped error to contain %q, got %v", shutdownErr, err)
		}
	})

	t.Run("GOOD - shutdown deadline context is created with correct timeout", func(t *testing.T) {
		sigCh := make(chan os.Signal, 1)
		listenCh := make(chan struct{})
		var capturedCtx context.Context
		var shutdownObservedAt time.Time
		go func() { sigCh <- syscall.SIGTERM }()
		err := StartServerWith(&StartServerWithRequest{
			Host:                    "localhost",
			Port:                    "8080",
			Handler:                 dummyHandler,
			GracefulShutdownTimeout: 500 * time.Millisecond,
			Signals:                 sigCh,
			ListenAndServe: func(s *http.Server) error {
				<-listenCh
				return nil
			},
			Shutdown: func(s *http.Server, ctx context.Context) error {
				shutdownObservedAt = time.Now()
				capturedCtx = ctx
				close(listenCh)
				return nil
			},
		})
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if capturedCtx == nil {
			t.Fatal("expected shutdown context to be set")
		}
		deadline, ok := capturedCtx.Deadline()
		if !ok {
			t.Fatal("expected shutdown context to have deadline")
		}
		gotTimeout := deadline.Sub(shutdownObservedAt)
		if gotTimeout <= 0 || gotTimeout > 500*time.Millisecond {
			t.Fatalf("expected deadline within 500ms from shutdown, got %v", gotTimeout)
		}
	})
}

func readLogMessages(t *testing.T, messages <-chan string, count int) []string {
	t.Helper()

	got := make([]string, 0, count)
	timeout := time.After(500 * time.Millisecond)
	for len(got) < count {
		select {
		case msg := <-messages:
			got = append(got, msg)
		case <-timeout:
			t.Fatalf("expected %d log messages, got %d: %v", count, len(got), got)
		}
	}

	return got
}

func hasLogMessage(messages []string, contains string) bool {
	for _, msg := range messages {
		if strings.Contains(msg, contains) {
			return true
		}
	}

	return false
}
