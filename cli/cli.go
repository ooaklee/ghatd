package main

import (
	"os"

	"github.com/ooaklee/ghatd/cli/cmd"
	"github.com/spf13/cobra"
)

func main() {

	// Highest Level Command
	rootCmd := cobra.Command{
		Use:   "ghatdcli",
		Short: "ghatdcli - initialise, manage, and develop your GHAT(D) apps.",
		Long: `
NAME: 
ghatdcli - initialise, manage, and develop and extend your 
GHAT(D) application with community-made building blocks.

DESCRIPTION:
GHATDCLI is the official command-line interface for the 
GHAT(D) framework. This versatile tool streamlines Go web
app project development by allowing users to access and use
the framework's building segments (Details). Details are
preconstructed blocks (created by the community and others)
that enable users to start projects quickly, add 
functionality to existing GHAT(D) compatible projects and
retain autonomy over their application design/functionality.

To get help for each command, users can type: 
'ghatdcli help <command>'.

We invite you to explore the demo site at 
https://demo.ghatd.com to see the framework in action.`,
	}

	rootCmd.AddCommand(cmd.NewCommandNew())
	rootCmd.AddCommand(cmd.NewCommandCreateDetail())
	rootCmd.AddCommand(cmd.NewCommandTemplate())
	rootCmd.AddCommand(cmd.NewCommandVersion())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
