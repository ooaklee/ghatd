package observability

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

// The SDK's WithService detector creates a new ID on every detection. Cache
// only our fallback ID so repeated resource construction retains process
// identity while configuration is read afresh. No process metadata is needed.
var processInstanceID = sync.OnceValues(func() (string, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return "", errors.New("generate OpenTelemetry service instance ID failed")
	}
	return id.String(), nil
})

// newResource preserves application overrides while reading environment
// attributes afresh. resource.Default caches the first observed environment,
// which would make later SDK construction depend on initialization order.
func newResource(config Config) (*resource.Resource, error) {
	configured := []struct {
		key   attribute.Key
		value string
	}{
		{semconv.ServiceNameKey, config.ServiceName},
		{semconv.ServiceVersionKey, config.Version},
		{semconv.DeploymentEnvironmentNameKey, config.Environment},
		{semconv.ServiceNamespaceKey, config.Namespace},
		{semconv.ServiceInstanceIDKey, config.InstanceID},
	}
	for _, field := range configured {
		if strings.IndexFunc(field.value, unicode.IsControl) >= 0 {
			return nil, fmt.Errorf("OpenTelemetry %s contains control characters", field.key)
		}
	}
	if err := validateResourceEnvironment(); err != nil {
		return nil, err
	}

	// Retain SDK metadata and explicitly configured attributes, without
	// detecting executable paths, command arguments, usernames, or host IDs.
	base, err := resource.New(context.Background(), resource.WithTelemetrySDK(), resource.WithFromEnv())
	if err != nil {
		// Detector errors can include an invalid environment value. Do not wrap
		// that text into an error callers might log or return in diagnostics.
		return nil, errors.New("create OpenTelemetry resource: invalid resource configuration")
	}
	attributes := make([]attribute.KeyValue, 0, len(configured))
	for _, field := range configured {
		value := strings.TrimSpace(field.value)
		if value == "" {
			if detected, ok := base.Set().Value(field.key); ok {
				value = strings.TrimSpace(detected.AsString())
			}
		}
		if value == "" {
			switch field.key {
			case semconv.ServiceNameKey:
				value = DefaultServiceName
			case semconv.ServiceInstanceIDKey:
				value, err = processInstanceID()
				if err != nil {
					return nil, err
				}
			}
		}
		if value != "" {
			attributes = append(attributes, field.key.String(value))
		}
	}
	return resource.Merge(base, resource.NewWithAttributes(semconv.SchemaURL, attributes...))
}

// validateResourceEnvironment checks values before the standard detector sees
// them. In particular, malformed percent escapes make that detector call the
// global error handler with raw input; rejecting them here keeps diagnostics
// safe while retaining standard OTEL parsing and service-name precedence.
func validateResourceEnvironment() error {
	if strings.IndexFunc(os.Getenv("OTEL_SERVICE_NAME"), unicode.IsControl) >= 0 {
		return errors.New("OTEL_SERVICE_NAME contains control characters")
	}
	raw := os.Getenv("OTEL_RESOURCE_ATTRIBUTES")
	if strings.IndexFunc(raw, unicode.IsControl) >= 0 {
		return errors.New("OTEL_RESOURCE_ATTRIBUTES contains control characters")
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	for _, pair := range strings.Split(raw, ",") {
		key, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(key) == "" {
			return errors.New("OTEL_RESOURCE_ATTRIBUTES must contain comma-separated key=value pairs")
		}
		decoded, err := url.PathUnescape(strings.TrimSpace(value))
		if err != nil {
			return errors.New("OTEL_RESOURCE_ATTRIBUTES contains invalid percent encoding")
		}
		if strings.IndexFunc(decoded, unicode.IsControl) >= 0 {
			return errors.New("OTEL_RESOURCE_ATTRIBUTES contains control characters")
		}
	}
	return nil
}
