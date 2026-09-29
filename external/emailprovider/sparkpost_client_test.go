package emailprovider

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// roundTripperFunc is a transport that delegates to a callback. Tests use
// its pointer type so transports remain comparable (a struct holding a func
// value would panic under ==).
type roundTripperFunc func(request *http.Request) (*http.Response, error)

// RoundTrip invokes the configured test transport callback.
func (f *roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return (*f)(request)
}

// roundTripperFuncPtr returns a pointer to the given transport function so
// instances can be compared with ==.
func roundTripperFuncPtr(f roundTripperFunc) *roundTripperFunc { return &f }

func TestNewSparkPostClient(t *testing.T) {
	tests := []struct {
		name       string
		req        *NewSparkPostClientRequest
		wantErr    string
		wantAPIKey string
	}{
		{
			name: "SUCCESS - defaults API version",
			req: &NewSparkPostClientRequest{
				BaseURL: "https://api.sparkpost.com",
				APIKey:  "test-key",
			},
			wantAPIKey: "test-key",
		},
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: "emailprovider/sparkpost-client-nil-request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSparkPostClient(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("NewSparkPostClient() error = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewSparkPostClient() error = %v", err)
			}
			if got == nil {
				t.Fatal("NewSparkPostClient() returned nil client")
			}
			if got.Config.ApiKey != tt.wantAPIKey {
				t.Fatalf("ApiKey = %q, want %q", got.Config.ApiKey, tt.wantAPIKey)
			}
		})
	}
}

// TestNewSparkPostClientKeepsLegacyTransportOverride verifies that the
// pre-existing transport-only initialisation path still works.
func TestNewSparkPostClientKeepsLegacyTransportOverride(t *testing.T) {
	transport := roundTripperFuncPtr(func(*http.Request) (*http.Response, error) { return nil, nil })

	client, err := NewSparkPostClient(&NewSparkPostClientRequest{
		BaseURL:   "https://sparkpost.example.com",
		APIKey:    "api-key",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("NewSparkPostClient returned an error: %v", err)
	}
	if client.Client == nil || client.Client == http.DefaultClient {
		t.Fatal("expected a private HTTP client, not http.DefaultClient")
	}
	if client.Client.Transport != http.RoundTripper(transport) {
		t.Fatal("explicit transport override was not applied")
	}
}

// TestNewSparkPostClientWithHTTPClientCopiesPolicyWithoutMutation verifies
// that an injected client's timeout, redirect policy, jar and transport are
// copied onto a private client while the supplied client is never mutated.
func TestNewSparkPostClientWithHTTPClientCopiesPolicyWithoutMutation(t *testing.T) {
	jar := &cookieJarStub{}
	transport := roundTripperFuncPtr(func(*http.Request) (*http.Response, error) { return nil, nil })
	redirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	httpClient := &http.Client{
		Transport:     transport,
		Timeout:       17 * time.Second,
		Jar:           jar,
		CheckRedirect: redirect,
	}

	client, err := NewSparkPostClient(&NewSparkPostClientRequest{
		BaseURL:    "https://sparkpost.example.com",
		APIKey:     "api-key",
		HTTPClient: httpClient,
	})
	if err != nil {
		t.Fatalf("NewSparkPostClient returned an error: %v", err)
	}
	if client.Client == httpClient {
		t.Fatal("client aliases the supplied HTTP client instead of copying it")
	}
	if client.Client == http.DefaultClient {
		t.Fatal("client aliases http.DefaultClient")
	}
	if client.Client.Timeout != 17*time.Second {
		t.Fatalf("timeout policy not copied, got %v", client.Client.Timeout)
	}
	if client.Client.Transport != http.RoundTripper(transport) {
		t.Fatal("transport policy not copied")
	}
	if client.Client.Jar != jar {
		t.Fatal("cookie jar policy not copied")
	}
	if client.Client.CheckRedirect == nil {
		t.Fatal("redirect policy not copied")
	}
	// Mutating the private client must not touch the supplied client.
	client.Client.Timeout = time.Second
	if httpClient.Timeout != 17*time.Second {
		t.Fatal("adjusting the private client mutated the supplied client")
	}
}

// TestNewSparkPostClientTransportOverrideUsesShallowCopy verifies precedence:
// an explicit Transport override is applied to a shallow copy of the supplied
// HTTPClient policy, without mutating the supplied client or its transport.
func TestNewSparkPostClientTransportOverrideUsesShallowCopy(t *testing.T) {
	suppliedTransport := roundTripperFuncPtr(func(*http.Request) (*http.Response, error) { return nil, nil })
	overrideTransport := roundTripperFuncPtr(func(*http.Request) (*http.Response, error) { return nil, nil })
	httpClient := &http.Client{
		Transport: suppliedTransport,
		Timeout:   9 * time.Second,
	}

	client, err := NewSparkPostClient(&NewSparkPostClientRequest{
		BaseURL:    "https://sparkpost.example.com",
		APIKey:     "api-key",
		HTTPClient: httpClient,
		Transport:  overrideTransport,
	})
	if err != nil {
		t.Fatalf("NewSparkPostClient returned an error: %v", err)
	}
	if client.Client.Transport != http.RoundTripper(overrideTransport) {
		t.Fatal("explicit transport override does not take precedence")
	}
	if client.Client.Timeout != 9*time.Second {
		t.Fatal("timeout policy not carried onto the overridden copy")
	}
	if httpClient.Transport != http.RoundTripper(suppliedTransport) {
		t.Fatal("supplied client transport was mutated")
	}
	if httpClient.Timeout != 9*time.Second {
		t.Fatal("supplied client timeout was mutated")
	}
}

// TestWithHTTPClientIsRequestScoped verifies that the fluent option returns a
// copy and never mutates the receiver.
func TestWithHTTPClientIsRequestScoped(t *testing.T) {
	original := &NewSparkPostClientRequest{BaseURL: "https://sparkpost.example.com"}
	httpClient := &http.Client{Timeout: 5 * time.Second}

	updated := original.WithHTTPClient(httpClient)
	if updated == original {
		t.Fatal("WithHTTPClient mutated the receiver instead of returning a copy")
	}
	if original.HTTPClient != nil {
		t.Fatal("WithHTTPClient mutated the original request")
	}
	if updated.HTTPClient != httpClient {
		t.Fatal("WithHTTPClient did not set the HTTP client on the copy")
	}
}

// TestNewSparkPostClientNilHTTPClientCopiesDefaults verifies that a nil
// HTTPClient yields a private copy of the process-wide default policy rather
// than an alias of http.DefaultClient.
func TestNewSparkPostClientNilHTTPClientCopiesDefaults(t *testing.T) {
	defaultTransport := http.DefaultClient.Transport
	defaultTimeout := http.DefaultClient.Timeout

	client, err := NewSparkPostClient(&NewSparkPostClientRequest{
		BaseURL: "https://sparkpost.example.com",
		APIKey:  "api-key",
	})
	if err != nil {
		t.Fatalf("NewSparkPostClient returned an error: %v", err)
	}
	if client.Client == http.DefaultClient {
		t.Fatal("client aliases http.DefaultClient")
	}
	if client.Client.Transport != defaultTransport {
		t.Fatal("default transport policy not copied")
	}
	if client.Client.Timeout != defaultTimeout {
		t.Fatal("default timeout policy not copied")
	}

	// Mutating the private copy must not leak into the global default.
	client.Client.Timeout = 3 * time.Second
	if http.DefaultClient.Timeout != defaultTimeout {
		t.Fatal("private client mutation leaked into http.DefaultClient")
	}
}

// TestNewSparkPostClientConcurrentProvidersShareDefaultsWithoutGlobalMutation
// verifies that concurrent providers built without an HTTPClient never mutate
// http.DefaultClient or each other.
func TestNewSparkPostClientConcurrentProvidersShareDefaultsWithoutGlobalMutation(t *testing.T) {
	defaultTransport := http.DefaultClient.Transport
	defaultTimeout := http.DefaultClient.Timeout

	const providers = 16
	clients := make([]*http.Client, providers)
	var waitGroup sync.WaitGroup
	waitGroup.Add(providers)
	for i := range providers {
		go func() {
			defer waitGroup.Done()
			client, err := NewSparkPostClient(&NewSparkPostClientRequest{
				BaseURL: "https://sparkpost.example.com",
				APIKey:  "api-key",
			})
			if err != nil {
				t.Errorf("NewSparkPostClient returned an error: %v", err)
				return
			}
			client.Client.Timeout = time.Duration(time.Now().UnixNano()%1000+1) * time.Millisecond
			clients[i] = client.Client
		}()
	}
	waitGroup.Wait()

	if http.DefaultClient.Transport != defaultTransport {
		t.Fatal("http.DefaultClient.Transport was mutated")
	}
	if http.DefaultClient.Timeout != defaultTimeout {
		t.Fatal("http.DefaultClient.Timeout was mutated")
	}
	for i, client := range clients {
		if client == nil || client == http.DefaultClient {
			t.Fatalf("provider %d aliased http.DefaultClient", i)
		}
		for j, other := range clients {
			if i != j && client == other {
				t.Fatalf("providers %d and %d share an HTTP client", i, j)
			}
		}
	}
}

// cookieJarStub is a minimal cookie jar used to detect jar policy copying.
type cookieJarStub struct{}

func (*cookieJarStub) SetCookies(*url.URL, []*http.Cookie) {}
func (*cookieJarStub) Cookies(*url.URL) []*http.Cookie     { return nil }
