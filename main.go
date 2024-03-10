package main

import (
	"embed"
	"log"

	_ "embed"

	"github.com/ooaklee/ghatd/cmd/server"
	"github.com/spf13/cobra"
)

// content holds our static web server content.
//
//go:embed internal/webapp/ui/html/*.tmpl.html internal/webapp/ui/static/* internal/webapp/ui/html/pages/*.tmpl.html internal/webapp/ui/html/partials/**/*.tmpl.html internal/webapp/ui/html/partials/*.tmpl.html internal/webapp/ui/html/responses/*.tmpl.html
var content embed.FS

func main() {

	// Highest Level Command
	rootCmd := cobra.Command{
		Use:   "ghatd",
		Short: "The entry point of the ghatd application",
		Long:  "The entry point of the ghatd application",
	}

	rootCmd.AddCommand(server.NewCommand(&content))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal("ghatd/error-executing-command-tree")
	}

}
