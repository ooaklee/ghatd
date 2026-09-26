package observability

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
)

var errBrowserBatch = errors.New("invalid browser trace batch")

// Decode tokens instead of maps directly: encoding/json otherwise silently
// accepts duplicate keys, including conflicting trace identities and times.
func browserJSONValue(decoder *json.Decoder, depth int) (any, error) {
	if depth > 32 {
		return nil, errBrowserBatch
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, errBrowserBatch
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return token, nil
	}
	switch delimiter {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return nil, errBrowserBatch
			}
			if _, duplicate := object[key]; duplicate {
				return nil, errBrowserBatch
			}
			value, err := browserJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return nil, errBrowserBatch
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			value, err := browserJSONValue(decoder, depth+1)
			if err != nil {
				return nil, err
			}
			array = append(array, value)
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return nil, errBrowserBatch
		}
		return array, nil
	default:
		return nil, errBrowserBatch
	}
}

func (intake *BrowserTraceIntake) decode(body []byte, now time.Time) (*collectortrace.ExportTraceServiceRequest, error) {
	if len(body) > browserIntakeMaximumBytes || !utf8.Valid(body) {
		return nil, errBrowserBatch
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	value, err := browserJSONValue(decoder, 0)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, errBrowserBatch
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil, errBrowserBatch
	}
	resources, ok := root["resourceSpans"].([]any)
	if !ok || len(resources) != 1 {
		return nil, errBrowserBatch
	}
	resource, ok := resources[0].(map[string]any)
	if !ok {
		return nil, errBrowserBatch
	}
	scopes, ok := resource["scopeSpans"].([]any)
	if !ok || len(scopes) != 1 {
		return nil, errBrowserBatch
	}
	scope, ok := scopes[0].(map[string]any)
	if !ok {
		return nil, errBrowserBatch
	}
	spans, ok := scope["spans"].([]any)
	if !ok || len(spans) < 1 || len(spans) > browserIntakeMaximumSpans {
		return nil, errBrowserBatch
	}
	output := make([]*tracepb.Span, 0, len(spans))
	identities := make(map[string]struct{}, len(spans))
	for _, value := range spans {
		span, err := intake.decodeSpan(value, now)
		if err != nil {
			return nil, err
		}
		identity := string(span.TraceId) + string(span.SpanId)
		if _, duplicate := identities[identity]; duplicate {
			return nil, errBrowserBatch
		}
		identities[identity] = struct{}{}
		output = append(output, span)
	}
	return &collectortrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{{Resource: intake.resource, ScopeSpans: []*tracepb.ScopeSpans{{Scope: &commonpb.InstrumentationScope{Name: browserIntakeScope}, Spans: output}}}}}, nil
}

func browserID(value any, size int) ([]byte, bool) {
	text, ok := value.(string)
	if !ok || len(text) != size*2 {
		return nil, false
	}
	decoded, err := hex.DecodeString(text)
	if err != nil {
		return nil, false
	}
	for _, item := range decoded {
		if item != 0 {
			return decoded, true
		}
	}
	return nil, false
}

func browserTime(value any) (uint64, bool) {
	text, ok := value.(string)
	if !ok || text == "" || strings.Trim(text, "0123456789") != "" {
		return 0, false
	}
	result, err := strconv.ParseUint(text, 10, 64)
	return result, err == nil
}

func browserInteger(value any, minimum, maximum int64) (int64, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	integer, err := number.Int64()
	return integer, err == nil && integer >= minimum && integer <= maximum
}

func (intake *BrowserTraceIntake) decodeSpan(value any, now time.Time) (*tracepb.Span, error) {
	input, ok := value.(map[string]any)
	if !ok {
		return nil, errBrowserBatch
	}
	name, ok := input["name"].(string)
	if !ok || name != "browser.navigation" && name != "browser.document" && name != "browser.request" && name != "browser.error" {
		return nil, errBrowserBatch
	}
	traceID, validTrace := browserID(input["traceId"], 16)
	spanID, validSpan := browserID(input["spanId"], 8)
	if !validTrace || !validSpan {
		return nil, errBrowserBatch
	}
	var parentID []byte
	if parent, present := input["parentSpanId"]; present {
		var valid bool
		parentID, valid = browserID(parent, 8)
		if !valid || bytes.Equal(parentID, spanID) {
			return nil, errBrowserBatch
		}
	}
	start, validStart := browserTime(input["startTimeUnixNano"])
	end, validEnd := browserTime(input["endTimeUnixNano"])
	if !validStart || !validEnd || end < start || end-start > uint64(120*time.Second) || end < uint64(now.Add(-10*time.Minute).UnixNano()) || end > uint64(now.Add(2*time.Minute).UnixNano()) {
		return nil, errBrowserBatch
	}
	if kind, present := input["kind"]; present {
		if _, valid := browserInteger(kind, 0, 5); !valid {
			return nil, errBrowserBatch
		}
	}
	if raw, present := input["status"]; present {
		status, valid := raw.(map[string]any)
		if !valid {
			return nil, errBrowserBatch
		}
		if code, present := status["code"]; present {
			if _, valid := browserInteger(code, 0, 2); !valid {
				return nil, errBrowserBatch
			}
		}
	}
	rawAttributes, ok := input["attributes"].([]any)
	if !ok || len(rawAttributes) > 16 {
		return nil, errBrowserBatch
	}
	attributes := make(map[string]map[string]any, len(rawAttributes))
	for _, raw := range rawAttributes {
		attribute, valid := raw.(map[string]any)
		if !valid {
			return nil, errBrowserBatch
		}
		key, valid := attribute["key"].(string)
		if !valid || key == "" {
			return nil, errBrowserBatch
		}
		if _, duplicate := attributes[key]; duplicate {
			return nil, errBrowserBatch
		}
		value, valid := attribute["value"].(map[string]any)
		if !valid {
			return nil, errBrowserBatch
		}
		attributes[key] = value
	}
	span := &tracepb.Span{TraceId: traceID, SpanId: spanID, ParentSpanId: parentID, Name: name, Kind: tracepb.Span_SPAN_KIND_INTERNAL, StartTimeUnixNano: start, EndTimeUnixNano: end, Flags: 1, Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_UNSET}}
	group, valid := browserGroupAttribute(attributes, "browser.route.group", intake.routes)
	if !valid {
		return nil, errBrowserBatch
	}
	span.Attributes = append(span.Attributes, browserStringAttribute("browser.route.group", group))
	var outcome string
	switch name {
	case "browser.navigation":
		outcome, valid = browserEnumAttribute(attributes, "browser.outcome", "complete", "cancelled", "redirected", "error", "timeout")
	case "browser.document":
		for _, key := range []string{"browser.document.ttfb_ms", "browser.document.dom_content_loaded_ms", "browser.document.load_ms"} {
			if raw, present := attributes[key]; present {
				if len(raw) != 1 {
					return nil, errBrowserBatch
				}
				number, ok := raw["doubleValue"].(json.Number)
				if !ok {
					number, ok = raw["intValue"].(json.Number)
					if ok {
						if _, valid := browserInteger(number, 0, 120000); !valid {
							return nil, errBrowserBatch
						}
					}
				}
				if !ok {
					return nil, errBrowserBatch
				}
				value, err := number.Float64()
				if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 120000 {
					return nil, errBrowserBatch
				}
				span.Attributes = append(span.Attributes, &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_DoubleValue{DoubleValue: value}}})
			}
		}
	case "browser.request":
		span.Kind = tracepb.Span_SPAN_KIND_CLIENT
		api, valid := browserGroupAttribute(attributes, "browser.api.group", intake.apis)
		if !valid {
			return nil, errBrowserBatch
		}
		method, valid := browserEnumAttribute(attributes, "http.request.method", "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "OTHER")
		if !valid {
			return nil, errBrowserBatch
		}
		span.Attributes = append(span.Attributes, browserStringAttribute("browser.api.group", api), browserStringAttribute("http.request.method", method))
		if raw, present := attributes["http.response.status_code"]; present {
			code, valid := browserInteger(raw["intValue"], 100, 599)
			if !valid || len(raw) != 1 {
				return nil, errBrowserBatch
			}
			span.Attributes = append(span.Attributes, &commonpb.KeyValue{Key: "http.response.status_code", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_IntValue{IntValue: code}}})
		}
		parsedOutcome, accepted := browserEnumAttribute(attributes, "browser.outcome", "success", "http-error", "network-error", "cancelled", "timeout")
		if !accepted {
			return nil, errBrowserBatch
		}
		outcome = parsedOutcome
	case "browser.error":
		source, valid := browserEnumAttribute(attributes, "browser.error.source", "vue", "window", "unhandledrejection", "navigation")
		if !valid {
			return nil, errBrowserBatch
		}
		errorType, valid := browserEnumAttribute(attributes, "error.type", "error", "type-error", "reference-error", "range-error", "syntax-error", "uri-error", "eval-error", "aggregate-error", "unknown")
		if !valid {
			return nil, errBrowserBatch
		}
		span.Attributes = append(span.Attributes, browserStringAttribute("browser.error.source", source), browserStringAttribute("error.type", errorType))
		span.Status.Code = tracepb.Status_STATUS_CODE_ERROR
	}
	if !valid {
		return nil, errBrowserBatch
	}
	if outcome != "" {
		span.Attributes = append(span.Attributes, browserStringAttribute("browser.outcome", outcome))
		if outcome == "error" || outcome == "http-error" || outcome == "network-error" || outcome == "timeout" {
			span.Status.Code = tracepb.Status_STATUS_CODE_ERROR
		}
	}
	return span, nil
}

func browserGroupAttribute(attributes map[string]map[string]any, key string, groups map[string]struct{}) (string, bool) {
	if _, present := attributes[key]; !present {
		return "other", true
	}
	value, ok := browserTextAttribute(attributes, key)
	if !ok {
		return "", false
	}
	if _, known := groups[value]; !known {
		return "other", true
	}
	return value, true
}

func browserTextAttribute(attributes map[string]map[string]any, key string) (string, bool) {
	value, ok := attributes[key]["stringValue"].(string)
	return value, ok && len(attributes[key]) == 1
}

func browserEnumAttribute(attributes map[string]map[string]any, key string, values ...string) (string, bool) {
	value, ok := browserTextAttribute(attributes, key)
	if !ok {
		return "", false
	}
	for _, known := range values {
		if value == known {
			return value, true
		}
	}
	return "", false
}
