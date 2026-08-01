// Package mongo provides the shared MongoDB migration command used by GHAT(D)
// host applications.
package mongo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	repositoryhelpers "github.com/ooaklee/ghatd/external/repository/helpers"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/spf13/cobra"
	migrate "github.com/xakep666/mongo-migrate"
	mongodb "go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

var (
	// ErrInvalidMigrationName is returned when a migration name cannot safely
	// be used as part of a Go filename.
	ErrInvalidMigrationName = errors.New("migrator/invalid-migration-name")

	migrationNamePattern = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)
)

const maximumMigrationNameLength = 128

type settingsLoader func() (*Settings, error)

type commandOptions struct {
	loadSettings settingsLoader
}

// CommandOption customises the shared MongoDB migrator command.
type CommandOption func(*commandOptions)

// WithSettings uses a complete settings value supplied by the host instead of
// loading settings from the environment when a migration action is executed.
// Use focused options such as WithMigrationDirectory when the host only needs
// to override one setting while retaining environment loading.
func WithSettings(settings Settings) CommandOption {
	return func(options *commandOptions) {
		options.loadSettings = func() (*Settings, error) {
			settingsCopy := settings
			return &settingsCopy, nil
		}
	}
}

// WithMigrationDirectory sets the host-owned migration and template directory
// while retaining the configured settings loader for database connection
// values and other migrator settings.
func WithMigrationDirectory(directory string) CommandOption {
	return func(options *commandOptions) {
		loadSettings := options.loadSettings
		options.loadSettings = func() (*Settings, error) {
			settings, err := loadSettings()
			if err != nil || settings == nil {
				return settings, err
			}

			settings.MongoMigrationDirectory = directory
			return settings, nil
		}
	}
}

type mongoClient interface {
	// Database returns a handle for the named MongoDB database.
	Database(string, ...options.Lister[options.DatabaseOptions]) *mongodb.Database
	// Disconnect closes the client and releases its resources.
	Disconnect(context.Context) error
	// Ping verifies that the MongoDB deployment is reachable.
	Ping(context.Context, *readpref.ReadPref) error
}

type migrationRunner interface {
	// Up applies the requested number of pending migrations.
	Up(context.Context, int) error
	// Down reverts the requested number of applied migrations.
	Down(context.Context, int) error
}

type commandDependencies struct {
	connect              func(*options.ClientOptions) (mongoClient, error)
	newMigrationRunner   func(*mongodb.Database, []migrate.Migration, string) migrationRunner
	registeredMigrations func() []migrate.Migration
	now                  func() time.Time
}

type commandRunner struct {
	loadSettings settingsLoader
	dependencies commandDependencies
}

// NewCommand returns the shared Cobra command for creating, applying, and
// reverting MongoDB migrations registered by the host application.
//
// Host applications must blank-import their migration package before calling
// NewCommand. The command returns errors through Cobra and never terminates the
// host process itself.
func NewCommand(commandOptions ...CommandOption) *cobra.Command {
	options := commandOptionsWithDefaults()
	for _, option := range commandOptions {
		if option != nil {
			option(&options)
		}
	}

	return newCommand(commandRunner{
		loadSettings: options.loadSettings,
		dependencies: commandDependenciesWithDefaults(),
	})
}

// commandOptionsWithDefaults returns command options that load settings from
// the environment.
func commandOptionsWithDefaults() commandOptions {
	return commandOptions{loadSettings: NewSettings}
}

// commandDependenciesWithDefaults returns the production implementations used
// to connect to MongoDB and construct migration runners.
func commandDependenciesWithDefaults() commandDependencies {
	return commandDependencies{
		connect: func(clientOptions *options.ClientOptions) (mongoClient, error) {
			return mongodb.Connect(clientOptions)
		},
		newMigrationRunner: func(database *mongodb.Database, migrations []migrate.Migration, collection string) migrationRunner {
			runner := migrate.NewMigrate(database, migrations...)
			runner.SetMigrationsCollection(collection)
			return runner
		},
		registeredMigrations: migrate.RegisteredMigrations,
		now:                  time.Now,
	}
}

// newCommand builds the Cobra command tree around the supplied runner.
func newCommand(runner commandRunner) *cobra.Command {
	command := &cobra.Command{
		Use:           "mongo-migrator",
		Short:         "Run MongoDB migrations",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("migrator/missing-options: up, down or new")
		},
	}

	command.Long = `Run MongoDB migrations registered by the host application.

Use:
  <host-command> mongo-migrator new <hyphen-separated-migration-name>
  <host-command> mongo-migrator up
  <host-command> mongo-migrator down

"up" applies all available migrations and "down" reverts all applied migrations.`

	command.AddCommand(
		&cobra.Command{
			Use:   "new <hyphen-separated-migration-name>",
			Short: "Create a migration from the host template",
			Args:  cobra.ExactArgs(1),
			RunE: func(command *cobra.Command, args []string) error {
				return runner.run(command, "new", func(settings Settings) error {
					migrationPath, err := runner.createMigration(settings, args[0])
					if err != nil {
						return err
					}
					fmt.Fprintf(command.OutOrStdout(), "New migration created: %s\n", migrationPath)
					return nil
				})
			},
		},
		&cobra.Command{
			Use:   "up",
			Short: "Apply all available migrations",
			Args:  cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runner.run(command, "up", func(settings Settings) error {
					return runner.runDatabaseAction(command.Context(), settings, "up")
				})
			},
		},
		&cobra.Command{
			Use:   "down",
			Short: "Revert all applied migrations",
			Args:  cobra.NoArgs,
			RunE: func(command *cobra.Command, _ []string) error {
				return runner.run(command, "down", func(settings Settings) error {
					return runner.runDatabaseAction(command.Context(), settings, "down")
				})
			},
		},
	)

	return command
}

// run loads and validates settings, executes an action, and reports its
// lifecycle through the command output.
func (runner commandRunner) run(command *cobra.Command, action string, execute func(Settings) error) error {
	settings, err := runner.loadSettings()
	if err != nil {
		return fmt.Errorf("migrator/unable-to-load-migration-settings: %w", err)
	}
	if settings == nil {
		return errors.New("migrator/unable-to-load-migration-settings: settings cannot be nil")
	}
	if err := validateSettings(*settings, action); err != nil {
		return err
	}

	fmt.Fprintln(command.OutOrStdout(), toolbox.OutputBasicLogString("info", "starting-service-migrations"))
	if err := execute(*settings); err != nil {
		return err
	}
	fmt.Fprintln(command.OutOrStdout(), toolbox.OutputBasicLogString("info", "completed-service-migrations"))
	return nil
}

// validateSettings checks the settings required by the requested action.
func validateSettings(settings Settings, action string) error {
	if action == "new" {
		if strings.TrimSpace(settings.MongoMigrationDirectory) == "" {
			return errors.New("migrator/migration-directory-cannot-be-empty")
		}
		return nil
	}

	if strings.TrimSpace(settings.MongoDatabaseName) == "" {
		return errors.New("migrator/mongo-database-name-cannot-be-empty")
	}
	if strings.TrimSpace(settings.MongoMigrationCollection) == "" {
		return errors.New("migrator/migration-collection-cannot-be-empty")
	}
	if settings.MongoConnectionPool <= 0 {
		return errors.New("migrator/mongo-connection-pool-must-be-positive")
	}
	if settings.MongoMigrationTimeout <= 0 {
		return errors.New("migrator/migration-timeout-must-be-positive")
	}
	if settings.MongoDisconnectTimeout <= 0 {
		return errors.New("migrator/disconnect-timeout-must-be-positive")
	}

	return nil
}

// runDatabaseAction applies or reverts all registered migrations. It succeeds
// without connecting to MongoDB when no migrations are registered.
func (runner commandRunner) runDatabaseAction(parentContext context.Context, settings Settings, action string) (err error) {
	registeredMigrations := runner.dependencies.registeredMigrations()
	if len(registeredMigrations) == 0 {
		return nil
	}

	mongoURI, err := repositoryhelpers.GenerateMongoURI(repositoryhelpers.MongoURIConfig{
		Username: settings.MongoDatabaseUsername,
		Password: settings.MongoDatabasePassword,
		Host:     settings.MongoDatabaseHost,
		AppName:  settings.MongoDatabaseAppName,
		Atlas:    settings.MongoDatabaseAtlas,
	})
	if err != nil {
		return fmt.Errorf("migrator/unable-to-build-mongo-uri: %w", err)
	}

	clientOptions := options.Client().
		ApplyURI(mongoURI).
		SetConnectTimeout(settings.MongoMigrationTimeout).
		SetMaxPoolSize(uint64(settings.MongoConnectionPool))

	client, err := runner.dependencies.connect(clientOptions)
	if err != nil {
		return fmt.Errorf("migrator/unable-to-create-mongo-client: %w", err)
	}

	defer func() {
		disconnectContext, cancel := context.WithTimeout(context.Background(), settings.MongoDisconnectTimeout)
		defer cancel()
		disconnectErr := client.Disconnect(disconnectContext)
		if disconnectErr != nil {
			disconnectErr = fmt.Errorf("migrator/unable-to-disconnect-from-mongo: %w", disconnectErr)
			err = errors.Join(err, disconnectErr)
		}
	}()

	migrationContext, cancel := context.WithTimeout(parentContext, settings.MongoMigrationTimeout)
	defer cancel()
	if err := client.Ping(migrationContext, nil); err != nil {
		return fmt.Errorf("migrator/unable-to-connect-to-mongo: %w", err)
	}

	database := client.Database(settings.MongoDatabaseName)
	migration := runner.dependencies.newMigrationRunner(database, registeredMigrations, settings.MongoMigrationCollection)
	switch action {
	case "up":
		err = migration.Up(migrationContext, migrate.AllAvailable)
	case "down":
		err = migration.Down(migrationContext, migrate.AllAvailable)
	default:
		return fmt.Errorf("migrator/unsupported-action: %s", action)
	}
	if err != nil {
		return fmt.Errorf("migrator/failed-to-execute-action-on-migration: %w", err)
	}

	return nil
}

// createMigration copies the host migration template to a timestamped file. It
// removes a partially written file when copying or closing fails.
func (runner commandRunner) createMigration(settings Settings, migrationName string) (string, error) {
	if len(migrationName) > maximumMigrationNameLength || !migrationNamePattern.MatchString(migrationName) {
		return "", fmt.Errorf("%w: %q must contain only lowercase letters, numbers, hyphens, or underscores", ErrInvalidMigrationName, migrationName)
	}

	templatePath := filepath.Join(settings.MongoMigrationDirectory, "template.go")
	template, err := os.Open(templatePath)
	if err != nil {
		return "", fmt.Errorf("migrator/unable-to-open-template: %w", err)
	}
	defer template.Close()

	migrationPath := filepath.Join(
		settings.MongoMigrationDirectory,
		fmt.Sprintf("%s_%s.go", runner.dependencies.now().UTC().Format("20060102150405"), migrationName),
	)
	migrationFile, err := os.OpenFile(migrationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("migrator/unable-to-create-migration-file: %w", err)
	}

	copySucceeded := false
	defer func() {
		_ = migrationFile.Close()
		if !copySucceeded {
			_ = os.Remove(migrationPath)
		}
	}()

	if _, err := io.Copy(migrationFile, template); err != nil {
		return "", fmt.Errorf("migrator/failed-to-copy-template-contents: %w", err)
	}
	if err := migrationFile.Close(); err != nil {
		return "", fmt.Errorf("migrator/unable-to-close-migration-file: %w", err)
	}
	copySucceeded = true

	return migrationPath, nil
}
