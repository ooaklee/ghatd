package repositoryhelpers

import "net/url"

// WithTopology merges an explicit replica set and opt-in direct connection into
// the URI while retaining other options. Empty/false values leave existing URI
// selections unchanged. It performs no I/O and leaves parse failures for the
// driver's validation; never log the credential-bearing URI. A nil config is inert.
func WithTopology(replicaSet string, directConnection bool) ConfigOption {
	return func(config *Config) {
		if config == nil {
			return
		}
		uri, err := url.Parse(config.ConnectionString)
		if err != nil {
			return // The shared driver's URI validation reports invalid input.
		}
		query := uri.Query()
		if replicaSet != "" {
			query.Set("replicaSet", replicaSet)
		}
		if directConnection {
			query.Set("directConnection", "true")
		}
		uri.RawQuery = query.Encode()
		if uri.RawQuery != "" && uri.Path == "" {
			uri.Path = "/"
		}
		config.ConnectionString = uri.String()
	}
}
