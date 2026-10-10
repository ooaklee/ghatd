package settings

import (
	"github.com/kelseyhightower/envconfig"
)

// Settings holds client-side configuration loaded from the environment.
// Component defaults to "ghatdcli" and LogLevel (envconfig key log_level)
// defaults to "info".
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
