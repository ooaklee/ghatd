package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/toolbox"
)

func TestMiddleware_HTTPLogger(t *testing.T) {

	type expectedResponseAttributes struct {
		Status        int64
		URI           string
		Verb          string
		CorrelationId string
	}

	const (
		correlationId string = "fbd4046f-0f1c-4f98-b71c-d4cd61443f90"
		URI           string = "/v1/services?rand=true"
	)

	tests := []struct {
		name                   string
		clientReqCorrelationId string
		clientReqURI           string
		expectedResponseAttr   *expectedResponseAttributes
	}{
		{
			name:                   "Successful",
			clientReqCorrelationId: correlationId,
			clientReqURI:           URI,
			expectedResponseAttr: &expectedResponseAttributes{
				Status:        int64(400),
				URI:           URI,
				Verb:          "GET",
				CorrelationId: correlationId,
			},
		},
		{
			name:                   "Successful - generating correlation Id",
			clientReqCorrelationId: "",
			clientReqURI:           URI,
			expectedResponseAttr: &expectedResponseAttributes{
				Status: int64(400),
				URI:    URI,
				Verb:   "GET",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			core, watcher := observer.New(zapcore.InfoLevel)
			logger := zap.New(core)

			request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("http://localhost%s", tt.clientReqURI), nil)
			request.Header.Set("X-Correlation-Id", tt.clientReqCorrelationId)
			response := httptest.NewRecorder()
			handler := middleware.NewLogger(logger).HTTPLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))

			handler.ServeHTTP(response, request)

			t.Log(watcher.All()[0].ContextMap())

			assert.Equal(t, 1, len(watcher.All()))
			logs := watcher.All()

			responseCorrelationId := logs[0].ContextMap()["correlation-id"]

			if tt.expectedResponseAttr.CorrelationId != "" {
				assert.Equal(t, tt.expectedResponseAttr.CorrelationId, responseCorrelationId)
			}
			assert.Equal(t, tt.expectedResponseAttr.URI, logs[0].ContextMap()["uri"])
			assert.Equal(t, tt.expectedResponseAttr.Verb, logs[0].ContextMap()["method"])
			assert.Equal(t, tt.expectedResponseAttr.Status, logs[0].ContextMap()["status"])

			// Check Uuid is valid
			assert.Regexpf(t, regexp.MustCompile(toolbox.UuidV4Regex), responseCorrelationId, "returned correlation Id not correctly formatted: %s", responseCorrelationId)
		})
	}

}
