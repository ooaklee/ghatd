package middleware

import (
	"fmt"
	"net/http"
	"slices"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"

	"go.uber.org/zap"
)

// Middleware of logger
type Middleware struct {
	logger *zap.Logger

	// uriIgnoreList the list of Uris that should not be logged on completion
	// of request
	uriIgnoreList []string
}

// NewLogger returns middleware
func NewLogger(logger *zap.Logger, uriIgnoreList []string) *Middleware {
	return &Middleware{
		logger:        logger,
		uriIgnoreList: uriIgnoreList,
	}
}

// getOrCreateCorrelationId attempts to pull correlation Id from request header, if exists.
// If correlation Id is not present, a new Id will be generated
func getOrCreateCorrelationId(req *http.Request) string {
	correlationId := req.Header.Get(common.CorrelationIdHttpHeader)
	if correlationId == "" {
		return toolbox.GenerateUuidV4()
	}
	return correlationId
}

// HTTPLogger is a middleware that adds correlation ID tracking and logging to HTTP requests.
// It generates or retrieves a correlation ID, attaches it to the request context and response headers,
// creates a logger with the correlation ID, and logs request details after the handler completes.
func (m *Middleware) HTTPLogger(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		fetchedCorrelationId := getOrCreateCorrelationId(req)
		w.Header().Add(common.CorrelationIdHttpHeader, fetchedCorrelationId)

		// attach correlation id to request context
		req = req.WithContext(toolbox.TransitWithCtxByKey[string](req.Context(), toolbox.CtxKeyCorrelationId, fetchedCorrelationId))

		// attach correlation id to logger
		reqLogger := m.logger.With(zap.String("correlation-id", fetchedCorrelationId))
		//nolint Sync the request logger
		defer reqLogger.Sync()

		request := req.WithContext(logger.TransitWith(req.Context(), reqLogger))

		responseWriter := middlewareResponseWriter(w)
		handler.ServeHTTP(responseWriter, request)

		// Log request data
		reqLogger.Info(
			fmt.Sprintf("concluded request for %s [correlation-id: %s]", req.URL.RequestURI(), fetchedCorrelationId),
			zap.Int("status", responseWriter.statusCode),
			zap.String("method", req.Method),
			zap.String("clientip", req.RemoteAddr),
			zap.String("forwarded-for", req.Header.Get("X-Forwarded-For")),
			zap.String("host", req.Host),
			zap.String("uri", req.URL.RequestURI()),
			zap.String("user-agent", req.UserAgent()),
		)
	})
}

// HTTPLoggerWithCustomUriIgnoreList is a middleware that adds correlation ID tracking and logging to HTTP requests
// with the ability to ignore specific URIs from logging. It generates or retrieves a correlation ID,
// attaches it to the request context and response headers, creates a logger with the correlation ID,
// and logs request details after the handler completes, skipping logging for URIs in the ignore list.
func (m *Middleware) HTTPLoggerWithCustomUriIgnoreList(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		fetchedCorrelationId := getOrCreateCorrelationId(req)
		w.Header().Add(common.CorrelationIdHttpHeader, fetchedCorrelationId)

		// attach correlation id to request context
		req = req.WithContext(toolbox.TransitWithCtxByKey[string](req.Context(), toolbox.CtxKeyCorrelationId, fetchedCorrelationId))

		// attach correlation id to logger
		reqLogger := m.logger.With(zap.String("correlation-id", fetchedCorrelationId))
		//nolint Sync the request logger
		defer reqLogger.Sync()

		request := req.WithContext(logger.TransitWith(req.Context(), reqLogger))

		responseWriter := middlewareResponseWriter(w)
		handler.ServeHTTP(responseWriter, request)

		// handle uri ignore list
		if len(m.uriIgnoreList) > 0 && slices.Contains(m.uriIgnoreList, req.URL.RequestURI()) {
			return
		}

		// Log request data
		reqLogger.Info(
			fmt.Sprintf("concluded request for %s [correlation-id: %s]", req.URL.RequestURI(), fetchedCorrelationId),
			zap.Int("status", responseWriter.statusCode),
			zap.String("method", req.Method),
			zap.String("clientip", req.RemoteAddr),
			zap.String("forwarded-for", req.Header.Get("X-Forwarded-For")),
			zap.String("host", req.Host),
			zap.String("uri", req.URL.RequestURI()),
			zap.String("user-agent", req.UserAgent()),
		)
	})
}

type httpResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

// middlewareResponseWriter handles events when responses that implicitly returns 200 OK do
// no call WriteHeader(int).
func middlewareResponseWriter(w http.ResponseWriter) *httpResponseWriter {
	return &httpResponseWriter{w, http.StatusOK}
}

func (lrw *httpResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}
