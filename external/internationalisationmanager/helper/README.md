# Native catalogue setup

`InitialiseNative(ctx, db, Config)` explicitly prepares indexes, inserts missing
reference definitions and composes the owning internationalisation services over
the supplied database. It borrows the host pool without opening or closing one.
This is seed-writing setup, separate from passive service constructors.

`Config.Enabled` selects the operation. Disabled setup returns `(nil, nil)` and
ignores unused database/context/reachability values. Enabled setup requires a live
context and database. Optional `Reachability` is trusted `telenumcoder.HTTPConfig`;
its endpoint/timeout are validated before writes and its client is constructed
without contacting a provider. Empty endpoint leaves SMS reachability unavailable.
Phone normalisation does not invoke that provider and no messages are sent.

Flags migrate before currency, phone-country and timezone definitions. Every
migration is insertion-only: existing administrator edits, availability, deleted
state, revisions and attribution survive. Failure returns no service; previously
committed seed progress may remain. A later explicit invocation resumes rather
than replacing existing records. Unknown outcomes are not automatically retried.

The host owns explicit setup timing and deadline, timestamped migration
registration, database/configuration lifetime and feature policy. Importing
these packages registers nothing and starts no worker or listener. For separate
operator migrations, call the owning child `Migrate(ctx, db)` functions directly
in the same order.

Mount [the manager's handler](../README.md) with a trusted live `HTTPSecurity`
adapter. It owns verified administrator identity, the selected CSRF guard/cookie
and rate limiting. This helper accepts no host settings, member/guest credentials
or application domain types. Agreement, payment and reminder policy belong to
the consuming application.

Run `go test ./external/internationalisationmanager/helper`. With
`GHATD_TEST_MONGO_URI` configured for an isolated replica set, native fixtures
exercise original catalogue counts, bounded pagination and administrator-state
preservation across setup. Guard tables cover disabled/nil/cancelled/invalid
configuration before storage or provider I/O.
