package ephemeral

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/go-redis/redis/v7"
)

func TestNewRedisRuntime(t *testing.T) {
	tests := []struct {
		name    string
		req     *NewRedisRuntimeRequest
		wantErr string
	}{
		{
			name: "SUCCESS - builds runtime without ping",
			req: &NewRedisRuntimeRequest{
				Options: &redis.Options{
					Addr: "127.0.0.1:6379",
				},
				MaxUnauthedRequestAllowance: 10,
				Component:                   "Example API",
				Environment:                 "local",
				SkipPing:                    true,
			},
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "ephemeral/redis-runtime-nil-request",
		},
		{
			name:    "FAILURE - missing options",
			req:     &NewRedisRuntimeRequest{},
			wantErr: "ephemeral/redis-runtime-missing-options",
		},
		{
			name: "FAILURE - ping failure",
			req: &NewRedisRuntimeRequest{
				Options: &redis.Options{
					Addr:        "127.0.0.1:1",
					DialTimeout: time.Millisecond,
				},
			},
			wantErr: "ephemeral/redis-runtime-ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewRedisRuntime(context.Background(), tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewRedisRuntime() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewRedisRuntime() error = %v", err)
			}
			if got == nil || got.Client == nil || got.Store == nil {
				t.Fatalf("NewRedisRuntime() = %+v, want client and store", got)
			}
			if err := got.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}

func TestRedisRuntimeClose(t *testing.T) {
	var runtime *RedisRuntime
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("nil runtime Close() error = %v", err)
	}

	runtime = &RedisRuntime{}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("empty runtime Close() error = %v", err)
	}
}
