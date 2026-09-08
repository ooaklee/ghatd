package observability

import (
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type otlpConfigurationLayer struct {
	prefix                        string
	endpoint                      *url.URL
	endpointSet                   bool
	headers                       map[string]string
	headersSet                    bool
	timeout                       time.Duration
	timeoutSet                    bool
	compression                   string
	compressionSet                bool
	insecure                      bool
	insecureSet                   bool
	ca                            *x509.CertPool
	certificate                   *tls.Certificate
	caSet, certificateSet, keySet bool
}

func configurationTextSafe(value string) bool {
	return utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}

func (report *ConfigurationReport) inspectOTLP(getenv func(string) string, item *SignalConfiguration, settings *resolvedSignalConfiguration) {
	prefix := "OTEL_EXPORTER_OTLP_" + strings.ToUpper(item.Signal)
	protocol, source := configurationValue(getenv, prefix+"_PROTOCOL", "OTEL_EXPORTER_OTLP_PROTOCOL", "http/protobuf")
	item.ProtocolSource = source
	if protocol != "http/protobuf" && protocol != "grpc" {
		item.Protocol = "invalid"
		field := "OTEL_EXPORTER_OTLP_PROTOCOL"
		if source == "signal" {
			field = prefix + "_PROTOCOL"
		}
		report.issue("invalid_protocol", field, "error", "Protocol must be http/protobuf or grpc with no surrounding whitespace.")
	} else {
		item.Protocol, settings.protocol = protocol, protocol
	}
	// The pinned trace and metric exporters parse generic settings before
	// applying signal overrides. Validate both layers before any SDK parser
	// can emit a diagnostic containing an invalid value.
	base := report.inspectOTLPLayer(getenv, "OTEL_EXPORTER_OTLP")
	signal := report.inspectOTLPLayer(getenv, prefix)
	endpoint, endpointSource := base.endpoint, "generic"
	endpointExplicit := base.endpointSet
	if signal.endpointSet {
		endpoint, endpointSource, endpointExplicit = signal.endpoint, "signal", true
	}
	insecure, insecureSet := base.insecure, base.insecureSet
	if signal.insecureSet {
		insecure, insecureSet = signal.insecure, true
	}
	if !endpointExplicit {
		endpointSource = "default"
		scheme, port := "https", "4318"
		if insecureSet && insecure {
			scheme = "http"
		}
		if protocol == "grpc" {
			port = "4317"
		}
		endpoint = &url.URL{Scheme: scheme, Host: net.JoinHostPort("localhost", port)}
	}
	item.Endpoint.Source = endpointSource
	if endpoint != nil {
		copyURL := *endpoint
		endpoint = &copyURL
		if endpointExplicit && insecureSet && insecure != (endpoint.Scheme == "http") {
			field := "OTEL_EXPORTER_OTLP_INSECURE"
			if signal.insecureSet {
				field = prefix + "_INSECURE"
			}
			report.issue("contradictory_tls", field, "error", "The selected insecure flag must agree with the selected endpoint scheme.")
		}
		insecure = endpoint.Scheme == "http"
		if protocol == "grpc" {
			if endpoint.Path != "" && endpoint.Path != "/" {
				field := "OTEL_EXPORTER_OTLP_ENDPOINT"
				if endpointSource == "signal" {
					field = prefix + "_ENDPOINT"
				}
				report.issue("grpc_endpoint_path", field, "error", "A gRPC endpoint must be an origin without a custom path.")
			}
			endpoint.Path, endpoint.RawPath = "", ""
			item.Endpoint.PathKind = "origin"
		} else if endpointSource != "signal" {
			if strings.HasSuffix(endpoint.Path, "/") && endpoint.Path != "" {
				report.issue("base_trailing_slash", "OTEL_EXPORTER_OTLP_ENDPOINT", "warning", "A trailing slash on the shared HTTP base is preserved differently by signal exporters.")
			}
			if item.Signal == "logs" {
				endpoint.Path += "/v1/logs"
			} else {
				endpoint.Path = path.Join(endpoint.Path, "/v1/"+item.Signal)
			}
			endpoint.RawPath = ""
		} else if endpoint.Path == "" {
			endpoint.Path = "/"
		}
		// Every pinned HTTP exporter extracts the decoded Path and reconstructs
		// its request URL, including for signal-specific endpoints.
		endpoint.RawPath = ""
		item.Endpoint.Scheme = endpoint.Scheme
		item.Endpoint.HostKind = "remote"
		if strings.EqualFold(strings.TrimSuffix(endpoint.Hostname(), "."), "localhost") || net.ParseIP(endpoint.Hostname()).IsLoopback() {
			item.Endpoint.HostKind = "loopback"
		}
		item.Endpoint.Port = 443
		if endpoint.Scheme == "http" && protocol != "grpc" {
			item.Endpoint.Port = 80
		}
		if endpoint.Port() != "" {
			item.Endpoint.Port, _ = strconv.Atoi(endpoint.Port())
		}
		if protocol != "grpc" {
			switch endpoint.Path {
			case "/v1/" + item.Signal:
				item.Endpoint.PathKind = "standard"
			case "/":
				item.Endpoint.PathKind = "root"
				report.issue("signal_endpoint_root", prefix+"_ENDPOINT", "warning", "A signal-specific HTTP endpoint uses its exact root path; no signal suffix is appended.")
			default:
				item.Endpoint.PathKind = "custom"
			}
		}
	} else {
		item.Endpoint.Scheme, item.Endpoint.HostKind, item.Endpoint.PathKind = "invalid", "invalid", "invalid"
	}
	settings.endpoint, settings.insecure, item.TLS = endpoint, insecure, !insecure
	settings.headers, item.HeadersSource = base.headers, "default"
	if base.headersSet {
		item.HeadersSource = "generic"
	}
	if signal.headersSet {
		settings.headers, item.HeadersSource = signal.headers, "signal"
	}
	item.HeadersPresent = len(settings.headers) > 0
	if protocol == "grpc" && !configurationGRPCHeaders(settings.headers) {
		field := "OTEL_EXPORTER_OTLP_HEADERS"
		if item.HeadersSource == "signal" {
			field = prefix + "_HEADERS"
		}
		report.issue("invalid_grpc_headers", field, "error", "gRPC metadata requires letter, digit, hyphen, underscore, or dot keys and printable ASCII values except for binary metadata.")
	}
	settings.timeout, item.TimeoutSource = 10*time.Second, "default"
	if base.timeoutSet {
		settings.timeout, item.TimeoutSource = base.timeout, "generic"
	}
	if signal.timeoutSet {
		settings.timeout, item.TimeoutSource = signal.timeout, "signal"
	}
	item.TimeoutMilliseconds = settings.timeout.Milliseconds()
	settings.compression = "none"
	if base.compressionSet {
		settings.compression = base.compression
	}
	if signal.compressionSet {
		settings.compression = signal.compression
	}
	item.Compression = settings.compression
	ca, certificate := base.ca, base.certificate
	item.ServerCertificatePresent, item.ClientCertificatePresent, item.ClientKeyPresent = base.caSet, base.certificateSet, base.keySet
	if signal.caSet {
		ca, item.ServerCertificatePresent = signal.ca, true
	}
	if signal.certificateSet || signal.keySet {
		certificate = signal.certificate
		item.ClientCertificatePresent, item.ClientKeyPresent = signal.certificateSet, signal.keySet
	}
	if item.ServerCertificatePresent || item.ClientCertificatePresent || item.ClientKeyPresent {
		if insecure {
			report.issue("plaintext_credentials", prefix+"_ENDPOINT", "error", "TLS certificates require a secure endpoint.")
		}
		settings.tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: ca}
		if certificate != nil {
			settings.tlsConfig.Certificates = []tls.Certificate{*certificate}
		}
	}
}

func configurationValue(getenv func(string) string, signalKey, genericKey, fallback string) (string, string) {
	if value := getenv(signalKey); value != "" {
		return value, "signal"
	}
	if value := getenv(genericKey); value != "" {
		return value, "generic"
	}
	return fallback, "default"
}

func (report *ConfigurationReport) inspectOTLPLayer(getenv func(string) string, prefix string) otlpConfigurationLayer {
	layer := otlpConfigurationLayer{prefix: prefix, timeout: 10 * time.Second, compression: "none"}
	if raw := getenv(prefix + "_ENDPOINT"); raw != "" {
		layer.endpointSet = true
		layer.endpoint = configurationEndpoint(raw)
		if layer.endpoint == nil {
			report.issue("invalid_endpoint", prefix+"_ENDPOINT", "error", "Endpoint must be an absolute HTTP or HTTPS URL with a valid host and port, without credentials, query, fragment, whitespace, or control characters.")
		}
	}
	if raw := getenv(prefix + "_HEADERS"); raw != "" {
		layer.headersSet = true
		var valid bool
		layer.headers, valid = configurationHeaders(raw)
		if !valid {
			report.issue("invalid_headers", prefix+"_HEADERS", "error", "Headers must be unique comma-separated HTTP token=value pairs with valid percent encoding and no control characters.")
		}
	}
	if raw := getenv(prefix + "_TIMEOUT"); raw != "" {
		layer.timeoutSet = true
		if milliseconds, valid := configurationInteger(raw, 1, configurationMaxInteger); valid {
			layer.timeout = time.Duration(milliseconds) * time.Millisecond
		} else {
			report.issue("invalid_timeout", prefix+"_TIMEOUT", "error", "Timeout must be an integer from 1 through 2147483647 milliseconds.")
		}
	}
	if raw := getenv(prefix + "_COMPRESSION"); raw != "" {
		layer.compressionSet = true
		if raw == "none" || raw == "gzip" {
			layer.compression = raw
		} else {
			layer.compression = "invalid"
			report.issue("invalid_compression", prefix+"_COMPRESSION", "error", "Compression must be none or gzip with no surrounding whitespace.")
		}
	}
	if raw := getenv(prefix + "_INSECURE"); raw != "" {
		layer.insecureSet = true
		switch strings.ToLower(raw) {
		case "true":
			layer.insecure = true
		case "false":
		default:
			report.issue("invalid_insecure", prefix+"_INSECURE", "error", "Insecure must be true or false with no surrounding whitespace.")
		}
	}
	if filename := getenv(prefix + "_CERTIFICATE"); filename != "" {
		layer.caSet = true
		data, err := configurationReadTLSFile(filename)
		pool := x509.NewCertPool()
		if err != nil || !pool.AppendCertsFromPEM(data) {
			report.issue("invalid_certificate", prefix+"_CERTIFICATE", "error", "The configured server certificate must be a readable PEM certificate file.")
		} else {
			layer.ca = pool
		}
	}
	certFile, keyFile := getenv(prefix+"_CLIENT_CERTIFICATE"), getenv(prefix+"_CLIENT_KEY")
	layer.certificateSet, layer.keySet = certFile != "", keyFile != ""
	if layer.certificateSet != layer.keySet {
		report.issue("incomplete_client_certificate", prefix+"_CLIENT_CERTIFICATE", "error", "Client certificate and key must both be configured at the same generic or signal level.")
	} else if layer.certificateSet {
		certData, certErr := configurationReadTLSFile(certFile)
		keyData, keyErr := configurationReadTLSFile(keyFile)
		certificate, parseErr := tls.X509KeyPair(certData, keyData)
		if certErr != nil || keyErr != nil || parseErr != nil {
			report.issue("invalid_client_certificate", prefix+"_CLIENT_CERTIFICATE", "error", "The configured client certificate and key must be readable matching PEM files.")
		} else {
			layer.certificate = &certificate
		}
	}
	return layer
}

func configurationReadTLSFile(filename string) ([]byte, error) {
	if !configurationTextSafe(filename) || strings.TrimSpace(filename) != filename {
		return nil, os.ErrInvalid
	}
	// Check regular-file status before reading so a FIFO or device cannot make
	// the configuration-only inspector wait indefinitely.
	info, err := os.Stat(filename)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(filename)
}

func configurationEndpoint(raw string) *url.URL {
	if !configurationTextSafe(raw) || strings.TrimSpace(raw) != raw {
		return nil
	}
	endpoint, err := url.Parse(raw)
	if err != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") || endpoint.Hostname() == "" || endpoint.Opaque != "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.ForceQuery || endpoint.Fragment != "" || strings.Contains(raw, "#") {
		return nil
	}
	if !configurationTextSafe(endpoint.Path) || !configurationTextSafe(endpoint.Host) || strings.IndexFunc(endpoint.Host, unicode.IsSpace) >= 0 {
		return nil
	}
	if strings.Contains(endpoint.Hostname(), ":") && net.ParseIP(strings.Split(endpoint.Hostname(), "%")[0]) == nil {
		return nil
	}
	if strings.HasPrefix(endpoint.Host, "[") && net.ParseIP(strings.Split(endpoint.Hostname(), "%")[0]) == nil {
		return nil
	}
	if endpoint.Port() != "" {
		if _, valid := configurationInteger(endpoint.Port(), 1, 65535); !valid {
			return nil
		}
	} else if strings.HasSuffix(endpoint.Host, ":") {
		return nil
	}
	return endpoint
}

func configurationHeaders(raw string) (map[string]string, bool) {
	if !configurationTextSafe(raw) {
		return nil, false
	}
	headers, seen := make(map[string]string), make(map[string]bool)
	for _, member := range strings.Split(raw, ",") {
		key, value, found := strings.Cut(member, "=")
		key = strings.TrimSpace(key)
		if !found || !configurationHeaderName(key) || seen[strings.ToLower(key)] {
			return nil, false
		}
		decoded, err := url.PathUnescape(value)
		if err != nil || !configurationTextSafe(decoded) {
			return nil, false
		}
		headers[key], seen[strings.ToLower(key)] = strings.TrimSpace(decoded), true
	}
	return headers, true
}

func configurationHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", char) {
			continue
		}
		return false
	}
	return true
}

func configurationGRPCHeaders(headers map[string]string) bool {
	for key, value := range headers {
		for _, char := range key {
			if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_' || char == '.' {
				continue
			}
			return false
		}
		if strings.HasSuffix(strings.ToLower(key), "-bin") {
			continue
		}
		for _, char := range value {
			if char < 32 || char > 126 {
				return false
			}
		}
	}
	return true
}
