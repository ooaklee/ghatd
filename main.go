package main

import (
	"embed"
	"log"

	_ "embed"

	migrator "github.com/ooaklee/ghatd/cmd/mongo-migrator"
	"github.com/ooaklee/ghatd/cmd/server"
	"github.com/spf13/cobra"
)

// content holds our static web server content.
//
// //go:embed internal/web/ui/static/* internal/web/ui/html/*
var content embed.FS

func main() {

	// Highest Level Command
	rootCmd := cobra.Command{
		Use:           "ghatd",
		Short:         "The entry point of the ghatd application",
		Long:          "The entry point of the ghatd application",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	rootCmd.AddCommand(server.NewCommand(&content, "internal/"))
	rootCmd.AddCommand(migrator.NewCommand())

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}

}
