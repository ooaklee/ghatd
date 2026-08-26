package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ooaklee/ghatd/external/common"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// TestMiddlewareHTTPLoggerUsesRouteTemplateAndTraceCorrelation verifies safe request logging and span correlation.
func TestMiddlewareHTTPLoggerUsesRouteTemplateAndTraceCorrelation(t *testing.T) {
	const suppliedCorrelationID = "fbd4046f-0f1c-4f98-b71c-d4cd61443f90"

	tests := []struct {
		name                   string
		correlationID          string
		preservesCorrelationID bool
	}{
		{name: "uses supplied correlation ID", correlationID: suppliedCorrelationID, preservesCorrelationID: true},
		{name: "generates correlation ID"},
		{name: "replaces non UUID correlation ID", correlationID: "driver@example.com/raw-secret"},
		{name: "replaces non v4 UUID correlation ID", correlationID: "6ba7b810-9dad-11d1-80b4-00c04fd430c8"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, observed := observer.New(zapcore.InfoLevel)
			baseLogger := zap.New(core)

			spanRecorder := tracetest.NewSpanRecorder()
			tracerProvider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
			t.Cleanup(func() { require.NoError(t, tracerProvider.Shutdown(context.Background())) })
			ctx, span := tracerProvider.Tracer("middleware-test").Start(context.Background(), "request")

			request := httptest.NewRequest(http.MethodGet, "http://localhost/v1/vehicles/AB12CDE?token=raw-secret", nil).WithContext(ctx)
			request.RemoteAddr = "192.0.2.1:1234"
			request.Header.Set("X-Forwarded-For", "192.0.2.2")
			request.Header.Set("User-Agent", "privacy-sensitive-client")
			if tt.correlationID != "" {
				request.Header.Set(common.CorrelationIdHttpHeader, tt.correlationID)
			}

			response := httptest.NewRecorder()
			router := mux.NewRouter()
			router.Handle("/v1/vehicles/{vrn}", middleware.NewLogger(baseLogger, nil).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ghatdlogger.AcquireFrom(req.Context()).Info("handler event")
				w.WriteHeader(http.StatusBadRequest)
			}))).Methods(http.MethodGet)

			router.ServeHTTP(response, request)
			span.End()

			require.Equal(t, http.StatusBadRequest, response.Code)
			responseCorrelationID := response.Header().Get(common.CorrelationIdHttpHeader)
			if tt.preservesCorrelationID {
				assert.Equal(t, tt.correlationID, responseCorrelationID)
			} else if tt.correlationID != "" {
				assert.NotEqual(t, tt.correlationID, responseCorrelationID)
			}
			assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), responseCorrelationID)

			entries := observed.All()
			require.Len(t, entries, 2)
			for _, entry := range entries {
				fields := entry.ContextMap()
				assert.Equal(t, responseCorrelationID, fields["correlation-id"])
				assert.Equal(t, span.SpanContext().TraceID().String(), fields["trace_id"])
				assert.Equal(t, span.SpanContext().SpanID().String(), fields["span_id"])
			}

			completion := entries[1]
			fields := completion.ContextMap()
			assert.Equal(t, "http request completed", completion.Message)
			assert.EqualValues(t, http.StatusBadRequest, fields["status"])
			assert.Equal(t, http.MethodGet, fields["method"])
			assert.Equal(t, "/v1/vehicles/{vrn}", fields["route"])
			assert.NotContains(t, fields, "uri")
			assert.NotContains(t, fields, "clientip")
			assert.NotContains(t, fields, "forwarded-for")
			assert.NotContains(t, fields, "host")
			assert.NotContains(t, fields, "user-agent")
			assert.NotContains(t, completion.Message, "AB12CDE")
			assert.NotContains(t, completion.Message, "raw-secret")

			ended := spanRecorder.Ended()
			require.Len(t, ended, 1)
			spanAttributes := make(map[string]string)
			for _, spanAttribute := range ended[0].Attributes() {
				spanAttributes[string(spanAttribute.Key)] = spanAttribute.Value.AsString()
			}
			assert.Equal(t, responseCorrelationID, spanAttributes["correlation.id"])
		})
	}
}

// TestMiddlewareHTTPLoggerCustomIgnoreListMatchesPathOrRoute verifies ignore entries accept paths and route templates.
func TestMiddlewareHTTPLoggerCustomIgnoreListMatchesPathOrRoute(t *testing.T) {
	core, observed := observer.New(zapcore.InfoLevel)
	baseLogger := zap.New(core)

	tests := []struct {
		name       string
		ignoreList []string
	}{
		{name: "path", ignoreList: []string{"/health/ready"}},
		{name: "route template", ignoreList: []string{"/health/{probe}"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observed.TakeAll()
			request := httptest.NewRequest(http.MethodGet, "http://localhost/health/ready?verbose=true", nil)
			response := httptest.NewRecorder()

			router := mux.NewRouter()
			router.Handle("/health/{probe}", middleware.NewLogger(baseLogger, tt.ignoreList).HTTPLoggerWithCustomUriIgnoreList(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))).Methods(http.MethodGet)

			router.ServeHTTP(response, request)
			assert.Empty(t, observed.All())
		})
	}
}
