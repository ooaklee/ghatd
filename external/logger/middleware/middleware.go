package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/toolbox"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

const unknownRoute = "unknown"

// Middleware of logger
type Middleware struct {
	logger *zap.Logger

	// uriIgnoreList is the list of URIs that should not be logged when a
	// request completes.
	uriIgnoreList []string
}

// NewLogger returns request-logging middleware.
func NewLogger(logger *zap.Logger, uriIgnoreList []string) *Middleware {
	return &Middleware{
		logger:        logger,
		uriIgnoreList: uriIgnoreList,
	}
}

// getOrCreateCorrelationId preserves canonical UUIDv4 request IDs and replaces
// every other value. Correlation IDs are exported to logs and traces, so
// accepting arbitrary caller-controlled values would leak sensitive data and
// create unbounded telemetry cardinality.
func getOrCreateCorrelationId(req *http.Request) string {
	correlationID := strings.TrimSpace(req.Header.Get(common.CorrelationIdHttpHeader))
	parsed, err := uuid.Parse(correlationID)
	if err == nil && parsed.Version() == uuid.Version(4) && strings.EqualFold(correlationID, parsed.String()) {
		return parsed.String()
	}

	return toolbox.GenerateUuidV4()
}

// HTTPLogger is a middleware that adds correlation ID tracking and logging to HTTP requests.
// It generates or retrieves a correlation ID, attaches it to the request context and response headers,
// creates a logger with the correlation ID, and logs request details after the handler completes.
func (m *Middleware) HTTPLogger(handler http.Handler) http.Handler {
	return m.httpLogger(handler, false)
}

// HTTPLoggerWithCustomUriIgnoreList is a middleware that adds correlation ID tracking and logging to HTTP requests
// with the ability to ignore specific URIs from logging. It generates or retrieves a correlation ID,
// attaches it to the request context and response headers, creates a logger with the correlation ID,
// and logs request details after the handler completes, skipping logging for URIs in the ignore list.
func (m *Middleware) HTTPLoggerWithCustomUriIgnoreList(handler http.Handler) http.Handler {
	return m.httpLogger(handler, true)
}

// httpLogger builds the request logger with optional route suppression.
func (m *Middleware) httpLogger(handler http.Handler, useIgnoreList bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		fetchedCorrelationId := getOrCreateCorrelationId(req)
		w.Header().Add(common.CorrelationIdHttpHeader, fetchedCorrelationId)

		// Attach the correlation ID to the request context.
		requestContext := toolbox.TransitWithCtxByKey[string](req.Context(), toolbox.CtxKeyCorrelationId, fetchedCorrelationId)
		trace.SpanFromContext(requestContext).SetAttributes(attribute.String("correlation.id", fetchedCorrelationId))

		// Attach the active OTel context and stable correlation fields to every
		// logger acquired downstream from this request.
		reqLogger := observability.WithTraceContext(requestContext, m.logger).With(zap.String("correlation-id", fetchedCorrelationId))
		//nolint Sync the request logger
		defer reqLogger.Sync()

		request := req.WithContext(logger.TransitWith(requestContext, reqLogger))

		responseWriter := middlewareResponseWriter(w)
		handler.ServeHTTP(responseWriter, request)

		route := routeTemplate(req)
		if useIgnoreList && m.shouldIgnore(req, route) {
			return
		}

		reqLogger.Info(
			"http request completed",
			zap.String(logger.FieldSource, logger.SourceGHATD),
			zap.String(logger.FieldPackage, "external/logger/middleware"),
			zap.String(logger.FieldOperation, "http-request"),
			zap.Int("status", responseWriter.statusCode),
			zap.String("method", req.Method),
			zap.String("route", route),
		)
	})
}

// shouldIgnore reports whether the request path or route is suppressed.
func (m *Middleware) shouldIgnore(req *http.Request, route string) bool {
	if len(m.uriIgnoreList) == 0 {
		return false
	}

	return slices.Contains(m.uriIgnoreList, route) || slices.Contains(m.uriIgnoreList, req.URL.Path)
}

// routeTemplate returns the matched route pattern or a stable fallback.
func routeTemplate(req *http.Request) string {
	route := mux.CurrentRoute(req)
	if route == nil {
		return unknownRoute
	}

	template, err := route.GetPathTemplate()
	if err != nil || template == "" {
		return unknownRoute
	}
	return template
}

type httpResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// middlewareResponseWriter records an implicit 200 OK when a handler does not
// call WriteHeader.
func middlewareResponseWriter(w http.ResponseWriter) *httpResponseWriter {
	return &httpResponseWriter{w, http.StatusOK}
}

// WriteHeader records the response status before forwarding it.
func (lrw *httpResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}
