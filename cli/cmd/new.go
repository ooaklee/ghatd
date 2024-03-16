package cmd

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/internal/cli/common"
	"github.com/spf13/cobra"
)

// CommandNewFlags holds the variables that will be set by flags
type CommandNewFlags struct {
	AppName       string
	AppModuleName string
	DetailUrls    string
}

// NewCommandNew returns a command for
// creating ghat(d) applications
func NewCommandNew() *cobra.Command {

	cmdFlags := &CommandNewFlags{}

	webAppCmd := &cobra.Command{
		Use:   "new",
		Short: "Creates a GHAT(D) compatible Detail",
		Long:  "Creates a GHAT(D) app with the given module while utilising the associated details (building segments)",
		Example: `
# Example command (short-args): 
ghatdcli new -n "awesome-service" -m "github.com/ooaklee/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"

# Example command (long-args):
ghatdcli new --name "awesome-service"  --module "github.com/ooaklee/awesome-service" --with-details "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
`,
	}

	webAppCmd.Run = runCmdNew(cmdFlags)

	// Flags
	webAppCmd.Flags().StringVarP(&cmdFlags.AppName, "name", "n", "", "the name of the app being created")
	webAppCmd.Flags().StringVarP(&cmdFlags.AppModuleName, "module", "m", "", "(optional) the name that should be given to the generated app. must start with 'github.com/'")
	webAppCmd.Flags().StringVarP(&cmdFlags.DetailUrls, "with-details", "w", "", "a comma separated list of github urls pointing to valid ghatd details")

	return webAppCmd
}

// runCmdNew the function that is called when the command is ran
func runCmdNew(flags *CommandNewFlags) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runCmdNewHolder(flags); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runCmdNewHolder handles initialising and running the "new" command
func runCmdNewHolder(flags *CommandNewFlags) error {

	// ghatdcli new -n "awesome-service" -m "github.com/ooaklee/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
	const defaultModuleTemplate string = "github.com/ooaklee/%s"
	const deafultGithubDomain string = "github.com"
	var defaultGithubDomainWithHttps string = "https://" + deafultGithubDomain

	var appName string = flags.AppName
	var appModuleName string = flags.AppModuleName
	var detailUrls []string = strings.Split(flags.DetailUrls, ",")

	// Validate
	if appName == "" {
		log.Default().Println("app name not provided")
		return errors.New(common.ErrKeyAppNameInvalidError)
	}

	appName = strings.ReplaceAll(toolbox.StringStandardisedToLower(appName), " ", "-")

	if appModuleName == "" {
		appModuleName = fmt.Sprintf(defaultModuleTemplate, appName)
	}

	if appModuleName != "" {

		// Make sure everything is lowercase
		appModuleName = toolbox.StringStandardisedToLower(appModuleName)

		// Check if module has a valid github name
		if !strings.HasPrefix(appModuleName, deafultGithubDomain) {
			log.Default().Println("app module name not in expected format")
			return errors.New(common.ErrKeyAppModuleNameInvalidError)
		}

	}

	var validDetailUrls []string
	var invalidDetailUrls []string

	// standardise
	if len(detailUrls) > 0 {

		for _, detailUrl := range detailUrls {
			detailUrl = toolbox.StringStandardisedToLower(detailUrl)

			// todo: add better validation
			// on mvp should:
			// - start with github.com or https://github.com
			if strings.HasPrefix(detailUrl, deafultGithubDomain) || strings.HasPrefix(detailUrl, defaultGithubDomainWithHttps) {
				validDetailUrls = append(validDetailUrls, detailUrl)
				continue
			}

			invalidDetailUrls = append(invalidDetailUrls, detailUrl)
			continue
		}
	}

	if len(invalidDetailUrls) > 0 {
		log.Default().Println("invalid detail url(s) provided")
		return errors.New(common.ErrKeyDetailUrlInvalidError)
	}

	// DO THINGS
	fmt.Println("\nThis will handle creating new full stack application based on params passed")
	fmt.Println("\nDOES SOMETHINGS WITH:", "\n  - APP NAME:", appName, "\n  - APP MODULE:", appModuleName, "\n  - APP DETAILS:", validDetailUrls)

	// TODO:
	//
	// Steps to creating a base directory for new app
	//
	// Utilise packages: https://github.com/go-git/go-git  https://github.com/otiai10/copy
	//
	// from ghatd:
	// - cmd
	// - internal
	//   - exclude internal/cli
	// - testing
	// - main.go
	// - go.mod (will have to replace module name with one generated from user, only take the first 'require' block)
	//
	// from ghatd WEB detail:
	// - [verify]
	//   - the detail's ghatd-conf.yaml
	// - [verify]
	//   - the version go in go.mod (base) matches the version in detail
	// - [merge]
	//   - merge require in go.mod (base) with require from detail's go.mod
	// - external -> internal/web
	//   - (update module references accordingly throughout to use)
	// - [reference injection]
	//   - copy packages reference in details' web.go `//>ghatd {{ define "WebDetailImports"..` to base's `ghatd/cmd/server/server.go`  in `//>ghatd {{ block "WebDetailImports" . }}...`
	//   - copy code reference within details' web.go `//>ghatd {{ define "WebDetailInit" }}...` to base's `ghatd/cmd/server/server.go`  in `//>ghatd {{ block "WebDetailInit" . }}...`
	//   - get the embed reference(s) from the details' web.go, defined within `//>ghatd {{ define "WebDetailEmbeds" }}...`, update path accordingly, i.e. external/ui/html/*.tmpl.html -> internal/web/ui/html/*.tmpl.html, and add to base's main.go `//go:embed tag` (which can be found by looking for the nearest embed.FS to the `//>ghatd {{ define "WebDetailEmbeds" }}...`)
	//
	// from ghatd API detail:
	// - [verify]
	//   - the detail's ghatd-conf.yaml
	// - [verify]
	//   - the version go in go.mod (base) matches the version in detail
	// - [merge]
	//   - merge require in go.mod (base) with require from detail's go.mod
	// - external -> internal
	//   - (update module references accordingly throughout to use)
	// - [reference injection]
	//   - copy packages reference in details' api.go `//>ghatd {{ define "ApiDetailImports"..` to base's `ghatd/cmd/server/server.go`  in `//>ghatd {{ block "ApiDetailImports" . }}...`
	//   - copy code reference within details' api.go `//>ghatd {{ define "ApiDetailInit" }}...` to base's `ghatd/cmd/server/server.go`  in `//>ghatd {{ block "ApiDetailInit" . }}...`
	//   - get the embed reference(s) from the details' api.go, defined within `//>ghatd {{ define "ApiDetailEmbeds" }}...`, update path accordingly, i.e. external/web/ui/html/responses/*.tmpl.html -> internal/web/ui/html/responses/*.tmpl.html, and add to base's main.go `//go:embed tag` (which can be found by looking for the nearest embed.FS to the `//>ghatd {{ define "ApiDetailEmbeds" }}...`)
	//
	return nil
}
