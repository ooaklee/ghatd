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
	// Cors
	AllowOrigins string `envconfig:"allow_origins" default:"http://localhost:3000" required:"true"`
	// Web App
	ExternalServiceName    string `default:"GHATD Template" required:"true"`
	ExternalServiceWebsite string `default:"https://ghatd.boasi.io" required:"true"`
	ExternalServiceEmail   string `default:"ghatd@boasi.io" required:"true"`
	LegalBusinessName      string `default:"Boasi Ltd" required:"true"`
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
