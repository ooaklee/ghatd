package mongo

import (
	"time"

	"github.com/kelseyhightower/envconfig"
)

const (
	// DefaultMigrationDirectory is where host MongoDB migrations are stored.
	DefaultMigrationDirectory = "./migrations/mongo"

	// DefaultMigrationCollection stores the migration history in MongoDB.
	DefaultMigrationCollection = "migrations"
)

// Settings contains the host configuration used by the MongoDB migrator.
//
// The existing MONGO_DB_* environment variable names are retained so hosts can
// adopt the shared migrator without changing their deployment configuration.
type Settings struct {
	Environment              string        `envconfig:"environment" default:"local"`
	MongoDatabaseUsername    string        `envconfig:"mongo_db_username" default:"mongoadmin"`
	MongoDatabasePassword    string        `envconfig:"mongo_db_password" default:"secret"`
	MongoDatabaseHost        string        `envconfig:"mongo_db_host" default:"127.0.0.1:27027"`
	MongoDatabaseName        string        `envconfig:"mongo_db_name" default:"local"`
	MongoConnectionPool      int           `envconfig:"mongo_db_connection_pool" default:"5"`
	MongoDatabaseAtlas       bool          `envconfig:"mongo_db_atlas" default:"false"`
	MongoDatabaseAppName     string        `envconfig:"mongo_db_app_name"`
	MongoMigrationDirectory  string        `envconfig:"mongo_migration_directory" default:"./migrations/mongo"`
	MongoMigrationCollection string        `envconfig:"mongo_migration_collection" default:"migrations"`
	MongoMigrationTimeout    time.Duration `envconfig:"mongo_migration_timeout" default:"60s"`
	MongoDisconnectTimeout   time.Duration `envconfig:"mongo_disconnect_timeout" default:"10s"`
}

// NewSettings loads MongoDB migrator settings from the environment.
func NewSettings() (*Settings, error) {
	var settings Settings
	if err := envconfig.Process("", &settings); err != nil {
		return nil, err
	}

	return &settings, nil
}
