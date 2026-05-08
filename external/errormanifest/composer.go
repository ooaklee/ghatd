package errormanifest

import (
	"github.com/ooaklee/reply/v2"
)

type Composer struct {
	base      []reply.ErrorManifest
	overrides []reply.ErrorManifest
}

// NewComposer returns a Composer for building a final []reply.ErrorManifest
// with explicit last-wins semantics: overrides are placed after base manifests
// so they overwrite duplicate keys when merged by reply.NewReplier.
func NewComposer() *Composer {
	return &Composer{}
}

// Add appends one or more manifests to the base layer. Manifests added here
// form the general pool; duplicate keys across base manifests are resolved
// last-wins by reply.NewReplier at runtime.
func (c *Composer) Add(maps ...reply.ErrorManifest) *Composer {
	c.base = append(c.base, maps...)
	return c
}

// AddOverrides appends one or more manifests as the final override layer.
// Override manifests are placed after all base manifests in the output slice,
// guaranteeing that their entries take precedence for any duplicate keys.
// This is the canonical way to inject endpoint-specific error response
// overrides (e.g. returning HTTP 202 instead of 401 for /me).
func (c *Composer) AddOverrides(maps ...reply.ErrorManifest) *Composer {
	c.overrides = append(c.overrides, maps...)
	return c
}

// Build returns the final []reply.ErrorManifest slice suitable for passing
// directly to reply.NewReplier, handler constructors, or middleware
// constructors.
func (c *Composer) Build() []reply.ErrorManifest {
	result := make([]reply.ErrorManifest, 0, len(c.base)+len(c.overrides))
	result = append(result, c.base...)
	result = append(result, c.overrides...)
	return result
}

// Duplicates returns the set of error message strings whose corresponding
// errors appear in more than one manifest across the full composed set
// (base + overrides).
//
// Comparison is by error message string (err.Error()), not by error identity
// (pointer/reference equality). This catches cases where two distinct error
// values from different packages share the same message string, which is the
// more actionable debugging signal — it reveals potential namespace collisions
// between independently-defined manifest entries.
//
// This method is opt-in metadata intended for tests and debugging; it does
// not alter runtime behaviour. At runtime, reply.NewReplier merges manifests
// by error identity (the map[error] key), so errors with the same message but
// different identities coexist harmlessly until overwritten by a later manifest
// with the same identity key.
func (c *Composer) Duplicates() []string {
	all := append(c.base, c.overrides...)
	seen := make(map[string]int)
	for _, m := range all {
		for errKey := range m {
			seen[errKey.Error()]++
		}
	}

	var dups []string
	for msg, count := range seen {
		if count > 1 {
			dups = append(dups, msg)
		}
	}
	return dups
}

// Reset clears all collected manifests so the Composer can be reused.
func (c *Composer) Reset() {
	c.base = nil
	c.overrides = nil
}
