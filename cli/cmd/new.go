package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"

	"github.com/ooaklee/ghatd/internal/cli/common"
	"github.com/ooaklee/ghatd/internal/cli/config"
	"github.com/ooaklee/ghatd/internal/cli/reader"
	clirepository "github.com/ooaklee/ghatd/internal/cli/repository"

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

// NewCommandNew builds the `ghatdcli new` command.
//
// The command collects the base application name, module path, optional Detail
// repositories, output directory, and GHATD base branch before handing execution
// to the orchestration flow in runCmdNewHolder.
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

// runCmdNew adapts the new-command workflow to Cobra's Run callback shape.
//
// It keeps Cobra-specific error handling at the command boundary so the deeper
// creation functions can return regular errors for tests and reuse.
func runCmdNew(flags *CommandNewFlags) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runCmdNewHolder(flags); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runCmdNewHolder orchestrates creation of a new GHAT(D) application.
//
// The flow validates flags, clones the base GHAT(D) repository, rewrites the
// generated module, integrates each requested Detail, cleans up temporary clone
// directories, and finally moves the generated project into the requested
// output directory.
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
	outputDirectory, err = filepath.Abs(outputDirectory)
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
		case config.DetailTypeWebVite:
			fmt.Printf("\nstarting integration of %s detail - %s\n", detailConfig.Type, detailsRepo)

			err = configureWebViteMainGo(newAppRepoPath)
			if err != nil {
				_ = cleanUpDirectory(detailOutput)
				return err
			}

			err = configureWebViteServerGo(newAppRepoPath)
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

		case config.DetailTypeAPI, config.DetailTypeWeb:
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
			return common.ErrDetailTypeInvalidError
		}
	}

	_ = cleanUpDirectory(pathToDirectoryOfBaseFiles)

	return moveNewAppToOutputDirectory(newAppRepoPath, outputDirectory, appName)
}

// getBaseGhatdFiles clones the selected GHAT(D) branch into a temporary folder.
//
// The returned directory is used as the source tree for the generated
// application before any Detail-specific files are merged in.
func getBaseGhatdFiles(ghatdRepoUrl, branch string) (string, error) {
	baseGhatdOutput := fmt.Sprintf("%s/%s", os.TempDir(), toolbox.GenerateNanoId())

	fmt.Printf("\nfetching ghat(d) base repo - %s @ %s\n", ghatdRepoUrl, branch)

	err := clirepository.Clone(context.Background(), clirepository.CloneRequest{
		Source:            ghatdRepoUrl,
		Destination:       baseGhatdOutput,
		Branch:            branch,
		RecurseSubmodules: true,
	})

	if err != nil {
		log.Default().Printf("unable to clone provided ghat(d) base repo - %s @ %s\n", ghatdRepoUrl, branch)
		return "", err
	}

	return baseGhatdOutput, nil

}

// moveNewAppToOutputDirectory moves the generated app out of temporary storage.
//
// The destination is the requested output directory plus the generated
// application name, which gives callers a predictable final project path.
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

// inspectNewCmdFlags validates and standardises the `new` command inputs.
//
// It normalises the app name, derives a default module path when one is not
// supplied, checks that the module cannot collide with GHAT(D)'s own module, and
// filters the requested Detail repository list down to supported GitHub source
// formats.
func inspectNewCmdFlags(appName, appModuleName string, detailUrls []string, outputDirectory, defaultGhatdModule, defaultModuleTemplate, deafultGithubDomain, defaultGithubDomainWithHttps string) (string, string, []string, error) {
	var validDetailUrls []string
	var invalidDetailUrls []string

	// Validate
	if appName == "" {
		log.Default().Println("app name not provided")
		return "", "", []string{}, common.ErrAppNameInvalidError
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
			return "", "", []string{}, common.ErrAppModuleNameInvalidError
		}

		// Check to make sure module isn't the same name as the ghatd
		// repo
		if appModuleName == defaultGhatdModule {
			log.Default().Println("generated app module name will clash with base ghatd module name")
			return "", "", []string{}, common.ErrAppModuleNameInvalidError
		}

	}

	// standardise
	if len(detailUrls) > 0 {

		for _, detailUrl := range detailUrls {
			detailUrl = strings.TrimSpace(toolbox.StringStandardisedToLower(detailUrl))

			if detailUrl == "" {
				continue
			}

			if isSupportedDetailRepoSource(detailUrl, deafultGithubDomain, defaultGithubDomainWithHttps) {
				validDetailUrls = append(validDetailUrls, detailUrl)
				continue
			}

			invalidDetailUrls = append(invalidDetailUrls, detailUrl)
			continue
		}
	}

	if len(invalidDetailUrls) > 0 {
		log.Default().Println("invalid detail url(s) provided")
		return "", "", []string{}, common.ErrDetailUrlInvalidError
	}

	return appName, appModuleName, validDetailUrls, nil
}

// isSupportedDetailRepoSource reports whether a Detail source can be cloned.
//
// The CLI accepts the common GitHub forms users are likely to paste in:
// owner/repo, github.com/owner/repo, HTTPS, SCP-style SSH, and ssh:// URLs. It
// rejects local paths and non-GitHub schemes so the generated app only consumes
// expected remote Detail repositories.
func isSupportedDetailRepoSource(detailUrl, defaultGithubDomain, defaultGithubDomainWithHttps string) bool {
	if strings.HasPrefix(detailUrl, defaultGithubDomain+"/") {
		return hasOwnerRepoPath(strings.TrimPrefix(detailUrl, defaultGithubDomain+"/"))
	}

	if strings.HasPrefix(detailUrl, defaultGithubDomainWithHttps+"/") {
		return hasOwnerRepoPath(strings.TrimPrefix(detailUrl, defaultGithubDomainWithHttps+"/"))
	}

	if strings.HasPrefix(detailUrl, "git@"+defaultGithubDomain+":") {
		return hasOwnerRepoPath(strings.TrimPrefix(detailUrl, "git@"+defaultGithubDomain+":"))
	}

	if strings.HasPrefix(detailUrl, "ssh://git@"+defaultGithubDomain+"/") {
		return hasOwnerRepoPath(strings.TrimPrefix(detailUrl, "ssh://git@"+defaultGithubDomain+"/"))
	}

	if strings.Contains(detailUrl, "://") || strings.HasPrefix(detailUrl, "/") || strings.HasPrefix(detailUrl, ".") || strings.Contains(detailUrl, ":") {
		return false
	}

	return hasOwnerRepoPath(detailUrl)
}

// hasOwnerRepoPath checks that a repository path contains exactly owner and repo.
//
// A trailing `.git` suffix is ignored so both `owner/repo` and
// `owner/repo.git` are treated as the same valid source shape.
func hasOwnerRepoPath(ownerRepoPath string) bool {
	ownerRepoPath = strings.TrimSuffix(ownerRepoPath, ".git")
	parts := strings.Split(ownerRepoPath, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

// getDetailRepo clones a Detail repository and validates its GHAT(D) config.
//
// The returned temporary directory is later copied into the generated app. The
// Detail's `ghatd-conf.yaml` controls which integration path is used.
func getDetailRepo(detailsRepoUrl string) (string, *config.DetailConfig, error) {

	detailOutput := fmt.Sprintf("%s/%s", os.TempDir(), toolbox.GenerateNanoId())

	err := clirepository.Clone(context.Background(), clirepository.CloneRequest{
		Source:            detailsRepoUrl,
		Destination:       detailOutput,
		RecurseSubmodules: true,
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
	err = config.ValidateDetailConfig(&detailConfig)
	if err != nil {
		log.Default().Println("unable to validate the config file in the detail repo ")
		return "", nil, err
	}

	// TODO: Verification steps
	// - [verify]
	//   - the detail's ghatd-conf.yaml

	return detailOutput, &detailConfig, nil
}

// initNewAppRepo creates the temporary application tree from the GHAT(D) base.
//
// It copies the commands, migration scaffold, internal packages, testing
// support, main entrypoint, and go.mod into a temporary app directory while
// excluding CLI internals that should not be shipped in generated projects. It
// also rewrites host-owned command and migration imports to the generated app
// module.
//
// Example of command (pre-compiled):
//
// go run cli/cli.go new -n "awesome-service" -m "github.com/some-user/awesome-service" -w "github.com/ooaklee/ghatd-detail-web-demo-landing-dash-and-more,github.com/ooaklee/ghatd-detail-api-demo-endpoints"
//
// from ghatd:
//   - cmd
//   - migrations
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

	err = cp.Copy(fmt.Sprintf("%s/migrations", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/migrations", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy migrations directory to new destination")
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

	err = toolbox.Refactor(true, fmt.Sprintf("%s/cmd/mongo-migrator", defaultGhatdModule), fmt.Sprintf("%s/cmd/mongo-migrator", appModuleName), fmt.Sprintf("%s/.", newAppRepoPath), "main.go")
	if err != nil {
		log.Default().Println("unable to replace mongo migrator command import")
		return "", nil, err
	}

	err = toolbox.Refactor(true, fmt.Sprintf("%s/migrations/mongo", defaultGhatdModule), fmt.Sprintf("%s/migrations/mongo", appModuleName), fmt.Sprintf("%s/.", newAppRepoPath), "migrator.go")
	if err != nil {
		log.Default().Println("unable to replace mongo migrations import")
		return "", nil, err
	}

	err = cp.Copy(fmt.Sprintf("%s/go.mod", pathToDirectoryOfBaseFiles), fmt.Sprintf("%s/go.mod", newAppRepoPath), opt)
	if err != nil {
		log.Default().Println("unable to copy directory to new destination")
		return "", nil, err
	}

	return newAppRepoPath, &opt, nil
}

// updateNewAppStructureGoMod rewrites the generated app's module metadata.
//
// The base `go.mod` starts as GHAT(D)'s own module file. This function changes
// the module path to the user's app module and replaces the GHAT(D) package
// placeholder with the pinned version used by generated apps.
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

// copyDetailStructureToNewAppStructure copies a Detail into the generated app.
//
// API Details are copied into the generated `internal` and `testing` trees,
// classic web Details are copied under `internal/web`, and Vite web Details are
// delegated to the web-vite copy path because they own a separate frontend app.
func copyDetailStructureToNewAppStructure(detailType, detailOutput, newAppRepoPath string, opt *cp.Options) error {

	var detailExternalDir string
	var newAppStructureTargetDir string
	var detailTestingDir string
	var newAppStructureTestingDir string

	if detailType == config.DetailTypeWebVite {
		return copyWebViteAppToNewAppStructure(detailOutput, newAppRepoPath, opt)
	}

	if detailType == config.DetailTypeWeb {
		detailExternalDir = fmt.Sprintf("%s/external", detailOutput)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal/web", newAppRepoPath)
	}

	if detailType == config.DetailTypeAPI {
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

	if detailType == config.DetailTypeAPI {
		err = cp.Copy(detailTestingDir, newAppStructureTestingDir, *opt)
		if err != nil {
			log.Default().Printf("unable to copy %s detail testing dir to new destination", detailType)
			return err
		}
	}

	return nil
}

// copyWebViteAppToNewAppStructure copies a Vite frontend Detail into the app.
//
// It moves the Detail's `web` directory and supported package-manager metadata
// into the generated project while excluding build artefacts and dependency
// directories that should be recreated by the user.
func copyWebViteAppToNewAppStructure(detailOutput, newAppRepoPath string, opt *cp.Options) error {
	detailWebDir := filepath.Join(detailOutput, "web")
	if exists, err := pathExists(detailWebDir); err != nil {
		return err
	} else if !exists {
		return fmt.Errorf("web-vite detail missing web directory: %s", detailWebDir)
	}

	copyOptions := webViteCopyOptions(opt)
	if err := cp.Copy(detailWebDir, filepath.Join(newAppRepoPath, "web"), copyOptions); err != nil {
		log.Default().Printf("unable to copy web-vite web dir to new destination")
		return err
	}

	for _, filename := range webViteRootFiles() {
		src := filepath.Join(detailOutput, filename)
		if exists, err := pathExists(src); err != nil {
			return err
		} else if !exists {
			continue
		}

		if err := cp.Copy(src, filepath.Join(newAppRepoPath, filename), copyOptions); err != nil {
			log.Default().Printf("unable to copy web-vite root file %s to new destination", filename)
			return err
		}
	}

	return ensureWebViteGitignore(newAppRepoPath)
}

// webViteRootFiles lists root-level frontend files copied from web-vite Details.
//
// These files preserve the package manager and toolchain choices from the
// Detail without copying transient directories such as `node_modules`.
func webViteRootFiles() []string {
	return []string{
		"package.json",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"bun.lockb",
		".npmrc",
		".yarnrc",
		".yarnrc.yml",
		".tool-versions",
	}
}

// webViteCopyOptions extends copy options for frontend-specific exclusions.
//
// It preserves any base skip behaviour and adds safeguards for common generated
// frontend directories so the output stays small and reproducible.
func webViteCopyOptions(base *cp.Options) cp.Options {
	opt := cp.Options{}
	if base != nil {
		opt = *base
	}

	previousSkip := opt.Skip
	opt.Skip = func(info os.FileInfo, src, dest string) (bool, error) {
		if previousSkip != nil {
			skip, err := previousSkip(info, src, dest)
			if skip || err != nil {
				return skip, err
			}
		}

		if info.IsDir() {
			switch info.Name() {
			case "node_modules", "dist", "dev-dist":
				return true, nil
			}
		}

		return false, nil
	}

	return opt
}

// ensureWebViteGitignore adds frontend build artefacts to the generated gitignore.
//
// The function is idempotent, so repeated web-vite integration does not create
// duplicate entries.
func ensureWebViteGitignore(newAppRepoPath string) error {
	gitignorePath := filepath.Join(newAppRepoPath, ".gitignore")
	gitignore := ""

	input, err := os.ReadFile(gitignorePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		gitignore = string(input)
	}

	for _, entry := range []string{"node_modules/", "web/node_modules/", "web/dist/", "web/dev-dist/", "dist/"} {
		if gitignoreHasLine(gitignore, entry) {
			continue
		}
		if gitignore != "" && !strings.HasSuffix(gitignore, "\n") {
			gitignore += "\n"
		}
		gitignore += entry + "\n"
	}

	return os.WriteFile(gitignorePath, []byte(gitignore), 0644)
}

// gitignoreHasLine reports whether a gitignore already contains an entry.
//
// Lines are trimmed before comparison so incidental spacing does not create
// duplicate generated ignore rules.
func gitignoreHasLine(gitignore, entry string) bool {
	for _, line := range strings.Split(gitignore, "\n") {
		if strings.TrimSpace(line) == entry {
			return true
		}
	}

	return false
}

// pathExists checks for a file or directory without hiding unexpected stat errors.
//
// Missing paths return false with no error; permission and filesystem errors are
// returned so callers can fail the generation flow clearly.
func pathExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// updateDetailPackageReferenceForNewAppStructure rewrites Detail import paths.
//
// Details are authored in their own modules under `external`. Once copied into a
// generated app, those imports need to point at the app module's `internal` or
// `internal/web` package paths instead.
func updateDetailPackageReferenceForNewAppStructure(detailType, detailOutput, detailModuleName, appModuleName string) error {

	var detailExternalDir string
	var newAppStructureTargetDir string

	if detailType == config.DetailTypeWeb {
		detailExternalDir = fmt.Sprintf("%s/external", detailModuleName)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal/web", appModuleName)
	}

	if detailType == config.DetailTypeAPI {
		detailExternalDir = fmt.Sprintf("%s/external", detailModuleName)
		newAppStructureTargetDir = fmt.Sprintf("%s/internal", appModuleName)
	}

	err := toolbox.Refactor(false, detailExternalDir, newAppStructureTargetDir, fmt.Sprintf("%s/.", detailOutput), "*.go")
	if err != nil {
		log.Default().Println("unable to replace strings found")
		return err
	}

	if detailType == config.DetailTypeAPI {
		err = toolbox.Refactor(false, fmt.Sprintf("%s/testing", detailModuleName), fmt.Sprintf("%s/testing", appModuleName), fmt.Sprintf("%s/.", detailOutput), "*.go")
		if err != nil {
			log.Default().Println("unable to replace strings found")
			return err
		}
	}

	return nil
}

// addDetailGoModRequireBlockToGoMod injects a Detail's dependency block.
//
// Detail templates mark the relevant `go.mod` insertion point with GHAT(D)
// template comments; this function appends the extracted require lines at the
// matching API or web placeholder in the generated app.
func addDetailGoModRequireBlockToGoMod(detailGoModRequirePackages []string, detailType, newAppRepoPath string) error {

	var ghatdGoModRequirePackagesTag string

	if detailType == config.DetailTypeWeb {
		ghatdGoModRequirePackagesTag = "WebDetailGoModRequirePackages"
	}

	if detailType == config.DetailTypeAPI {
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

// addDetailImportsAndInitBlockToServerGo wires a Detail into server.go.
//
// It inserts Detail imports and initialisation statements at the generated
// server's GHAT(D) template markers, keeping API and web Detail wiring separate
// so multiple Detail types can coexist.
func addDetailImportsAndInitBlockToServerGo(detailImports, detailInitSteps []string, detailType, newAppRepoPath string) error {

	var ghatdImportsTag string
	var ghatdInitTag string

	if detailType == config.DetailTypeWeb {
		ghatdImportsTag = "WebDetailImports"
		ghatdInitTag = "WebDetailInit"
	}

	if detailType == config.DetailTypeAPI {
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

// addDetailEmbedsToMainGo merges Detail embed patterns into main.go.
//
// Details can bring static assets that need to be included in the generated
// binary. This function updates the existing `go:embed` line and restores it if
// the base template left it commented out.
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

			newAppMainGoLines[i] = toolbox.AddStringIfItDoesExistInBaseString(line, detailEmbeds)
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

// configureWebViteMainGo points the generated embed configuration at Vite output.
//
// Web-vite Details build assets into `web/dist`, so the generated main entrypoint
// must embed that directory and pass the `web/` prefix into the server command.
func configureWebViteMainGo(newAppRepoPath string) error {
	newAppMainGoPath := filepath.Join(newAppRepoPath, "main.go")

	newAppMainGoInput, err := os.ReadFile(newAppMainGoPath)
	if err != nil {
		log.Default().Printf("unable to read new app main.go file - %s\n", newAppMainGoPath)
		return err
	}

	newAppMainGoLines := strings.Split(string(newAppMainGoInput), "\n")
	filteredLines := make([]string, 0, len(newAppMainGoLines))
	for _, line := range newAppMainGoLines {
		if strings.TrimSpace(line) == `_ "embed"` {
			continue
		}
		filteredLines = append(filteredLines, line)
	}
	newAppMainGoLines = filteredLines

	embedSet := false
	for i, line := range newAppMainGoLines {
		if strings.Contains(line, "//go:embed ") {
			newAppMainGoLines[i] = "//go:embed all:web/dist/*"
			embedSet = true
		}
	}

	if !embedSet {
		for i, line := range newAppMainGoLines {
			if strings.TrimSpace(line) == "var content embed.FS" {
				newAppMainGoLines = append(newAppMainGoLines[:i], append([]string{"//go:embed all:web/dist/*"}, newAppMainGoLines[i:]...)...)
				embedSet = true
				break
			}
		}
	}

	if !embedSet {
		return fmt.Errorf("main.go missing embed content declaration")
	}

	newAppMainGoOutput := strings.Join(newAppMainGoLines, "\n")
	newAppMainGoOutput = strings.ReplaceAll(newAppMainGoOutput, `server.NewCommand(&content, "internal/")`, `server.NewCommand(&content, "web/")`)
	newAppMainGoOutput = strings.ReplaceAll(newAppMainGoOutput, `server.NewCommand(content, "internal/")`, `server.NewCommand(content, "web/")`)

	err = os.WriteFile(newAppMainGoPath, []byte(newAppMainGoOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to update new app main.go file - %s\n", newAppMainGoPath)
		return err
	}

	return nil
}

// configureWebViteServerGo adds SPA serving support to the generated server.
//
// It rewrites the generated server source with the import, handler, and route
// wiring needed for a Vite single-page app.
func configureWebViteServerGo(newAppRepoPath string) error {
	newAppServerGoPath := filepath.Join(newAppRepoPath, "cmd", "server", "server.go")

	newAppServerGoInput, err := os.ReadFile(newAppServerGoPath)
	if err != nil {
		log.Default().Printf("unable to read new app server.go file - %s\n", newAppServerGoPath)
		return err
	}

	newAppServerGoOutput, err := addWebViteSPAServerWiring(string(newAppServerGoInput))
	if err != nil {
		return err
	}

	err = os.WriteFile(newAppServerGoPath, []byte(newAppServerGoOutput), 0644)
	if err != nil {
		log.Default().Printf("unable to update new app server.go file - %s\n", newAppServerGoPath)
		return err
	}

	return nil
}

// addWebViteSPAServerWiring applies all server.go changes for web-vite support.
//
// The operation is split into small string transforms so tests can pinpoint
// whether an import, handler, or route marker changed unexpectedly.
func addWebViteSPAServerWiring(serverGo string) (string, error) {
	output := addWebViteSPAImport(serverGo)

	var err error
	output, err = addWebViteSPAHandler(output)
	if err != nil {
		return "", err
	}

	output, err = addWebViteSPAAttachRoutes(output)
	if err != nil {
		return "", err
	}

	return output, nil
}

// addWebViteSPAImport inserts the GHAT(D) SPA package import when needed.
//
// Existing imports are left untouched so repeated calls remain idempotent.
func addWebViteSPAImport(serverGo string) string {
	if strings.Contains(serverGo, `"github.com/ooaklee/ghatd/external/spa"`) {
		return serverGo
	}

	const importMarker = `//>ghatd {{ block "WebDetailImports" . }}`
	return strings.Replace(serverGo, importMarker, importMarker+"\n\t\"github.com/ooaklee/ghatd/external/spa\"", 1)
}

// addWebViteSPAHandler replaces the default not-found handler with the SPA handler.
//
// The SPA handler lets client-side routes fall back to the embedded index file
// while preserving the normal router construction path for generated servers.
func addWebViteSPAHandler(serverGo string) (string, error) {
	if strings.Contains(serverGo, "spa.NewSpaHandler") || strings.Contains(serverGo, "spaHandler.GetResourceNotFoundError") {
		return serverGo, nil
	}

	const routerInit = `httpRouter := router.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)`
	const spaRouterInit = `spaHandler := spa.NewSpaHandler(&spa.NewSpaHandlerRequest{
		EmbeddedContent:               embeddedContent,
		EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
		HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
	})

	httpRouter := router.NewRouter(spaHandler.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)`

	if !strings.Contains(serverGo, routerInit) {
		return "", fmt.Errorf("server.go missing expected router initialisation for web-vite SPA wiring")
	}

	return strings.Replace(serverGo, routerInit, spaRouterInit, 1), nil
}

// addWebViteSPAAttachRoutes attaches embedded Vite assets before server startup.
//
// It inserts `spa.AttachRoutes` at the generated server marker used just before
// server construction, and skips work when the route attachment already exists.
func addWebViteSPAAttachRoutes(serverGo string) (string, error) {
	if strings.Contains(serverGo, "spa.AttachRoutes(&spa.AttachRoutesRequest{") {
		return serverGo, nil
	}

	const defineServerMarker = "\n\t// Define server"
	if !strings.Contains(serverGo, defineServerMarker) {
		return "", fmt.Errorf("server.go missing expected server definition marker for web-vite SPA wiring")
	}

	const attachRoutes = `
	// Serve web app assets.
	spa.AttachRoutes(&spa.AttachRoutesRequest{
		Router:                        httpRouter,
		SpaFileSystem:                 embeddedContent,
		EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
		HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
	})
`

	return strings.Replace(serverGo, defineServerMarker, attachRoutes+defineServerMarker, 1), nil
}

// convertExtractedDetailInfoToNewAppStructure maps Detail metadata to app paths.
//
// Imports are rewritten from the Detail module to the generated module, and
// embed paths are rewritten from the Detail's `external` layout to the generated
// app's target package layout.
func convertExtractedDetailInfoToNewAppStructure(detailImports []string, detailEmbeds, detailType, detailModuleName, appModuleName string) ([]string, string) {

	var targetModulePath string
	var migratedEmbedPath string

	if detailType == config.DetailTypeWeb {
		targetModulePath = fmt.Sprintf("%s/internal/web", appModuleName)
		migratedEmbedPath = "internal/web/"

	}

	if detailType == config.DetailTypeAPI {
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

// getDetailEntryGoInfo extracts imports, embeds, and init statements from a Detail.
//
// Detail entry files use GHAT(D) template markers to declare what should be
// copied into the generated server. The function returns those marked blocks in
// a form that the app-structure update helpers can insert later.
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

	if detailType == config.DetailTypeWeb {
		ghatdImportTag = "WebDetailImports"
		ghatdInitTag = "WebDetailInit"
		detailEntryPoint = "web.go"
	}

	if detailType == config.DetailTypeAPI {
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

		if detailType == config.DetailTypeWeb {
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

// getDetailModInfo extracts module, Go version, and dependency data from a Detail.
//
// The require block is read from the Detail's GHAT(D) template markers so the
// generated app can depend on the same packages without copying unrelated
// module-file content.
func getDetailModInfo(detailPath, detailType string) (string, string, []string, error) {

	var ghatdRequireTag string
	var detailModuleName string
	var detailGoVersion string
	var startOfDetailModRequirePackages int
	var endOfDetailModRequirePackages int

	if detailType == config.DetailTypeWeb {
		ghatdRequireTag = "WebDetailGoModRequirePackages"

	}
	if detailType == config.DetailTypeAPI {
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

// cleanUpDirectories removes temporary directories created during generation.
//
// Cleanup is best-effort across the full list so one failed removal does not
// prevent the command from attempting to remove later temporary clones.
func cleanUpDirectories(dirs []string) error {
	for _, dir := range dirs {
		_ = cleanUpDirectory(dir)
	}
	return nil
}

// cleanUpDirectory removes one temporary generation directory.
//
// It wraps os.RemoveAll with command logging so failed cleanup is visible while
// preserving the underlying filesystem error for callers that care.
func cleanUpDirectory(dir string) error {
	err := os.RemoveAll(dir)
	if err != nil {
		log.Default().Printf("unable to remove temp detail target dir - %s\n", dir)
		return err
	}

	return err
}
