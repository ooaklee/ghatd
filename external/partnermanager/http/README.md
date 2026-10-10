# Partners HTTP transport

`partnerhttp` mounts an optional member-only JSON API at `/api/v1/partners`.
It composes [partnermanager](../README.md), current live sessions and exact action
permissions. It has no agreement dependency, store, financial formula, payout
sender or worker. A generic administrator role confers no Partners permission.

## Composition and browser policy

Construct `NewService(ServiceConfig)` with the human-authorized `Manager`, the
same live `Sessions` and `Authority`, host-approved `ProgramView`, `PublicOrigin`
and optional `ReferralPath` (default `/ref`). `ProgramView` is disclosure and
admission configuration; it never substitutes for approved effective policy.
Only an explicit `AllowLocalHTTP` permits HTTP on loopback. Referral prefixes
must be clean absolute paths without host, query, fragment or escaped segments.
Constructors validate wiring without I/O; prepare stores through their owners.
Do not pass a worker-authorized manager to the browser service.

Construct `New(service, resolver, Config)` with a trusted `PrincipalResolver` and
an immutable [browser guard](../../http/browsersecurity/README.md). For example,
the following composition assumes already acquired owners and host configuration:

```go
security, err := browsersecurity.New(browsersecurity.Config{
    Key: hostCSRFKey,
    AllowedOrigins: hostOrigins,
    CSRFCookieName: hostCSRFCookieName,
    TrustedNativeClientIDs: hostVerifiedNativeAudiences,
})
if err != nil { return err }
service, err := partnerhttp.NewService(partnerhttp.ServiceConfig{
    Manager: humanManager, Sessions: liveSessions, Authority: humanAuthority,
    Program: programDisclosure, PublicOrigin: publicOrigin, ReferralPath: "/ref",
})
if err != nil { return err }
handler, err := partnerhttp.New(service, currentMembers, partnerhttp.Config{
    Security: security, Limiter: requestLimiter,
})
if err != nil { return err }
router.Path(partnerhttp.BasePath).Handler(handler)
router.PathPrefix(partnerhttp.BasePath + "/").Handler(handler)
```

The host chooses the cookie name when constructing the guard and may pass the
same guard to other APIs. Shared cookies/tokens require the same key, cookie name
and ordered principal binding for the same member. Different binding purposes
with one cookie name force rebinding and invalidate the other token. The host
resolver supplies `Principal.Transport`; the package does not read another
application's guest/draft cookies or import its session types. Native admission
requires a host-verified, allow-listed token audience with no Cookie header.
An unsigned client header, API token, role or JSON actor is not a principal.

Every principal must have a nonempty caller `ActorID` and `Credential` and current
verified account admission. The host resolves and checks the live session before
disclosure; the service binds the credential with `partneraccess.WithVerifiedSession`,
and owning managers recheck exact permissions for both new commands and replay.
Selected resource/account IDs never become the actor. Access guidance checks
current sessions before and after policy; only a sole known denial becomes
`allowed=false`. Guidance is not a grant and does not enumerate financial records.

`Config.MaxBodyBytes` defaults to 64 KiB and cannot exceed that bound. A nil
limiter selects the bounded process limiter (120 requests/minute); hosts may
supply their own current admission port. A supplied observer receives finite
methods, operation names, route templates, stages, safe error codes/statuses and
durations, never raw paths, selected IDs, actors, credentials, query values or
payloads. Hosts retain CORS, cache exclusion, route mounting and resource lifetime.

## Protocol and routes

`GET /api/v1/partners/csrf` returns 204 and the signed-cookie/token bootstrap for
an authenticated member. Its only optional query is `audience=session`; duplicate
or other bootstrap parameters reject. Qualifying same-site ordinary GETs also
issue the `X-CSRF-Token` header. Browser unsafe requests require the signed cookie,
token header and one trusted Origin/Referer. Verified native requests use the
configured cookie-free bearer admission. Responses are `no-store, no-transform`.

All unsafe requests require exactly one printable `Idempotency-Key` of 1–128
bytes, including preview and CAS operations. The header does not make a CAS
operation replayable. Requests with a body use one `application/json` content
type (optional UTF-8 charset) and one JSON object. DTOs reject unknown fields
and trailing values; GET bodies reject. Duplicate JSON object keys follow the
standard decoder's last-value behavior. An optional `If-Match` must be strong;
it is validated but is not forwarded or used for concurrency control. Partners
revisions remain the explicit body fields documented by their owners.

Paths below are relative to `/api/v1/partners`. `GET /csrf` is the additional
bootstrap protocol endpoint, outside these 32 operations.

| Method | Path | Fixed operation |
| --- | --- | --- |
| GET | `/program` | `partners.program.read` |
| GET | `/overview` | `partners.overview.read` |
| GET | `/share-link` | `partners.share-link.read` |
| POST | `/share-link/rotate` | `partners.share-link.rotate` |
| GET | `/referrals` | `partners.referrals.read` |
| GET | `/ledger` | `partners.ledger.read` |
| GET | `/claims` | `partners.claims.read` |
| GET | `/claims/{id}` | `partners.claim.read` |
| POST | `/claims/{id}/cancel` | `partners.claims.cancel` |
| POST | `/claims` | `partners.claims.create` |
| POST | `/enrollment` | `partners.enroll` |
| GET | `/destination` | `partners.destination.read` |
| PATCH | `/destination` | `partners.destination.update` |
| GET | `/admin/access` | `admin.partners.access.read` |
| GET | `/admin/claims/{id}/action` | `admin.partners.claim.read` |
| GET | `/admin/{id}/status` | `admin.partners.status.read` |
| GET | `/admin/{id}/claim-preparation` | `admin.partners.claim-preparation.read` |
| GET | `/admin/policy/individual/{id}` | `admin.partners.policy.individual.read` |
| GET | `/admin/policy` | `admin.partners.policy.read` |
| POST | `/admin/policy` | `admin.partners.policy.publish` |
| GET | `/admin/{id}/inspect` | `admin.partners.inspect` |
| GET | `/admin/claims` | `admin.partners.claims.queue` |
| POST | `/admin/attribution/preview` | `admin.partners.attribution.preview` |
| POST | `/admin/attribution` | `admin.partners.attribution.apply` |
| POST | `/admin/claims` | `admin.partners.claims.create` |
| POST | `/admin/claims/amendment` | `admin.partners.claims.amend` |
| POST | `/admin/claims/return` | `admin.partners.claims.return` |
| GET | `/admin/operations` | `admin.partners.operations.read` |
| POST | `/admin/claims/decision` | `admin.partners.claims.decide` |
| POST | `/admin/claims/payment-observation` | `admin.partners.claims.observe` |
| POST | `/admin/claims/payment` | `admin.partners.claims.payment` |
| POST | `/admin/status` | `admin.partners.status` |

Successful operation responses retain `{ "data": ... }`: enrollment, claim
creation and policy publication return 201; other operations return 200.
Projections enumerate fields rather than serializing private owner records.
Customer ledger/claims omit payer/provider IDs, policy/group identities and
operator details. Selected operator views omit unneeded audit identities.
Publication and accepted command receipts include only their reviewed fields.

Pagination limits are 1–100 (default 50). Business date ranges are inclusive-from,
exclusive-to; ledger cursors use owning sequence snapshots. Claim state filters
are a bounded union of the six known states, with duplicates/unknown values
rejected. A full claim page permits another read without proving one exists.
Selected claim actions admit processing/recording/amendment/return capabilities;
selected status, preparation and individual policy history use the appropriate
partner/customer scopes. A program-list denial never supplies or disproves a
selected-target grant. Inspect `Routes()` and the owning manager README for exact
command semantics, selected scopes and financial validation.

## Revisions, receipts and uncertainty

Enrollment/destination/status/policy/decision changes use owner CAS revisions
or versions and current-read reconciliation after uncertainty. Financial claim
creation/cancellation/payment/amendment/return, link rotation and prospective
attribution apply keep their original key and canonical request. Their owners
recover original receipts before mutable state/revision checks, after current
authority checks. This package adds no HTTP idempotency table or cross-owner
transaction. Never rotate a key or treat an uncertain outcome as failure merely
to send a replacement. Receipt responses distinguish original accepted intent
from the current claim head after later corrections or returns.

Manual recording requires the actual external transfer date/method/reference;
the host binds actor and immutable claim amount/currency. Incomplete, mismatched
or unknown transfers stay under review and cannot settle. Amendments and returns
use the existing append-only owner contracts. Program pause does not erase
retained earnings, reservations or the evidence for a previous transfer.

## Error responses

Errors use `{ "error": { "code": "PARTNERS_...", "message": "..." } }` with a
generic message. Dependency diagnostics and error strings are never serialized.

| Code suffix | HTTP status |
| --- | --- |
| `INVALID_REQUEST` | 400; oversized body 413 |
| `AUTH_REQUIRED` | 401 |
| `VERIFICATION_REQUIRED`, `ACCOUNT_UNAVAILABLE`, `FORBIDDEN` | 403 |
| `NOT_FOUND` | 404 |
| `METHOD_NOT_ALLOWED` | 405, with Allow |
| `CONFLICT`, `INSUFFICIENT_FUNDS` | 409 |
| `STALE_WRITE` | 412 |
| `RATE_LIMITED` | 429, with rounded-up Retry-After |
| `DEPENDENCY_UNAVAILABLE`, `OUTCOME_UNCERTAIN` | 503 |
| `INTERNAL_ERROR`, `CONFIGURATION_INVALID` | 500 |

Unknown errors fail closed. Unavailable/uncertain joined outcomes retain their
honest recovery status; unknown causes cannot become successful denial/absence.
Construction and route ownership do not alter owner record kinds, permission
names, scope hashes, encryption metadata or financial receipt fingerprints.
Configure clients against the routes and error codes above when mounting this
optional transport.

## Verification scope

The table-driven transport suite covers all 32 route bindings, reserved-looking
selected IDs, admission, strict headers/body/query limits, response errors and
bounded observations. Projection/access suites cover retained evidence,
revocation and joined failures. Financial concurrency, transaction and durable
replay guarantees remain covered by owning-service native tests; a routing
fixture does not establish provider delivery or deployment.
