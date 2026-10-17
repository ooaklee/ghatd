# Internationalisation manager

The owning composition service and HTTP boundary for currencies, phone countries,
flags and timezones. `Service` calls the child services, validates cross-domain
flag references and builds public/admin views. `Handler` calls the `HTTPService`
port and the host's `HTTPSecurity` adapter. Neither layer accesses Mongo, cache
or a reachability provider directly.

## Composition and operations

`NewService(currencies, phones, flags, timezones, clock)` requires all four
owning services. A nil clock uses the real UTC clock. The host constructs these
over its existing database and mounts `NewHandler(service, security)` in its router.
Optional [native setup helpers](helper/README.md) compose these existing owners
and perform explicit insertion-only migrations; they are separate from passive
manager constructors, which perform no storage or provider I/O.

`List` and `Get` project catalogue records. Public views expose selectable
records; administrator views may include unavailable/deleted records and audit
actors. Lists are bounded to 200 records per HTTP page. Cursors bind the kind,
admin/public view and page size; they are continuation positions, not authority
or a stable snapshot across concurrent catalogue edits.

`Create`, `Update`, `Remove` and `Restore` delegate to the relevant child service.
Definition-specific fields remain immutable where required. Flag references
are checked through [globalflagger](../globalflagger/README.md), not its
repository. Actor context comes from live host admission; update/delete/restore
require a matching revision. Restoration does not silently re-enable a choice.

`ValidateCurrency`, `ValidatePhone` and `ValidateTimezone` recheck selection
policy at new-choice boundaries. `NormalisePhone` delegates to
[telenumcoder](../telenumcoder/README.md) without invoking reachability.
`CheckPhone` is the separate legacy SMS reachability operation; it does not
send SMS or perform a WhatsApp registration check.

### Optional flag enrichment

Currency, phone-country and timezone lists accept `include_flags=false` on
both public and administrator routes, for example
`GET /api/v1/i18n/currencies?limit=200&include_flags=false`. This skips all
referenced-flag service/storage calls, omits `flag_url` and preserves `flag_id`
and the catalogue's selection, precision, calling-code and timezone fields.
Consequently, flag-storage failure cannot block these list responses. Errors
from the requested catalogue itself still propagate normally.

Omitting the parameter or using `include_flags=true` preserves the existing
revisioned URLs and flag visibility checks. Only the exact values `true` and
`false` are accepted; empty, malformed or repeated values return HTTP 400.
The parameter is not supported on `/flags` lists or individual-record reads.
Go callers opt out with `ListQuery.OmitFlagURLs = true`; its zero value keeps
enrichment, and enabling it for a flag list returns an invalid-request error.

Repeat `include_flags=false` on every cursor request. Cursors remain compatible
with either projection and do not retain the enrichment preference. Consumers
can render text immediately and load selected/visible images independently from
`GET /api/v1/i18n/flags/{flag_id}/svg` (URL-escape the identifier), without a
revision parameter. That existing endpoint rechecks public flag availability;
hidden, disabled, deleted or missing flags remain unavailable even though a
catalogue may retain their `flag_id`. The corresponding administrator endpoint
requires live administrator admission. Keep image failures independent of form
selection and submission; loading images must not reset user-entered values.

## HTTP and failure semantics

The base path is `/api/v1/i18n`. Public catalogue/SVG reads and administrative
operations under `/admin` share strict request parsing, request limits and
`no-store` responses. `GET /csrf` bootstraps the host's protection;
`POST /phonecodes/check` requires it before provider work. `HTTPSecurity`
owns live administrator resolution, CSRF issuance/verification and rate limiting.
The `admin` argument to service methods is a trusted projection choice, not
authentication in its own right.

Revision-bound writes use strong `If-Match` headers. Errors distinguish missing
or unavailable records, stale writes, duplicate identities and invalid records.
The HTTP edge hides native failure text. Sanitised SVG serving and catalogue
changes do not bypass child ownership.

Run `go test ./external/internationalisationmanager` from the repository root.
[Service tests](service_test.go) cover composition, references, availability,
pagination, date-aware offsets and phone normalisation; [HTTP tests](http_test.go)
cover live admin admission, CSRF, revisions, public SVG and protected lookup.
