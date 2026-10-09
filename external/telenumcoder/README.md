# Telephone number coder

Phone-country definitions, explicit-country parsing and an optional reachability
port. `Service` composes [catalogue](../catalogue/README.md) lifecycle policy
through a typed repository. [repository.go](repository.go) owns storage mapping;
[reachability_http.go](reachability_http.go) owns the optional provider protocol.
The [internationalisation manager](../internationalisationmanager/README.md)
composes the owning service, rather than accessing either adapter directly.

## Number parsing and availability

`Country` embeds `catalogue.Entry` with immutable calling code and dial prefixes.
`Definition(code)` returns a copy of the adopted numbering metadata.
`NewService` validates these definitions and supplies audited catalogue commands;
the host/manager owns administrator admission.

`Normalise(ctx, phone, region)` accepts a national number only with an explicit,
selectable region. A number starting with `+` selects its actual region instead.
Libphonenumber validates the number and extensions are rejected. The actual
region's availability is rechecked, so selecting another country cannot bypass
a disabled region. The returned `NormalisedNumber` is complete canonical E.164
with `+` and a country calling code. There is no locale-based country inference.

Normalisation is formatting/numbering evidence, not residency, contact ownership
or WhatsApp registration. A separate messaging provider checks channel registration; it must accept only
canonical numbers.

## Optional reachability

`WithReachability` injects the narrow `Reachability.Check` port.
`Service.Check` first normalises, then reports `reachable`, `unreachable` or
`unavailable`. A missing provider, provider failure or unknown state remains
unavailable; cancellation propagates. The result's `channel` is `sms` because
this is the legacy SMS reachability contract. It does not enable SMS messaging
or determine WhatsApp registration; this adapter does not send SMS.

`NewHTTPReachability` accepts trusted HTTPS configuration, copies an injected
client, disables redirects and bounds its timeout and response body. It posts
`{number, channel}` and expects a strict `{state}` response, with an optional
bearer token. It never sends a message. Credentials and phone numbers must not
enter logs; the host owns endpoint and credential configuration.

`NewMongoRepository` borrows the host database for `i18n_phonecodes`.
`Migrate(ctx, db)` and `Service.Migrate(ctx)` insert missing seeds without
resetting administrator edits; timestamped registration belongs to the host.
[seeds.json](seeds.json) and [sources.json](sources.json) retain provenance.

Run `go test ./external/telenumcoder` from the repository root. The
[tests](service_test.go) cover actual-country policy, national/international
parsing, unavailable reachability, HTTPS/response bounds and cancellation.


The retained seed snapshot was generated with phonenumbers v1.7.5; runtime
validation uses the independently pinned module dependency (currently v1.8.1).
[The metadata guard](seed_metadata_test.go) checks every region/calling code and
supported-set size before accepting dependency changes. This is not an automatic
metadata refresh. [NOTICE](NOTICE) distinguishes the Go port's MIT licence from
upstream numbering metadata's Apache-2.0 licence; both copies accompany the data.
