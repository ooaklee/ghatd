// Package observability provides shared, provider-neutral telemetry helpers.
package observability

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router/routecontext"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const redactedURLPath = "/"

type clientRequestStateKey struct{}
type serverRequestTargetKey struct{}

// requestTarget contains the request fields that can hold a raw path or query.
// It is carried only between the privacy wrapper and its private restore layer.
type requestTarget struct {
	url        *url.URL
	requestURI string
	pattern    string
}

type clientRequestState struct {
	target       requestTarget
	transportErr error
}

// serverRequestState carries request data that handlers need but that must not
// be visible while otelhttp derives automatic span and metric attributes.
type serverRequestState struct {
	target     requestTarget
	header     http.Header
	remoteAddr string
	host       string
	method     string
	panicked   bool
}

var clientIdentityHeaders = []string{
	"Forwarded",
	"X-Forwarded-For",
	"X-Real-IP",
	"X-Client-IP",
	"CF-Connecting-IP",
	"True-Client-IP",
	"Fastly-Client-IP",
	"Fly-Client-IP",
	"User-Agent",
}

// telemetrySafeTransportError is deliberately constant and contains no data
// supplied by an upstream transport. The public wrapper restores the original
// error after otelhttp has finished recording the client span.
var telemetrySafeTransportError = errors.New("outbound HTTP transport failed")

// NewRoundTripper instruments outbound HTTP requests with OpenTelemetry while
// keeping raw URL paths and query strings out of telemetry. The wrapped
// transport still receives the complete original request target.
// Response stream errors are sanitised for telemetry and restored for callers,
// including reads and writes on upgraded connections.
//
// A nil base uses http.DefaultTransport. The caller's request is never mutated.
func NewRoundTripper(base http.RoundTripper) http.RoundTripper {
	return newRoundTripper(base)
}

// newRoundTripper builds the privacy wrapper around an otelhttp transport.
func newRoundTripper(base http.RoundTripper, options ...otelhttp.Option) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}

	restoringTransport := &requestTargetRestoringRoundTripper{base: base}
	return &privacySafeRoundTripper{
		instrumented: otelhttp.NewTransport(restoringTransport, options...),
	}
}

type privacySafeRoundTripper struct {
	instrumented http.RoundTripper
}

// RoundTrip hides sensitive request-target data while otelhttp records telemetry.
func (transport *privacySafeRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	state := &clientRequestState{target: captureRequestTarget(request)}
	ctx := context.WithValue(request.Context(), clientRequestStateKey{}, state)

	sanitisedRequest := request.Clone(ctx)
	applySanitisedTarget(sanitisedRequest, "")

	response, err := transport.instrumented.RoundTrip(sanitisedRequest)
	if response != nil {
		response.Body = transformHTTPBody(response.Body, restoreHTTPIOError)
	}
	if state.transportErr != nil {
		return response, state.transportErr
	}

	return response, err
}

// requestTargetRestoringRoundTripper is placed immediately below otelhttp. It
// restores the real target after otelhttp has derived its attributes and
// injected W3C trace headers, then delegates exactly once to the caller's base
// transport.
type requestTargetRestoringRoundTripper struct {
	base http.RoundTripper
}

// RoundTrip restores the original request target immediately before dispatch.
func (transport *requestTargetRestoringRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	state, ok := request.Context().Value(clientRequestStateKey{}).(*clientRequestState)
	if !ok || state == nil {
		return transport.base.RoundTrip(request)
	}

	restoredRequest := request.Clone(request.Context())
	applyRequestTarget(restoredRequest, state.target)

	response, err := transport.base.RoundTrip(restoredRequest)
	if err != nil {
		// otelhttp records error strings as span status descriptions. Give it a
		// stable, data-free error, then return the original error to the caller
		// from privacySafeRoundTripper.
		state.transportErr = err
		return response, telemetrySafeTransportError
	}

	if response != nil {
		response.Body = transformHTTPBody(response.Body, sanitiseHTTPIOError)
	}
	return response, nil
}

// NewHTTPClient creates an HTTP client with privacy-safe OpenTelemetry
// instrumentation and the supplied timeout.
func NewHTTPClient(base http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: NewRoundTripper(base),
		Timeout:   timeout,
	}
}

// HTTPServerMiddleware instruments inbound HTTP requests with OpenTelemetry.
// Span names and http.route use the matched route template, while automatic URL
// attributes see only a constant path, no query, and no client-identifying
// transport metadata. The application handler still receives the original URL,
// RequestURI, route data, headers, RemoteAddr, and traced context.
// Wrap the complete router to cover generated 404/405 responses and redirects.
// GHATD routers capture route templates automatically; direct Gorilla Mux
// routers should install routecontext.ObserveMiddleware before other middleware.
func HTTPServerMiddleware(serviceName string) func(http.Handler) http.Handler {
	return httpServerMiddleware(serviceName)
}

// httpServerMiddleware builds privacy-safe server instrumentation with optional
// otelhttp settings for tests and advanced callers.
func httpServerMiddleware(serviceName string, options ...otelhttp.Option) func(http.Handler) http.Handler {
	instrument := otelhttp.NewMiddleware(
		serviceName,
		append(options, otelhttp.WithSpanNameFormatter(serverSpanName))...,
	)

	return func(next http.Handler) http.Handler {
		restoreTarget := http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			state, ok := request.Context().Value(serverRequestTargetKey{}).(*serverRequestState)
			if !ok {
				next.ServeHTTP(responseWriter, request)
				return
			}

			restoredRequest := request.Clone(request.Context())
			applyRequestTarget(restoredRequest, state.target)
			restoreServerRequestMetadata(restoredRequest, *state)
			restoredRequest.Body = transformHTTPBody(restoredRequest.Body, restoreHTTPIOError)
			next.ServeHTTP(transformHTTPWriter(responseWriter, restoreHTTPIOError), restoredRequest)
			// Mux passes a different request to matched handlers. Transfer only
			// its tracked template back to otelhttp's request so final span and
			// metric attributes use the match without exposing raw request data.
			request.Pattern = routecontext.Template(restoredRequest)
			if request.Pattern != "" {
				trace.SpanFromContext(request.Context()).SetAttributes(attribute.String("http.route", request.Pattern))
			}

			// Keep parsed multipart state visible to the instrumented request and
			// ultimately net/http's request cleanup.
			if restoredRequest.MultipartForm != nil {
				request.MultipartForm = restoredRequest.MultipartForm
			}
		})
		instrumentedHandler := instrument(restoreTarget)

		return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
			if _, active := request.Context().Value(serverRequestTargetKey{}).(*serverRequestState); active {
				// An outer boundary already owns this request's server telemetry.
				// Retain compatibility with existing route-level installations.
				next.ServeHTTP(responseWriter, request)
				return
			}
			request = routecontext.Begin(request)
			state := captureServerRequestState(request)
			ctx := context.WithValue(request.Context(), serverRequestTargetKey{}, &state)

			sanitisedRequest := request.Clone(ctx)
			applySanitisedTarget(sanitisedRequest, matchedRouteTemplate(request))
			sanitiseServerRequestMetadata(sanitisedRequest)
			sanitisedRequest.Body = transformHTTPBody(sanitisedRequest.Body, sanitiseHTTPIOError)
			instrumentedHandler.ServeHTTP(transformHTTPWriter(responseWriter, sanitiseHTTPIOError), sanitisedRequest)

			if sanitisedRequest.MultipartForm != nil {
				request.MultipartForm = sanitisedRequest.MultipartForm
			}
		})
	}
}

// captureServerRequestState preserves application-visible request metadata.
func captureServerRequestState(request *http.Request) serverRequestState {
	return serverRequestState{
		target:     captureRequestTarget(request),
		header:     request.Header.Clone(),
		remoteAddr: request.RemoteAddr,
		host:       request.Host,
		method:     request.Method,
	}
}

// sanitiseServerRequestMetadata removes client-controlled transport metadata
// and bounds non-standard HTTP method cardinality.
func sanitiseServerRequestMetadata(request *http.Request) {
	request.RemoteAddr = ""
	request.Host = ""
	request.Method = telemetrySafeHTTPMethod(request.Method)
	if request.URL != nil {
		request.URL.Host = ""
	}
	request.Header = request.Header.Clone()
	for _, header := range clientIdentityHeaders {
		request.Header.Del(header)
	}
}

// restoreServerRequestMetadata restores metadata hidden from instrumentation.
func restoreServerRequestMetadata(request *http.Request, state serverRequestState) {
	request.Header = state.header.Clone()
	request.RemoteAddr = state.remoteAddr
	request.Host = state.host
	request.Method = state.method
}

// captureRequestTarget copies request-target fields before redaction.
func captureRequestTarget(request *http.Request) requestTarget {
	return requestTarget{
		url:        cloneURL(request.URL),
		requestURI: request.RequestURI,
		pattern:    request.Pattern,
	}
}

// applyRequestTarget restores captured request-target fields.
func applyRequestTarget(request *http.Request, target requestTarget) {
	request.URL = cloneURL(target.url)
	request.RequestURI = target.requestURI
	request.Pattern = target.pattern
}

// applySanitisedTarget replaces request-target fields with telemetry-safe values.
func applySanitisedTarget(request *http.Request, routeTemplate string) {
	request.URL = sanitisedURL(request.URL)
	request.RequestURI = redactedURLPath
	request.Pattern = routeTemplate
}

// sanitisedURL returns a copy containing only a constant path and safe origin data.
func sanitisedURL(original *url.URL) *url.URL {
	if original == nil {
		return nil
	}

	sanitised := *original
	sanitised.User = nil
	sanitised.Opaque = ""
	sanitised.Path = redactedURLPath
	sanitised.RawPath = ""
	sanitised.ForceQuery = false
	sanitised.RawQuery = ""
	sanitised.Fragment = ""
	sanitised.RawFragment = ""
	return &sanitised
}

// cloneURL returns an independent copy of a URL.
func cloneURL(original *url.URL) *url.URL {
	if original == nil {
		return nil
	}

	cloned := *original
	return &cloned
}

// matchedRouteTemplate returns the stable mux or net/http route pattern.
func matchedRouteTemplate(request *http.Request) string {
	if route := mux.CurrentRoute(request); route != nil {
		if template, err := route.GetPathTemplate(); err == nil && template != "" {
			return template
		}
	}

	return request.Pattern
}

// serverSpanName derives a low-cardinality span name from method and route.
func serverSpanName(_ string, request *http.Request) string {
	method := telemetrySafeHTTPMethod(request.Method)
	if method == "OTHER" {
		method = "HTTP"
	}

	if request.Pattern == "" {
		return method
	}
	return method + " " + request.Pattern
}

// telemetrySafeHTTPMethod preserves standard methods and maps every other
// caller-controlled value to one stable fallback.
func telemetrySafeHTTPMethod(method string) string {
	method = strings.ToUpper(method)
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodConnect,
		http.MethodOptions,
		http.MethodTrace:
		return method
	default:
		return "OTHER"
	}
}

// HTTPMethodForTelemetry returns a standard uppercase HTTP method or OTHER.
// Use it in request logs and metrics to keep unknown caller-supplied methods
// bounded and consistent with the HTTP instrumentation's privacy policy.
func HTTPMethodForTelemetry(method string) string {
	return telemetrySafeHTTPMethod(method)
}
