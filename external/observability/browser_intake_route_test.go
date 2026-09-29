package observability_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestBrowserIntakeMountPreservesCanonicalRoutingAndOuterLogs(t *testing.T) {
	const path = "/telemetry/traces"
	var received string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.URL.RequestURI()
		w.WriteHeader(418)
	})
	intake := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(405)
			return
		}
		w.WriteHeader(202)
	})
	mounted, err := observability.MountBrowserTraceIntake(next, intake, path)
	require.NoError(t, err)
	core, logs := observer.New(zap.InfoLevel)
	handler := otelhttp.Wrap("example", zap.New(core), mounted)
	for _, target := range []string{path, path + "?private=canary", "/telemetry/%74races", path + "/", "/telemetry/../telemetry/traces", "/telemetry//traces", "/elsewhere?private=canary"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", target, nil))
		if target == path || target == path+"?private=canary" {
			assert.Equal(t, 202, response.Code)
		} else {
			assert.Equal(t, 418, response.Code)
			assert.Equal(t, target, received, "fallback sees the original request target")
		}
		assert.Empty(t, response.Header().Get("Location"), "the mount does not clean/redirect aliases")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	assert.Equal(t, 405, response.Code, "method handling belongs to intake")
	entries := logs.FilterMessage("http request completed").All()
	require.Len(t, entries, 8)
	assert.Equal(t, path, entries[0].ContextMap()["route"])
	assert.Equal(t, path, entries[1].ContextMap()["route"])
	assert.Equal(t, "unknown", entries[2].ContextMap()["route"])
	for _, entry := range entries {
		encoded, err := json.Marshal(entry.ContextMap())
		require.NoError(t, err)
		assert.NotContains(t, string(encoded), "canary", "neither intake nor fallback logs may expose queries")
	}
}

func TestBrowserIntakeMountValidationAndDisabledIdentity(t *testing.T) {
	next := http.NewServeMux()
	for _, path := range []string{"", "/", "/private-canary/{id}", "/private-canary?token=value", "/private-canary/%2f", "/private-canary/../traces"} {
		_, err := observability.MountBrowserTraceIntake(next, next, path)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), "private-canary")
		mounted, err := observability.MountBrowserTraceIntake(next, nil, path)
		require.NoError(t, err)
		assert.Same(t, next, mounted)
	}
	_, err := observability.MountBrowserTraceIntake(nil, next, "/telemetry/traces")
	require.Error(t, err)
	var disabled *observability.BrowserTraceIntake
	mounted, err := observability.MountBrowserTraceIntake(next, disabled, "unused-disabled-path")
	require.NoError(t, err)
	assert.Same(t, next, mounted)
}
