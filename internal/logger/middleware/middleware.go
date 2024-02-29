package middleware

import (
	"net/http"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Middleware of logger
type Middleware struct {
	logger *zap.Logger
}

// NewLogger returns middleware
func NewLogger(logger *zap.Logger) *Middleware {
	return &Middleware{logger: logger}
}

// getOrCreateCorrelationId attempts to pull correlation Id from request header, if exists.
// If correlation Id is not present, a new Id will be generated
func getOrCreateCorrelationId(req *http.Request) string {
	correlationId := req.Header.Get("X-Correlation-Id")
	if correlationId == "" {
		return uuid.New().String()
	}
	return correlationId
}

// HTTPLogger creates a middleware that affixes the correlation Id to the logger, and as well logs
// request specific data
func (m *Middleware) HTTPLogger(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {

		reqLogger := m.logger.With(zap.String("correlation-id", getOrCreateCorrelationId(req)))
		//nolint Sync the request logger
		defer reqLogger.Sync()

		request := req.WithContext(logger.TransitWith(req.Context(), reqLogger))

		responseWriter := middlewareResponseWriter(w)
		handler.ServeHTTP(responseWriter, request)

		// Log request data
		reqLogger.Info("request",
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
