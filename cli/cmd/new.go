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
	"github.com/go-git/go-git/v5/plumbing"
	cp "github.com/otiai10/copy"
	"github.com/spf13/cobra"
)

// CommandNewFlags holds the variables that will be set by flags
type CommandNewFlags struct {
	AppName       string
	AppModuleName string
	DetailUrls    string
	BaseBranch    string
	// Output where the created app dir should be created
	Output string
}

// NewCommandNew returns a command for
// creating ghat(d) applications
func NewCommandNew() *cobra.Command {

	var workingDir string
	cmdFlags := &CommandNewFlags{}

	webAppCmd := &cobra.Command{
		Use:   "new",
		Short: "Creates a GHAT(D) compatible Detail",
		Long:  "Creates a GHAT(D) app with the given module while utilising the associated details (building segments)",
		Example: `
# Example command: 
ghatdcli new -n "awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints" -o /tmp

# Example command (short-args): 
ghatdcli new -n "awesome-service" -m "github.com/some-user-org/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"

# Example command (long-args):
ghatdcli new --name "awesome-service"  --module "github.com/some-user-org/awesome-service" --with-details "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
`,
	}

	webAppCmd.Run = runCmdNew(cmdFlags)

	workingDir, _ = os.Getwd()
	if workingDir == "" {
		workingDir = "."
	}

	// Flags
	webAppCmd.Flags().StringVarP(&cmdFlags.AppName, "name", "n", "", "the name of the app being created")
	webAppCmd.Flags().StringVarP(&cmdFlags.AppModuleName, "module", "m", "", "(optional) the name that should be given to the generated app. must start with 'github.com/'")
	webAppCmd.Flags().StringVarP(&cmdFlags.DetailUrls, "with-details", "w", "", "a comma separated list of github urls pointing to valid ghatd details")
	webAppCmd.Flags().StringVarP(&cmdFlags.Output, "output", "o", workingDir, "the storage location for the rendered app")
	////////// TODO: update these references before merged into main
	// will need to point to main
	webAppCmd.Flags().StringVarP(&cmdFlags.BaseBranch, "base-branch", "b", "ghatd-x-implement-cli-new-command", "the ghat(d) branch that the new app's core will be based on")

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
	//
	// Example of command (pre-compiled):
	// go run cli/cli.go new -n "awesome-service" -m "github.com/some-user/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
	const defaultGhatdModule string = "github.com/ooaklee/ghatd"
	const defaultModuleTemplate string = "github.com/ooaklee/%s"
	const deafultGithubDomain string = "github.com"
	var defaultGithubDomainWithHttps string = "https://" + deafultGithubDomain
	// NICE_TO_HAVE: Inclue the required files with the binary
	var pathToDirectoryOfBaseFiles string = "."
	////////// TODO: update these references before merged into main
	// will need to update to correct version for release
	const defaultGhatdGoModVersion string = "github.com/ooaklee/ghatd v0.1.1-0.20240316161116-dc3d856805a7"
	/////////
	var appName string = flags.AppName
	var appModuleName string = flags.AppModuleName
	var detailUrls []string = strings.Split(flags.DetailUrls, ",")
	var outputDirectory string = flags.Output
	var defaultGhatdBranch string = flags.BaseBranch

	appName, appModuleName, validDetailUrls, err := inspectNewCmdFlags(appName, appModuleName, detailUrls, outputDirectory, defaultGhatdModule, defaultModuleTemplate, deafultGithubDomain, defaultGithubDomainWithHttps)
	if err != nil {
		return err
	}

	log.Default().Println(fmt.Sprintf("\ncreating ghat(d) application...\n  - name: %s\n  - app module: %s\n  - including detail(s):", appName, appModuleName), validDetailUrls)

	// NICE_TO_HAVE: Verify user has internet connection

	pathToDirectoryOfBaseFiles, err = getBaseGhatdFiles("https://"+defaultGhatdModule, defaultGhatdBranch)
	if err != nil {
		return err
	}

	newAppRepoPath, opt, err := initNewAppRepo(appName, appModuleName, pathToDirectoryOfBaseFiles, defaultGhatdModule)
	if err != nil {
		return err
	}

	err = updateNewAppStructureGoMod(defaultGhatdGoModVersion, defaultGhatdModule, appModuleName, newAppRepoPath)
	if err != nil {
		return err
	}

	// Note: At this point, if you go to path of the new service and run `go mod tidy` inside,
	// you have a running app

	for _, detailsRepo := range validDetailUrls {

		detailOutput, detailConfig, err := getDetailRepo(detailsRepo)
		if err != nil {
			return err
		}

		switch detailConfig.Type {
		case "api", "web":
			// from ghatd API detail:
			// - [verify]
			//   - the version go in go.mod (base) matches the version in detail

			fmt.Printf("\nstarting integration of %s detail - %s\n", detailConfig.Type, detailsRepo)

			// TODO: use detailGoVersion for verification
			detailModuleName, _, detailGoModRequirePackages, err := getDetailModInfo(detailOutput, detailConfig.Type)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			detailImports, detailEmbeds, detailInitSteps, err := getDetailEntryGoInfo(detailOutput, detailConfig.Type)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			detailImports, detailEmbeds = convertExtractedDetailInfoToNewAppStructure(detailImports, detailEmbeds, detailConfig.Type, detailModuleName, appModuleName)

			err = addDetailEmbedsToMainGo(detailEmbeds, newAppRepoPath)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			err = addDetailImportsAndInitBlockToServerGo(detailImports, detailInitSteps, detailConfig.Type, newAppRepoPath)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			err = updateDetailPackageReferenceForNewAppStructure(detailConfig.Type, detailOutput, detailModuleName, appModuleName)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			err = addDetailGoModRequireBlockToGoMod(detailGoModRequirePackages, detailConfig.Type, newAppRepoPath)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			err = copyDetailStructureToNewAppStructure(detailConfig.Type, detailOutput, newAppRepoPath, opt)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			log.Default().Printf("completed integration of %s detail - %s\n\n", detailConfig.Type, detailsRepo)
			_ = cleanUpDirectory(detailOutput)
			continue

		default:
			log.Default().Printf("\nunsupported type provided in the detail repo (%s): %s\n", detailsRepo, detailConfig.Type)
			_ = cleanUpDirectories([]string{detailOutput, pathToDirectoryOfBaseFiles})
			return errors.New(common.ErrKeyDetailTypeInvalidError)
		}
	}

	_ = cleanUpDirectory(pathToDirectoryOfBaseFiles)

	return moveNewAppToOutputDirectory(newAppRepoPath, outputDirectory, appName)
}

// getBaseGhatdFiles handles pulling the base repo for ghat(d) to use on base
func getBaseGhatdFiles(ghatdRepoUrl, branch string) (string, error) {
	baseGhatdOutput := fmt.Sprintf("%s/%s", os.TempDir(), toolbox.GenerateNanoId())

	fmt.Printf("\nfetching ghat(d) base repo - %s @ %s\n", ghatdRepoUrl, branch)

	// Clone the given repository to the given directory
	_, err := git.PlainClone(baseGhatdOutput, false, &git.CloneOptions{
		URL:               ghatdRepoUrl,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
		ReferenceName:     plumbing.ReferenceName(branch),
	})

	if err != nil {
		log.Default().Printf("unable to clone provided ghat(d) base repo - %s @ %s\n", ghatdRepoUrl, branch)
		return "", err
	}

	return baseGhatdOutput, nil

}

// moveNewAppToOutputDirectory manages the task of moving an app from its
// temporary directory to a specified location.
func moveNewAppToOutputDirectory(newAppRepoPath, outputDirectory, appName string) error {
	var oldLocation string = newAppRepoPath
	var newLocation string = outputDirectory + "/" + appName
	err := os.Rename(oldLocation, newLocation)
	if err != nil {
		log.Default().Printf("unable to move app from %s to %s", oldLocation, newLocation)

		return err
	}

	log.Default().Printf(`great news! the ghat(d) application '%s' has been created! be sure to check out its folder at %s
	
get ready for an awesome experience with ghat(d)!
`, appName, newLocation)

	return nil
}

// inspectNewCmdFlags is responsible for validating and standardising the flags that are passed while
// calling the "new" command. Its purpose is to ensure that the command has all the necessary components
// required to create a valid Ghat(d) compatible application. At this stage, most of the checks and
// validations are only of a surface level.
func inspectNewCmdFlags(appName, appModuleName string, detailUrls []string, outputDirectory, defaultGhatdModule, defaultModuleTemplate, deafultGithubDomain, defaultGithubDomainWithHttps string) (string, string, []string, error) {
	var validDetailUrls []string
	var invalidDetailUrls []string

	// Validate
	if appName == "" {
		log.Default().Println("app name not provided")
		return "", "", []string{}, errors.New(common.ErrKeyAppNameInvalidError)
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
			return "", "", []string{}, errors.New(common.ErrKeyAppModuleNameInvalidError)
		}

		// Check to make sure module isn't the same name as the ghatd
		// repo
		if appModuleName == defaultGhatdModule {
			log.Default().Println("generated app module name will clash with base ghatd module name")
			return "", "", []string{}, errors.New(common.ErrKeyAppModuleNameInvalidError)
		}

	}

	// TODO: Implement logic to get absolute path
	// for passed output directory
	filepath.Abs(outputDirectory)

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
		return "", "", []string{}, errors.New(common.ErrKeyDetailUrlInvalidError)
	}

	return appName, appModuleName, validDetailUrls, nil
}

// getDetailRepo clones the provided detail to a local temporary directory, extracts the ghatd
// conf file and verifies if it's safe to proceed with the given detail.
func getDetailRepo(detailsRepoUrl string) (string, *config.DetailConfig, error) {

	detailOutput := fmt.Sprintf("%s/%s", os.TempDir(), toolbox.GenerateNanoId())

	if !strings.HasPrefix(detailsRepoUrl, "https://") {
		detailsRepoUrl = fmt.Sprintf("https://%s", detailsRepoUrl)
	}

	// Clone the given repository to the given directory
	_, err := git.PlainClone(detailOutput, false, &git.CloneOptions{
		URL:               detailsRepoUrl,
		RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
	})

	if err != nil {
		log.Default().Printf("unable to clone provided detail repo - %s\n", detailsRepoUrl)
		return "", nil, err
	}

	detailConfig := config.DetailConfig{}
	err = reader.UnmarshalLocalFile(fmt.Sprintf("%s/ghatd-conf.yaml", detailOutput), &detailConfig)
	if err != nil {
		log.Default().Println("unable to read the config file in the detail repo ")
		return "", nil, err
	}

	// TODO: Verification steps
	// - [verify]
	//   - the detail's ghatd-conf.yaml

	return detailOutput, &detailConfig, nil
}

// initNewAppRepo creates a new app directory/repo based on the ghatd repository's default.
// It overwrites existing directories or base files.
//
// Example of command (pre-compiled):
//
// go run cli/cli.go new -n "awesome-service" -m "github.com/some-user/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
//
// from ghatd:
//   - cmd
//   - internal (exclude internal/cli)
//   - testing (exclude anything to do with cli)
//   - main.go
//   - go.mod (will have to replace module name with one generated from user, only take the first 'require' block)
func initNewAppRepo(appName, appModuleName, pathToDirectoryOfBaseFiles, defaultGhatdModule string) (string, *cp.Options, error) {

	newAppRepoPath := filepath.Join(os.TempDir(), appName)

	opt := cp.Options{
		Skip: func(info os.FileInfo, src, dest string) (bool, error) {

			// Skip copy if cli
			if strings.HasPrefix(dest, fmt.Sprintf("%s/internal/cli", newAppRepoPath)) {
				return true, nil
			}
			return false, nil
		},
	}

	fmt.Printf("\npath to new service temp dir: %s\n", newAppRepoPath)

	err := os.MkdirAll(newAppRepoPath, os.ModePerm)
	if err != nil {
		log.Default().Printf("unable to create new app's dir at %s\n", newAppRepoPath)
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/cmd", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/cmd", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/internal", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/internal", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/testing", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/testing", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/main.go", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/main.go", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	// Update to use new app server
	err = toolbox.Refactor(true, fmt.Sprintf("%s/cmd/server", defaultGhatdModule), fmt.Sprintf("%s/cmd/server", appModuleName), fmt.Sprintf("%s/.", newAppRepoPath), "main.go")
	if err != nil {
		log.Default().Println("unable to replace server found")
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/go.mod", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/go.mod", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	return newAppRepoPath, &opt, nil
}

func updateNewAppStructureGoMod(defaultGhatdGoModVersion, defaultGhatdModule, appModuleName, newAppRepoPath string) error {
	// edit go.mod - Replace lines
	newAppGoModPath := fmt.Sprintf("%s/go.mod", newAppRepoPath)
	newAppGoMod, err := os.ReadFile(newAppGoModPath)
	if err != nil {
		log.Default().Printf("unable to get new app's go.mod - %s", newAppGoModPath)
		return err

	}

	newAppGoModLines := strings.Split(string(newAppGoMod), "\n")

	for i, line := range newAppGoModLines {
		if strings.HasPrefix(line, fmt.Sprintf("module %s", defaultGhatdModule)) {
			newAppGoModLines[i] = fmt.Sprintf("module %s", appModuleName)
		}

		if strings.Contains(line, "//>ghatd {{ block .DetailModGhatdPackage }}{{ end }}") {
			newAppGoModLines[i] = fmt.Sprintf("	%s", defaultGhatdGoModVersion)
		}

	}
	newAppGoModOutput := strings.Join(newAppGoModLines, "\n")
	err = os.WriteFile(newAppGoModPath, []byte(newAppGoModOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to updadte new app's go.mod - %s", newAppGoModPath)
		return err
	}
	return nil
}

func copyDetailStructureToNewAppStructure(detailType, detailOutput, newAppRepoPath string, opt *cp.Options) error {

	var detailExternalDir string
	var newAppStructureTargetDir string
	var detailTestingDir string
	var newAppStructureTestingDir string

	if detailType == "web" {
		detailExternalDir = fmt.Sprintf("%s/external", detailOutput)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal/web", newAppRepoPath)
	}

	if detailType == "api" {
		detailExternalDir = fmt.Sprintf("%s/external", detailOutput)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal", newAppRepoPath)
		detailTestingDir = fmt.Sprintf("%s/testing", detailOutput)
		newAppStructureTestingDir = fmt.Sprintf("%s/testing", newAppRepoPath)

	}

	err := cp.Copy(detailExternalDir, newAppStructureTargetDir, *opt)
	if err != nil {
		log.Default().Printf("unable to copy %s detail external dir to new destination", detailType)
		return err
	}

	if detailType == "api" {
		err = cp.Copy(detailTestingDir, newAppStructureTestingDir, *opt)
		if err != nil {
			log.Default().Printf("unable to copy %s detail testing dir to new destination", detailType)
			return err
		}
	}

	return nil
}

func updateDetailPackageReferenceForNewAppStructure(detailType, detailOutput, detailModuleName, appModuleName string) error {

	var detailExternalDir string
	var newAppStructureTargetDir string

	if detailType == "web" {
		detailExternalDir = fmt.Sprintf("%s/external", detailModuleName)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal/web", appModuleName)
	}

	if detailType == "api" {
		detailExternalDir = fmt.Sprintf("%s/external", detailModuleName)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal", appModuleName)
	}

	err := toolbox.Refactor(false, detailExternalDir, newAppStructureTargetDir, fmt.Sprintf("%s/.", detailOutput), "*.go")
	if err != nil {
		log.Default().Println("unable to replace strings found")
		return err
	}

	if detailType == "api" {
		err = toolbox.Refactor(false, fmt.Sprintf("%s/testing", detailModuleName), fmt.Sprintf("%s/testing", appModuleName), fmt.Sprintf("%s/.", detailOutput), "*.go")
		if err != nil {
			log.Default().Println("unable to replace strings found")
			return err
		}
	}

	return nil
}

func addDetailGoModRequireBlockToGoMod(detailGoModRequirePackages []string, detailType, newAppRepoPath string) error {

	var ghatdGoModRequirePackagesTag string

	if detailType == "web" {
		ghatdGoModRequirePackagesTag = "WebDetailGoModRequirePackages"
	}

	if detailType == "api" {
		ghatdGoModRequirePackagesTag = "ApiDetailGoModRequirePackages"
	}

	newAppGoModPath := fmt.Sprintf("%s/go.mod", newAppRepoPath)

	newAppGoModInput, err := os.ReadFile(newAppGoModPath)
	if err != nil {
		log.Default().Printf("unable to read new app go.mod file - %s\n", newAppGoModPath)
		return err
	}

	newAppGoModLines := strings.Split(string(newAppGoModInput), "\n")

	for i, line := range newAppGoModLines {
		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ block .%s }}{{ end }}", ghatdGoModRequirePackagesTag)) {
			newAppGoModLines[i] = line + "\n" + strings.Join(detailGoModRequirePackages, "\n")
		}

	}
	newAppGoModOutput := strings.Join(newAppGoModLines, "\n")
	err = os.WriteFile(newAppGoModPath, []byte(newAppGoModOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to update new app go.mod file - %s\n", newAppGoModPath)
		return err
	}

	return nil

}

func addDetailImportsAndInitBlockToServerGo(detailImports, detailInitSteps []string, detailType, newAppRepoPath string) error {

	var ghatdImportsTag string
	var ghatdInitTag string

	if detailType == "web" {
		ghatdImportsTag = "WebDetailImports"
		ghatdInitTag = "WebDetailInit"
	}

	if detailType == "api" {
		ghatdImportsTag = "ApiDetailImports"
		ghatdInitTag = "ApiDetailInit"
	}

	newAppServerGoPath := fmt.Sprintf("%s/cmd/server/server.go", newAppRepoPath)

	newAppCmdServerInput, err := os.ReadFile(newAppServerGoPath)
	if err != nil {
		log.Default().Printf("unable to read new app server.go file - %s\n", newAppServerGoPath)
		return err
	}

	newAppCmdServerLines := strings.Split(string(newAppCmdServerInput), "\n")

	for i, line := range newAppCmdServerLines {
		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ block \"%s\" . }}", ghatdImportsTag)) {
			newAppCmdServerLines[i] = line + "\n" + strings.Join(detailImports, "\n")
		}

		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ block \"%s\" . }}", ghatdInitTag)) {
			newAppCmdServerLines[i] = line + "\n" + strings.Join(detailInitSteps, "\n")
		}
	}

	newAppCmdServerOutput := strings.Join(newAppCmdServerLines, "\n")
	err = os.WriteFile(newAppServerGoPath, []byte(newAppCmdServerOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to update new app server.go file - %s\n", newAppServerGoPath)
		return err
	}

	return nil

}

func addDetailEmbedsToMainGo(detailEmbeds, newAppRepoPath string) error {

	// edit main.go - add embeds
	newAppMainGoPath := fmt.Sprintf("%s/main.go", newAppRepoPath)

	newAppMainGoInput, err := os.ReadFile(newAppMainGoPath)
	if err != nil {
		log.Default().Printf("unable to read new app main.go file - %s\n", newAppMainGoPath)
		return err
	}

	newAppMainGoLines := strings.Split(string(newAppMainGoInput), "\n")

	for i, line := range newAppMainGoLines {
		if strings.Contains(line, "//go:embed ") {

			// if go:embed is commented, uncomment
			if strings.HasPrefix(line, "// //go:embed ") {
				line = strings.Replace(line, "// //go:embed ", "//go:embed ", -1)
			}

			newAppMainGoLines[i] = addStringIfItDoesExist(line, detailEmbeds)
		}
	}

	newAppMainGoOutput := strings.Join(newAppMainGoLines, "\n")
	err = os.WriteFile(newAppMainGoPath, []byte(newAppMainGoOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to update new app main.go file - %s\n", newAppMainGoPath)
		return err
	}

	return nil
}

func convertExtractedDetailInfoToNewAppStructure(detailImports []string, detailEmbeds, detailType, detailModuleName, appModuleName string) ([]string, string) {

	var targetModulePath string
	var migratedEmbedPath string

	if detailType == "web" {
		targetModulePath = fmt.Sprintf("%s/internal/web", appModuleName)
		migratedEmbedPath = "internal/web/"

	}

	if detailType == "api" {
		targetModulePath = fmt.Sprintf("%s/internal", appModuleName)
		migratedEmbedPath = "internal/"
	}

	for i, path := range detailImports {
		detailImports[i] = strings.ReplaceAll(path, fmt.Sprintf("%s/external", detailModuleName), targetModulePath)
	}

	embeds := strings.Split(detailEmbeds, " ")
	for i, path := range embeds {
		embeds[i] = strings.ReplaceAll(path, "external/", migratedEmbedPath)
	}
	detailEmbeds = strings.Join(embeds, " ")

	return detailImports, detailEmbeds

}

// getDetailEntryGoInfo handles pulling out information from the
func getDetailEntryGoInfo(detailPath, detailType string) ([]string, string, []string, error) {

	var ghatdImportTag string
	var startOfDetailImports int
	var endOfDetailImports int
	var ghatdInitTag string
	var startOfDetailInit int
	var endOfDetailInit int
	var detailEmbeds string
	var detailEntryPoint string

	var usedGhatdEndTagPoints []int

	if detailType == "web" {
		ghatdImportTag = "WebDetailImports"
		ghatdInitTag = "WebDetailInit"
		detailEntryPoint = "web.go"
	}

	if detailType == "api" {
		ghatdImportTag = "ApiDetailImports"
		ghatdInitTag = "ApiDetailInit"
		detailEntryPoint = "api.go"
	}

	detailEntryPointPath := fmt.Sprintf("%s/%s", detailPath, detailEntryPoint)

	detailGoEntryPoint, err := os.ReadFile(detailEntryPointPath)
	if err != nil {
		log.Default().Printf("unable to get detail entry point information - %s", detailEntryPointPath)
		return []string{}, "", []string{}, err
	}
	detailGoEntryPointLines := strings.Split(string(detailGoEntryPoint), "\n")

	for i, line := range detailGoEntryPointLines {

		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ define \"%s\" }}", ghatdImportTag)) {
			startOfDetailImports = i
		}

		if strings.Contains(line, "//>ghatd {{ end }}") && len(usedGhatdEndTagPoints) == 0 {
			endOfDetailImports = i

		}
		if strings.Contains(line, "//>ghatd {{ end }}") {
			usedGhatdEndTagPoints = append(usedGhatdEndTagPoints, i)
		}

		if strings.HasPrefix(line, "//go:embed ") {
			detailEmbeds = strings.ReplaceAll(line, "//go:embed ", "")
		}

		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ define \"%s\" }}", ghatdInitTag)) {
			startOfDetailInit = i
		}

		if detailType == "web" {
			if strings.Contains(line, "embeddedContentFilePathPrefix,") {
				detailGoEntryPointLines[i] = strings.ReplaceAll(line, "embeddedContentFilePathPrefix,", "embeddedContentFilePathPrefix + \"web/\",")
			}
		}

		if strings.Contains(line, "//>ghatd {{ end }}") && len(usedGhatdEndTagPoints) == 2 {
			endOfDetailInit = i
		}

	}

	return detailGoEntryPointLines[(startOfDetailImports + 1):endOfDetailImports], detailEmbeds, detailGoEntryPointLines[(startOfDetailInit + 1):endOfDetailInit], nil
}

// getDetailModInfo handles pulling information out of the detail's go.mod file and returning it
func getDetailModInfo(detailPath, detailType string) (string, string, []string, error) {

	var ghatdRequireTag string
	var detailModuleName string
	var detailGoVersion string
	var startOfDetailModRequirePackages int
	var endOfDetailModRequirePackages int

	if detailType == "web" {
		ghatdRequireTag = "WebDetailGoModRequirePackages"

	}
	if detailType == "api" {
		ghatdRequireTag = "ApiDetailGoModRequirePackages"

	}

	detailGoModPath := fmt.Sprintf("%s/go.mod", detailPath)
	detailGoMod, err := os.ReadFile(detailGoModPath)

	if err != nil {
		log.Default().Printf("unable to get detail go.mod information - %s", detailGoModPath)
		return "", "", []string{}, err
	}
	detailGoModLines := strings.Split(string(detailGoMod), "\n")

	for i, line := range detailGoModLines {
		if strings.HasPrefix(line, "module ") {
			detailModuleName = strings.ReplaceAll(line, "module ", "")
		}

		if strings.HasPrefix(line, "go ") {
			detailGoVersion = strings.ReplaceAll(line, "go ", "")
		}

		if strings.Contains(line, fmt.Sprintf("//>ghatd {{ define \"%s\" }}", ghatdRequireTag)) {
			startOfDetailModRequirePackages = i
		}

		if strings.Contains(line, "//>ghatd {{ end }}") {
			endOfDetailModRequirePackages = i
		}

	}

	return detailModuleName, detailGoVersion, detailGoModLines[(startOfDetailModRequirePackages + 1):endOfDetailModRequirePackages], err

}

// cleanUpDirectories handles removing passed directories.
// Note: we should only run on ghatdbase files if pulling
func cleanUpDirectories(dirs []string) error {
	for _, dir := range dirs {
		_ = cleanUpDirectory(dir)
	}
	return nil
}

// cleanUpDirectory handles removing passed directory
func cleanUpDirectory(dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		log.Default().Printf("unable to remove temp detail target dir - %s\n", dir)
		return err
	}

	return err
}

// addStringIfItDoesExist handles trying to merge to passed strings into one
func addStringIfItDoesExist(baseString, additionalString string) string {
	var additionalValidStrings string
	var splitExtraString []string = strings.Split(additionalString, " ")

	for _, str := range splitExtraString {
		if !strings.Contains(baseString, str) {
			additionalValidStrings += (" " + str)
		}
	}

	return baseString + additionalValidStrings
}
