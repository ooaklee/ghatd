package settings

import (
	"github.com/kelseyhightower/envconfig"
)

// Settings for migration tool
type Settings struct {
	Environment           string `default:"local"`
	MongoDatabaseUsername string `envconfig:"mongo_db_username" default:"mongoadmin"`
	MongoDatabasePassword string `envconfig:"mongo_db_password" default:"secret"`
	MongoDatabaseHost     string `envconfig:"mongo_db_host" default:"127.0.0.1:27027"`
	MongoDatabaseName     string `envconfig:"mongo_db_name" default:"local"`
	MongoConnectionPool   int    `envconfig:"mongo_db_connection_pool" default:"5"`
	MongoDatabaseAtlas    bool   `envconfig:"mongo_db_atlas" default:"false"`
	MongoDatabaseAppName  string `envconfig:"mongo_db_app_name"`
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
