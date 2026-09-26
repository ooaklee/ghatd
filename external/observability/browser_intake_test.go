package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const browserCanary = "browser-secret-canary@example.invalid"

func browserFixtureSpan(now time.Time, name string) map[string]any {
	span := map[string]any{"name": name, "traceId": "0102030405060708090a0b0c0d0e0f10", "spanId": "0102030405060708", "parentSpanId": "1112131415161718", "kind": 1, "startTimeUnixNano": fmt.Sprint(now.Add(-time.Second).UnixNano()), "endTimeUnixNano": fmt.Sprint(now.UnixNano()), "status": map[string]any{"code": 0}, "attributes": []any{}}
	attributes := []any{browserFixtureAttribute("browser.route.group", "stringValue", "home")}
	switch name {
	case "browser.navigation":
		attributes = append(attributes, browserFixtureAttribute("browser.outcome", "stringValue", "complete"))
	case "browser.request":
		attributes = append(attributes, browserFixtureAttribute("browser.outcome", "stringValue", "success"), browserFixtureAttribute("http.request.method", "stringValue", "GET"), browserFixtureAttribute("http.response.status_code", "intValue", 200), browserFixtureAttribute("browser.api.group", "stringValue", "vehicles"))
	case "browser.error":
		attributes = append(attributes, browserFixtureAttribute("browser.error.source", "stringValue", "vue"), browserFixtureAttribute("error.type", "stringValue", "type-error"))
	case "browser.document":
		attributes = append(attributes, browserFixtureAttribute("browser.document.ttfb_ms", "doubleValue", 12.5), browserFixtureAttribute("browser.document.load_ms", "intValue", 24))
	}
	span["attributes"] = attributes
	return span
}

func browserFixtureAttribute(key, valueType string, value any) map[string]any {
	return map[string]any{"key": key, "value": map[string]any{valueType: value}}
}

func browserFixtureBody(t *testing.T, spans ...map[string]any) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{"resourceSpans": []any{map[string]any{"resource": map[string]any{"attributes": []any{browserFixtureAttribute("service.name", "stringValue", browserCanary), browserFixtureAttribute("service.instance.id", "stringValue", browserCanary)}}, "schemaUrl": browserCanary, "scopeSpans": []any{map[string]any{"scope": map[string]any{"name": browserCanary}, "schemaUrl": browserCanary, "spans": spans}}}}})
	require.NoError(t, err)
	return body
}

func browserFixtureIntake(t *testing.T, endpoint string, customize ...func(*BrowserTraceIntakeConfig)) *BrowserTraceIntake {
	t.Helper()
	clearRuntimeOTELTestEnvironment(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", endpoint)
	t.Setenv("OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	config := BrowserTraceIntakeConfig{ServiceName: "example-web", Namespace: "example", Environment: "testing", Version: "fixture", AllowedOrigins: []string{"https://example.test"}, RouteGroups: []string{"home"}, APIGroups: []string{"vehicles"}, MeterProvider: noop.NewMeterProvider()}
	for _, change := range customize {
		change(&config)
	}
	intake, err := NewBrowserTraceIntake(config)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, intake.Shutdown(context.Background())) })
	return intake
}

func TestBrowserIntakeRebuildsAllNamesAndDiscardsUntrustedMetadata(t *testing.T) {
	intake := browserFixtureIntake(t, "http://127.0.0.1:4318")
	now := time.Now()
	for _, name := range []string{"browser.navigation", "browser.document", "browser.request", "browser.error"} {
		t.Run(name, func(t *testing.T) {
			span := browserFixtureSpan(now, name)
			span["kind"] = 5 // Valid input enum is normalized by the fixed name.
			span["status"] = map[string]any{"code": 2, "message": browserCanary}
			span["traceState"], span["events"], span["links"] = browserCanary, []any{map[string]any{"name": browserCanary}}, []any{map[string]any{"traceState": browserCanary}}
			span["attributes"] = append(span["attributes"].([]any), browserFixtureAttribute("url.full", "stringValue", browserCanary))
			batch, err := intake.decode(browserFixtureBody(t, span), now)
			require.NoError(t, err)
			encoded, err := proto.Marshal(batch)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), browserCanary)
			resource := batch.ResourceSpans[0]
			require.Len(t, resource.Resource.Attributes, 4)
			require.Empty(t, resource.SchemaUrl)
			require.Equal(t, browserIntakeScope, resource.ScopeSpans[0].Scope.Name)
			output := resource.ScopeSpans[0].Spans[0]
			require.Equal(t, []byte{1, 2, 3, 4, 5, 6, 7, 8}, output.SpanId)
			require.Equal(t, []byte{17, 18, 19, 20, 21, 22, 23, 24}, output.ParentSpanId)
			require.Equal(t, uint32(1), output.Flags)
			require.Empty(t, output.Status.Message)
			require.Empty(t, output.Events)
			require.Empty(t, output.Links)
			if name == "browser.error" {
				require.Equal(t, tracepb.Status_STATUS_CODE_ERROR, output.Status.Code)
			} else {
				require.Equal(t, tracepb.Status_STATUS_CODE_UNSET, output.Status.Code)
			}
			if name == "browser.request" {
				require.Equal(t, tracepb.Span_SPAN_KIND_CLIENT, output.Kind)
			} else {
				require.Equal(t, tracepb.Span_SPAN_KIND_INTERNAL, output.Kind)
			}
		})
	}
}

func TestBrowserIntakeRejectsMalformedBatchesAtomically(t *testing.T) {
	intake := browserFixtureIntake(t, "http://127.0.0.1:4318")
	now := time.Now()
	cases := map[string]func(map[string]any){
		"name":           func(s map[string]any) { s["name"] = browserCanary },
		"zero trace":     func(s map[string]any) { s["traceId"] = strings.Repeat("0", 32) },
		"base64 trace":   func(s map[string]any) { s["traceId"] = "AQIDBAUGBwgJCgsMDQ4PEA==" },
		"short span":     func(s map[string]any) { s["spanId"] = "01" },
		"zero parent":    func(s map[string]any) { s["parentSpanId"] = strings.Repeat("0", 16) },
		"self parent":    func(s map[string]any) { s["parentSpanId"] = s["spanId"] },
		"numeric time":   func(s map[string]any) { s["endTimeUnixNano"] = now.UnixNano() },
		"negative time":  func(s map[string]any) { s["startTimeUnixNano"] = "-1" },
		"duration":       func(s map[string]any) { s["startTimeUnixNano"] = fmt.Sprint(now.Add(-121 * time.Second).UnixNano()) },
		"backwards time": func(s map[string]any) { s["startTimeUnixNano"] = fmt.Sprint(now.Add(time.Second).UnixNano()) },
		"past": func(s map[string]any) {
			s["startTimeUnixNano"], s["endTimeUnixNano"] = fmt.Sprint(now.Add(-11*time.Minute).UnixNano()), fmt.Sprint(now.Add(-11*time.Minute).UnixNano())
		},
		"future": func(s map[string]any) {
			s["startTimeUnixNano"], s["endTimeUnixNano"] = fmt.Sprint(now.Add(3*time.Minute).UnixNano()), fmt.Sprint(now.Add(3*time.Minute).UnixNano())
		},
		"kind type":       func(s map[string]any) { s["kind"] = "1" },
		"kind enum":       func(s map[string]any) { s["kind"] = 9 },
		"status enum":     func(s map[string]any) { s["status"] = map[string]any{"code": 3} },
		"missing outcome": func(s map[string]any) { s["attributes"] = []any{} },
		"unknown outcome": func(s map[string]any) {
			s["attributes"] = []any{browserFixtureAttribute("browser.outcome", "stringValue", browserCanary)}
		},
		"duplicate attribute": func(s map[string]any) {
			s["attributes"] = append(s["attributes"].([]any), browserFixtureAttribute("browser.outcome", "stringValue", "complete"))
		},
		"unknown attribute duplicate": func(s map[string]any) {
			s["attributes"] = append(s["attributes"].([]any), browserFixtureAttribute("secret", "stringValue", browserCanary), browserFixtureAttribute("secret", "stringValue", browserCanary))
		},
		"group type": func(s map[string]any) {
			s["attributes"].([]any)[0] = browserFixtureAttribute("browser.route.group", "intValue", 1)
		},
		"attribute bound": func(s map[string]any) {
			for index := 0; index < 15; index++ {
				s["attributes"] = append(s["attributes"].([]any), browserFixtureAttribute(fmt.Sprint(index), "stringValue", browserCanary))
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			span := browserFixtureSpan(now, "browser.navigation")
			mutate(span)
			batch, err := intake.decode(browserFixtureBody(t, span), now)
			require.ErrorIs(t, err, errBrowserBatch)
			require.Nil(t, batch)
			require.NotContains(t, err.Error(), browserCanary)
		})
	}
	valid := browserFixtureBody(t, browserFixtureSpan(now, "browser.navigation"))
	for name, body := range map[string][]byte{"duplicate field": bytesReplace(valid, `"name":"browser.navigation"`, `"name":"browser.navigation","name":"browser.navigation"`), "trailing": append(append([]byte{}, valid...), []byte(` {}`)...), "utf8": append(append([]byte{}, valid...), 255), "oversize": []byte(strings.Repeat(" ", browserIntakeMaximumBytes+1)), "null": []byte(`null`)} {
		t.Run(name, func(t *testing.T) { _, err := intake.decode(body, now); require.ErrorIs(t, err, errBrowserBatch) })
	}
	var spans []map[string]any
	for index := 0; index < 17; index++ {
		span := browserFixtureSpan(now, "browser.navigation")
		span["spanId"] = fmt.Sprintf("%016x", index+1)
		spans = append(spans, span)
	}
	_, err := intake.decode(browserFixtureBody(t, spans...), now)
	require.Error(t, err)
	_, err = intake.decode(browserFixtureBody(t, spans[0], spans[0]), now)
	require.Error(t, err)
	spans[1]["name"] = browserCanary
	batch, err := intake.decode(browserFixtureBody(t, spans[:2]...), now)
	require.Error(t, err)
	require.Nil(t, batch)
}

func bytesReplace(value []byte, old, next string) []byte {
	return []byte(strings.Replace(string(value), old, next, 1))
}

func TestBrowserIntakeRequestAndDocumentValueValidation(t *testing.T) {
	intake := browserFixtureIntake(t, "http://127.0.0.1:4318")
	now := time.Now()
	for _, name := range []string{"browser.request", "browser.document", "browser.error"} {
		span := browserFixtureSpan(now, name)
		if name == "browser.request" {
			span["attributes"] = []any{browserFixtureAttribute("http.request.method", "stringValue", "GET")}
		} else if name == "browser.document" {
			span["attributes"] = []any{browserFixtureAttribute("browser.document.ttfb_ms", "doubleValue", 120001)}
		} else {
			span["attributes"] = []any{browserFixtureAttribute("browser.error.source", "stringValue", "vue"), browserFixtureAttribute("error.type", "stringValue", browserCanary)}
		}
		_, err := intake.decode(browserFixtureBody(t, span), now)
		require.Error(t, err, name)
	}
	span := browserFixtureSpan(now, "browser.request")
	span["attributes"] = []any{browserFixtureAttribute("http.request.method", "stringValue", "OTHER"), browserFixtureAttribute("browser.outcome", "stringValue", "timeout"), browserFixtureAttribute("browser.route.group", "stringValue", browserCanary)}
	batch, err := intake.decode(browserFixtureBody(t, span), now)
	require.NoError(t, err)
	output := batch.ResourceSpans[0].ScopeSpans[0].Spans[0]
	require.Equal(t, tracepb.Status_STATUS_CODE_ERROR, output.Status.Code)
	require.Equal(t, "other", output.Attributes[0].Value.GetStringValue())
	require.Equal(t, "other", output.Attributes[1].Value.GetStringValue())
}

func TestBrowserIntakeConstructorIsImmutableAndDoesNotChangeGlobals(t *testing.T) {
	clearRuntimeOTELTestEnvironment(t)
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:4318")
	tracer, meter, logger := otel.GetTracerProvider(), otel.GetMeterProvider(), otel.GetTextMapPropagator()
	origins, routes := []string{"https://example.test"}, []string{"home"}
	intake, err := NewBrowserTraceIntake(BrowserTraceIntakeConfig{ServiceName: "example-web", AllowedOrigins: origins, RouteGroups: routes})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, intake.Shutdown(context.Background())) })
	origins[0], routes[0] = browserCanary, browserCanary
	require.Contains(t, intake.origins, "https://example.test")
	require.Contains(t, intake.routes, "home")
	require.NotContains(t, intake.origins, browserCanary)
	require.Len(t, intake.resource.Attributes, 1)
	require.Same(t, tracer, otel.GetTracerProvider())
	require.Same(t, meter, otel.GetMeterProvider())
	require.Equal(t, logger, otel.GetTextMapPropagator())
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:9999")
	require.Equal(t, "127.0.0.1:4318", intake.sender.settings.endpoint.Host)
	for _, change := range []func(*BrowserTraceIntakeConfig){func(c *BrowserTraceIntakeConfig) { c.ServiceName = "" }, func(c *BrowserTraceIntakeConfig) { c.AllowedOrigins = []string{"*"} }, func(c *BrowserTraceIntakeConfig) {
		c.AllowedOrigins = []string{"https://user:" + browserCanary + "@example.test"}
	}, func(c *BrowserTraceIntakeConfig) { c.RouteGroups = []string{browserCanary} }, func(c *BrowserTraceIntakeConfig) { c.MaxConcurrent = 33 }, func(c *BrowserTraceIntakeConfig) { c.Timeout = -time.Second }, func(c *BrowserTraceIntakeConfig) { c.Burst = 129 }, func(c *BrowserTraceIntakeConfig) { c.BatchesPerMinute = 601 }, func(c *BrowserTraceIntakeConfig) { c.Version = browserCanary + "\n" }} {
		config := BrowserTraceIntakeConfig{ServiceName: "example-web", AllowedOrigins: []string{"https://example.test"}}
		change(&config)
		_, err := NewBrowserTraceIntake(config)
		require.Error(t, err)
		require.NotContains(t, err.Error(), browserCanary)
	}
}

func TestBrowserIntakeUnsupportedReadDeadlineReturnsEmptyUnavailable(t *testing.T) {
	intake := browserFixtureIntake(t, "http://127.0.0.1:4318")
	request := httptest.NewRequest(http.MethodPost, "http://example.test/intake", strings.NewReader(string(browserFixtureBody(t, browserFixtureSpan(time.Now(), "browser.navigation")))))
	request.Header.Set("Origin", "https://example.test")
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	intake.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	require.Empty(t, recorder.Body.String())
	require.True(t, request.Close)
}

func TestBrowserIntakeCanonicalOrigin(t *testing.T) {
	for _, origin := range []string{"https://example.test", "http://localhost:5173", "http://127.0.0.1:8080", "http://[::1]:8080", "https://[2001:db8::1]"} {
		require.True(t, browserCanonicalOrigin(origin), origin)
	}
	for _, origin := range []string{"https://*.example", "https://example:", "https://[invalid]", "https://[127.0.0.1]", "http://::1", "https://example.test:00443", "https://example.test:443", "http://example.test:80", "https://example.test:0", "https://example.test:65536", "https://example..test", "https://EXAMPLE.test", "https://example.test/", "http://127.1", "https://example_test", "null", "https://example.test?secret=value"} {
		require.False(t, browserCanonicalOrigin(origin), origin)
	}
}
