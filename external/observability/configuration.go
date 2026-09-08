package observability

import (
	"crypto/tls"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"
)

// ConfigurationReport describes configuration without returning application
// identity, endpoint hosts or paths, credentials, certificate paths, or invalid
// input. Sources and descriptions use a finite vocabulary.
type ConfigurationReport struct {
	Signals                    []SignalConfiguration `json:"signals"`
	Identity                   IdentityConfiguration `json:"identity"`
	Sampler                    string                `json:"sampler"`
	SamplerRatio               *float64              `json:"sampler_ratio,omitempty"`
	MetricIntervalMilliseconds int64                 `json:"metric_interval_milliseconds"`
	MetricTimeoutMilliseconds  int64                 `json:"metric_timeout_milliseconds"`
	Temporality                string                `json:"temporality"`
	HistogramAggregation       string                `json:"histogram_aggregation"`
	Issues                     []ConfigurationIssue  `json:"issues"`
}

// SignalConfiguration contains sanitized effective settings for one signal.
// Custom exporter names are reported as "custom"; their configuration belongs
// to the application's registered exporter and cannot be inspected here.
type SignalConfiguration struct {
	Signal                   string                `json:"signal"`
	Exporter                 string                `json:"exporter"`
	ExporterSource           string                `json:"exporter_source"`
	Protocol                 string                `json:"protocol"`
	ProtocolSource           string                `json:"protocol_source"`
	Enabled                  bool                  `json:"enabled"`
	Endpoint                 EndpointConfiguration `json:"endpoint"`
	HeadersPresent           bool                  `json:"headers_present"`
	HeadersSource            string                `json:"headers_source"`
	TimeoutMilliseconds      int64                 `json:"timeout_milliseconds"`
	TimeoutSource            string                `json:"timeout_source"`
	Compression              string                `json:"compression"`
	TLS                      bool                  `json:"tls"`
	ServerCertificatePresent bool                  `json:"server_certificate_present"`
	ClientCertificatePresent bool                  `json:"client_certificate_present"`
	ClientKeyPresent         bool                  `json:"client_key_present"`
}

// EndpointConfiguration deliberately omits the host name and the URL path.
type EndpointConfiguration struct {
	Source   string `json:"source"`
	Scheme   string `json:"scheme"`
	HostKind string `json:"host_kind"`
	PathKind string `json:"path_kind"`
	Port     int    `json:"port"`
}

// IdentityConfiguration reports presence and precedence, never identity values.
// Sources has only the five standard identity keys owned by Config.
type IdentityConfiguration struct {
	ServiceName bool              `json:"service_name"`
	Namespace   bool              `json:"namespace"`
	InstanceID  bool              `json:"instance_id"`
	Version     bool              `json:"version"`
	Environment bool              `json:"environment"`
	Sources     map[string]string `json:"sources"`
}

// ConfigurationIssue uses fixed codes, field names, severities and messages.
type ConfigurationIssue struct {
	Code     string `json:"code"`
	Field    string `json:"field"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type resolvedConfiguration struct {
	signals []resolvedSignalConfiguration
}

type resolvedSignalConfiguration struct {
	signal, exporter, protocol string
	endpoint                   *url.URL
	headers                    map[string]string
	compression                string
	timeout                    time.Duration
	tlsConfig                  *tls.Config
	insecure                   bool
}

// InspectConfiguration validates the settings consumed by Start and describes
// their precedence. It never creates providers, changes globals, opens a
// listener, or contacts an endpoint. Configured TLS files are read to validate
// certificates and keys; their paths and contents never enter the report.
// Custom exporter registrations are preserved and reported as uninspected.
func InspectConfiguration(config Config) (ConfigurationReport, error) {
	report, _, err := resolveConfiguration(config, os.Getenv)
	return report, err
}

func resolveConfiguration(config Config, getenv func(string) string) (ConfigurationReport, *resolvedConfiguration, error) {
	report := ConfigurationReport{Signals: make([]SignalConfiguration, 0, 3), Issues: []ConfigurationIssue{}}
	resolved := &resolvedConfiguration{signals: make([]resolvedSignalConfiguration, 0, 3)}
	report.inspectIdentity(config, getenv)
	report.inspectSampler(getenv)
	for _, signal := range []string{"traces", "metrics", "logs"} {
		exporter := getenv("OTEL_" + strings.ToUpper(signal) + "_EXPORTER")
		source := "environment"
		if exporter == "" {
			exporter, source = "otlp", "default"
		}
		display := exporter
		if exporter != "otlp" && exporter != "console" && exporter != "none" && !(signal == "metrics" && exporter == "prometheus") {
			display = "custom"
			report.issue("custom_exporter", "OTEL_"+strings.ToUpper(signal)+"_EXPORTER", "warning", "Custom exporter settings require application-specific inspection.")
		}
		item := SignalConfiguration{Signal: signal, Exporter: display, ExporterSource: source, Enabled: exporter != "none", Protocol: "none", ProtocolSource: "none", HeadersSource: "none", TimeoutSource: "none", Compression: "none", Endpoint: EndpointConfiguration{Source: "none", Scheme: "none", HostKind: "none", PathKind: "none"}}
		settings := resolvedSignalConfiguration{signal: signal, exporter: exporter, timeout: 10 * time.Second}
		if exporter == "otlp" {
			report.inspectOTLP(getenv, &item, &settings)
		}
		report.Signals = append(report.Signals, item)
		resolved.signals = append(resolved.signals, settings)
	}
	report.inspectSDK(getenv)
	var failures []string
	for _, issue := range report.Issues {
		if issue.Severity == "error" {
			failures = append(failures, issue.Field+" ("+issue.Code+")")
		}
	}
	if len(failures) != 0 {
		return report, nil, errors.New("invalid OpenTelemetry configuration: " + strings.Join(failures, "; "))
	}
	return report, resolved, nil
}

func (report *ConfigurationReport) issue(code, field, severity, message string) {
	// Generic settings are checked for every applicable signal. Report each
	// fixed diagnostic once rather than repeating it three times.
	for _, existing := range report.Issues {
		if existing.Code == code && existing.Field == field && existing.Severity == severity {
			return
		}
	}
	report.Issues = append(report.Issues, ConfigurationIssue{Code: code, Field: field, Severity: severity, Message: message})
}
