package config

import (
	"os"

	"github.com/ooaklee/ghatd/internal/cli/reader"
)

// DetailConfig is the representation of a valid Detail configuration file
type DetailConfig struct {
	// Version is the version of the detail configuration file
	Version string `json:"version" yaml:"version"`

	// Type is the kind of the detail
	Type string `json:"type" yaml:"type"`

	// Name is the name of the detail
	Name string `json:"name" yaml:"name"`

	// Description is the information about the detail
	Description string `json:"description" yaml:"description"`

	// Features outlines the elements that are included in detail
	Features []string `json:"features" yaml:"features"`

	// Experimental whether the corresponding detail is in a stable state
	Experimental bool `json:"experimental" yaml:"experimental"`
}

// SetConfigDefaults loads up the deafult conf stats
func (c *DetailConfig) SetConfigDefaults() *DetailConfig {

	c.Version = "detail/v0alpha1"

	return c
}

// ValidateDetailConfig is making sure the options set in the
// configs are valid
func ValidateDetailConfig(config *DetailConfig) error {
	// TODO: Consider what logic should be used for validating
	// conf
	// if config.Version == "" {
	// 	return fmt.Errorf("version-must-be-provided")
	// }

	if config == nil {
		return nil
	}

	return nil
}

// ReadDetailConfig loads up passed configuration file. Returns nil if config does not exist
func ReadDetailConfig(path string) (*DetailConfig, error) {
	var err error
	var config DetailConfig

	// check file permission only when unistry config exists
	if fi, err := os.Stat(path); err == nil {
		err = getFilePermission(fi)
		if err != nil {
			return nil, err
		}
	}

	err = reader.UnmarshalLocalFile(path, &config)
	if os.IsNotExist(err) {
		return nil, nil
	}
	err = ValidateDetailConfig(&config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
