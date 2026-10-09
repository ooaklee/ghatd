# Timezone coder

Named timezone availability and offsets for an explicit instant. `Service`
validates adopted zone identifiers and composes [catalogue](../catalogue/README.md)
lifecycle policy through a typed repository; [repository.go](repository.go)
adapts [cataloguestore](../cataloguestore/README.md). The
[internationalisation manager](../internationalisationmanager/README.md) owns
composition and the host owns administrative admission.

`Timezone` embeds `catalogue.Entry`. Supported identifiers and reviewed aliases
come from [seeds.json](seeds.json), with provenance in [sources.json](sources.json).
Definitions must also load through Go's timezone database, embedded using
`time/tzdata`. Arbitrary fixed offsets, `Local` and unrecognised identifiers are
not accepted as catalogue definitions.

`NewService` supplies the shared audited create/update/delete/restore and list
operations. `RequireSelectable` is for new application choices. `Describe(ctx,
code, at)` reads a record and returns its `OffsetSeconds` and `LocalTime` at the
given instant; `DescribeRecord` operates on a record already read through the
service. Description does not require current selectability, so historical
records can retain their timezone context.

An offset is date-dependent, including daylight-saving rules. Persist the named
zone, not today's `GMT` label. This package describes instants; local due-date
validation and ambiguous/nonexistent clock handling belong to
the host's domain and delivery policy.

`NewMongoRepository` borrows the host database for `i18n_timezones`.
`Migrate(ctx, db)` ensures indexes and seeds missing definitions;
`Service.Migrate(ctx)` seeds an injected repository. Reruns preserve
administrator state, and host migrations own timestamped registration.

Run `go test ./external/timezonecoder` from the repository root. The
[tests](service_test.go) cover date-aware London/New York offsets, fractional
offsets, accepted aliases, rejected fixed/unknown zones and identifier-based
display names. Shared lifecycle and Mongo contracts are covered by the child
catalogue and cataloguestore tests.
