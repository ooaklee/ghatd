package publication

import (
	"io/fs"
	"net/http"
	"path"

	"github.com/ooaklee/ghatd/external/spa"
)

// The host supplies its build locations, public shell path and asset bypass.
// Matching and build validation belong to the shared pathname-only resolver.
func frontendDescriptionPaths(content fs.FS, prefix string) (func(*http.Request) *http.Request, error) {
	return spa.NewDescriptionPathResolver(spa.DescriptionInventoryConfig{
		FS:                    content,
		InventoryPath:         path.Join(prefix, "dist/page-descriptions.json"),
		ShellDirectory:        path.Join(prefix, "dist/__pages"),
		PublicShellPrefix:     "/__pages",
		Fallback:              spa.NewHandleUpdatePathToIndex(spa.BypassWithFileExtension("webp"), spa.BypassWithFileExtension("html"), spa.BypassWithFileExtension("json")),
		AllowMissingInventory: true, // Older host builds may have no inventory.
	})
}
