package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// NewCommandVersion returns a command for
// displaying the CLI version
func NewCommandVersion() *cobra.Command {
	webAppCmd := &cobra.Command{
		Use:   "version",
		Short: "print the version information",
	}

	webAppCmd.Run = runCmdVersion()
	return webAppCmd
}

// runCmdVersion the function that is called when the command is ran
func runCmdVersion() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runCmdVersionHolder(); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runCmdVersionHolder handles initialising and running the "version" command
func runCmdVersionHolder() error {

	fmt.Println("printing out cli version number")

	return nil
}
