package errormanifest

import (
	"maps"

	"github.com/ooaklee/reply/v2"
)

// CloneManifests copies the slice and each map so callers may add, replace or
// delete entries without changing the source manifests. Order and nil entries
// are preserved. Manifest items are value copies, not deep copies: any values
// referenced by Meta must remain immutable or be replaced before modification.
// Sources must not be mutated concurrently with this function.
func CloneManifests(manifests ...reply.ErrorManifest) []reply.ErrorManifest {
	cloned := make([]reply.ErrorManifest, len(manifests))
	for i, manifest := range manifests {
		cloned[i] = maps.Clone(manifest)
	}
	return cloned
}
