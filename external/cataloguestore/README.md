# Catalogue store

Reusable document mapping and Mongo/memory adapters for the typed
[catalogue](../catalogue/README.md) repository port. This package moves
`Document` values and enforces storage result semantics. Definition validation,
audit generation, administrator admission and availability policy belong to the
owning services, not the persistence adapter.

## Adapter contract

`Store` exposes natural-key lookup, insert-if-absent, revision-bound replacement
and bounded listing. `NewRepository[T]` maps a child record through its
`catalogue.Entry` into `Document` metadata and payload; `DecodePayload[T]`
restores its typed BSON representation. Typed repositories derive both `ID` and
`Key` from the entry code; unrelated identifiers are not their record contract.
Child domains expose this as their own typed repository, so service code need
not handle generic documents.

`OpenMongo(db, collection)` borrows an existing database without opening or
closing a connection pool. `NewMongoStore` accepts the shared helper and
collection explicitly. `EnsureIndexes` creates the unique catalogue key and
selection index; the child package's migration owns when to call it.
`NewMemoryStore` provides an isolated adapter with equivalent revision behaviour.

Important result semantics:

- Cancellation is checked around driver operations; native failures remain
  failures rather than becoming `ErrNotFound`.
- Known absence, duplicates and revision conflicts map to their distinct
  catalogue sentinels. A replacement must match the expected revision.
- Insert-if-absent returns the existing record for a known duplicate, preserving
  administrator changes during seeding.
- Ambiguous/native writes are not retried. The caller must reconcile unknown
  outcomes rather than assuming no change occurred.
- Lists apply availability/deletion filters and bounded ordering. No cache
  retains newly disabled choices. `Close` invalidates the memory store.

Collection names, lifecycle and migration registration remain with
[currencycoder](../currencycoder/README.md), [telenumcoder](../telenumcoder/README.md)
and [timezonecoder](../timezonecoder/README.md). Extend reusable storage
capabilities here or in the shared adapter boundary instead of adding driver
calls to those services. Relocating the package changes no collection/index or stored document contract.

## Verification

Run `go test ./external/cataloguestore` from the repository root. The
[contract tests](store_test.go) exercise memory and Mongo adapters, cancellation,
compare-and-swap, duplicate handling and native/uncertain failures. Mongo tests
require `GHATD_TEST_MONGO_URI` for an isolated test database and otherwise skip;
an isolated-memory pass alone is not evidence of a Mongo run.
