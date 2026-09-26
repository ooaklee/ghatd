package observability

import (
	"math"
	"net"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

// Bounds are portable across 32-bit and 64-bit SDK integer parsers and keep
// millisecond-to-duration conversion below time.Duration overflow.
const configurationMaxInteger int64 = 1<<31 - 1

func configurationInteger(raw string, minimum, maximum int64) (int64, bool) {
	value, err := strconv.ParseInt(raw, 10, 64)
	return value, err == nil && value >= minimum && value <= maximum
}

func (report *ConfigurationReport) inspectIdentity(config Config, getenv func(string) string) {
	identity := IdentityConfiguration{Sources: make(map[string]string, 5)}
	resourceValues := make(map[string]string)
	raw := getenv("OTEL_RESOURCE_ATTRIBUTES")
	if !configurationTextSafe(raw) {
		report.issue("invalid_resource", "OTEL_RESOURCE_ATTRIBUTES", "error", "Resource attributes must contain valid text without control characters.")
	} else if strings.TrimSpace(raw) != "" {
		for _, member := range strings.Split(raw, ",") {
			key, value, found := strings.Cut(member, "=")
			decoded, err := url.PathUnescape(strings.TrimSpace(value))
			if !found || strings.TrimSpace(key) == "" || err != nil || !configurationTextSafe(decoded) {
				report.issue("invalid_resource", "OTEL_RESOURCE_ATTRIBUTES", "error", "Resource attributes must be comma-separated key=value pairs with valid percent encoding and no control characters.")
				continue
			}
			resourceValues[strings.TrimSpace(key)] = strings.TrimSpace(decoded)
		}
	}
	serviceName := getenv("OTEL_SERVICE_NAME")
	if !configurationTextSafe(serviceName) {
		report.issue("invalid_identity", "OTEL_SERVICE_NAME", "error", "Service identity must contain valid text without control characters.")
	}
	for _, field := range []struct {
		key, value string
		present    *bool
	}{
		{"service.name", config.ServiceName, &identity.ServiceName},
		{"service.namespace", config.Namespace, &identity.Namespace},
		{"service.instance.id", config.InstanceID, &identity.InstanceID},
		{"service.version", config.Version, &identity.Version},
		{"deployment.environment.name", config.Environment, &identity.Environment},
	} {
		if !configurationTextSafe(field.value) {
			report.issue("invalid_identity", field.key, "error", "Service identity must contain valid text without control characters.")
		}
		source := "none"
		switch {
		case strings.TrimSpace(field.value) != "":
			source = "application"
		case field.key == "service.name" && strings.TrimSpace(serviceName) != "":
			source = "environment"
		case resourceValues[field.key] != "":
			source = "resource"
		case field.key == "service.name":
			source = "default"
		case field.key == "service.instance.id":
			source = "process"
		}
		*field.present = source != "none"
		identity.Sources[field.key] = source
	}
	report.Identity = identity
}

func (report *ConfigurationReport) inspectSampler(getenv func(string) string) {
	name := strings.ToLower(strings.TrimSpace(getenv("OTEL_TRACES_SAMPLER")))
	if name == "" {
		name = "parentbased_always_on"
	}
	switch name {
	case "always_on", "always_off", "parentbased_always_on", "parentbased_always_off":
		report.Sampler = name
	case "traceidratio", "parentbased_traceidratio":
		report.Sampler = name
		ratio, err := strconv.ParseFloat(strings.TrimSpace(getenv("OTEL_TRACES_SAMPLER_ARG")), 64)
		if err != nil || math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
			report.issue("invalid_sampler_ratio", "OTEL_TRACES_SAMPLER_ARG", "error", "Ratio sampling requires a finite number between zero and one.")
		} else {
			report.SamplerRatio = &ratio
		}
	default:
		report.Sampler = "invalid"
		report.issue("invalid_sampler", "OTEL_TRACES_SAMPLER", "error", "Sampler must be a supported always-on, always-off, ratio, or parent-based sampler.")
	}
}

func (report *ConfigurationReport) inspectSDK(getenv func(string) string) {
	// These providers are constructed even when their exporter is disabled.
	// Retain the SDK's zero/negative limit semantics, but reject malformed
	// integers before upstream diagnostics can include their raw values.
	for _, key := range []string{
		"OTEL_ATTRIBUTE_VALUE_LENGTH_LIMIT", "OTEL_ATTRIBUTE_COUNT_LIMIT",
		"OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT", "OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT",
		"OTEL_SPAN_EVENT_COUNT_LIMIT", "OTEL_EVENT_ATTRIBUTE_COUNT_LIMIT",
		"OTEL_SPAN_LINK_COUNT_LIMIT", "OTEL_LINK_ATTRIBUTE_COUNT_LIMIT",
		"OTEL_LOGRECORD_ATTRIBUTE_COUNT_LIMIT", "OTEL_LOGRECORD_ATTRIBUTE_VALUE_LENGTH_LIMIT",
	} {
		report.inspectInteger(getenv, key, -1<<31, configurationMaxInteger, 0)
	}
	report.inspectInteger(func(key string) string { return strings.TrimSpace(getenv(key)) }, "OTEL_GO_X_CARDINALITY_LIMIT", -1<<31, configurationMaxInteger, 2000)
	if raw := getenv("OTEL_METRICS_EXEMPLAR_FILTER"); raw != "" {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case "always_on", "always_off", "trace_based":
		default:
			report.issue("invalid_exemplar_filter", "OTEL_METRICS_EXEMPLAR_FILTER", "error", "Exemplar filter must be always_on, always_off, or trace_based.")
		}
	}
	report.MetricIntervalMilliseconds, report.MetricTimeoutMilliseconds = 60000, 30000
	report.Temporality, report.HistogramAggregation = "cumulative", "explicit_bucket_histogram"
	for _, signal := range report.Signals {
		if signal.Enabled && signal.Signal != "metrics" {
			prefix := "OTEL_BSP_"
			if signal.Signal == "logs" {
				prefix = "OTEL_BLRP_"
			}
			for _, suffix := range []string{"SCHEDULE_DELAY", "EXPORT_TIMEOUT", "MAX_QUEUE_SIZE", "MAX_EXPORT_BATCH_SIZE"} {
				report.inspectInteger(getenv, prefix+suffix, 1, configurationMaxInteger, 0)
			}
		}
		if signal.Signal != "metrics" {
			continue
		}
		if signal.Exporter == "otlp" || signal.Exporter == "console" {
			report.MetricIntervalMilliseconds = report.inspectInteger(getenv, "OTEL_METRIC_EXPORT_INTERVAL", 1, configurationMaxInteger, 60000)
			report.MetricTimeoutMilliseconds = report.inspectInteger(getenv, "OTEL_METRIC_EXPORT_TIMEOUT", 1, configurationMaxInteger, 30000)
			report.inspectInteger(getenv, "OTEL_GO_X_METRIC_EXPORT_BATCH_SIZE", 0, configurationMaxInteger, 0)
		}
		if signal.Exporter == "otlp" {
			report.Temporality = report.inspectEnum(getenv, "OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE", "cumulative", "cumulative", "delta", "lowmemory")
			report.HistogramAggregation = report.inspectEnum(getenv, "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "explicit_bucket_histogram", "explicit_bucket_histogram", "base2_exponential_bucket_histogram")
			if report.HistogramAggregation == "base2_exponential_bucket_histogram" {
				report.issue("exponential_histogram", "OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION", "warning", "Exponential histograms require compatible dashboards; classic bucket queries will not apply.")
			}
		}
		if signal.Exporter == "prometheus" {
			report.inspectInteger(getenv, "OTEL_EXPORTER_PROMETHEUS_PORT", 1, 65535, 9464)
			if host := getenv("OTEL_EXPORTER_PROMETHEUS_HOST"); host != "" {
				if !configurationTextSafe(host) || strings.IndexFunc(host, unicode.IsSpace) >= 0 || strings.ContainsAny(host, "/?#@") || strings.Contains(host, ":") && net.ParseIP(host) == nil {
					report.issue("invalid_prometheus_host", "OTEL_EXPORTER_PROMETHEUS_HOST", "error", "Prometheus host must be a host name or IP address without a URL, port, or whitespace.")
				}
			}
		}
		if signal.Exporter == "otlp" || signal.Exporter == "console" || signal.Exporter == "prometheus" {
			for _, producer := range strings.Split(getenv("OTEL_METRICS_PRODUCERS"), ",") {
				if producer != "" && producer != "none" && producer != "prometheus" {
					report.issue("custom_metric_producer", "OTEL_METRICS_PRODUCERS", "warning", "Custom metric producers require application-specific inspection.")
				}
			}
		}
	}
}

func (report *ConfigurationReport) inspectInteger(getenv func(string) string, key string, minimum, maximum, fallback int64) int64 {
	if raw := getenv(key); raw != "" {
		if value, valid := configurationInteger(raw, minimum, maximum); valid {
			return value
		}
		report.issue("invalid_integer", key, "error", "Configured integer is malformed or outside its supported portable range.")
	}
	return fallback
}

func (report *ConfigurationReport) inspectEnum(getenv func(string) string, key, fallback string, allowed ...string) string {
	if raw := getenv(key); raw != "" {
		value := strings.ToLower(raw)
		for _, candidate := range allowed {
			if value == candidate {
				return value
			}
		}
		report.issue("invalid_metric_preference", key, "error", "Metric preference must be a supported value without surrounding whitespace.")
		return "invalid"
	}
	return fallback
}
