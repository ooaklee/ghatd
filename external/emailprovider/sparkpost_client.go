package emailprovider

import (
	"fmt"
	"net/http"

	sp "github.com/SparkPost/gosparkpost"
)

// NewSparkPostClientRequest holds SparkPost client setup inputs.
type NewSparkPostClientRequest struct {
	BaseURL    string
	APIKey     string
	APIVersion int
	Transport  http.RoundTripper
}

// NewSparkPostClient initialises a SparkPost client with optional HTTP transport
// instrumentation.
func NewSparkPostClient(request *NewSparkPostClientRequest) (*sp.Client, error) {
	if request == nil {
		return nil, fmt.Errorf("emailprovider/sparkpost-client-nil-request")
	}

	apiVersion := request.APIVersion
	if apiVersion == 0 {
		apiVersion = 1
	}

	client := &sp.Client{}
	if err := client.Init(&sp.Config{
		BaseUrl:    request.BaseURL,
		ApiKey:     request.APIKey,
		ApiVersion: apiVersion,
	}); err != nil {
		return nil, fmt.Errorf("emailprovider/sparkpost-client-init: %w", err)
	}

	if request.Transport != nil {
		client.Client.Transport = request.Transport
	}

	return client, nil
}
