package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/internal/cli/config"

	cp "github.com/otiai10/copy"
)

func TestInitNewAppRepoKeepsMigratorHostOwned(t *testing.T) {
	baseDirectory := t.TempDir()
	defaultModule := "github.com/ooaklee/ghatd"
	appModule := "github.com/example/generated-app"
	appName := fmt.Sprintf("ghatd-migrator-test-%d", time.Now().UnixNano())
	generatedDirectory := filepath.Join(os.TempDir(), appName)
	if _, err := os.Stat(generatedDirectory); !os.IsNotExist(err) {
		t.Fatalf("generated test directory already exists: %s", generatedDirectory)
	}
	t.Cleanup(func() { _ = os.RemoveAll(generatedDirectory) })

	files := map[string]string{
		"cmd/server/server.go":           "package server\n",
		"cmd/mongo-migrator/migrator.go": "package migrator\nimport (\n shared \"github.com/ooaklee/ghatd/external/migrator/mongo\"\n _ \"github.com/ooaklee/ghatd/migrations/mongo\"\n)\nvar _ = shared.NewCommand\n",
		"migrations/mongo/template.go":   "package migrations\n",
		"internal/example/example.go":    "package example\n",
		"testing/example/example.go":     "package example\n",
		"main.go":                        "package main\nimport (\n \"github.com/ooaklee/ghatd/cmd/mongo-migrator\"\n \"github.com/ooaklee/ghatd/cmd/server\"\n)\n",
		"go.mod":                         "module github.com/ooaklee/ghatd\n",
	}
	for name, contents := range files {
		path := filepath.Join(baseDirectory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create parent for %s: %v", name, err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	gotDirectory, _, err := initNewAppRepo(appName, appModule, baseDirectory, defaultModule)
	if err != nil {
		t.Fatalf("initNewAppRepo() error = %v", err)
	}
	if gotDirectory != generatedDirectory {
		t.Fatalf("generated directory = %q, want %q", gotDirectory, generatedDirectory)
	}

	mainContents, err := os.ReadFile(filepath.Join(generatedDirectory, "main.go"))
	if err != nil {
		t.Fatalf("read generated main.go: %v", err)
	}
	if !strings.Contains(string(mainContents), appModule+"/cmd/mongo-migrator") || !strings.Contains(string(mainContents), appModule+"/cmd/server") {
		t.Fatalf("generated main.go has stale command imports:\n%s", mainContents)
	}

	migratorContents, err := os.ReadFile(filepath.Join(generatedDirectory, "cmd/mongo-migrator/migrator.go"))
	if err != nil {
		t.Fatalf("read generated migrator.go: %v", err)
	}
	if !strings.Contains(string(migratorContents), appModule+"/migrations/mongo") {
		t.Fatalf("generated migrator does not import host migrations:\n%s", migratorContents)
	}
	if !strings.Contains(string(migratorContents), defaultModule+"/external/migrator/mongo") {
		t.Fatalf("generated migrator does not use the shared implementation:\n%s", migratorContents)
	}
	if _, err := os.Stat(filepath.Join(generatedDirectory, "migrations/mongo/template.go")); err != nil {
		t.Fatalf("generated migration template missing: %v", err)
	}
}

func TestIsSupportedDetailRepoSource(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "Success - github host path", source: "github.com/example/app-detail", want: true},
		{name: "Success - github https URL", source: "https://github.com/example/app-detail", want: true},
		{name: "Success - github SSH scp URL", source: "git@github.com:example/app-detail.git", want: true},
		{name: "Success - github SSH URL", source: "ssh://git@github.com/example/app-detail.git", want: true},
		{name: "Success - owner repo shorthand", source: "example/app-detail", want: true},
		{name: "Success - owner repo shorthand with git suffix", source: "example/app-detail.git", want: true},
		{name: "Failure - empty source", source: "", want: false},
		{name: "Failure - unsupported host", source: "https://gitlab.com/example/app-detail", want: false},
		{name: "Failure - local relative path", source: "../app-detail", want: false},
		{name: "Failure - local absolute path", source: "/tmp/app-detail", want: false},
		{name: "Failure - incomplete shorthand", source: "example", want: false},
		{name: "Failure - too many shorthand segments", source: "example/app-detail/extra", want: false},
		{name: "Failure - github host with too many segments", source: "github.com/example/app-detail/extra", want: false},
		{name: "Failure - github https URL with too many segments", source: "https://github.com/example/app-detail/extra", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isSupportedDetailRepoSource(tt.source, "github.com", "https://github.com")
			if got != tt.want {
				t.Fatalf("isSupportedDetailRepoSource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfigureWebViteMainGo(t *testing.T) {
	newAppRepoPath := t.TempDir()
	mainGo := `package main

import (
	"embed"
	"log"

	_ "embed"

	"github.com/example/app/cmd/server"
	"github.com/spf13/cobra"
)

// content holds our static web server content.
//
// //go:embed internal/web/ui/static/* internal/web/ui/html/*
var content embed.FS

func main() {
	rootCmd := cobra.Command{}
	rootCmd.AddCommand(server.NewCommand(&content, "internal/"))
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
`
	if err := os.WriteFile(filepath.Join(newAppRepoPath, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	if err := configureWebViteMainGo(newAppRepoPath); err != nil {
		t.Fatalf("configureWebViteMainGo() error = %v", err)
	}

	output, err := os.ReadFile(filepath.Join(newAppRepoPath, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	got := string(output)

	for _, want := range []string{
		"//go:embed all:web/dist/*",
		`server.NewCommand(&content, "web/")`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("updated main.go missing %q:\n%s", want, got)
		}
	}

	if strings.Contains(got, `_ "embed"`) {
		t.Fatalf("updated main.go should remove blank embed import:\n%s", got)
	}
	if strings.Contains(got, "internal/web/ui/static") {
		t.Fatalf("updated main.go should replace old embed target:\n%s", got)
	}
}

func TestConfigureWebViteMainGoUpdatesBareContentServerCommand(t *testing.T) {
	newAppRepoPath := t.TempDir()
	mainGo := `package main

import "embed"

var content embed.FS

func main() {
	rootCmd.AddCommand(server.NewCommand(content, "internal/"))
}
`
	if err := os.WriteFile(filepath.Join(newAppRepoPath, "main.go"), []byte(mainGo), 0644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	if err := configureWebViteMainGo(newAppRepoPath); err != nil {
		t.Fatalf("configureWebViteMainGo() error = %v", err)
	}

	output, err := os.ReadFile(filepath.Join(newAppRepoPath, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	got := string(output)
	if !strings.Contains(got, `server.NewCommand(content, "web/")`) {
		t.Fatalf("updated main.go missing bare content server command rewrite:\n%s", got)
	}
}

func TestConfigureWebViteServerGo(t *testing.T) {
	newAppRepoPath := t.TempDir()
	serverGoPath := filepath.Join(newAppRepoPath, "cmd", "server", "server.go")
	serverGo := `package server

import (
	"io/fs"

	"github.com/ooaklee/ghatd/external/response"
	"github.com/ooaklee/ghatd/external/router"
	//>ghatd {{ block "WebDetailImports" . }}
	//>ghatd {{ end }}
)

func runServer(embeddedContent fs.FS, embeddedContentFilePathPrefix string) error {
	routerMiddlewares := []mux.MiddlewareFunc{}
	httpRouter := router.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)

	//>ghatd {{ block "ApiDetailInit" . }}
	//>ghatd {{ end }}

	// Define server
	_ = httpRouter
	return nil
}
`
	writeFile(t, serverGoPath, serverGo)

	if err := configureWebViteServerGo(newAppRepoPath); err != nil {
		t.Fatalf("configureWebViteServerGo() error = %v", err)
	}

	output, err := os.ReadFile(serverGoPath)
	if err != nil {
		t.Fatalf("read server.go: %v", err)
	}
	got := string(output)
	for _, want := range []string{
		`"github.com/ooaklee/ghatd/external/spa"`,
		"spaHandler := spa.NewSpaHandler",
		"router.NewRouter(spaHandler.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)",
		"spa.AttachRoutes(&spa.AttachRoutesRequest{",
		"SpaFileSystem:                 embeddedContent",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("updated server.go missing %q:\n%s", want, got)
		}
	}

	if strings.Index(got, "spa.AttachRoutes") > strings.Index(got, "// Define server") {
		t.Fatalf("spa routes should be attached before server definition:\n%s", got)
	}
}

func TestCopyWebViteAppToNewAppStructure(t *testing.T) {
	detailOutput := t.TempDir()
	newAppRepoPath := t.TempDir()

	writeFile(t, filepath.Join(detailOutput, "web", "src", "main.ts"), "console.log('ok')\n")
	writeFile(t, filepath.Join(detailOutput, "web", "dist", "asset.js"), "built\n")
	writeFile(t, filepath.Join(detailOutput, "web", "node_modules", "pkg", "index.js"), "module\n")
	writeFile(t, filepath.Join(detailOutput, "package.json"), `{"scripts":{"build":"vite build"}}`+"\n")
	writeFile(t, filepath.Join(detailOutput, "yarn.lock"), "# yarn lock\n")
	writeFile(t, filepath.Join(detailOutput, "ghatd-conf.yaml"), "type: web-vite\n")

	if err := copyWebViteAppToNewAppStructure(detailOutput, newAppRepoPath, &cp.Options{}); err != nil {
		t.Fatalf("copyWebViteAppToNewAppStructure() error = %v", err)
	}

	for _, wantPath := range []string{
		filepath.Join(newAppRepoPath, "web", "src", "main.ts"),
		filepath.Join(newAppRepoPath, "package.json"),
		filepath.Join(newAppRepoPath, "yarn.lock"),
	} {
		if exists, err := pathExists(wantPath); err != nil || !exists {
			t.Fatalf("expected %s to exist, exists=%v err=%v", wantPath, exists, err)
		}
	}

	for _, unwantedPath := range []string{
		filepath.Join(newAppRepoPath, "web", "dist", "asset.js"),
		filepath.Join(newAppRepoPath, "web", "node_modules", "pkg", "index.js"),
		filepath.Join(newAppRepoPath, "ghatd-conf.yaml"),
	} {
		if exists, err := pathExists(unwantedPath); err != nil || exists {
			t.Fatalf("expected %s to be absent, exists=%v err=%v", unwantedPath, exists, err)
		}
	}

	gitignore, err := os.ReadFile(filepath.Join(newAppRepoPath, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, want := range []string{"node_modules/", "web/node_modules/", "web/dist/", "web/dev-dist/", "dist/"} {
		if !gitignoreHasLine(string(gitignore), want) {
			t.Fatalf(".gitignore missing %q:\n%s", want, string(gitignore))
		}
	}
}

func TestCopyDetailStructureToNewAppStructureWebViteDoesNotCopyLegacyWebAdapter(t *testing.T) {
	detailOutput := t.TempDir()
	newAppRepoPath := t.TempDir()

	writeFile(t, filepath.Join(detailOutput, "external", "routes.go"), "package webapp\n")
	writeFile(t, filepath.Join(detailOutput, "web", "src", "main.ts"), "console.log('ok')\n")
	writeFile(t, filepath.Join(detailOutput, "package.json"), `{"scripts":{"build":"vite build"}}`+"\n")

	if err := copyDetailStructureToNewAppStructure(config.DetailTypeWebVite, detailOutput, newAppRepoPath, &cp.Options{}); err != nil {
		t.Fatalf("copyDetailStructureToNewAppStructure(web-vite) error = %v", err)
	}

	for _, wantPath := range []string{
		filepath.Join(newAppRepoPath, "web", "src", "main.ts"),
		filepath.Join(newAppRepoPath, "package.json"),
	} {
		if exists, err := pathExists(wantPath); err != nil || !exists {
			t.Fatalf("expected %s to exist, exists=%v err=%v", wantPath, exists, err)
		}
	}

	for _, unwantedPath := range []string{
		filepath.Join(newAppRepoPath, "internal", "web", "routes.go"),
		filepath.Join(newAppRepoPath, "web", "routes.go"),
	} {
		if exists, err := pathExists(unwantedPath); err != nil || exists {
			t.Fatalf("expected %s to be absent, exists=%v err=%v", unwantedPath, exists, err)
		}
	}
}

func TestGetDetailEntryGoInfoWebPreservesLegacyEmbeddedPrefix(t *testing.T) {
	detailPath := t.TempDir()
	writeFile(t, filepath.Join(detailPath, "web.go"), webDetailEntryPointFixture())

	_, _, webInit, err := getDetailEntryGoInfo(detailPath, config.DetailTypeWeb)
	if err != nil {
		t.Fatalf("getDetailEntryGoInfo(web) error = %v", err)
	}
	if !strings.Contains(strings.Join(webInit, "\n"), `embeddedContentFilePathPrefix + "web/"`) {
		t.Fatalf("web init should preserve legacy internal/web prefix rewrite:\n%s", strings.Join(webInit, "\n"))
	}
}

func writeFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(value), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func webDetailEntryPointFixture() string {
	return `package main

import (
	"embed"
	"io/fs"

	//>ghatd {{ define "WebDetailImports" }}
	webapp "github.com/example/detail/external"
	//>ghatd {{ end }}
)

//go:embed web/*
var content embed.FS

func NewWebDetail(embeddedContent fs.FS, embeddedContentFilePathPrefix string) {
	//>ghatd {{ define "WebDetailInit" }}
	webapp.AttachRoutes(&webapp.AttachRoutesRequest{
		WebAppFileSystem:              embeddedContent,
		EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
	})
	//>ghatd {{ end }}
}
`
}
