package repositoryhelpers

import (
	"net/url"
	"strings"
	"testing"
)

func TestGenerateMongoURI(t *testing.T) {
	tests := []struct {
		name       string
		config     MongoURIConfig
		want       string
		wantPrefix string
		wantErr    bool
		assert     func(t *testing.T, got string)
	}{
		{
			name: "Success - generic MongoDB URI",
			config: MongoURIConfig{
				Username: "mongoadmin",
				Password: "secret",
				Host:     "localhost:27017",
			},
			want: "mongodb://mongoadmin:secret@localhost:27017",
		},
		{
			name: "Success - generic MongoDB URI without credentials",
			config: MongoURIConfig{
				Host: "localhost:27017",
			},
			want: "mongodb://localhost:27017",
		},
		{
			name: "Success - Atlas MongoDB URI",
			config: MongoURIConfig{
				Username: "mongoadmin",
				Password: "secret",
				Host:     "cluster.example.mongodb.net",
				AppName:  "amazingapp",
				Atlas:    true,
			},
			wantPrefix: "mongodb+srv://",
			assert: func(t *testing.T, got string) {
				t.Helper()
				parsed := mustParseMongoURI(t, got)

				if parsed.Scheme != "mongodb+srv" {
					t.Fatalf("expected mongodb+srv scheme, got %q", parsed.Scheme)
				}
				if parsed.User.Username() != "mongoadmin" {
					t.Fatalf("expected username mongoadmin, got %q", parsed.User.Username())
				}
				password, ok := parsed.User.Password()
				if !ok || password != "secret" {
					t.Fatalf("expected password secret, got %q", password)
				}
				if parsed.Host != "cluster.example.mongodb.net" {
					t.Fatalf("expected host cluster.example.mongodb.net, got %q", parsed.Host)
				}

				query := parsed.Query()
				if query.Get("retryWrites") != "true" {
					t.Fatalf("expected retryWrites=true, got %q", query.Get("retryWrites"))
				}
				if query.Get("w") != "majority" {
					t.Fatalf("expected w=majority, got %q", query.Get("w"))
				}
				if query.Get("appName") != "amazingapp" {
					t.Fatalf("expected appName=amazingapp, got %q", query.Get("appName"))
				}
			},
		},
		{
			name: "Success - Atlas MongoDB URI omits empty appName",
			config: MongoURIConfig{
				Username: "mongoadmin",
				Password: "secret",
				Host:     "cluster.example.mongodb.net",
				Atlas:    true,
			},
			wantPrefix: "mongodb+srv://",
			assert: func(t *testing.T, got string) {
				t.Helper()
				if strings.Contains(got, "appName=") {
					t.Fatalf("expected URI to omit empty appName, got %q", got)
				}
			},
		},
		{
			name: "Success - encodes credential and appName special characters",
			config: MongoURIConfig{
				Username: "mongo admin",
				Password: "p@ss:word/with?chars",
				Host:     "cluster.example.mongodb.net",
				AppName:  "amazingapp app",
				Atlas:    true,
			},
			wantPrefix: "mongodb+srv://",
			assert: func(t *testing.T, got string) {
				t.Helper()
				parsed := mustParseMongoURI(t, got)

				if parsed.User.Username() != "mongo admin" {
					t.Fatalf("expected decoded username, got %q", parsed.User.Username())
				}
				password, _ := parsed.User.Password()
				if password != "p@ss:word/with?chars" {
					t.Fatalf("expected decoded password, got %q", password)
				}
				if parsed.Query().Get("appName") != "amazingapp app" {
					t.Fatalf("expected decoded appName, got %q", parsed.Query().Get("appName"))
				}
			},
		},
		{
			name: "Failure - empty host",
			config: MongoURIConfig{
				Username: "mongoadmin",
				Password: "secret",
			},
			wantErr: true,
		},
		{
			name: "Failure - whitespace-only host",
			config: MongoURIConfig{
				Username: "mongoadmin",
				Password: "secret",
				Host:     "   ",
			},
			wantErr: true,
		},
		{
			name: "Failure - password without username",
			config: MongoURIConfig{
				Password: "secret",
				Host:     "localhost:27017",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := GenerateMongoURI(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("expected no error, got %v", err)
			}
			if tt.want != "" && got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
			if tt.wantPrefix != "" && !strings.HasPrefix(got, tt.wantPrefix) {
				t.Fatalf("expected prefix %q, got %q", tt.wantPrefix, got)
			}
			if tt.assert != nil {
				tt.assert(t, got)
			}
		})
	}
}

func TestGenerateSpecificMongoURIHelpers(t *testing.T) {
	tests := []struct {
		name string
		run  func() string
		want string
	}{
		{
			name: "Success - GenerateGenericMongoURI",
			run: func() string {
				return GenerateGenericMongoURI("mongoadmin", "secret", "localhost:27017")
			},
			want: "mongodb://mongoadmin:secret@localhost:27017",
		},
		{
			name: "Success - GenerateAtlasMongoURI",
			run: func() string {
				return GenerateAtlasMongoURI("mongoadmin", "secret", "cluster.example.mongodb.net", "amazingapp")
			},
			want: "mongodb+srv://mongoadmin:secret@cluster.example.mongodb.net/?appName=amazingapp&retryWrites=true&w=majority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.run(); got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func mustParseMongoURI(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	if err != nil {
		t.Fatalf("expected valid URI, got error: %v", err)
	}
	return parsed
}
