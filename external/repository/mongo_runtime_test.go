package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
)

func TestNewMongoRuntime(t *testing.T) {
	tests := []struct {
		name    string
		req     *NewMongoRuntimeRequest
		wantErr string
	}{
		{
			name: "SUCCESS - builds runtime without warmup",
			req: &NewMongoRuntimeRequest{
				URIConfig: repositoryhelpers.MongoURIConfig{
					Host: "localhost:27017",
				},
				Database:   "ghatd_test",
				SkipWarmup: true,
			},
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "repository/mongo-runtime-nil-request",
		},
		{
			name: "FAILURE - invalid URI config",
			req: &NewMongoRuntimeRequest{
				Database:   "ghatd_test",
				SkipWarmup: true,
			},
			wantErr: "repository/mongo-runtime-uri",
		},
		{
			name: "FAILURE - invalid handler config",
			req: &NewMongoRuntimeRequest{
				URIConfig: repositoryhelpers.MongoURIConfig{
					Host: "localhost:27017",
				},
				SkipWarmup: true,
			},
			wantErr: "repository/mongo-runtime-handler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewMongoRuntime(context.Background(), tt.req)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("NewMongoRuntime() expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewMongoRuntime() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewMongoRuntime() error = %v", err)
			}
			if got == nil || got.Handler == nil || got.CoreRepository == nil {
				t.Fatalf("NewMongoRuntime() = %+v, want handler and core repository", got)
			}
			if err := got.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
		})
	}
}

func TestMongoRuntimeClose(t *testing.T) {
	var runtime *MongoRuntime
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("nil runtime Close() error = %v", err)
	}

	runtime = &MongoRuntime{}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("empty runtime Close() error = %v", err)
	}
}

func TestNewMongoRuntimeWarmupFailure(t *testing.T) {
	_, err := NewMongoRuntime(context.Background(), &NewMongoRuntimeRequest{
		URIConfig: repositoryhelpers.MongoURIConfig{
			Host: "127.0.0.1:1",
		},
		Database: "ghatd_test",
		Options: []repositoryhelpers.ConfigOption{
			repositoryhelpers.WithTimeouts(time.Millisecond, time.Millisecond, time.Millisecond),
		},
	})
	if err == nil {
		t.Fatal("NewMongoRuntime() expected warmup error, got nil")
	}
	if !strings.Contains(err.Error(), "repository/mongo-runtime-warmup") {
		t.Fatalf("NewMongoRuntime() error = %v, want warmup error", err)
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("NewMongoRuntime() error should wrap connection failure, got %v", err)
	}
}
