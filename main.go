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
// //go:embed internal/web/ui/static/* internal/web/ui/html/*
var content embed.FS

func main() {

	// Highest Level Command
	rootCmd := cobra.Command{
		Use:   "ghatd",
		Short: "The entry point of the ghatd application",
		Long:  "The entry point of the ghatd application",
	}

	rootCmd.AddCommand(server.NewCommand(&content, "internal/"))

	if err := rootCmd.Execute(); err != nil {
		log.Fatal("ghatd/error-executing-command-tree")
	}

}
