package settings

import (
	"github.com/kelseyhightower/envconfig"
)

// Settings for client
type Settings struct {
	Component string `default:"ghatdcli"`
	LogLevel  string `envconfig:"log_level" default:"info"`
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
