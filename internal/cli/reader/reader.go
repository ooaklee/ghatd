package reader

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ghodss/yaml"
	"github.com/ooaklee/ghatd/external/observability"
)

// UnmarshalReader is used to read manifests from stdin
func UnmarshalReader(reader io.Reader, obj interface{}) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	return unmarshalObject(data, obj)
}

// unmarshalObject tries to convert a YAML or JSON byte array into the provided type.
func unmarshalObject(data []byte, obj interface{}) error {
	// First, try unmarshalling as JSON.
	// Based on technique from Kubectl, which supports both YAML and JSON:
	//   https://mlafeldt.github.io/blog/teaching-go-programs-to-love-json-and-yaml/
	//   http://ghodss.com/2014/the-right-way-to-handle-yaml-in-golang/
	// Short version: JSON unmarshalling will not zero out null fields; YAML unmarshalling will.
	// This may have unintended effects or hard-to-catch issues when populating our application object.
	jsonData, err := yaml.YAMLToJSON(data)
	if err != nil {
		return err
	}

	err = json.Unmarshal(jsonData, &obj)
	if err != nil {
		return err
	}

	return err
}

// MarshalLocalYAMLFile writes JSON or YAML to a file on disk.
// The caller is responsible for checking error return values.
func MarshalLocalYAMLFile(path string, obj interface{}) error {
	yamlData, err := yaml.Marshal(obj)
	if err == nil {
		err = os.WriteFile(path, yamlData, 0600)
	}
	return err
}

// UnmarshalLocalFile retrieves JSON or YAML from a file on disk.
// The caller is responsible for checking error return values.
func UnmarshalLocalFile(path string, obj interface{}) error {
	data, err := os.ReadFile(path)
	if err == nil {
		err = unmarshalObject(data, obj)
	}
	return err
}

// Unmarshal converts YAML or JSON bytes into the provided value.
func Unmarshal(data []byte, obj interface{}) error {
	return unmarshalObject(data, obj)
}

// UnmarshalRemoteFile retrieves JSON or YAML through a GET request.
// The caller is responsible for checking error return values.
func UnmarshalRemoteFile(url string, obj interface{}) error {
	return UnmarshalRemoteFileContext(context.Background(), nil, url, obj)
}

// UnmarshalRemoteFileContext is the context-aware form of UnmarshalRemoteFile.
func UnmarshalRemoteFileContext(ctx context.Context, client *http.Client, url string, obj interface{}) error {
	data, err := ReadRemoteFileContext(ctx, client, url)
	if err == nil {
		err = unmarshalObject(data, obj)
	}
	return err
}

// ReadRemoteFile issues a GET request to retrieve the contents of the specified URL as a byte array.
// The caller is responsible for checking error return values.
func ReadRemoteFile(url string) ([]byte, error) {
	return ReadRemoteFileContext(context.Background(), nil, url)
}

// ReadRemoteFileContext issues a traced, context-aware GET request. Passing a
// nil client uses GHATD's privacy-safe OpenTelemetry transport.
func ReadRemoteFileContext(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	var data []byte
	if client == nil {
		client = observability.NewHTTPClient(http.DefaultTransport, 30*time.Second)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(request)
	if err == nil {
		defer func() {
			_ = resp.Body.Close()
		}()
		data, err = io.ReadAll(resp.Body)
	}
	return data, err
}
