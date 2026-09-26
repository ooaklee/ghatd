package observability

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"
)

func TestHTTPRequestLogProxyTrust(t *testing.T) {
	policy, err := NewHTTPRequestLogPolicy(HTTPRequestLogConfig{
		IncludeClientAddress: true, TrustedProxyCIDRs: []string{"10.0.0.0/24", "2001:db8:1::/48"},
	})
	require.NoError(t, err)
	for _, test := range []struct {
		name, peer, client, source string
		headers                    []string
	}{
		{"direct ignores spoofed header", "192.0.2.1:1234", "192.0.2.1", "peer", []string{"198.51.100.2"}},
		{"trusted chain", "10.0.0.1:80", "192.0.2.1", "forwarded", []string{"192.0.2.1, 10.0.0.2"}},
		{"ignore forged left entry", "10.0.0.1:80", "192.0.2.1", "forwarded", []string{"198.51.100.99, 192.0.2.1, 10.0.0.2"}},
		{"multiple header lines", "10.0.0.1:80", "192.0.2.1", "forwarded", []string{"192.0.2.1", "10.0.0.2"}},
		{"IPv6", "[2001:db8:1::1]:80", "2001:db8:2::1", "forwarded", []string{"2001:db8:2::1, 2001:db8:1::2"}},
		{"mapped IPv4 peer", "[::ffff:10.0.0.1]:80", "192.0.2.1", "forwarded", []string{"::ffff:192.0.2.1"}},
		{"trusted missing header", "10.0.0.1:80", "", "", nil},
		{"invalid chain", "10.0.0.1:80", "", "", []string{"private-invalid, 192.0.2.1"}},
		{"empty entry", "10.0.0.1:80", "", "", []string{"192.0.2.1,,10.0.0.2"}},
		{"all trusted", "10.0.0.1:80", "", "", []string{"10.0.0.2"}},
		{"zone rejected", "10.0.0.1:80", "", "", []string{"fe80::1%eth0"}},
		{"port rejected", "10.0.0.1:80", "", "", []string{"192.0.2.1:80"}},
		{"too many hops", "10.0.0.1:80", "", "", []string{strings.Repeat("192.0.2.1,", 32) + "10.0.0.2"}},
		{"too many bytes", "10.0.0.1:80", "", "", []string{strings.Repeat(" ", 4096) + "192.0.2.1"}},
		{"invalid peer", "unix-socket", "", "", []string{"192.0.2.1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/", nil)
			request.RemoteAddr = test.peer
			request.Header["X-Forwarded-For"] = test.headers
			request = request.WithContext(context.WithValue(request.Context(), httpRequestLogPolicyKey{}, policy))
			fields := zapcore.NewMapObjectEncoder()
			for _, field := range CaptureHTTPRequestLog(request).Fields("/") {
				field.AddTo(fields)
			}
			if test.client == "" {
				require.NotContains(t, fields.Fields, "client.address")
			} else {
				require.Equal(t, test.client, fields.Fields["client.address"])
				require.Equal(t, test.source, fields.Fields["client.address.source"])
			}
			require.NotContains(t, fields.Fields, "forwarded-for")
		})
	}
}

func TestHTTPRequestLogPolicyValidationAndBounds(t *testing.T) {
	for _, config := range []HTTPRequestLogConfig{
		{MaxPathBytes: -1}, {MaxPathBytes: 4097}, {MaxUserAgentBytes: 2049},
		{RedactPathPrefixes: []string{"/"}}, {RedactPathPrefixes: []string{"/secret"}},
		{RedactPathPrefixes: []string{"/secret/../"}}, {RedactPathPrefixes: []string{"/secret/%2f/"}},
		{TrustedProxyCIDRs: []string{"invalid-private-value"}}, {TrustedProxyCIDRs: []string{"0.0.0.0/0"}},
		{TrustedProxyCIDRs: []string{"::/0"}}, {TrustedProxyCIDRs: []string{"::ffff:10.0.0.0/120"}},
		{TrustedProxyCIDRs: make([]string, 65)}, {RedactPathPrefixes: make([]string, 65)},
	} {
		_, err := NewHTTPRequestLogPolicy(config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), "invalid-private-value")
	}
	config := HTTPRequestLogConfig{IncludePath: true, IncludeUserAgent: true, RedactPathPrefixes: []string{"/private/"}, MaxPathBytes: 32, MaxUserAgentBytes: 32}
	policy, err := NewHTTPRequestLogPolicy(config)
	require.NoError(t, err)
	config.RedactPathPrefixes[0] = "/other/"
	for _, test := range []struct{ target, want string }{
		{"/private/secret", "/private/[redacted]"},
		{"/%70rivate/secret", "/private/[redacted]"},
		{"/x/../private/secret", "/private/[redacted]"},
		{"/private%2Fsecret", "/private/[redacted]"},
		{"/public%2Fname?secret=ignored", "/public%2Fname"},
	} {
		request := httptest.NewRequest("GET", test.target, nil)
		request = request.WithContext(context.WithValue(request.Context(), httpRequestLogPolicyKey{}, policy))
		snapshot := CaptureHTTPRequestLog(request)
		require.Equal(t, test.want, snapshot.path)
	}
	value, truncated := boundedRequestLogValue(strings.Repeat("é", 40), 33)
	require.True(t, truncated)
	require.True(t, utf8.ValidString(value))
	require.LessOrEqual(t, len(value), 33)
	value, _ = boundedRequestLogValue("a\n\x00b\xff", 32)
	require.Equal(t, "a  b?", value)
	require.Empty(t, CaptureHTTPRequestLog(httptest.NewRequest("GET", "/", nil)).Fields("/"))
}

func TestHTTPRequestLogRetainsBoundedUnverifiedHeaderClaims(t *testing.T) {
	policy, err := NewHTTPRequestLogPolicy(HTTPRequestLogConfig{IncludeClientAddress: true})
	require.NoError(t, err)
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	request.Header["X-Forwarded-For"] = []string{"198.51.100.7", strings.Repeat("x", 2000)}
	request.Header.Set("Forwarded", "for=203.0.113.8;proto=https")
	request.Header.Set("X-Real-IP", "supplied-not-an-ip")
	request.Header.Set("CF-Connecting-IP", "203.0.113.9")
	request.Header.Set("True-Client-IP", "203.0.113.10")
	request.Header.Set("Authorization", "secret-canary")
	request.Header.Set("Cookie", "secret-canary")
	request = request.WithContext(context.WithValue(request.Context(), httpRequestLogPolicyKey{}, policy))
	snapshot := CaptureHTTPRequestLog(request)
	request.Header.Set("Forwarded", "mutated")
	fields := zapcore.NewMapObjectEncoder()
	for _, field := range snapshot.Fields("/") {
		field.AddTo(fields)
	}
	require.Equal(t, "192.0.2.1", fields.Fields["client.address"])
	require.Equal(t, "peer", fields.Fields["client.address.source"])
	require.Equal(t, "for=203.0.113.8;proto=https", fields.Fields["http.request.header.forwarded"])
	require.Equal(t, "supplied-not-an-ip", fields.Fields["http.request.header.x_real_ip"])
	require.Equal(t, "203.0.113.9", fields.Fields["http.request.header.cf_connecting_ip"])
	require.Equal(t, "203.0.113.10", fields.Fields["http.request.header.true_client_ip"])
	require.Len(t, fields.Fields["http.request.header.x_forwarded_for"], 1024)
	require.Equal(t, true, fields.Fields["http.request.header.x_forwarded_for.truncated"])
	require.NotContains(t, fields.Fields, "Authorization")
	require.NotContains(t, fields.Fields, "Cookie")
}
