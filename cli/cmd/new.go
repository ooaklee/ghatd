package cmd

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"

	"github.com/ooaklee/ghatd/internal/cli/common"
	"github.com/ooaklee/ghatd/internal/cli/config"
	"github.com/ooaklee/ghatd/internal/cli/reader"

	"github.com/go-git/go-git/v5"
	cp "github.com/otiai10/copy"
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

	// TARGET: ghatdcli new -n "awesome-service" -m "github.com/ooaklee/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
	const defaultGhatdModule string = "github.com/ooaklee/ghatd"
	const defaultModuleTemplate string = "github.com/ooaklee/%s"
	const deafultGithubDomain string = "github.com"
	var defaultGithubDomainWithHttps string = "https://" + deafultGithubDomain

	// TODO: will need to update to correct version for release
	const defaultGhatdGoModVersion string = "github.com/ooaklee/ghatd v0.1.1-0.20240316161116-dc3d856805a7"

	// TODO: Update cli to use packr so the required files can be passed with binary
	// or update to clone ghatd repo at specific reference
	var pathToDirectoryOfBaseFiles string = "."

	var appName string = flags.AppName
	var appModuleName string = flags.AppModuleName
	var detailUrls []string = strings.Split(flags.DetailUrls, ",")
	var validDetailUrls []string
	var invalidDetailUrls []string

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

		// Check to make sure module isn't the same name as the ghatd
		// repo
		if appModuleName == defaultGhatdModule {
			log.Default().Println("generated app module name will clash with base ghatd module name")
			return errors.New(common.ErrKeyAppModuleNameInvalidError)
		}

	}

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

	// Steps to creating a base directory for new app
	//
	// Utilise packages: https://github.com/go-git/go-git  https://github.com/otiai10/copy
	//
	// Example of command (pre-compiled):
	// go run cli/cli.go new -n "awesome-service" -m "github.com/some-user/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
	//
	//
	// from ghatd:
	// - cmd
	// - internal
	//   - exclude internal/cli
	// - testing
	// - main.go
	// - go.mod (will have to replace module name with one generated from user, only take the first 'require' block)
	//
	newAppRepoPath := filepath.Join(os.TempDir(), appName)

	fmt.Println("\npath to new service temp dir\n", newAppRepoPath)

	err := os.MkdirAll(newAppRepoPath, os.ModePerm)
	if err != nil {
		log.Default().Printf("unable to create new app's dir at %s\n", newAppRepoPath)
		return err
	}

	opt := cp.Options{
		Skip: func(info os.FileInfo, src, dest string) (bool, error) {

			// Skip copy if cli
			if strings.HasPrefix(dest, fmt.Sprintf("%s/internal/cli", newAppRepoPath)) {
				return true, nil
			}
			return false, nil
		},
	}
	err = cp.Copy(fmt.Sprintf("%s/cmd", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/cmd", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return err
	}

	err = cp.Copy(fmt.Sprintf("%s/internal", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/internal", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return err
	}

	err = cp.Copy(fmt.Sprintf("%s/testing", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/testing", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return err
	}

	err = cp.Copy(fmt.Sprintf("%s/main.go", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/main.go", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return err
	}

	err = cp.Copy(fmt.Sprintf("%s/go.mod", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/go.mod", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return err
	}

	// edit go.mod - Replace lines
	input, err := os.ReadFile(fmt.Sprintf("%s/go.mod", newAppRepoPath))
	if err != nil {
		log.Fatalln(err)
	}

	lines := strings.Split(string(input), "\n")

	for i, line := range lines {
		if strings.HasPrefix(line, "module github.com/ooaklee/ghatd") {
			lines[i] = fmt.Sprintf("module %s", appModuleName)
		}

		if strings.Contains(line, "//>ghatd {{ block .DetailModGhatdPackage }}{{ end }}") {
			lines[i] = fmt.Sprintf("	%s", defaultGhatdGoModVersion)
		}

	}
	output := strings.Join(lines, "\n")
	err = os.WriteFile(fmt.Sprintf("%s/go.mod", newAppRepoPath), []byte(output), 0644)
	if err != nil {
		log.Fatalln(err)
	}

	// Note: At this point, if you go to path of the new service and run `go mod tidy` inside,
	// you have a running app

	for _, detailsRepo := range validDetailUrls {

		detailOutput := fmt.Sprintf("%s/%s", os.TempDir(), toolbox.GenerateNanoId())

		if !strings.HasPrefix(detailsRepo, "https://") {
			detailsRepo = fmt.Sprintf("https://%s", detailsRepo)
		}

		// Clone the given repository to the given directory
		_, err := git.PlainClone(detailOutput, false, &git.CloneOptions{
			URL:               detailsRepo,
			RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		})

		if err != nil {
			log.Default().Printf("unable to clone provided detail repo - %s\n", detailsRepo)
			return err
		}

		detailConfig := config.DetailConfig{}
		err = reader.UnmarshalLocalFile(fmt.Sprintf("%s/ghatd-conf.yaml", detailOutput), &detailConfig)
		if err != nil {
			log.Default().Println("unable to read the config file in the detail repo ")
			return err
		}

		// TODO: Verification steps
		// - [verify]
		//   - the detail's ghatd-conf.yaml

		switch detailConfig.Type {
		case "web":
			// from ghatd WEB detail:
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
			log.Default().Println("action web detail")
			_ = cleanUpDetail(detailOutput)
			continue

		case "api":
			// from ghatd API detail:
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
			log.Default().Println("action api detail")
			_ = cleanUpDetail(detailOutput)
			continue

		default:
			log.Default().Printf("unsupported type provided in the detail repo: %s", detailConfig.Type)
			_ = cleanUpDetail(detailOutput)
			return errors.New(common.ErrKeyDetailTypeInvalidError)
		}

		// edit go.mod - Replace lines
		// baseGoModinput, err := os.ReadFile(fmt.Sprintf("%s/go.mod", newAppRepoPath))
		// if err != nil {
		// 	log.Fatalln(err)
		// }

	}

	return nil
}

// cleanUpDetail handles removing detail directory
func cleanUpDetail(dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		log.Default().Printf("unable to remove temp detail target dir - %s\n", dir)
		return err
	}

	return err
}
