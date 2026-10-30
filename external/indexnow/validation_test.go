package indexnow

import (
	"errors"
	"math"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"
)

func TestNewServiceConfiguration(t *testing.T) {
	type testCase struct {
		name, key string
		config    Config
		want      error
	}
	cases := []testCase{
		{name: "minimum key", key: "aB0123-4", config: Config{SiteURL: "https://example.com"}},
		{name: "maximum key", key: strings.Repeat("a", 128), config: Config{SiteURL: "https://example.com/"}},
		{name: "short key", key: "short", config: Config{SiteURL: "https://example.com"}, want: ErrInvalidAPIKey},
		{name: "long key", key: strings.Repeat("a", 129), config: Config{SiteURL: "https://example.com"}, want: ErrInvalidAPIKey},
		{name: "non ASCII key", key: "abcdefghé", config: Config{SiteURL: "https://example.com"}, want: ErrInvalidAPIKey},
		{name: "key punctuation", key: "abcdefgh_", config: Config{SiteURL: "https://example.com"}, want: ErrInvalidAPIKey},
		{name: "missing origin", key: "abcdefgh", want: ErrMissingSiteURL},
		{name: "origin path", key: "abcdefgh", config: Config{SiteURL: "https://example.com/blog"}, want: ErrInvalidSiteURL},
		{name: "origin credentials", key: "abcdefgh", config: Config{SiteURL: "https://user:password@example.com"}, want: ErrInvalidSiteURL},
		{name: "origin query", key: "abcdefgh", config: Config{SiteURL: "https://example.com?key=secret"}, want: ErrInvalidSiteURL},
		{name: "origin empty query", key: "abcdefgh", config: Config{SiteURL: "https://example.com?"}, want: ErrInvalidSiteURL},
		{name: "origin fragment", key: "abcdefgh", config: Config{SiteURL: "https://example.com/#"}, want: ErrInvalidSiteURL},
		{name: "origin invalid DNS label", key: "abcdefgh", config: Config{SiteURL: "https://-example.com"}, want: ErrInvalidSiteURL},
		{name: "origin empty DNS label", key: "abcdefgh", config: Config{SiteURL: "https://example..com"}, want: ErrInvalidSiteURL},
		{name: "origin invalid port", key: "abcdefgh", config: Config{SiteURL: "https://example.com:65536"}, want: ErrInvalidSiteURL},
		{name: "local IPv4 origin", key: "abcdefgh", config: Config{SiteURL: "http://127.0.0.1:8097"}},
		{name: "local IPv6 origin", key: "abcdefgh", config: Config{SiteURL: "http://[::1]:8097"}},
		{name: "endpoint credentials", key: "abcdefgh", config: Config{SiteURL: "https://example.com", Endpoint: "https://user:password@engine.test/indexnow"}, want: ErrInvalidEndpoint},
		{name: "endpoint query", key: "abcdefgh", config: Config{SiteURL: "https://example.com", Endpoint: "https://engine.test/indexnow?key=x"}, want: ErrInvalidEndpoint},
		{name: "endpoint invalid scheme", key: "abcdefgh", config: Config{SiteURL: "https://example.com", Endpoint: "file:///indexnow"}, want: ErrInvalidEndpoint},
		{name: "custom root file", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "/ownership.txt"}},
		{name: "custom nested file", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "/catalog/ownership.txt"}},
		{name: "absolute custom location", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "https://example.com/catalog/ownership.txt"}},
		{name: "custom different host", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "https://other.example.com/key.txt"}, want: ErrInvalidKeyLocation},
		{name: "custom different port", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "https://example.com:8443/key.txt"}, want: ErrInvalidKeyLocation},
		{name: "custom query", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "https://example.com/key.txt?q=1"}, want: ErrInvalidKeyLocation},
		{name: "custom wildcard", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "/{anything}.txt"}, want: ErrInvalidKeyLocation},
		{name: "custom traversal", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "/catalog/../key.txt"}, want: ErrInvalidKeyLocation},
		{name: "custom encoded traversal", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "https://example.com/catalog/%2e%2e/key.txt"}, want: ErrInvalidKeyLocation},
		{name: "custom slash alias", key: "abcdefgh", config: Config{SiteURL: "https://example.com", KeyLocation: "//key.txt"}, want: ErrInvalidKeyLocation},
		{name: "negative timeout", key: "abcdefgh", config: Config{SiteURL: "https://example.com", Timeout: -time.Second}, want: ErrInvalidConfiguration},
		{name: "typed nil audit store", key: "abcdefgh", config: Config{SiteURL: "https://example.com", Repository: (*MemoryRepository)(nil)}, want: ErrInvalidConfiguration},
		{name: "typed nil cookie jar", key: "abcdefgh", config: Config{SiteURL: "https://example.com", HTTPClient: &http.Client{Jar: (*cookiejar.Jar)(nil)}}, want: ErrInvalidConfiguration},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, err := NewService(tc.key, tc.config)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewService error=%v, want %v", err, tc.want)
			}
			if tc.want == nil && service == nil {
				t.Fatal("missing valid service")
			}
			if tc.want != nil && service != nil {
				t.Fatal("invalid config returned service")
			}
		})
	}
}

func TestRetryAfterBoundaries(t *testing.T) {
	type testCase struct {
		name, value string
		want        time.Duration
	}
	cases := []testCase{{"seconds", "30", 30 * time.Second}, {"zero", "0", 0}, {"negative", "-1", 0}, {"bad", "tomorrow", 0}, {"past HTTP date", "Mon, 02 Jan 2006 15:04:05 GMT", 0}, {"overflow bounded", "18446744073709551615", time.Duration(math.MaxInt64)}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRetryAfter(tc.value); got != tc.want {
				t.Fatalf("delay=%v, want %v", got, tc.want)
			}
		})
	}
}
