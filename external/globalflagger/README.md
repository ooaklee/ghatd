# globalflagger

Stores reusable, sanitised SVG flags behind a typed repository port. Services own
validation, audit and revision rules; Mongo and memory adapters own persistence.
The host calls `Migrate(ctx, db)` or `EnsureIndexes(ctx, db)` using its existing
database. Seeding never overwrites administrator state. The host registers its
own timestamped migration; importing this package has no registration side effect.

Artwork comes from the pinned MIT-licensed flag-icons revision recorded in
`seedassets/source.json`. The complete licence is in `seedassets/LICENSE`.
`sh-ac.svg` is retained as source evidence but excluded because its gradient
references do not resolve; the other 270 assets pass the same sanitiser as uploads.

Flags have stable uppercase identifiers, including bloc and subdivision flags.
Consumers persist explicit `flag_id` references and use the manager's SVG URL.
See [the internationalisation manager](../internationalisationmanager/README.md)
for serving, security, migration and verification details.


`Service.Seed(ctx)` returns an exported `SeedResult` with inserted/skipped
counts. A seed is an explicit storage operation, not a service constructor.
Run `go test ./external/globalflagger` for SVG active-content/resource/reference
rejection, stable sanitisation of the 270 publishable pinned assets, exclusion
of the retained broken asset and revision/audit preservation during reseeding.
