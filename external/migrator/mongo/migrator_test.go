package mongo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	migrate "github.com/xakep666/mongo-migrate"
	mongodb "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

func TestNewCommandExposesDedicatedActions(t *testing.T) {
	command := NewCommand()
	want := map[string]bool{"new": true, "up": true, "down": true}

	for _, child := range command.Commands() {
		delete(want, child.Name())
	}
	if len(want) != 0 {
		t.Fatalf("NewCommand() missing actions: %v", want)
	}
}

func TestWithMigrationDirectoryPreservesLoadedDatabaseSettings(t *testing.T) {
	loaded := Settings{
		MongoDatabaseHost:       "mongo.example.test:27017",
		MongoDatabaseName:       "host-database",
		MongoMigrationDirectory: DefaultMigrationDirectory,
	}
	options := commandOptions{
		loadSettings: func() (*Settings, error) {
			settingsCopy := loaded
			return &settingsCopy, nil
		},
	}

	WithMigrationDirectory("./migrations")(&options)
	settings, err := options.loadSettings()
	if err != nil {
		t.Fatalf("loadSettings() error = %v", err)
	}
	if settings.MongoDatabaseHost != loaded.MongoDatabaseHost || settings.MongoDatabaseName != loaded.MongoDatabaseName {
		t.Fatalf("database settings changed: %+v", settings)
	}
	if settings.MongoMigrationDirectory != "./migrations" {
		t.Fatalf("MongoMigrationDirectory = %q, want ./migrations", settings.MongoMigrationDirectory)
	}
}

func TestNewActionCopiesTemplateWithoutConnectingToMongo(t *testing.T) {
	migrationDirectory := t.TempDir()
	template := []byte("package migrations\n")
	if err := os.WriteFile(filepath.Join(migrationDirectory, "template.go"), template, 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	settings := validTestSettings(migrationDirectory)
	runner := commandRunner{
		loadSettings: func() (*Settings, error) { return &settings, nil },
		dependencies: commandDependencies{
			connect: func(*options.ClientOptions) (mongoClient, error) {
				t.Fatal("new must not connect to MongoDB")
				return nil, nil
			},
			now: func() time.Time { return time.Date(2026, time.August, 1, 12, 34, 56, 0, time.FixedZone("test", 3600)) },
		},
	}
	command := newCommand(runner)
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"new", "create-users"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	migrationPath := filepath.Join(migrationDirectory, "20260801113456_create-users.go")
	created, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if !bytes.Equal(created, template) {
		t.Fatalf("migration content = %q, want %q", created, template)
	}
	if !strings.Contains(output.String(), migrationPath) {
		t.Fatalf("command output %q does not include %q", output.String(), migrationPath)
	}
}

func TestCreateMigrationRejectsUnsafeNames(t *testing.T) {
	runner := commandRunner{dependencies: commandDependencies{now: time.Now}}
	settings := validTestSettings(t.TempDir())

	for _, name := range []string{"", "Create-Users", "create users", "../create-users", "create.users", "create--users", strings.Repeat("a", maximumMigrationNameLength+1)} {
		t.Run(name, func(t *testing.T) {
			_, err := runner.createMigration(settings, name)
			if !errors.Is(err, ErrInvalidMigrationName) {
				t.Fatalf("createMigration(%q) error = %v, want ErrInvalidMigrationName", name, err)
			}
		})
	}
}

func TestCreateMigrationDoesNotOverwriteAnExistingFile(t *testing.T) {
	migrationDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationDirectory, "template.go"), []byte("new template"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}

	fixedTime := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	runner := commandRunner{dependencies: commandDependencies{now: func() time.Time { return fixedTime }}}
	settings := validTestSettings(migrationDirectory)
	migrationPath := filepath.Join(migrationDirectory, "20260801120000_create-users.go")
	if err := os.WriteFile(migrationPath, []byte("existing migration"), 0o644); err != nil {
		t.Fatalf("write existing migration: %v", err)
	}

	_, err := runner.createMigration(settings, "create-users")
	if err == nil || !errors.Is(err, os.ErrExist) {
		t.Fatalf("createMigration() error = %v, want os.ErrExist", err)
	}
	contents, readErr := os.ReadFile(migrationPath)
	if readErr != nil {
		t.Fatalf("read existing migration: %v", readErr)
	}
	if string(contents) != "existing migration" {
		t.Fatalf("existing migration was changed to %q", contents)
	}
}

func TestRunDatabaseActionUsesLocalRunnerForUpAndDown(t *testing.T) {
	for _, action := range []string{"up", "down"} {
		t.Run(action, func(t *testing.T) {
			client := &fakeMongoClient{}
			migration := &fakeMigrationRunner{}
			settings := validTestSettings(t.TempDir())
			runner := commandRunner{dependencies: commandDependencies{
				connect: func(*options.ClientOptions) (mongoClient, error) { return client, nil },
				registeredMigrations: func() []migrate.Migration {
					return []migrate.Migration{{Version: 1, Description: "test"}}
				},
				newMigrationRunner: func(_ *mongodb.Database, registrations []migrate.Migration, collection string) migrationRunner {
					if len(registrations) != 1 || registrations[0].Version != 1 {
						t.Fatalf("registrations = %+v, want test migration", registrations)
					}
					if collection != settings.MongoMigrationCollection {
						t.Fatalf("collection = %q, want %q", collection, settings.MongoMigrationCollection)
					}
					return migration
				},
			}}

			if err := runner.runDatabaseAction(context.Background(), settings, action); err != nil {
				t.Fatalf("runDatabaseAction() error = %v", err)
			}
			if client.pingCalls != 1 || client.disconnectCalls != 1 || client.databaseName != settings.MongoDatabaseName {
				t.Fatalf("client calls = ping:%d disconnect:%d database:%q", client.pingCalls, client.disconnectCalls, client.databaseName)
			}
			if action == "up" && (migration.upCalls != 1 || migration.upCount != migrate.AllAvailable) {
				t.Fatalf("up calls = %d count = %d", migration.upCalls, migration.upCount)
			}
			if action == "down" && (migration.downCalls != 1 || migration.downCount != migrate.AllAvailable) {
				t.Fatalf("down calls = %d count = %d", migration.downCalls, migration.downCount)
			}
		})
	}
}

func TestDatabaseActionsSucceedWithoutRegisteredMigrationsOrConnecting(t *testing.T) {
	for _, action := range []string{"up", "down"} {
		t.Run(action, func(t *testing.T) {
			settings := validTestSettings(t.TempDir())
			runner := commandRunner{
				loadSettings: func() (*Settings, error) { return &settings, nil },
				dependencies: commandDependencies{
					registeredMigrations: func() []migrate.Migration { return nil },
					connect: func(*options.ClientOptions) (mongoClient, error) {
						t.Fatal("the migrator must not connect without registered migrations")
						return nil, nil
					},
				},
			}
			command := newCommand(runner)
			output := &bytes.Buffer{}
			command.SetOut(output)
			command.SetErr(output)
			command.SetArgs([]string{action})

			if err := command.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if !strings.Contains(output.String(), "completed-service-migrations") {
				t.Fatalf("command output %q does not report completion", output.String())
			}
		})
	}
}

func TestRunDatabaseActionDisconnectsAfterPingFailure(t *testing.T) {
	pingErr := errors.New("ping failed")
	client := &fakeMongoClient{pingErr: pingErr}
	settings := validTestSettings(t.TempDir())
	runner := commandRunner{dependencies: commandDependencies{
		registeredMigrations: func() []migrate.Migration { return []migrate.Migration{{Version: 1}} },
		connect:              func(*options.ClientOptions) (mongoClient, error) { return client, nil },
	}}

	err := runner.runDatabaseAction(context.Background(), settings, "up")
	if !errors.Is(err, pingErr) {
		t.Fatalf("runDatabaseAction() error = %v, want ping error", err)
	}
	if client.disconnectCalls != 1 {
		t.Fatalf("Disconnect() calls = %d, want 1", client.disconnectCalls)
	}
}

func validTestSettings(migrationDirectory string) Settings {
	return Settings{
		MongoDatabaseUsername:    "mongoadmin",
		MongoDatabasePassword:    "secret",
		MongoDatabaseHost:        "127.0.0.1:27017",
		MongoDatabaseName:        "test",
		MongoConnectionPool:      5,
		MongoMigrationDirectory:  migrationDirectory,
		MongoMigrationCollection: "migrations",
		MongoMigrationTimeout:    time.Minute,
		MongoDisconnectTimeout:   time.Second,
	}
}

type fakeMongoClient struct {
	pingErr         error
	disconnectErr   error
	pingCalls       int
	disconnectCalls int
	databaseName    string
}

func (client *fakeMongoClient) Database(name string, _ ...options.Lister[options.DatabaseOptions]) *mongodb.Database {
	client.databaseName = name
	return nil
}

func (client *fakeMongoClient) Disconnect(context.Context) error {
	client.disconnectCalls++
	return client.disconnectErr
}

func (client *fakeMongoClient) Ping(context.Context, *readpref.ReadPref) error {
	client.pingCalls++
	return client.pingErr
}

type fakeMigrationRunner struct {
	upCalls   int
	upCount   int
	upErr     error
	downCalls int
	downCount int
	downErr   error
}

func (runner *fakeMigrationRunner) Up(_ context.Context, count int) error {
	runner.upCalls++
	runner.upCount = count
	return runner.upErr
}

func (runner *fakeMigrationRunner) Down(_ context.Context, count int) error {
	runner.downCalls++
	runner.downCount = count
	return runner.downErr
}
