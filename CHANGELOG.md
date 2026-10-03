# Changelog

Notable changes to GHATD are recorded here for application authors and
contributors. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
with release versions expressed using [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This record starts with the current upgrade. Earlier releases have not been
retrospectively catalogued. An unreleased entry is not a release announcement or
evidence that a feature has been deployed.

## Maintaining this file

- Update the current unreleased section in the same change as a notable feature,
  fix, security improvement, deprecation or compatibility change. Describe the
  effect on users rather than copying commit messages.
- Keep plans and unimplemented features in the project tracker. State limitations
  of partial or opt-in implementations and link to their canonical package guides.
- Mark incompatible changes with **Breaking:** and include the required migration
  action. Use public issue, PR or documentation links where useful; never include
  private project details, credentials or machine-specific paths.
- Use one `Unreleased` section until a version is selected. Then name it
  `[x.y.z] - Unreleased`. On publication, replace `Unreleased` with the actual
  `YYYY-MM-DD` release date and start a fresh unreleased section above it.
- Keep newest releases first and omit empty categories. Do not invent versions,
  dates or historical entries. Preserve published entries; clarify inaccuracies
  explicitly rather than silently changing the recorded release scope.

Before `1.0.0`, advance the minor version for features or incompatible API
changes and the patch version for fixes-only releases. Pre-release identifiers
such as `alpha`, `beta` and `rc` distinguish candidates from stable releases.
Choosing or documenting a version does not authorise creating a tag or publishing
a release.

## Changelog entry template

Use the categories that apply:

- `Added`: newly available capabilities.
- `Changed`: adjustments to existing behaviour.
- `Deprecated`: functionality scheduled for retirement.
- `Removed`: functionality no longer available.
- `Fixed`: corrected behaviour.
- `Security`: security-related changes.

The version below is a placeholder, not the next GHATD release. Replace it only
when the release version has been selected, and remove unused subsections.

```markdown
## [x.y.z] - Unreleased

### Added

- Describe the new capability and any opt-in requirements.

### Changed

- **Breaking:** Describe the compatibility impact and migration action, if any.

### Deprecated

- Identify the deprecated API and its replacement.

### Removed

- Identify the removed behaviour and migration path.

### Fixed

- Describe the corrected behaviour and affected users.

### Security

- Describe the security improvement and any required operator action.
```

---

## Unreleased

### Added

- Configurable [session-probe responses](external/accessmanager/middleware/README.md#session-probe-response-policy)
  through middleware and starter composition. Preserve the default `/me` 202
  error envelope, or explicitly select an empty 202 or structured 401 for a
  missing session. The private probe now sends no-store and noindex headers;
  other authentication failures and endpoints retain their existing contracts.
  An inconsistent identity returned as a successful verification is now a 503
  verification failure, not a missing-session response.
- Opt-in [display handles](external/user/v2/README.md#display-handles), with
  per-account-type generation during ordinary and OAuth creation, an explicit
  unique-index migration, independent revision metadata and atomic manual
  updates. Stale profile writes preserve handles; old names are released.
  The [session-only self-service API](external/usermanager/README.md#self-service-display-handles)
  uses strict payloads, required update ETags and shared reply error maps.
  Existing adapters and routes remain unchanged unless enabled. There is no
  automatic backfill, settings UI or change to identity/authorization claims.
- An opt-in [atomic Mongo update-and-decode helper](external/repository/README.md#atomic-update-and-selected-document-image)
  returns the selected document image without a separate read. It preserves
  native errors, caller sessions, collection codecs and driver options while
  keeping automatic logs payload-free. Legacy repository interfaces are
  unchanged; before-image upserts and decode failures can follow a successful
  write, so callers must reconcile outcomes before retrying.
- Opt-in [administrative token-policy management](external/accesspolicymanager/README.md)
  with strict JSON, preview ETags, create-only or revision-checked applies and
  write-time audit actors. The live administrator-session adapter rechecks current
  identity and session state; HTTP requires an explicit bearer with no cookie
  fallback. No automatic grant seeding, role migration or credential issuance.
- A [route-policy guard](external/accessmanager/middleware/README.md#route-policy-guard)
  connects verified session/API context to current grants, resource checks,
  strong-revision syntax and atomic request-budget admission. Hosts opt in and
  still recheck ownership and consequential writes at their domain boundary.
- [Native route error manifests](external/router/README.md#native-policy-error-manifests)
  extend router defaults with copied startup maps and ordered host overrides.
  Mapped native errors no longer need conversion to router sentinels; supplied
  responses must be 4xx/5xx. Joined or unknown policy failures remain opaque 503s.
  This extends the earlier sentinel-only route response contract.
- Opt-in [transactional API-token admission](external/accessmanager/README.md#transactional-api-token-policy),
  forwarded by the starter, using current grants and fenced owner-wide inventory.
  Limits, current account reads and insertion share the callback transaction;
  secrets are delivered only after successful completion. Explicit grant and
  inventory preparation on one managed Mongo client/database is required.
- Shared [live-session verification](external/accessmanager/README.md#live-session-authority)
  for explicit credentials and preloaded JWT guards, using current session owner,
  account identity, email revision and type. Active/admin guards enforce current
  stored status and roles instead of historical signed flags.
- Defensive session/API identity snapshots and an explicit bearer-only adapter,
  exposed through the [middleware suite](external/accessmanager/middleware/README.md#explicit-bearer-sessions).
  Hosts still enforce resource permissions and recheck sensitive mutations.
- Explicit [API-token inventory primitives](external/apitoken/README.md) with
  exact retained counts, owner-wide transaction fences and insert-only owner
  preparation on the managed Mongo client. These do not automatically replace
  legacy role admission, migrate grants or change route credential selection.
- Opt-in [system-scoped access policies](external/accesspolicy/README.md) with
  current grants, audited revision-CAS updates, fixed-window usage and atomic
  replay receipts on the managed Mongo repository. Business callbacks can share
  the transaction while rechecking current resource authority on replay.
- Explicit token-limit review, apply and revision-checked rollback that preserve
  other grant fields and usage. No automatic role translation, inventory setup,
  grant seeding, HTTP endpoints or middleware adoption is included.
- Safe policy error manifests with host overrides, plus context/dependency guards
  before custom store dispatch. Mongo initialization requires transactions;
  storage-retention tooling and production failover/load verification remain
  explicit adoption work.
- [Typed JWT identity context](external/auth/README.md) carrying stored account
  type, credential purpose, registered claims and optional issuer/audience
  binding. Legacy absent context remains explicit; type is classification, not
  authority. This does not automatically add middleware or route enforcement.
- Manager-owned dependency error inventories with copied maps and last-wins host
  overrides. Access, User, Content and Billing handlers include their collaborators
  by default; existing bundle injection remains supported. See the
  [composition guide](external/errormanifest/README.md#coverage-and-migration-checks).
- Opt-in [declarative routes](external/router/README.md#declarative-route-policies)
  with startup validation, defensive inventories and structured, fail-closed
  policy responses. Hosts supply enforcing middleware and authorizers; metadata
  is not a grant, and raw Mux routes remain outside the registry.
- [Request-local proof admission](external/accessproof/README.md) evaluates exact
  alternatives, capabilities, assurances, bindings and expiry. Hosts authenticate
  evidence and retain live transactional ownership and replay checks.
- [Strong-revision validation](external/router/README.md#shared-strong-revision-validation)
  parses singular If-Match headers with explicit size, whitespace and byte rules.
  It does not compare revisions or authorize writes. Unknown or ambiguous route
  authorizer failures produce 503; deliberate denials must use a route sentinel.
- Result-bearing Mongo mutation helpers, shared managed-client transactions,
  explicit index/collection setup and a transactional startup probe. Domains
  retain their schemas, authorization, revision checks and retention policy; see
  [repository migration guidance](external/repository/README.md#transaction-safe-operations).
- Reusable [AES-256-GCM payload encryption](external/encryption/README.md),
  preserving the standard nonce-prefixed byte format and requiring explicit AAD
  and a host-owned stable key. No automatic key rotation or data rewrite occurs.
- Opt-in [copy-on-write memory snapshots](external/ephemeral/README.md#process-local-transactional-snapshots)
  for local/test adapters, with cancellation-aware entry and deep-copy isolation.
  These are not a Redis replacement or production persistence fallback.
- Contributor guidance establishing table-driven tests as the default and
  requiring changelog updates for notable changes. The whole-suite test-style
  audit remains separate work.
- Reusable [manifest-driven HTTP error helpers](external/errormanifest/README.md#wrapped-errors-at-http-boundaries)
  preserve mapped wrappers and validation joins while rejecting unknown independent
  causes. A separate strict resolver supports single-cause authentication
  boundaries. Handler adoption and dependency-map wiring remain explicit.

### Changed

- **Breaking template change:** [Blueprint Mongo adapters](internal/blueprint/README.md#mongo-result-contract)
  now use result-returning update/delete helpers. Custom adapters and mocks must
  preserve acknowledgement and write counts instead of returning only an error.
- **Breaking template change:** [Blueprint](internal/blueprint/README.md#actorid-migration)
  uses `ActorID` for create, get-by-ID, update and delete requests. Actor-bearing
  HTTP mappers require explicit authenticated context; stored attribution and
  route permissions are unchanged. Update copied request types and adapters.
- **Breaking:** [Vision mutation commands](external/vision/README.md#actorid-migration)
  use explicit `ActorID` fields, including deletion. HTTP mappers require verified
  authentication; User Manager edits and deletion require agreement with the
  current caller. Stored authorship and vote ownership are unchanged.
- **Breaking:** [Pricer mutation commands](external/pricer/README.md#actorid-migration)
  use explicit `ActorID` fields instead of `UserID`. HTTP mappers require
  authenticated context and reject full plan/feature replacements. Trusted
  in-process replacements must agree with the selected resource and retain stored
  audit history; caller authorization remains the integrating workflow's duty.
- **Breaking:** [Post Mongo adapters](external/post/README.md#failure-and-snapshot-contracts)
  require result-bearing update/delete helpers so missing targets cannot produce
  false success. Custom stores must supply acknowledgement flags and real counts;
  unacknowledged writes report unavailability. Shared Mongo repositories already
  implement these methods.
  Post reads and writes no longer mutate caller-owned requests or snapshots;
  read pagination metadata from the response.
- **Breaking:** [Content Manager and Post callers](external/contentmanager/README.md#actorid-migration)
  now use `ActorID` instead of the eight reader and four mutation `UserId`
  fields. Stored authors and post targets are unchanged. Explicit actors must
  agree with supplied authentication context; trusted internal callers must
  establish authority themselves. HTTP updates now accept editable fields only,
  not embedded full-post replacements. Internal replacements must agree with
  an explicit target ID.
- **Breaking:** [Billing Manager caller fields](external/billingmanager/README.md#actorid-migration)
  now use `ActorID`; billing-read `UserID` remains the selected account. Update
  checkout, portal, pricing and former `RequestingUserID` callers. Private
  billing reads reject missing actors and HTTP mappers require verified context.
  Public pricing remains available anonymously with public-only projections.
  Cross-account lookup errors retain their mapped status (including user-not-found
  404 instead of the previous missing-ID 400); unavailable authority returns 503.
- **Breaking:** User Manager's direct caller fields now use `ActorID`, separate
  from embedded target IDs. Update Go callers using its former `UserId`/`UserID`
  fields or `GetGroupsByUserIDRequest.ID`; target parameters and stored ownership
  remain unchanged. Its actor-bearing request mappers require verified context,
  not an anonymous placeholder or context ID alone. Custom middleware must
  publish authentication state as described in the
  [migration guide](docs/how-to/request-identity.md). Optional anonymous contact
  submissions remain supported; other managers and delegated domain requests
  are not renamed by this change.
- Clarify [OAuth host adoption](docs/how-to/add-google-apple-sign-in.md) with
  host-independent startup instructions and per-deployment acceptance evidence.
  Account collisions do not imply a supported account-merge workflow. This is
  documentation guidance only; provider APIs and runtime behavior are unchanged.
- Access Manager login, verification, refresh and optional OAuth endpoints now
  use the [route registry](external/accessmanager/README.md#route-registry),
  preserving paths, methods, optional handler interfaces and OPTIONS precedence.
  **Breaking:** custom attachments must provide both the active-session and
  hardened code-rate-limit adapters and reject failed startup validation.
  Handler-verified labels document proof obligations; they do not enforce them.
- Authentication error replies consistently use canonical manifests. Native OAuth
  retains its fixed error vocabulary, with 500 for unknown or multi-cause failures
  and 503 for unavailable session verification instead of a client denial.
- Domain HTTP attachments now use the [shared route registry](external/router/README.md#coverage-and-explicit-raw-route-boundaries)
  for billing, content, groups, policy documents, pricing, users, user workflows,
  visions, the blueprint, SEO and communications. Paths, method order, middleware
  selection and legacy OPTIONS precedence are preserved. The unreachable later
  admin copy of the user-manager member lookup was removed, without changing the
  earlier reachable route or its service-level membership checks.
- **Compatibility:** migrated attachments require their authentication and
  optional-auth/rate-limit adapters. Missing adapters invalidate the registry,
  including public descriptors; hosts must reject startup on validation failure.
  Starter v0 now returns registry validation errors. Custom compositions must
  call validation after all attachments. SEO/communications expose explicit
  administrator-session-or-API selection; their default remains admin session.
- **Breaking:** credential-management and logout-other-sessions routes require
  the live active-session middleware. The mixed API-token/JWT route field is
  retained but ignored; missing session middleware returns 503. See the
  [migration contract](external/accessmanager/README.md#transactional-api-token-policy).
- Legacy role admission now requires exact inventory counts from token adapters;
  there is no paginated-list fallback. This transitional path remains non-atomic
  and must not share inventory with transactional writers during migration.
- **Breaking:** custom session adapters must preserve operational failures and
  return consistent live identities. Anonymous fallback requires an anonymous
  placeholder without credential metadata. Joined or unknown failures are not
  evidence of expired credentials; review the
  [cookie lifecycle contract](external/accessmanager/middleware/README.md#refresh-cookie-timing).
- Concurrent refresh wait timeouts now return 503 (`AM00-040`) without clearing
  cookies. Missing verification wiring returns 503 (`AM00-039`). Cookie-pair
  compatibility remains; atomic session-family revocation is not included.
- **Breaking:** custom API-token repositories/verifiers must provide exact
  digest lookup, owner-bound mutations and verified credential IDs. Activation
  and revocation require the trusted owner ID; usage updates require token ID,
  owner and digest. Access Manager's corresponding callers are migrated; see
  the [adapter contract](external/apitoken/README.md#repository-and-adapter-migration).
- **Breaking:** token digests are no longer JSON fields. Creation returns the
  one-time secret; management reads omit it and no longer delete expired tokens.
  Explicit owner-authorized deletion frees inventory. Description/status filters
  now treat search text as a literal substring, not a regular expression.
- **Breaking:** JWT verification is pinned to HS256, requires expiry and rejects
  future issuance times; metadata extraction rejects empty subjects and record
  IDs. Review custom issuers and plan session rollover before enabling optional
  issuer/audience constraints. See [rollout guidance](external/auth/README.md#compatibility-and-rollout).
- User ID/nano-ID lookups preserve repository failures and cancellation instead
  of reporting every failure as not-found. Custom adapters must distinguish
  absence from outages; [HTTP boundaries](external/user/v2/README.md) must keep
  raw diagnostics private.
- **Breaking:** Access Manager's handler error writer requires one unambiguous
  domain cause; custom service adapters returning joined failures now receive a
  generic 500, even when every joined cause is mapped. Classify those failures
  as one reviewed domain sentinel. Built-in invalid request fields remain mapped
  client errors; see the [strict response contract](external/errormanifest/README.md#strict-single-cause-authentication-boundaries).
- **Breaking:** the reply integration rejects invalid or shared custom response
  prototypes; custom factories must return independent state. Unknown error
  diagnostics are no longer printed to the standard logger, and joins containing
  an unmapped cause now return a generic 500. Review the
  [reply migration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md)
  and use its safe observer when failure telemetry is required.
- Redis session lookups now distinguish missing records from operational
  failures. Use `ephemeral.ErrAuthNotFound` or `errors.Is`; legacy `redis.Nil`
  remains detectable through wrapping, but direct equality is no longer safe.
  See [lookup semantics](external/ephemeral/README.md#live-session-lookup).
- Automatic repository logs now emit only fixed operation/outcome metadata;
  filters, documents, names and raw database errors are omitted even for custom
  loggers. Missing-document lookups use debug-level telemetry. Update log
  consumers for this privacy-oriented change; explicit application `Log*` calls
  and driver monitors still need their own redaction policy.

### Fixed

- Optional-authentication routes now admit rate-limited anonymous visitors when
  no placeholder user ID is configured. The shared context publisher accepts
  an explicit credential-free anonymous result without inventing an account;
  configured placeholders, authenticated identity checks and protected-route
  requirements retain their existing behavior.
- The [atomic Mongo update-and-decode helper](external/repository/README.md#atomic-update-and-selected-document-image)
  rejects unacknowledged receipts with `ErrUnacknowledgedMongoWrite` before
  decoding. An uncertain write is no longer misreported as a confirmed missing
  match; other native failures retain their identity. Callers must reconcile
  these outcomes rather than infer rollback or blindly retry. Absent images
  are logged as `no_document_image`, including successful before-image upserts.
- Blueprint validates insert, update and delete receipts, accepts matched no-op
  updates and reports missing targets separately from uncertain writes. Native
  read/setup failures retain their response mappings; operational failures are
  not collapsed into record-not-found. Repository entry/cancellation checks,
  query snapshots, cursor cleanup and overflow checks guard shared-helper calls,
  while setup logs exclude private dependency diagnostics.
- Blueprint services reject contradictory caller identity, invalid entry state
  and mismatched dependency results. Updates preserve fetched scalar state, list
  and count receive separate query copies, and native failures retain shared
  response mappings. Mapper/service logs no longer include raw diagnostics.
- Vision rejects contradictory caller context and invalid mutation dependencies
  or selected-record results. Scalar edits and User Manager metadata filtering no
  longer mutate caller-owned input. Native errors retain shared response mappings;
  invalid create receipts cannot trigger automatic retries or leak diagnostics.
- Pricer mutation boundaries reject contradictory actors, invalid wiring and
  inconsistent selected-record reads. Service normalization no longer changes
  caller-owned scalar history or cost IDs. Native errors retain shared reply
  mappings and host overrides; mutation service logs omit payloads and diagnostics.
- Post lookups consistently distinguish confirmed absence from operational errors.
  Failed slug checks prevent writes; native failures retain shared response mappings.
  Title and type changes regenerate slugs for field edits and trusted replacements.
  Invalid dependencies/results return mapped unavailability. Service logs exclude
  content and raw storage diagnostics; header-image count filters match list filters.
  Empty post-type selections now receive their mapped validation response.
- Content Manager validates administrator identity and malformed dependencies
  before dispatch, preserves native mutation error mappings, and reports invalid
  adapter results as unavailable. Public projections copy filters and latest
  overview slices; single-item reads exclude soft-deleted content.
- Billing subscription email association checks the selected account identity
  and preserves dependency failures instead of returning a false no-subscription
  result. Pricing visibility restrictions no longer mutate caller-owned filters.
- API credential lifecycle boundaries reject missing dependencies, cancellation
  and malformed custom-store results without publishing identity or a secret.
  Field-only usage/status writes preserve concurrent revocation and deletion;
  invalid status values no longer default to revocation. Display timestamps
  handle offsets/fractions and clear invalid values.
- Reject invalid UTF-8 account types before signing so JSON encoding cannot
  silently change identity context.
- Domain and blueprint handlers use the shared manifest writer for wrapped
  failures and all-mapped validation joins; unknown independent causes return a
  generic server failure. Access Manager uses strict single-cause resolution.
  Existing success replies and host overrides are preserved.
- Complete missing group, pricing and user error mappings, add stable sitemap
  error codes and include streak failures in User Manager's dependency bundle.
  Migrated domain-handler logs use safe resolved identities; this does not
  sanitize all service, authentication or repository logs automatically.
- Pin the reply integration upgrade for structural error resolution.
  This is an immutable, unreleased integration commit, not a tagged release.
  Reply uses request-local response state, safe opt-in unmapped diagnostics and
  deterministic error ordering; see
  its [migration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md).
- Repository find/count/cursor failures retain native error causes underneath
  existing error codes, enabling retry-label and cancellation inspection.
  Single-result cursor mapping closes its cursor and distinguishes iteration
  errors from missing documents.

### Deprecated

- API-token repository whole-record updates and unowned deletion remain trusted
  administrative APIs only. Prefer owner-bound status/delete and field-only
  usage methods to avoid stale authority overwrites.

### Security

- Self-service notification feeds now bind recipient identity and email to the
  authenticated caller. Explicit recipient selection remains on administrator
  routes and requires a matching live ACTIVE administrator account.
  **Breaking:** in-process notification-overview commands must set `AdminView`
  to select another recipient; the default is self-service. See
  [notification recipient boundaries](external/usermanager/README.md#notification-recipient-boundaries).
- [User Manager mutation boundaries](external/usermanager/README.md#mutation-identity-boundaries)
  bind caller identity independently of JSON and preserve URL-selected group,
  member and contact targets. Self-deletion always selects the verified caller;
  anonymous contact submissions cannot forge user attribution. **Breaking:**
  protected mutation mappers require an authenticated context flag as well as an
  ID; custom middleware must publish a trusted verification result. HTTP group
  updates no longer accept full nested group records; use editable fields and
  dedicated ownership/membership endpoints. Trusted Go service calls retain
  their existing request types and authorization responsibilities.
- [Email-proof admission](external/accessmanager/README.md#proof-admission-and-upgrade-compatibility)
  checks exact stored ownership, current account identity, signed type/revision
  and permitted purpose before atomically consuming the proof and issuing a
  session. Concurrent reuse has one winner. Custom stores must return accurate
  atomic exact-key deletion counts. Failures after consumption require a new
  proof; account updates and session storage are not one transaction.
- **Breaking:** login and email verification reject proofs without a signed
  token_use claim, as well as session/refresh credentials. Request fresh email
  links or codes after upgrading old signers. Ordinary legacy sessions remain
  compatible; missing account type remains unbound rather than inferred.
- OAuth linking reuses live-session authority and carries signed account type
  through pending browser/native flows. Duplicate browser access cookies are
  rejected. Current identity, type/revision, status and recent authentication
  are rechecked; operational errors no longer become reauthentication denials.
  These are check-time guarantees, not locks against concurrent account changes.
- Administrative target lookup distinguishes actual wrapped absence from joined
  failures or custom error aliases; inconsistent results fail closed. Cancellation
  stops work between adapters. Known policy commits retain their receipts even
  when transport cancellation races the return; uncertain outcomes remain errors.
  Session/account checks are not atomic with Mongo policy commits.
- Route-policy checks preserve original error causes and stop handler dispatch
  after observed cancellation. A charged request budget is not automatically
  refunded. Credential grants never inherit the owner's account grants, and
  response-map overrides cannot grant access.
- Configured token admission never falls back to role allowances on policy
  denial or outage. It validates adapter results, preserves failure causes and
  checks cancellation between calls before delivering a secret. Cancellation or
  an uncertain commit does not prove rollback; issuance is not a secret-recovery
  API. Native policy error manifests retain host override precedence.
- Cookie adapters refresh only known absent/expired credentials, preserve cookies
  on account denials or operational failures, and publish replacement cookies
  only after retry identity validation and the final cancellation check. Anonymous
  fallback strips selected credentials/inherited identity and rejects authenticated
  adapter results. Explicit bearer mode never refreshes or falls back to cookies.
- Authentication and hardened-limit middleware use canonical manifest responses.
  Joined storage failures cannot justify an IP ban. Refresh preserves failure
  causes, rejects malformed token results and omits raw adapter diagnostics from
  manager logs; injected dependencies retain their own logging responsibility.
- Session-store absence decisions now inspect every wrapped cause, preserving
  joined storage failures instead of treating them as missing sessions. Native
  and legacy Redis absence remain supported; typed-nil, cyclic, oversized and
  custom `Is`-only errors fail closed. Use
  [`ephemeral.IsAuthNotFound`](external/ephemeral/README.md#live-session-lookup)
  for control flow; one `errors.Is` match is not proof of pure absence.
- New opaque API secrets use cryptographic randomness. Verification resolves an
  exact stored digest, checks status/expiry and binds it to the current active
  owner rather than scanning a page or trusting a public prefix. Assess rotation
  of older secrets separately; no automatic revocation or reissue occurs.
