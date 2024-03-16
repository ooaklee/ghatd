package cmd

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

// NewCommandTemplate returns a command for
// rendering details with the specified base ghat(d) repo
func NewCommandTemplate() *cobra.Command {
	webAppCmd := &cobra.Command{
		Use:   "template",
		Short: "Verify & renders Details with base GHAT(D) app foundation",
		Example: `
		# Example command (short-args): 
		ghatdcli template -d ./ghatd-detail-web-starter-landing-with-dash -b "github.com/ooaklee/ghatd@test" -o /tmp/test-templating
		
		# Example command (long-args): 
		ghatdcli template --detail-dir ./ghatd-detail-web-starter-landing-with-dash --base "github.com/ooaklee/ghatd@test" --output /tmp/test-templating
`,
	}

	webAppCmd.Run = runCmdTemplate()
	return webAppCmd
}

// runCmdTemplate the function that is called when the command is ran
func runCmdTemplate() func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runCmdTemplateHolder(); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runCmdTemplateHolder handles initialising and running the "template" command
func runCmdTemplateHolder() error {

	fmt.Println("This will handle validating specified components and seeing if it could be rendered")

	return nil
}
