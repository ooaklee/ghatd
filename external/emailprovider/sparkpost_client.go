package emailprovider

import (
	"fmt"
	"net/http"

	sp "github.com/SparkPost/gosparkpost"
)

// NewSparkPostClientRequest holds SparkPost client setup inputs.
//
// HTTPClient accepts a host application's *http.Client. Its policy
// (timeout, redirect behaviour, cookie jar and transport) is copied onto a
// private client owned by the SparkPost client; the supplied client and
// http.DefaultClient are never mutated.
//
// Transport, when set, is an explicit transport override. It is applied to a
// shallow copy of the HTTPClient policy, so it takes precedence over the
// supplied (or default) transport without mutating either.
type NewSparkPostClientRequest struct {
	BaseURL    string
	APIKey     string
	APIVersion int
	HTTPClient *http.Client
	Transport  http.RoundTripper
}

// WithHTTPClient returns a request-scoped copy of the request with the
// supplied HTTP client set. The receiver is left untouched, so the option can
// be chained without mutating a shared request value:
//
//	request := (&NewSparkPostClientRequest{...}).WithHTTPClient(httpClient)
//
// The returned request still needs to be passed to NewSparkPostClient; the
// live provider client is never modified by this option.
func (r *NewSparkPostClientRequest) WithHTTPClient(httpClient *http.Client) *NewSparkPostClientRequest {
	if r == nil {
		return nil
	}
	updated := *r
	updated.HTTPClient = httpClient
	return &updated
}

// NewSparkPostClient initialises a SparkPost client with optional HTTP
// transport instrumentation and an optionally injected HTTP client.
//
// Client selection precedence:
//
//  1. If HTTPClient is set, a private copy of its policy (timeout, redirect
//     behaviour, cookie jar and transport) is used. The supplied client is
//     never mutated or aliased.
//  2. If HTTPClient is nil, a private copy of the process-wide default policy
//     (http.DefaultClient) is used instead, so the provider never aliases or
//     mutates http.DefaultClient itself.
//  3. If Transport is also set, the transport override is applied to the
//     private copy from step 1 or 2, overriding only its transport.
//
// All previously supported initialisation inputs (BaseURL, APIKey, APIVersion
// and Transport alone) continue to work unchanged.
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

	client.Client = newPrivateHTTPClient(request.HTTPClient)
	if request.Transport != nil {
		client.Client.Transport = request.Transport
	}

	return client, nil
}

// newPrivateHTTPClient returns a private *http.Client carrying the policy of
// the supplied client. When nil, the process-wide default policy is copied.
// The copy is shallow: transport, jar and redirect function are shared by
// reference (never mutated here), while the returned client itself is safe
// for the caller to adjust without affecting the source client or
// http.DefaultClient.
func newPrivateHTTPClient(source *http.Client) *http.Client {
	if source == nil {
		source = http.DefaultClient
	}
	httpClient := *source
	return &httpClient
}
