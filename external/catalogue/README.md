# Catalogue

Shared audit, availability and lifecycle rules for shared reference
catalogues. `Lifecycle[T]` operates through a narrow typed `Repository[T]`;
each child service supplies its record validation and `Entry` projection.
[cataloguestore](../cataloguestore/README.md) owns persistence. No datastore,
cache, HTTP handler or provider driver belongs in this package.

## Lifecycle contract

`Entry` contains identity, display name, availability, flag/region references and
UTC audit metadata. `NewLifecycle` requires a repository, entry accessor and
definition validator; a nil clock selects `RealClock`. The owning child domains
are [currencycoder](../currencycoder/README.md),
[telenumcoder](../telenumcoder/README.md) and
[timezonecoder](../timezonecoder/README.md). Global flags share the audit/query
types while owning their SVG-specific service.

- `Get` reads an identity; `List` bounds a `ListQuery` before passing it to the
  repository. Default page size is 100, maximum 500, with a bounded search.
- `RequireSelectable` rejects disabled, hidden or soft-deleted records for new
  choices. `Selectable` exposes the same availability rule.
- `Create` validates definition and actor context, creates audit metadata and
  inserts only if absent. Duplicate identities return `ErrAlreadyExists`.
- `Update` preserves creation attribution and requires an expected revision.
  The repository's compare-and-swap must enforce the write, not only the prior
  service read. Missing/mismatched revisions return `ErrStaleWrite`; updating a
  soft-deleted record returns `ErrNotSelectable` until it is restored.
- `Delete` soft-deletes and disables; `Restore` clears deletion but leaves the
  record disabled. Both require an expected revision and compare-and-swap.
  Availability must be explicitly re-enabled afterwards.
- `Seed` inserts missing definitions using `SystemSeedActor`, preserving all
  existing administrator edits, availability and audit state on reruns.

Actor context is not accepted from request JSON, and an actor identifier does
not authenticate an administrator. The
[internationalisation manager](../internationalisationmanager/README.md) and
host own admission. Disabled definitions can still describe historical records;
their immutable metadata must not be inferred from current selection lists.

Keep absence (`ErrNotFound`), stale writes, duplicates, invalid payloads, store
unavailability and availability rejection (`ErrNotSelectable`) distinct. An
unknown write outcome must not be blindly retried or translated into an absent
record.

## Verification

There is no standalone test file in this package. Run
`go test ./external/currencycoder ./external/telenumcoder ./external/timezonecoder ./external/cataloguestore`
from the repository root for lifecycle, validation and adapter contracts. The
child packages own their callable migrations; importing catalogue registers none.
