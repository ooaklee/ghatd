package observability

import (
	"context"
	"errors"
	"net/http"
	"strings"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	httpTracePolicyMaxEntries   = 64
	httpTracePolicyMaxPathBytes = 256
)

// HTTPTracePolicy selects explicit GET/HEAD paths whose request context and
// local descendants may be unrecorded when there is no sampled parent. The
// zero policy selects nothing. Its immutable path sets are never exported.
// This is a head-sampling choice: later errors on selected paths can lose their
// traces, while ordinary logs and HTTP measurements remain enabled.
type HTTPTracePolicy struct {
	exact    map[string]struct{}
	prefixes []string
}

// NewHTTPTracePolicy copies at most 64 literal paths/prefixes of at most 256
// bytes each. Paths must start with / and use ASCII unreserved path segments.
// Root, dot segments, repeated slashes, escapes, patterns, query strings,
// whitespace and control characters are rejected. Prefixes must end with /.
// Invalid configuration errors never include a supplied path.
func NewHTTPTracePolicy(exactPaths, pathPrefixes []string) (HTTPTracePolicy, error) {
	if len(exactPaths) > httpTracePolicyMaxEntries || len(pathPrefixes) > httpTracePolicyMaxEntries-len(exactPaths) {
		return HTTPTracePolicy{}, errors.New("HTTP trace policy supports at most 64 paths and prefixes")
	}
	policy := HTTPTracePolicy{exact: make(map[string]struct{}, len(exactPaths)), prefixes: make([]string, 0, len(pathPrefixes))}
	for _, path := range exactPaths {
		if !canonicalHTTPTracePath(path) {
			return HTTPTracePolicy{}, errors.New("HTTP trace policy requires bounded canonical literal paths excluding root")
		}
		policy.exact[path] = struct{}{}
	}
	for _, prefix := range pathPrefixes {
		if !canonicalHTTPTracePath(prefix) || !strings.HasSuffix(prefix, "/") {
			return HTTPTracePolicy{}, errors.New("HTTP trace policy requires bounded canonical directory prefixes ending in slash and excluding root")
		}
		policy.prefixes = append(policy.prefixes, prefix)
	}
	return policy, nil
}

func canonicalHTTPTracePath(path string) bool {
	if len(path) < 2 || len(path) > httpTracePolicyMaxPathBytes || path[0] != '/' || strings.Contains(path, "//") {
		return false
	}
	for _, char := range path {
		if char == '/' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '.' || char == '_' || char == '~' {
			continue
		}
		return false
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func (policy HTTPTracePolicy) matches(request *http.Request) bool {
	if len(policy.exact) == 0 && len(policy.prefixes) == 0 {
		return false
	}
	if request == nil || request.URL == nil || request.Method != http.MethodGet && request.Method != http.MethodHead || request.URL.RawPath != "" || !canonicalHTTPTracePath(request.URL.Path) {
		return false
	}
	if _, exists := policy.exact[request.URL.Path]; exists {
		return true
	}
	for _, prefix := range policy.prefixes {
		if strings.HasPrefix(request.URL.Path, prefix) {
			return true
		}
	}
	return false
}

// HTTPServerOption configures an outer HTTP telemetry boundary. Options do not
// bypass otelhttp, so its request metrics and normal completion remain active.
type HTTPServerOption interface {
	applyHTTPServer(*httpServerConfiguration)
}

type httpServerConfiguration struct{ tracePolicy HTTPTracePolicy }
type httpServerOptionFunc func(*httpServerConfiguration)

func (option httpServerOptionFunc) applyHTTPServer(config *httpServerConfiguration) { option(config) }

// WithHTTPTracePolicy installs an immutable path policy. Repeated options use
// the last policy. Start installs the required sampler decorator; adopters
// constructing their own provider must wrap its sampler with HTTPTraceSampler.
func WithHTTPTracePolicy(policy HTTPTracePolicy) HTTPServerOption {
	return httpServerOptionFunc(func(config *httpServerConfiguration) { config.tracePolicy = policy })
}

type httpTraceSuppressionKey struct{}

func contextWithHTTPTracePolicy(ctx context.Context, policy HTTPTracePolicy, request *http.Request) context.Context {
	if policy.matches(request) {
		return context.WithValue(ctx, httpTraceSuppressionKey{}, true)
	}
	// Unmatched nested work must retain an inherited suppression request.
	return ctx
}

type httpTraceSampler struct{ base sdktrace.Sampler }

// HTTPTraceSampler decorates a sampler to honor WithHTTPTracePolicy's private
// context marker. With no marker, or with a valid sampled parent, it delegates
// unchanged. Otherwise it drops all marked local spans, including descendants
// and explicitly restarted roots, preserving tracestate. The marker is never
// placed in baggage, headers or span attributes.
//
// A nil base uses ParentBased(AlwaysSample()), the package's default. Providers
// participating in distributed work should consistently use parent-based
// sampling; another provider using AlwaysSample can resume an unsampled trace.
func HTTPTraceSampler(base sdktrace.Sampler) sdktrace.Sampler {
	if base == nil {
		base = sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
	return httpTraceSampler{base: base}
}

func (sampler httpTraceSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parent := trace.SpanContextFromContext(parameters.ParentContext)
	if parameters.ParentContext != nil {
		if suppress, _ := parameters.ParentContext.Value(httpTraceSuppressionKey{}).(bool); suppress && !(parent.IsValid() && parent.IsSampled()) {
			return sdktrace.SamplingResult{Decision: sdktrace.Drop, Tracestate: parent.TraceState()}
		}
	}
	return sampler.base.ShouldSample(parameters)
}

func (sampler httpTraceSampler) Description() string {
	return "HTTPTraceSampler(" + sampler.base.Description() + ")"
}
