// Package errormanifest provides a reusable Composer for building
// []reply.ErrorManifest slices with explicit layering and last-wins
// semantics. It is the single authoritative way to compose error maps
// for handler and middleware wiring in GHATD.
//
// # Conventions
//
// 1. Named-bundle pattern
//
// Declare error bundles as package-scoped []reply.ErrorManifest variables
// near handler/middleware construction in app wiring, not inline at the
// call site. This makes the set of error maps visible, auditable, and
// reusable across multiple consumers (e.g. sharing a middleware error
// bundle with a rate-limiter).
//
//	accessManagerErrorBundle := []reply.ErrorManifest{
//	    user.UserErrorMap,
//	    auth.AuthErrorMap,
//	    billing.BillingErrorMap,
//	}
//	maps := errormanifest.NewComposer().Add(accessManagerErrorBundle...).Build()
//
// 2. Last-wins precedence
//
// reply.NewReplier merges manifests in slice order; later maps overwrite
// earlier maps for identical keys. The Composer enforces this contract:
//
//	base maps   →  Composer.Add()
//	overrides   →  Composer.AddOverrides()
//
// Override manifests are placed after all base manifests in the output
// slice, guaranteeing they win on key conflicts.
//
// 3. Handler-owned base, caller-owned overrides
//
// Each handler auto-includes its package-local ErrorManifest as the base
// layer via Composer.Add(). Caller-supplied maps received via the Handler
// struct are applied as overrides via Composer.AddOverrides(), so they
// can supplement or override the package defaults on a per-key basis.
// Callers pass only cross-package/shared maps; domain-specific maps
// belong to the handler:
//
//	func (h *Handler) getBaseResponseHandler() *reply.Replier {
//	    return reply.NewReplier(
//	        errormanifest.NewComposer().
//	            Add(MyPackageErrorMap).
//	            AddOverrides(h.ErrorMaps...).
//	            Build(),
//	    )
//	}
//
// 4. Endpoint-specific overrides
//
// For per-endpoint error overrides (e.g. HTTP 202 instead of 401 for
// unauthenticated /me), use Composer.AddOverrides() as the final layer.
// This is demonstrated by BuildCustomMeEndpointErrorMap in
// github.com/ooaklee/ghatd/external/accessmanager/middleware.
//
// 5. Duplicate detection
//
// Composer.Duplicates() returns error message strings that appear in
// more than one manifest across the full composed set. Use it in tests
// or non-production startup diagnostics to surface accidental
// collisions between independently-defined manifests.
//
//	dups := errormanifest.NewComposer().Add(bundle...).Duplicates()
//	if len(dups) > 0 {
//	    log.Printf("warn: duplicate error keys in manifests: %v", dups)
//	}
package errormanifest
