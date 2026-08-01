package migrator

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewCommandUsesEnvironmentSettings(t *testing.T) {
	migrationDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(migrationDirectory, "template.go"), []byte("package migrations\n"), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	t.Setenv("MONGO_MIGRATION_DIRECTORY", migrationDirectory)

	output := &bytes.Buffer{}
	command := NewCommand()
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"new", "environment-settings"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	entries, err := os.ReadDir(migrationDirectory)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), "_environment-settings.go") {
			return
		}
	}

	t.Fatalf("migration was not created in environment-configured directory; output: %s", output.String())
}
