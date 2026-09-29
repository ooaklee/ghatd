package observability

import (
	"fmt"
	"strings"
)

// HTTPServerOptionsConfig composes the common detailed-access-log and optional
// trace-suppression policies. Hosts supply every path and enable flag; the zero
// value enables neither policy. Use the lower-level policy constructors for
// partial field selection or custom field-size limits.
type HTTPServerOptionsConfig struct {
	RequestLogDetails      bool
	PreservePathParameters bool
	RedactPathPrefixes     []string
	// TrustedProxyCIDRs is a comma-separated list. Blank trusts no proxy;
	// empty entries inside a nonblank list are invalid, not silently dropped.
	TrustedProxyCIDRs string
	// TraceNoiseSuppression requires the provider's HTTPTraceSampler, as Start
	// configures. Metrics and logs remain enabled for suppressed requests.
	TraceNoiseSuppression   bool
	TraceSuppressedPaths    []string
	TraceSuppressedPrefixes []string
}

// NewHTTPServerOptions validates enabled policies and snapshots their inputs.
// Disabled policies ignore their settings. RequestLogDetails enables bounded
// original path, user-agent, peer and forwarding-header evidence in local logs;
// these details never become HTTP span attributes or metric dimensions.
func NewHTTPServerOptions(config HTTPServerOptionsConfig) ([]HTTPServerOption, error) {
	var options []HTTPServerOption
	if config.RequestLogDetails {
		var proxies []string
		if strings.TrimSpace(config.TrustedProxyCIDRs) != "" {
			for _, value := range strings.Split(config.TrustedProxyCIDRs, ",") {
				proxies = append(proxies, strings.TrimSpace(value))
			}
		}
		policy, err := NewHTTPRequestLogPolicy(HTTPRequestLogConfig{
			IncludePath: true, IncludeUserAgent: true, IncludeClientAddress: true,
			PreservePathParameters: config.PreservePathParameters,
			RedactPathPrefixes:     config.RedactPathPrefixes, TrustedProxyCIDRs: proxies,
		})
		if err != nil {
			return nil, fmt.Errorf("observability: request-log-policy: %w", err)
		}
		options = append(options, WithHTTPRequestLogPolicy(policy))
	}
	if config.TraceNoiseSuppression {
		policy, err := NewHTTPTracePolicy(config.TraceSuppressedPaths, config.TraceSuppressedPrefixes)
		if err != nil {
			return nil, fmt.Errorf("observability: trace-policy: %w", err)
		}
		options = append(options, WithHTTPTracePolicy(policy))
	}
	return options, nil
}
