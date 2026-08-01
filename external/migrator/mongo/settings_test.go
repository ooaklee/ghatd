package mongo

import (
	"os"
	"testing"
	"time"
)

func TestNewSettingsUsesCentralDefaults(t *testing.T) {
	clearSettingsEnvironment(t)

	settings, err := NewSettings()
	if err != nil {
		t.Fatalf("NewSettings() error = %v", err)
	}

	if settings.Environment != "local" {
		t.Fatalf("Environment = %q, want local", settings.Environment)
	}
	if settings.MongoDatabaseHost != "127.0.0.1:27027" {
		t.Fatalf("MongoDatabaseHost = %q, want 127.0.0.1:27027", settings.MongoDatabaseHost)
	}
	if settings.MongoDatabaseName != "local" {
		t.Fatalf("MongoDatabaseName = %q, want local", settings.MongoDatabaseName)
	}
	if settings.MongoConnectionPool != 5 {
		t.Fatalf("MongoConnectionPool = %d, want 5", settings.MongoConnectionPool)
	}
	if settings.MongoMigrationDirectory != DefaultMigrationDirectory {
		t.Fatalf("MongoMigrationDirectory = %q, want %q", settings.MongoMigrationDirectory, DefaultMigrationDirectory)
	}
	if settings.MongoMigrationCollection != DefaultMigrationCollection {
		t.Fatalf("MongoMigrationCollection = %q, want %q", settings.MongoMigrationCollection, DefaultMigrationCollection)
	}
	if settings.MongoMigrationTimeout != time.Minute {
		t.Fatalf("MongoMigrationTimeout = %s, want 1m", settings.MongoMigrationTimeout)
	}
	if settings.MongoDisconnectTimeout != 10*time.Second {
		t.Fatalf("MongoDisconnectTimeout = %s, want 10s", settings.MongoDisconnectTimeout)
	}
}

func TestNewSettingsUsesExistingAndMigratorEnvironmentVariables(t *testing.T) {
	clearSettingsEnvironment(t)
	t.Setenv("MONGO_DB_USERNAME", "service-user")
	t.Setenv("MONGO_DB_PASSWORD", "service-password")
	t.Setenv("MONGO_DB_HOST", "cluster.example.test")
	t.Setenv("MONGO_DB_NAME", "service-database")
	t.Setenv("MONGO_DB_CONNECTION_POOL", "12")
	t.Setenv("MONGO_DB_ATLAS", "true")
	t.Setenv("MONGO_DB_APP_NAME", "service-app")
	t.Setenv("MONGO_MIGRATION_DIRECTORY", "./database/migrations")
	t.Setenv("MONGO_MIGRATION_COLLECTION", "migration_history")
	t.Setenv("MONGO_MIGRATION_TIMEOUT", "2m")
	t.Setenv("MONGO_DISCONNECT_TIMEOUT", "15s")

	settings, err := NewSettings()
	if err != nil {
		t.Fatalf("NewSettings() error = %v", err)
	}

	if settings.MongoDatabaseUsername != "service-user" || settings.MongoDatabasePassword != "service-password" {
		t.Fatalf("credentials = %q/%q, want service-user/service-password", settings.MongoDatabaseUsername, settings.MongoDatabasePassword)
	}
	if settings.MongoDatabaseHost != "cluster.example.test" || settings.MongoDatabaseName != "service-database" {
		t.Fatalf("database = %q/%q, want cluster.example.test/service-database", settings.MongoDatabaseHost, settings.MongoDatabaseName)
	}
	if settings.MongoConnectionPool != 12 || !settings.MongoDatabaseAtlas || settings.MongoDatabaseAppName != "service-app" {
		t.Fatalf("connection settings were not loaded: %+v", settings)
	}
	if settings.MongoMigrationDirectory != "./database/migrations" || settings.MongoMigrationCollection != "migration_history" {
		t.Fatalf("migration locations were not loaded: %+v", settings)
	}
	if settings.MongoMigrationTimeout != 2*time.Minute || settings.MongoDisconnectTimeout != 15*time.Second {
		t.Fatalf("migration timeouts were not loaded: %+v", settings)
	}
}

func clearSettingsEnvironment(t *testing.T) {
	t.Helper()
	keys := []string{
		"ENVIRONMENT",
		"MONGO_DB_USERNAME",
		"MONGO_DB_PASSWORD",
		"MONGO_DB_HOST",
		"MONGO_DB_NAME",
		"MONGO_DB_CONNECTION_POOL",
		"MONGO_DB_ATLAS",
		"MONGO_DB_APP_NAME",
		"MONGO_MIGRATION_DIRECTORY",
		"MONGO_MIGRATION_COLLECTION",
		"MONGO_MIGRATION_TIMEOUT",
		"MONGO_DISCONNECT_TIMEOUT",
	}

	for _, key := range keys {
		value, exists := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		t.Cleanup(func() {
			if exists {
				_ = os.Setenv(key, value)
				return
			}
			_ = os.Unsetenv(key)
		})
	}
}
