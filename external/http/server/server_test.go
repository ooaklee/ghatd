package server

import (
	"context"
	"errors"
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
