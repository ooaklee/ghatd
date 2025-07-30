package migrator

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/cmd/mongo-migrator/settings"
	"github.com/ooaklee/ghatd/external/toolbox"
	_ "github.com/ooaklee/ghatd/migrations/mongo"
	"github.com/spf13/cobra"
	migrate "github.com/xakep666/mongo-migrate"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// MongoMigrationDirectory is the directory where mongo migrations are stored
	MongoMigrationDirectory = "./migrations/mongo"

	// MongoMigrationCollection is the name of the collection where migrations are stored
	MongoMigrationCollection = "migrations"
)

func main() {
	rootCmd := NewCommand()
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(toolbox.OutputBasicLogString("error", err.Error()))
	}
}

// NewCommand returns a command for starting
// the migrator aspect of this service
func NewCommand() *cobra.Command {
	migratorCmd := &cobra.Command{
		Use:   "mongo-migrator",
		Short: "Start the mongo migrations",
		Long: `Start the mongo migrations

Create a new migration using the 'new' argument, i.e: go run main.go start-migrator new <hyphen-seperated-migration-name> 

Run migrations using 'up' or 'down' arguments, i.e: go run main.go start-migrator up

> "up" will create misssing migrations and migrate "down" will revert migrations
		`,
	}

	migratorCmd.Run = run()
	return migratorCmd
}

// run the function that is called when the command is ran
func run() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		log.SetFlags(0)
		log.Default().Println(toolbox.OutputBasicLogString("info", "starting-service-migrations"))
		if err := runMigrator(args); err != nil {
			log.Fatal(toolbox.OutputBasicLogString("error", err.Error()))
		}
		log.Default().Println(toolbox.OutputBasicLogString("info", "completed-service-migrations"))
	}
}

// runMigrator handles initialising and running migrations
func runMigrator(args []string) error {

	validOptions := []string{"up", "down", "new"}

	if len(args) == 0 || !toolbox.StringInSlice(args[0], validOptions) {
		return fmt.Errorf("migrator/missing-options: %s", strings.Join(validOptions, ", "))
	}

	option := args[0]

	// Configure settings, logging & validator
	appSettings, err := settings.NewSettings()
	if err != nil {
		return fmt.Errorf("migrator/unable-to-load-migration-settings: %v", err)
	}

	// Create Mongo Uri
	var mongoHostUri string
	if appSettings.MongoDatabaseAtlas {
		mongoHostUri = fmt.Sprintf(
			"mongodb+srv://%s:%s@%s/?retryWrites=true&w=majority&appName=%s",
			appSettings.MongoDatabaseUsername,
			appSettings.MongoDatabasePassword,
			appSettings.MongoDatabaseHost,
			appSettings.MongoDatabaseAppName,
		)
	} else {
		mongoHostUri = fmt.Sprintf("mongodb://%v:%v@%v", appSettings.MongoDatabaseUsername,
			appSettings.MongoDatabasePassword,
			appSettings.MongoDatabaseHost)
	}

	maxWait := time.Duration(60 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), maxWait)
	defer cancel()

	opt := options.Client().ApplyURI(mongoHostUri)
	client, err := mongo.Connect(ctx, opt)
	if err != nil {
		return fmt.Errorf("migrator/unable-to-create-mongo-client: %v", err)
	}

	db := client.Database(appSettings.MongoDatabaseName)
	migrate.SetDatabase(db)
	migrate.SetMigrationsCollection(MongoMigrationCollection)

	switch option {
	case "new":
		detectedOsArgs := os.Args

		if len(detectedOsArgs) == 4 {
			detectedOsArgs = detectedOsArgs[1:]
		}

		if len(detectedOsArgs) != 3 {
			return fmt.Errorf("migrator/should-be: new description-of-migration")
		}
		fName := fmt.Sprintf("%s/%s_%s.go", MongoMigrationDirectory, time.Now().Format("20060102150405"), detectedOsArgs[2])
		from, err := os.Open(fmt.Sprintf("%s/template.go", MongoMigrationDirectory))
		if err != nil {
			return fmt.Errorf("migrator/should-be: new description-of-migration")
		}
		defer from.Close()

		to, err := os.OpenFile(fName, os.O_RDWR|os.O_CREATE, 0666)
		if err != nil {
			return fmt.Errorf("migrator/unable-to-open-file: %e", err)
		}
		defer to.Close()

		_, err = io.Copy(to, from)
		if err != nil {
			return fmt.Errorf("migrator/failed-to-copy-template-contents: %e", err)
		}
		log.Printf("New migration created: %s\n", fName)
	case "up":
		err = migrate.Up(migrate.AllAvailable)
	case "down":
		err = migrate.Down(migrate.AllAvailable)
	}

	if err != nil {
		return fmt.Errorf("migrator/failed-to-execute-action-on-migration: %e", err)
	}

	return nil
}
