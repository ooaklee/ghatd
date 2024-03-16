package common

// System wide variables
const (

	// SystemWideXApiToken is the token idetifier we expect when user wants to use their
	// apitoken
	SystemWideXApiToken string = "X-Api-Token"

	// HtmxHttpRequestHeader is the request header that is passed with all htmx request,
	// the value is set to "true" when a request is made with the library
	HtmxHttpRequestHeader string = "Hx-Request"

	// CorrelationIdHttpHeader is the header used to identify the request's id
	CorrelationIdHttpHeader string = "X-Correlation-Id"

	// WebPartialHttpRequestHeader is the header used to tell server that client only requires
	// a partial response from the endpoint
	WebPartialHttpRequestHeader string = "X-Web-Partial"

	// CacheSkipHttpResponseHeader is the response header used to tell server not to cache the
	// response from the endpoint
	CacheSkipHttpResponseHeader string = "X-Cache-Skip"

	// ApiV1UriPrefix the prefix that will be added to all of the Api's V1 URI routes
	ApiV1UriPrefix = "/api/v1"
)
