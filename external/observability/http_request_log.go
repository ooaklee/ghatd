package observability

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.uber.org/zap"
)

// HTTPRequestLogConfig opts into additional fields on the existing request
// completion log. These fields stay in the original Zap sink, never in GHATD's
// OTLP log branch, HTTP span attributes, or metric dimensions.
type HTTPRequestLogConfig struct {
	IncludePath      bool
	IncludeUserAgent bool
	// IncludeClientAddress records the socket peer and bounded, unverified
	// forwarding-header claims. Trusted proxy resolution is optional.
	IncludeClientAddress bool
	// RedactPathPrefixes replaces the suffix of matching directories with
	// [redacted], including unmatched requests. Prefixes must end in /.
	RedactPathPrefixes []string
	// TrustedProxyCIDRs must describe proxies that control X-Forwarded-For.
	// Empty trusts no proxy. Never infer trust from private address space.
	TrustedProxyCIDRs []string
	// Zero selects 2048 path bytes and 512 user-agent bytes respectively.
	MaxPathBytes      int
	MaxUserAgentBytes int
}

// HTTPRequestLogPolicy is an immutable, per-boundary access-log policy. The
// zero value adds no request details. Construct it with NewHTTPRequestLogPolicy.
type HTTPRequestLogPolicy struct {
	includePath, includeUserAgent, includeClientAddress bool
	prefixes                                            []string
	proxies                                             []netip.Prefix
	maxPath, maxUserAgent                               int
}

// NewHTTPRequestLogPolicy snapshots and validates configuration without
// including supplied values in errors. At most 64 prefixes/proxies are allowed.
// Limits must be 32–4096 bytes for paths and 32–2048 for user-agent strings.
func NewHTTPRequestLogPolicy(config HTTPRequestLogConfig) (HTTPRequestLogPolicy, error) {
	if config.MaxPathBytes == 0 {
		config.MaxPathBytes = 2048
	}
	if config.MaxUserAgentBytes == 0 {
		config.MaxUserAgentBytes = 512
	}
	if config.MaxPathBytes < 32 || config.MaxPathBytes > 4096 || config.MaxUserAgentBytes < 32 || config.MaxUserAgentBytes > 2048 {
		return HTTPRequestLogPolicy{}, errors.New("HTTP request log field limits are outside the supported byte range")
	}
	if len(config.RedactPathPrefixes) > 64 || len(config.TrustedProxyCIDRs) > 64 {
		return HTTPRequestLogPolicy{}, errors.New("HTTP request log policy supports at most 64 prefixes and 64 proxies")
	}
	policy := HTTPRequestLogPolicy{
		includePath: config.IncludePath, includeUserAgent: config.IncludeUserAgent, includeClientAddress: config.IncludeClientAddress,
		maxPath: config.MaxPathBytes, maxUserAgent: config.MaxUserAgentBytes,
	}
	for _, prefix := range config.RedactPathPrefixes {
		if !canonicalHTTPTracePath(prefix) || !strings.HasSuffix(prefix, "/") {
			return HTTPRequestLogPolicy{}, errors.New("HTTP request log redaction requires canonical directory prefixes ending in slash, excluding root")
		}
		policy.prefixes = append(policy.prefixes, prefix)
	}
	for _, cidr := range config.TrustedProxyCIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil || prefix.Addr().Is4In6() || prefix.Bits() == 0 {
			return HTTPRequestLogPolicy{}, errors.New("HTTP request log proxy requires an explicit IPv4 or IPv6 CIDR, excluding catch-all and mapped IPv4 prefixes")
		}
		policy.proxies = append(policy.proxies, prefix.Masked())
	}
	return policy, nil
}

type httpRequestLogPolicyKey struct{}

// WithHTTPRequestLogPolicy enables access-log details at an HTTP boundary.
// Repeated options use the last policy. Use with otelhttp.WrapWithOptions.
func WithHTTPRequestLogPolicy(policy HTTPRequestLogPolicy) HTTPServerOption {
	return httpServerOptionFunc(func(config *httpServerConfiguration) { config.requestLogPolicy = policy })
}

// HTTPRequestLogSnapshot holds bounded request details captured before a
// handler can mutate them. Fields combines them with the final route template.
type HTTPRequestLogSnapshot struct {
	policy                      HTTPRequestLogPolicy
	path                        string
	pathRedacted, pathTruncated bool
	fields                      []zap.Field
}

// CaptureHTTPRequestLog snapshots the policy-selected original request fields.
// Without WithHTTPRequestLogPolicy it captures nothing. Query strings and
// cookies are never captured. Forwarding headers are bounded unverified claims.
func CaptureHTTPRequestLog(request *http.Request) HTTPRequestLogSnapshot {
	policy, _ := request.Context().Value(httpRequestLogPolicyKey{}).(HTTPRequestLogPolicy)
	snapshot := HTTPRequestLogSnapshot{policy: policy}
	if policy.includePath && request.URL != nil {
		value := request.URL.EscapedPath()
		for _, prefix := range policy.prefixes {
			if strings.HasPrefix(request.URL.Path, prefix) || strings.HasPrefix(path.Clean(request.URL.Path), prefix) {
				value, snapshot.pathRedacted = prefix+"[redacted]", true
				break
			}
		}
		snapshot.path, snapshot.pathTruncated = boundedRequestLogValue(value, policy.maxPath)
	}
	if policy.includeUserAgent && request.UserAgent() != "" {
		value, truncated := boundedRequestLogValue(request.UserAgent(), policy.maxUserAgent)
		snapshot.fields = append(snapshot.fields, zap.String("user_agent.original", value))
		if truncated {
			snapshot.fields = append(snapshot.fields, zap.Bool("user_agent.original.truncated", true))
		}
	}
	if policy.includeClientAddress {
		for _, header := range []struct {
			name, field string
			limit       int
		}{
			{"X-Forwarded-For", "http.request.header.x_forwarded_for", 1024},
			{"Forwarded", "http.request.header.forwarded", 1024},
			{"X-Real-IP", "http.request.header.x_real_ip", 128},
			{"CF-Connecting-IP", "http.request.header.cf_connecting_ip", 128},
			{"True-Client-IP", "http.request.header.true_client_ip", 128},
		} {
			if values := request.Header.Values(header.name); len(values) > 0 {
				value, truncated := boundedRequestLogHeader(values, header.limit)
				snapshot.fields = append(snapshot.fields, zap.String(header.field, value))
				if truncated {
					snapshot.fields = append(snapshot.fields, zap.Bool(header.field+".truncated", true))
				}
			}
		}
		peer, ok := requestLogPeer(request.RemoteAddr)
		if ok {
			snapshot.fields = append(snapshot.fields, zap.String("network.peer.address", peer.String()))
			if !policy.trusted(peer) {
				snapshot.fields = append(snapshot.fields, zap.String("client.address", peer.String()), zap.String("client.address.source", "peer"))
			} else if client, ok := policy.forwardedClient(request.Header.Values("X-Forwarded-For")); ok {
				snapshot.fields = append(snapshot.fields, zap.String("client.address", client.String()), zap.String("client.address.source", "forwarded"))
			}
		}
	}
	return snapshot
}

// Fields returns log fields, preserving the route template separately from the
// original path. Parameterized routes use their template as url.path, keeping
// route parameter values private. Catch-all prefixes such as / retain the
// original path. Configured prefix redaction takes precedence.
func (snapshot HTTPRequestLogSnapshot) Fields(route string) []zap.Field {
	fields := append([]zap.Field(nil), snapshot.fields...)
	if snapshot.policy.includePath && snapshot.path != "" {
		if !snapshot.pathRedacted && strings.Contains(route, "{") {
			snapshot.path, snapshot.pathTruncated = boundedRequestLogValue(route, snapshot.policy.maxPath)
			snapshot.pathRedacted = true
		}
		fields = append(fields, zap.String("url.path", snapshot.path))
		if snapshot.pathRedacted {
			fields = append(fields, zap.Bool("url.path.redacted", true))
		}
		if snapshot.pathTruncated {
			fields = append(fields, zap.Bool("url.path.truncated", true))
		}
	}
	return fields
}

func boundedRequestLogValue(value string, limit int) (string, bool) {
	// Bound processing as well as output. Replace invalid UTF-8 and controls so
	// arbitrary headers cannot create misleading multiline console records.
	truncated := len(value) > limit
	if truncated {
		value = value[:limit]
	}
	value = strings.ToValidUTF8(value, "?")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, value)
	for len(value) > limit {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
		truncated = true
	}
	return value, truncated
}

func boundedRequestLogHeader(values []string, limit int) (string, bool) {
	var joined strings.Builder
	for index, value := range values {
		if index > 0 {
			joined.WriteString(", ")
		}
		remaining := limit + 1 - joined.Len()
		if remaining <= 0 {
			break
		}
		if len(value) > remaining {
			value = value[:remaining]
		}
		joined.WriteString(value)
		if joined.Len() > limit {
			break
		}
	}
	return boundedRequestLogValue(joined.String(), limit)
}

func requestLogPeer(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	address, err := netip.ParseAddr(host)
	return address.Unmap(), err == nil && address.Zone() == ""
}

func (policy HTTPRequestLogPolicy) trusted(address netip.Addr) bool {
	for _, prefix := range policy.proxies {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func (policy HTTPRequestLogPolicy) forwardedClient(values []string) (netip.Addr, bool) {
	// Parse every header line in wire order. Reject oversized/malformed chains
	// instead of truncating them into a different apparent client identity.
	if len(values) == 0 || len(values) > 32 {
		return netip.Addr{}, false
	}
	total := 0
	for _, value := range values {
		total += len(value) + 1
	}
	if total > 4096 {
		return netip.Addr{}, false
	}
	parts := strings.Split(strings.Join(values, ","), ",")
	if len(parts) > 32 {
		return netip.Addr{}, false
	}
	addresses := make([]netip.Addr, 0, len(parts))
	for _, part := range parts {
		address, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil || address.Zone() != "" {
			return netip.Addr{}, false
		}
		addresses = append(addresses, address.Unmap())
	}
	for index := len(addresses) - 1; index >= 0; index-- {
		if !policy.trusted(addresses[index]) {
			return addresses[index], true
		}
	}
	return netip.Addr{}, false
}
