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
//>ghatd {{ define "WebDetailEmbeds" }}{{ end }}
//>ghatd {{ define "ApiDetailEmbeds" }}{{ end }}

// //go:embed internal/web/ui/html/*.tmpl.html internal/web/ui/static/* internal/web/ui/html/pages/*.tmpl.html internal/web/ui/html/partials/**/*.tmpl.html internal/web/ui/html/partials/*.tmpl.html internal/web/ui/html/responses/*.tmpl.html
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
