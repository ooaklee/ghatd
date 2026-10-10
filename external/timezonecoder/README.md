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
zone, not today's `GMT` label. `ResolveLocalTime(date, timezone, clock)` accepts
strict `YYYY-MM-DD` and `HH:MM` input, rejects nonexistent local times and chooses
the earliest UTC occurrence of a repeated time. It rejects `Local` to avoid
process-dependent persisted instants, and returns `catalogue.ErrInvalidPayload`
for invalid input or a gap. It uses the timezone database without repository or
provider calls; it does not check catalogue availability. Hosts retain due-date
policy, selectability checks and domain error mapping. Persist both the chosen
instant and original civil fields when later replay must remain stable.

`NewMongoRepository` borrows the host database for `i18n_timezones`.
`Migrate(ctx, db)` ensures indexes and seeds missing definitions;
`Service.Migrate(ctx)` seeds an injected repository. Reruns preserve
administrator state, and host migrations own timestamped registration.

Run `go test ./external/timezonecoder` from the repository root. The
[tests](service_test.go) cover date-aware London/New York offsets, fractional
offsets, accepted aliases, rejected fixed/unknown zones and identifier-based
display names. The [civil-time tests](civil_time_test.go) cover gaps/folds,
fractional offsets, date boundaries and strict input. Shared lifecycle and Mongo
contracts are covered by the child catalogue and cataloguestore tests.
