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

	// CACHE
	CacheTtl                 int    `envconfig:"cache_ttl" default:"15" required:"true"`
	CacheRefreshParameterKey string `envconfig:"cache_refresh_key" default:"frais" required:"true"`
	CacheSkipHttpHeader      string `envconfig:"cache_skip_http_header" default:"x-cache-skip"`
	CacheSkipUriPathRegex    string `envconfig:"cache_skip_uri_path_regex" default:"^/api/v1/.*"`

	// CORS
	AllowOrigins string `envconfig:"allow_origins" default:"http://localhost:3000" required:"true"`

	// ENTITY
	BusinessEntityName      string `envconfig:"business_entity_name" default:"GHAT(D)" required:"true"`
	BusinessEntityWebsite   string `envconfig:"business_entity_website" default:"https://ghatd.com"`
	BusinessEntityEmail     string `envconfig:"business_entity_email" default:"leon+ghatd@boasi.io"`
	BusinessEntityNameLegal string `envconfig:"business_entity_name_legal" default:"GHAT(D)" required:"true"`
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
