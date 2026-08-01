// Package migrator connects a host application's migration registrations to
// GHAT(D)'s shared MongoDB migrator command.
package migrator

import (
	mongomigrator "github.com/ooaklee/ghatd/external/migrator/mongo"
	_ "github.com/ooaklee/ghatd/migrations/mongo"
	"github.com/spf13/cobra"
)

// NewCommand returns the shared MongoDB migrator command.
func NewCommand() *cobra.Command {
	return mongomigrator.NewCommand()
}
