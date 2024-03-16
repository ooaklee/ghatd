package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// NewCommandCreateDetail returns a command for
// creating base details
func NewCommandCreateDetail() *cobra.Command {
	webAppCmd := &cobra.Command{
		Use:   "create-detail",
		Short: "Creates a GHAT(D) compatible Detail",
		Example: `
# Example command (short-args): 
ghatdcli create-detail -n "example" -t "web" -m "github.com/ooaklee/ghatd-detail-web-example"
# Example command (long-args):
ghatdcli create-detail --name "example" --type "web" --module "github.com/ooaklee/ghatd-detail-web-example"

# Example command (short-args): 
ghatdcli create-detail -n "example" -t "api" -m "github.com/ooaklee/ghatd-detail-api-example"
# Example command (long-args): 
ghatdcli create-detail --name "example" --type "api" --module "github.com/ooaklee/ghatd-detail-api-example"
`,
	}

	webAppCmd.Run = runCmdCreateDetail()
	return webAppCmd
}

// runCmdCreateDetail the function that is called when the command is ran
func runCmdCreateDetail() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runCmdCreateDetailHolder(); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runCmdCreateDetailHolder handles initialising and running the "create-detail" command
func runCmdCreateDetailHolder() error {

	fmt.Println("This will handle creating a new detail skeleton directory")

	return nil
}
