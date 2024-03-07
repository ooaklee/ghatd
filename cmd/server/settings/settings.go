package settings

import "github.com/kelseyhightower/envconfig"

// Settings for server
type Settings struct {
	Environment           string `default:"local"`
	GracefulServerTimeout int    `envconfig:"graceful_server_timeout" default:"15"`
	Component             string `default:"ghatd"`
	LogLevel              string `envconfig:"log_level" default:"info"`
	Host                  string `default:"0.0.0.0"`
	Port                  string `default:"4000"`
	// Cache
	CacheTtl                 int    `envconfig:"cache_ttl" default:"15" required:"true"`
	CacheRefreshParameterKey string `envconfig:"cache_refresh_key" default:"frais" required:"true"`
	CacheSkipHttpHeader      string `envconfig:"cache_skip_http_header" default:"x-cache-skip"`
	CacheSkipUriPathRegex    string `envconfig:"cache_skip_uri_path_regex" default:"^/api/v1/.*"`
	// Cors
	AllowOrigins string `envconfig:"allow_origins" default:"http://localhost:3000" required:"true"`
	// Web App
	ExternalServiceName    string `envconfig:"external_service_name" default:"GHATD Template" required:"true"`
	ExternalServiceWebsite string `envconfig:"external_service_website"  default:"https://ghatd.boasi.io" required:"true"`
	ExternalServiceEmail   string `envconfig:"external_service_email"  default:"ghatd@boasi.io" required:"true"`
	LegalBusinessName      string `envconfig:"external_legal_business_name"  default:"Boasi Ltd" required:"true"`
}

// NewSettings returns app settings
func NewSettings() (*Settings, error) {
	var s Settings

	err := envconfig.Process("", &s)
	if err != nil {
		return nil, err
	}

	return &s, nil
}
