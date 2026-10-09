# Currency coder

Currency definitions, immutable accounting precision and audited availability.
`Service` validates definitions and composes [catalogue](../catalogue/README.md)
lifecycle rules through the typed `Repository` port. [repository.go](repository.go)
adapts the reusable [catalogue store](../cataloguestore/README.md); the service
does not issue Mongo queries or authorise administrators.

```go
repo, err := currencycoder.NewRepository(cataloguestore.NewMemoryStore(nil))
// Handle err before constructing the owning service.
service, err := currencycoder.NewService(repo, nil)
// Handle err, then seed the isolated catalogue before selecting a currency.
err = service.Migrate(ctx)
currency, err := service.RequireSelectable(ctx, "GBP")
// currency.MinorUnit is accounting precision, not a conversion rate.
```

`Currency` embeds `catalogue.Entry` and adds `MinorUnit`. Codes and precision
must match [seeds.json](seeds.json); administrators may change presentation and
availability but cannot invent currency definitions or alter their exponent.
[sources.json](sources.json) records the adopted source. `MinorUnit(code)` reads
immutable metadata independently of current availability, preserving precision
for historical records after a currency is disabled.

`NewService` supplies `Get`, `List`, `RequireSelectable`, `Create`, `Update`,
`Delete`, `Restore` and `Seed` through the shared lifecycle. New choices require
selectability. Administrative writes carry trusted actor context and expected
revisions; the [internationalisation manager](../internationalisationmanager/README.md)
and host authenticate those actors.

`NewMongoRepository` borrows the host database and uses `i18n_currencies`.
Package-level `Migrate(ctx, db)` ensures indexes and inserts missing seeds;
`Service.Migrate(ctx)` seeds an already-composed service. Neither overwrites
existing administrator state. The host registers the timestamped migration;
importing this package has no registration side effect.

Run `go test ./external/currencycoder` from the repository root. The
[service tests](service_test.go) cover audit attribution, stale revisions,
soft deletion/restoration, rerun preservation, immutable precision and excluded
definitions. Shared Mongo result semantics are tested in cataloguestore.
