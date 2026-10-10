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
- Describe the final change from the merge target. Omit discarded branch-only
  packages and intermediate migration steps; name the current package instead.
  Keep genuine compatibility and storage migrations from the target branch.
- Mark incompatible changes with **Breaking:** and include the required migration
  action. Use public issue, PR or documentation links where useful; never include
  private project details, credentials or machine-specific paths.
- Keep work under its selected `[x.y.z] - Unreleased` scope. Older pending
  scopes remain unreleased until publication; do not invent a release date. On
  publication, use the actual `YYYY-MM-DD` date and start the next scope above it.
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

## [0.5.0] - Unreleased

### Added

- Authenticated [paid-subscription evidence](external/paymentprovider/README.md#current-paid-subscription-evidence)
  and optional [paid-partner acquisition](external/partnermanager/README.md#optional-paid-partner-acquisition).
  New enrollment and referrals can require a current paid period while existing
  balances, claims and original receipts remain recoverable. Hosts opt in explicitly.
- A member eligibility read and configured policy hold maximum in the optional
  [Partners HTTP transport](external/partnermanager/http/README.md#protocol-and-routes).
  Strict response decoders must accept the added `max_hold_days` program field.

- Explicit, opt-in [user capability defaults](external/accesspolicy/README.md#explicit-user-defaults)
  for authoritative grant absence. Stored grants fully replace defaults; revocation,
  expiry, missing capabilities and storage failures continue to deny admission.
- An opt-in [partner program](external/partnerprogram/README.md) with enrollment,
  scoped policy, referral attribution and links, an [earnings ledger](external/partnerearnings/README.md),
  statements, withdrawal claims and manual payment recovery. [Partner Manager](external/partnermanager/README.md)
  coordinates the owning services; payment destinations and approval remain explicit.
- [Referral evidence and analytics](external/referral/README.md), including consented
  visits, retained partner relationships, grouped commission reports and original-cohort
  paid conversions. Reports use current scoped authority and retained financial evidence.
- [Partners runtime](external/partnermanager/runtime/README.md),
  [integration helpers](external/partnermanager/helper/README.md) and optional
  [HTTP transport](external/partnermanager/http/README.md). Hosts provide configuration,
  identity, browser protection and scheduling; construction creates no accounts or grants.
- [Partner authority adapters](external/partneraccess/README.md), immutable
  [signup attribution](external/user/v2/README.md#optional-immutable-signup-attribution),
  an [active direct-membership read](external/group/README.md#active-direct-membership-capability)
  and explicit [capability administration](external/accesspolicy/README.md#explicit-capability-administration).
- [Authenticated revenue evidence](external/paymentprovider/README.md#authenticated-paid-revenue-evidence)
  for Stripe paid invoices, cumulative refunds and dispute references, with billing-owned
  checkout/payer identity, quarantine reconciliation and confirmed revenue history.
  Acceptance never submits a charge, refund or payout.
- [Billing lifecycle recovery](external/billinglifecycle/README.md) retains original
  checkout/status inputs, admits bounded discovery pages, fences execution and recovers
  receipts after uncertainty. [Composition helpers](external/billinglifecycle/helper/README.md)
  support explicit preparation and sequential worker passes; hosts enable and schedule them.
- [Principal-bound browser protection](external/http/browsersecurity/README.md) with
  an explicit CSRF cookie, exact origins, verified native admission and a bounded
  process-local limiter. It does not replace authentication or domain authorization.
- Audited [currency, phone and timezone catalogues](external/internationalisationmanager/README.md)
  with host-configured HTTP admission and explicit native setup; callable migrations
  register no host migration or administrator authority automatically.
- Stateless [OpenWA transport](external/teleprovider/README.md) for registration,
  direct/group messages and cursor-based reply reads. Hosts own consent, durable delivery
  and reply processing.
- [Email-provider composition](external/emailmanager/helper/README.md) for resolved
  named accounts and explicit local interception; [notification credential checks](external/notifier/helper/README.md)
  validate resolved VAPID/FCM inputs without enabling a channel.
- [OAuth provider builders and Apple key loading](external/oauth/helper/README.md),
  plus optional native callback configuration validation. **Breaking import change:**
  replace `examples/oauth.LoadAppleSigningKey` with `oauthhelper.LoadAppleSigningKey`
  from `external/oauth/helper`; its signature and key-source precedence are unchanged.
- [Stripe construction options](external/paymentprovider/helpers/README.md#provider-construction-options)
  for a trusted HTTP client, revenue configuration and promotion-code policy.
  Existing constructors retain their defaults; promotion-code entry defaults to disabled.
  [Paid-service-period parsing](external/paymentprovider/helpers/README.md#paid-subscription-service-periods)
  validates complete renewal invoices against explicit identity, mode and price expectations.
- [SPA description shells](external/spa/README.md#description-shells-from-a-build-inventory)
  selected from a validated build inventory, preserving ordered pathname matching and
  host fallback/asset policy. Missing inventories require explicit opt-in.
- [Response middleware composition](external/middleware/helper/README.md) and
  [Mongo topology configuration](external/repository/README.md) over explicit trusted
  inputs. Hosts retain cache bypass, process lifetime and deployment policy.

### Fixed

- [Retained revenue recovery](external/paymentprovider/README.md#versioned-source-fingerprints-and-retained-snapshot-recovery)
  validates economic history while tolerating receipt-URL changes. Original checkout
  status and lifecycle receipts recover without resubmitting provider work.
- [Withdrawal and payment recovery](external/partnermanager/README.md#withdrawal-admission-and-recovery)
  survives policy pauses, lost replies, changed current destinations and concurrent refunds.
  Original request keys recover their receipts without a second reservation or debit.
- [Share-link rotation](external/referral/README.md#share-links-and-evidence) is atomic
  and recoverable; [customer claim cancellation](external/partnerearnings/README.md#financial-evidence-and-recovery)
  preserves its original decision and financial history.
- Complete HTTP mappings for bounded membership and referral-evidence failures.

### Security

- Partner reads and worker stages recheck current caller, selected target and exact
  scoped grants. Human sessions cannot create instance-bound worker invocations;
  provider uncertainty is retained through later revocation or cancellation.

### Changed

- [Partner policy hold limits](external/partnerprogram/README.md) are configurable,
  defaulting to 30 days with an explicit maximum up to 365 days. New-policy
  validation stays separate from retained referral and financial evidence, so
  lowering a programme limit does not invalidate existing commissions.

## [0.4.0] - Unreleased

### Added

- [Purpose-aware email routing](external/emailmanager/README.md#purpose-routing-and-submission-receipts)
  with named accounts, ordered preferences, truthful submission receipts and explicit
  campaign support. Legacy constructors remain supported; failures never trigger
  provider failover. [Bird and Postmark adapters](external/emailprovider/README.md)
  support bounded transactional sends, HTML/text and Reply-To; local capture exposes
  provider/purpose attribution and can intercept the same routed accounts.
  SparkPost preserves plain-text bodies as well as HTML.
- An opt-in [prerelease waitlist](external/waitlist/README.md) with deterministic
  identities, configured consent, transactional enrollment sequences, private CSV
  projections and saved announcement variants. Durable claims prevent automatic
  retries of uncertain sends. Sequencing requires transaction-capable MongoDB.
  Signup preflight returns public 204 without JSON decoding or rate-limit consumption.
- A shared [voter service](external/voter/README.md), bounded
  [user-reference lookup](external/user/v2/README.md#batch-user-lookup), and private
  [contact conversations](external/contacter/README.md#conversations-and-email-integration-hooks)
  with append-only entries and deduplication. [User Manager routes](external/usermanager/README.md#private-conversation-voting)
  bind current administrator sessions, participant projections and optional voting;
  hosts explicitly install the relevant indexes and ports.
- Opt-in [display handles](external/user/v2/README.md#display-handles), including
  per-account-type generation, a unique index and revision-checked manual updates.
  The [self-service API](external/usermanager/README.md#self-service-display-handles)
  requires update ETags; no account backfill or settings UI is installed automatically.
- [System-scoped policy grants](external/accesspolicy/README.md) with transactional
  usage, revision-CAS audit, replay checks and explicit token-limit migration/rollback.
  [Policy-management routes](external/accesspolicymanager/README.md) and an optional
  [email-confirmed browser approval bridge](external/accesspolicy/adminaccess/README.md)
  keep current session and target authority separate from transport.
- Opt-in [transactional API-token admission](external/accessmanager/README.md#transactional-api-token-policy)
  using current policy limits and [owner-wide inventory fences](external/apitoken/README.md#transactional-inventory-setup).
  Initialize the inventory and provision reviewed grants before enabling issuance;
  all writers sharing that inventory must use the same fence.
- [Typed JWT claims](external/auth/README.md), explicit
  [session-context authentication](external/accessmanager/middleware/README.md#explicitly-selected-sessions)
  and configurable `/me` probe responses. Verified classification does not grant
  permissions; context publication never refreshes credentials or changes cookies.
- [Request-local proof admission](external/accessproof/README.md),
  [declarative route policies](external/router/README.md#declarative-route-policies),
  startup validation, defensive route inventories and reusable strong ETag parsing.
  Raw Mux routes remain outside the registry; hosts supply enforcing adapters.
- [Result-bearing Mongo operations](external/repository/README.md#transaction-safe-operations),
  managed-client transactions, explicit index setup, a transaction probe and atomic
  update/decode helpers. Custom domains retain schema, authorization and revision checks.
- [AES-256-GCM payload encryption](external/encryption/README.md) with explicit AAD
  and stable host keys, and opt-in [memory snapshots](external/ephemeral/README.md#process-local-transactional-snapshots)
  for local/test adapters. Memory snapshots do not replace production persistence.
- [Manifest-driven HTTP errors](external/errormanifest/README.md#wrapped-errors-at-http-boundaries)
  preserve mapped wrappers and validation joins while rejecting unknown independent
  causes. Authentication boundaries can select strict single-cause resolution.

### Changed

- **Breaking:** Feedback voting uses [shared vote storage](external/voter/README.md#upgrading-existing-vision-voting).
  Inject the voter service into `vision.NewService` and register its explicit
  index migration; custom Vision repositories no longer implement vote methods.
  Raw Vision models expose summaries instead of voter arrays; UMS feedback
  vote-count and viewer-vote response shapes remain unchanged. No embedded-vote
  backfill/fallback is provided. Votes no longer update parent edit metadata,
  and parent deletion does not cascade vote erasure.
- **Breaking Go API:** `usermanager.UserService` now requires `GetUsersByIDs`.
  Custom adapters must implement the [lookup contract](external/user/v2/README.md#batch-user-lookup)
  rather than a single listing page. Standard starter composition is already
  wired; existing Vision, group and notification response shapes remain
  unchanged, and conversation participants use a dedicated private projection.
  User Manager delegates batching and avoids listing counts for optional
  enrichment.
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
  Follow the [HTTP update contract](external/pricer/README.md#http-update-contract)
  for editable fields, stable cost IDs, array replacement and the existing
  payment-terms clearing limitation. The corrected draft example and
  [publication workflow](external/pricer/README.md#bind-save-and-publish-a-stripe-catalogue)
  distinguish provider binding, public catalogue state and webhook-derived
  paid access; these documentation clarifications do not change runtime behaviour.
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
- **Breaking:** token digests are no longer JSON fields. Creation returns the
  one-time secret; management reads omit it and no longer delete expired tokens.
  Explicit owner-authorized deletion frees inventory. Description/status filters
  now treat search text as a literal substring, not a regular expression.
- **Breaking:** JWT verification is pinned to HS256, requires expiry and rejects
  future issuance times; metadata extraction rejects empty subjects and record
  IDs. Review custom issuers and plan session rollover before enabling optional
  issuer/audience constraints. See [rollout guidance](external/auth/README.md#compatibility-and-rollout).
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
- **Breaking:** credential management now requires an active owner session;
  API-token-only clients must use a session. Activate/revoke requests require
  owner IDs, custom repositories need owner-bound status/deletion, and Mongo
  wrappers need result-bearing mutations. Custom API services must implement
  exact inventory counting; see [adapter migration](external/apitoken/README.md#repository-and-adapter-migration).
- Redis session lookups now distinguish missing records from operational
  failures. Use `ephemeral.ErrAuthNotFound` or `errors.Is`; legacy `redis.Nil`
  remains detectable through wrapping, but direct equality is no longer safe.
  See [lookup semantics](external/ephemeral/README.md#live-session-lookup).
- User ID/nano-ID lookups now preserve repository failures. Custom session
  adapters must distinguish expected absence from operational failures.
- API-token reads retain expired/revoked records and no longer delete during
  GET. Explicit deletion frees inventory slots. Clients must not depend on
  read-triggered cleanup; authentication still rejects expired credentials.
- Automatic repository logs now emit only fixed operation/outcome metadata;
  filters, documents, names and raw database errors are omitted even for custom
  loggers. Missing-document lookups use debug-level telemetry. Update log
  consumers for this privacy-oriented change; explicit application `Log*` calls
  and driver monitors still need their own redaction policy.
- **Breaking:** custom `ApitokenRespository` implementations must provide exact
  `GetAPITokenByDigest` lookup and atomic `TouchAPIToken` updates. Custom verifiers
  must return validated credential ID, stored owner ID, owner prefix and validity;
  last-used requests now require the verified `TokenID`. See
  [API-token adapter migration](external/apitoken/README.md#repository-and-adapter-migration).
- Route attachment through `starter/v0` now returns invalid registry
  configuration errors. Hosts must supply required middleware, configure policy
  evaluation before creating route groups and check startup errors. Direct route
  attachment must call `ValidateRoutePolicies()` before serving; see the
  [router guide](external/router/README.md#declarative-route-policies).
- Enabling JWT issuer/audience constraints rejects older credentials that do not
  satisfy them. Plan session rollover when opting in. Legacy sessions without
  the new user-type or purpose claims remain compatible where supported; missing
  values do not establish fresh authentication or broader permissions. Login
  and email-verification proofs require token_use.

### Fixed

- [FCM notification delivery](external/notifier/README.md#fcm-delivery-lifecycle)
  preserves custom data, deduplicates registration tokens, and splits requests
  at Firebase's 500-token limit. Permanently unregistered tokens are disabled;
  other delivery and cleanup failures remain observable without returning raw
  provider messages that can expose registration tokens. Web Push payloads bind
  `recipient_id` to the address owner for client account-change checks.

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
- Optional-authentication routes now admit rate-limited anonymous visitors when
  no placeholder user ID is configured. The shared context publisher accepts
  an explicit credential-free anonymous result without inventing an account;
  configured placeholders, authenticated identity checks and protected-route
  requirements retain their existing behavior.
- Reject invalid UTF-8 account types before signing so JSON encoding cannot
  silently change identity context.
- Domain and blueprint HTTP handlers now share a [manifest-driven reply writer](external/errormanifest/README.md#wrapped-errors-at-http-boundaries)
  that preserves wrapped and joined validation errors, including singleton
  joins, while retaining host overrides and existing success responses. Unknown
  independent joined causes no longer disappear behind a client error. Existing
  authentication boundaries retain their strict single-cause behavior.
- Declarative route authorizers report unknown, joined and ambiguous failures
  as `ROUTE_UNAVAILABLE` (503). Custom adapters must return or wrap `ErrRouteDenied`
  or a reviewed native manifest error for deliberate denials; see the
  [adapter contract](external/router/README.md#native-policy-error-manifests).
  The router shares the strict manifest resolver and reply writer, preserving
  mapped single-cause errors and preventing handler execution on any failure.
  Typed-nil failure nodes are rejected before adapter methods are called.
- Pin the reply integration upgrade for structural error resolution.
  This is an immutable, unreleased integration commit, not a tagged release.
  Reply uses request-local response state, safe opt-in unmapped diagnostics and
  deterministic error ordering; see
  its [migration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md).
- Access, User, Content and Billing Manager handlers now own and automatically
  include their dependency error maps. Existing bundle APIs delegate to those
  inventories and host overrides still win; omitted host wiring no longer turns
  known dependency failures into generic 500 responses.
- Complete missing group-setting, pricing-database and user-extension error
  entries; add stable sitemap error codes and the streak map to the standard
  User Manager bundle. Expected group validation now returns 400 and missing
  user extensions return 404 instead of generic 500 responses. Native contact
  query validation errors are translated to the mapped invalid-payload error.
- Standard auth and hardened-limit middleware now resolve wrapped manifest
  errors without exposing backend diagnostics. Counter failures deny requests
  without creating an IP ban unless the attempt threshold was actually reached.
- Preserve management-authorizer dependency/context failures instead of
  classifying them as explicit policy denials. Transport boundaries must still
  sanitize operational error details; management remains fail-closed.
- Count stored API-token inventory beyond the first page; keep list/count type
  filters consistent for legacy empty/null expiry fields. Reject negative or
  overflowing creation TTLs and format expiry from the creation clock instant.
- Populate each API-token human-readable timestamp independently; malformed
  dates no longer produce fabricated ages or overwrite unrelated display fields.
- Include API-token lifecycle errors in Access Manager's default reply manifest,
  preserving structured client errors even without a host-supplied bundle.
- Resolve ordinary wrapped domain errors in Access Manager responses through
  a shared [manifest resolver](external/errormanifest/README.md#wrapped-errors-at-http-boundaries).
  Unknown or ambiguous multi-cause failures use an opaque generic response;
  recognized client errors cannot hide an unknown infrastructure failure.
- Repository find/count/cursor failures retain native error causes underneath
  existing error codes, enabling retry-label and cancellation inspection.
  Single-result cursor mapping closes its cursor and distinguishes iteration
  errors from missing documents.
- API-token last-used updates modify only the timestamp on the exact active
  credential, removing the paginated lookup and full-record rewrite that could
  overwrite a concurrent revocation. Deleted tokens are not recreated.

### Deprecated

- API-token repository whole-record updates and unowned deletion remain trusted
  administrative APIs only. Prefer owner-bound status/delete and field-only
  usage methods to avoid stale authority overwrites.

### Security

- [Initial sign-in email delivery](external/accessmanager/README.md#initial-email-delivery)
  now validates adapter receipts and wiring, preserves request context and uses
  no-store replies without raw diagnostic logging at the manager boundary.
  The existing uniform 202 blank envelope is retained; delivery is not confirmed.
  Malformed email input is now explicitly validated. **Breaking for custom
  callers:** email lookups preserve native operational failures instead of
  coercing them to absence/generic errors and return detached hydrated models.
  Invalid lookup receipts fail closed. Legacy cooldown ownership, non-atomic
  code reservations and lower mail-provider handling remain separate work.
- **Breaking for custom wiring:** [Administrative role changes](external/usermanager/README.md#administrative-account-roles)
  now use live manager authorization, actor-bound audit and narrow conditional
  repository writes. Custom handlers require a role manager and repositories
  require the documented role capability. Removal clears every duplicate role;
  invalid configured additions return 400 instead of silent success. Confirmed
  no-ops preserve timestamps and emit no mutation audit. Unrelated fields survive;
  conflicting snapshots return 409 and unconfirmed receipts 503 through native
  manifests. Existing HTTP shapes remain unchanged, with context and no-store.
  Trusted domain callers own audit. No automatic retry or revocation transaction
  is implied.
- **Breaking for custom wiring:** [Administrative status changes](external/usermanager/README.md#administrative-account-status)
  now pass through a manager that rechecks live administrator-session authority
  and records actor-bound audit. Custom handlers must install the manager and
  verifier; repositories must implement narrow conditional status writes. Status
  rules and EMAIL_CHANGE side effects are preserved without broad snapshots;
  security or owned-verification races return 409, unconfirmed receipts 503.
  Single and bulk HTTP routes retain their 200 data shapes with contextual
  no-store replies. Bulk rechecks authority per item and retains partial results.
  Trusted domain calls no longer emit unattributed audit. These operations are
  not transactions with administrator revocation or audit delivery.
- **Breaking for custom adapters:** [Proof login](external/accessmanager/README.md#conditional-login-account-transitions)
  now uses narrow conditional account commands before minting a session from the
  acknowledged post-image and its fresh-login timestamp. ACTIVE logins update only
  login metadata; PROVISIONED activation compares security state and merges only
  its owned fields. Custom user/repository adapters must implement both narrow
  capabilities; broad snapshot fallback is removed. Native conflicts map to 409,
  unconfirmed receipts to 503; both require a fresh sign-in after proof consumption.
  Success contracts remain unchanged, with contextual no-store replies. This is
  not a transaction with proof/session storage or session-family revocation.
- **Breaking for custom adapters:** [Legacy user updates](external/user/v2/README.md#legacy-broad-updates)
  now require acknowledged Mongo post-images through the shared repository helper,
  and configured clock/string utilities. Native validation and persistence errors
  retain their error trees; invalid receipts fail with 503. URL targets cannot be
  overwritten by body IDs, conflicting in-process selectors are rejected, and
  caller-owned models are isolated before mutation. The existing snapshot conflict
  has a domain-level 409 mapping; replies carry context and are no-store.
  Broad snapshot writes remain broad: general field CAS is not a guarantee of
  the legacy command; migrate callers to the operation-specific commands.
- **Breaking for custom adapters:** [Self-service profile names](external/usermanager/README.md#self-service-profile-names)
  now require the narrow user-domain/profile repository capability instead of a
  broad user snapshot write. Managers require verified session or API context,
  recheck live ACTIVE state and retain native errors for the shared reply map.
  Conditional Mongo writes preserve unrelated fields and return acknowledged
  post-images; stale names/account state return 409 and missing capabilities or
  invalid receipts return 503. Existing 200/no-op behavior remains, responses are
  no-store and model validation no longer prints private account values.
  Legacy generic writers and client-revision/ABA protection are not changed.
- **Breaking:** [Logout commands](external/accessmanager/README.md#logout-command-boundaries)
  now separate typed transport from verified deletion authority. Migrate
  LogoutUser to LogoutUserRequest and replace RemoveRefreshTokenWithCookieValue
  with its refresh-only command. Custom auth adapters implement the narrow
  deletion-only verifier; expired credentials remain unusable for admission.
  Other-session cleanup requires explicit ActorID and self-service UserID
  (formerly UserId), verified session context and matching live records/account.
  Blank 200/202 success contracts remain; native failures are no longer hidden
  behind success, handler responses are no-store and refreshed downstream
  cookies match the new bearer. Record deletion validates namespaces/receipts
  and treats confirmed absence idempotently. Neither command provides atomic
  session-family revocation or fences concurrent rotation/login.
- **Breaking:** [API-token management commands](external/accessmanager/README.md#session-bound-management-commands)
  now require an explicit session-bound ActorID and matching live ACTIVE owner,
  including in-process callers. Rename threshold request UserId to UserID and
  preserve verified session context in custom adapters and transaction callbacks.
  Forged selectors and API-token-only callers cannot manage credentials. List
  rows are owner-checked and copied without secrets; native errors retain shared
  reply mappings. Existing 201/200/202 success contracts are unchanged and the
  six handlers send no-store responses. Account checks are point-in-time, not
  locks against concurrent revocation; logout and legacy-tier retirement remain
  separate work.
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
- **Breaking:** [Email changes](external/accessmanager/README.md#conditional-email-changes)
  now use explicit actor/target commands and return confirmed-change receipts
  with separate cleanup, delivery and audit flags. The user domain atomically
  guards the prior account state, advances its email revision and clears
  verification; generic profile updates can no longer change the mailbox.
  Configure the documented unique index, database privileges and custom adapter
  capability before rollout. Old session/proof revisions are rejected even when
  target-only Redis cleanup fails. Uncertain writes are not automatically retried;
  native errors retain shared manifest mapping without private diagnostics.
- Self-service notification feeds now bind recipient identity and email to the
  authenticated caller. Explicit recipient selection remains on administrator
  routes and requires a matching live ACTIVE administrator account.
  **Breaking:** in-process notification-overview commands must set `AdminView`
  to select another recipient; the default is self-service. See
  [notification recipient boundaries](external/usermanager/README.md#notification-recipient-boundaries).
- Session-store absence decisions now inspect every wrapped cause, preserving
  joined storage failures instead of treating them as missing sessions. Native
  and legacy Redis absence remain supported; typed-nil, cyclic, oversized and
  custom `Is`-only errors fail closed. Use
  [`ephemeral.IsAuthNotFound`](external/ephemeral/README.md#live-session-lookup)
  for control flow; one `errors.Is` match is not proof of pure absence.
- **Breaking:** [SEO](external/seo/README.md) and
  [communications](external/contacter/README.md) routes now participate in the
  shared registry. Omitted administrator middleware no longer exposes admin
  endpoints. Supply the enforcing adapter, select `AdminSessionOrAPI` explicitly
  through `AdminAccess` when appropriate, and reject startup if
  `ValidateRoutePolicies()` fails. Invalid configuration closes every descriptor
  in the router with 503; raw routes remain outside this backstop. Public discovery
  and sitemap access remain public under valid configuration.
- Cookie middleware refreshes only known absent/expired access credentials.
  Account denials and dependency failures neither clear cookies nor downgrade
  authenticated attempts to public access. Refresh/retry and explicit refresh
  failures preserve operational causes; rotation wait timeout now returns `503`
  (`AM00-040`). Missing verification wiring returns `503` (`AM00-039`). Custom
  adapters must wrap known credential sentinels; matching error text or overriding
  an HTTP status does not select lifecycle behaviour.
  Existing cookie-pair selection remains compatible; atomic token-family logout
  is not supplied by this change. See [cookie timing](external/accessmanager/middleware/README.md#refresh-cookie-timing).
- Active/admin session guards now verify the live session owner and current
  account identity before checking current status/roles. Signed administrator
  flags cannot outlive demotion. Custom session adapters must preserve operational
  failures and return consistent live identities. Cookie refresh, optional-public fallback and
  access-only logout remain separate compatibility boundaries; see
  [live session authority](external/accessmanager/README.md#live-session-authority).
- Bind API-token deletion and activation/revocation to both owner and token ID
  inside the database operation, preventing cross-owner mutations by guessed IDs.
- Generate new API secrets from 32 cryptographically random bytes instead of a
  shared pseudo-random generator. Existing stored credentials are not automatically
  revoked; operators should assess rotation of older secrets separately.
- Verify API tokens through exact prefix/digest lookup with active-status,
  expiry and owner checks. Reject malformed or ambiguous API-token headers without
  logging credential fragments, and keep verified API identity separate from JWT
  session metadata.
- Pin JWT verification to HS256, require expiry and distinguish new session tokens
  from login/email-verification proofs. Session verification checks live stored
  identity, email revision and any signed user type; inconsistent authenticated
  context is rejected rather than published.
