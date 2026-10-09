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

## [0.5.0] - Unreleased

### Added

- Optional [Partners integration helpers](external/partnermanager/helper/README.md)
  compose explicit signup cookies, selected-account admission and worker execution
  over existing owners. [Live identity adapters](external/partneraccess/README.md)
  accept host admission, account type and status configuration. Separate
  [billing evidence](external/billingmanager/helper/README.md) and
  [lifecycle composition](external/billinglifecycle/helper/README.md) helpers retain
  native scope/receipt contracts; hosts keep scheduling and resource lifetime.

- Explicit [native lifecycle preparation](external/billinglifecycle/helper/README.md#explicit-native-preparation)
  composes encrypted stores and a bounded owning sweep after current preparation
  authority checks. [Retained Stripe snapshot input](external/paymentprovider/helpers/README.md#retained-refund-snapshot-files)
  screens private bounded files without changing original bytes or replacing
  owning financial proof. Neither helper starts workers or creates grants.

- Optional [Partners HTTP transport](external/partnermanager/http/README.md) with
  independently mounted member/customer/operator routes, current scoped authority,
  safe projections and host-configurable browser protection. Financial recovery
  stays with its existing owners; hosts adopt routes and code checks atomically.

- Add opt-in [browser transport protection](external/http/browsersecurity/README.md)
  with opaque host-resolved principal bindings, signed CSRF cookies, explicit
  verified native audience admission and a bounded process-local rate limiter.
  Hosts choose cookie names and retain session verification, action authority,
  response projection and distributed admission. Ambiguous fetch-site headers
  suppress CSRF issuance; native mode rejects even empty or malformed Cookie
  headers. Configuration rejects empty queries/hostnames, unsafe local cookie
  prefixes and invalid cookie names. Existing wire framing can be retained with
  identical keys, names and binding order; no storage conversion is required.

- Add opt-in [Partners authority](external/partneraccess/README.md),
  [billing lifecycle recovery](external/billinglifecycle/README.md),
  [referral route helpers](external/partnermanager/helper/README.md) and
  [native runtime composition](external/partnermanager/runtime/README.md).
  Hosts supply their own identity/session admission, explicit configuration,
  branded consent renderer and worker lifetime. Construction creates no grants
  or workers; native index preparation and transaction readiness are explicit.


- Explicit [capability administration](external/accesspolicy/README.md#explicit-capability-administration)
  reviews disabled or expired user policies and replaces named scopes,
  permissions, activation and expiry under current management authority and
  audited revision checks. Existing token allowances and usage budgets remain
  unchanged; first provisioning grants neither. Separately opted-in manager
  routes require an explicit live administrator session, a selected stored user,
  bounded fields and a reviewed ETag. Host attachment and operational approval
  remain separate integration work.

- Optional [private retained status resolution](external/billing/README.md#private-original-status-resolution)
  distinguishes an exact captured receipt, pending original and conclusively
  uncaptured superseded preparation from one native snapshot. Current global
  and selected manager permission applies to every outcome; joined failures
  never authorize replacement. Existing ports/receipt identities remain
  compatible. Durable host disposition and runtime adoption remain integration
  work; resolution performs no provider lookup or financial mutation.


- Private [retained status preparation validation](external/billingmanager/README.md#current-subscription-status)
  rechecks the current actor independently of the original author for payment
  and checkout provenance. Selected permission precedes owning validation and
  applies after errors; uncertain commits retain their recovery cause after
  denial or cancellation. Existing checkout preparation/read and payment-only
  adapters remain supported. Durable host collection remains integration work.

- Optional [private checkout lifecycle completion stages](external/billingmanager/README.md#private-acknowledged-checkout-completion-stages)
  reuse the same configured association owner and require current selected
  refresh authority before lookup, capture, replay and disclosure, including
  errors and absence. Separate stages support retaining original inputs between
  operations; paid-capture configuration is independent. Durable host recovery
  and recurring collection remain explicit integration work.

- Private [acknowledged-checkout preparation and receipt recovery](external/billing/README.md#private-acknowledged-checkout-preparation-and-receipt-recovery)
  joins original intent, acknowledgement and reverse-session ownership before
  provider lookup, and retains original first-anchor dependencies on receipt
  recovery. Missing provenance is unavailable rather than a fresh authorization;
  existing receipt formats remain compatible. Current manager authority and
  durable host original-evidence recovery remain integration work.

- Optional [private lifecycle discovery authority](external/billingmanager/README.md#private-lifecycle-source-discovery)
  derives sources from the same billing owner, checks current scope and selected
  payer/source permission, and withholds whole pages and cursors after denial or
  cancellation. Empty/error results also recheck scope authority. Billing owns
  canonical page validation; host service identity/grants, migration orchestration
  and recurring collection remain explicit integration work.

- Optional [bounded lifecycle preparation](external/billing/README.md#explicit-bounded-preparation)
  reconstructs source projections from original billing history with durable
  page progress, canonical evidence validation and atomic scope readiness.
  Current source writes fence completion; changed history restarts the sweep.
  Older writer instances must be drained first. Original financial history and
  customer-less legacy payments stay unchanged; host upgrade orchestration,
  collector adoption and deployment qualification remain separate work.

- Gated [bounded lifecycle discovery reads](external/billing/README.md#bounded-lifecycle-source-discovery)
  join acknowledged checkouts and scoped payment/lifecycle sources in one native
  snapshot, validate original provenance and withhold partial pages on failure.
  Scope-bound cursors do not grant permission or certify provider coverage.
  Explicit owning preparation remains required; manager/collector adoption is
  separate integration work. Missing preparation is unavailable rather than empty.

- [Native lifecycle source records](external/billing/revenuestore/README.md#native-lifecycle-source-records)
  atomically retain acknowledged subscription checkouts and immutable scoped
  payer/customer ownership across paid and pre-payment sources. Renewals preserve
  original source pointers; legacy facts without customer evidence stay financial
  only. Native history coverage requires explicit preparation; host upgrade
  orchestration and recurring collection remain integration work. These records
  create no status or commission.

- [Checkout-backed subscription status](external/billing/README.md#scoped-current-subscription-status)
  records pre-payment trial/lifecycle evidence under immutable checkout ownership
  and the same subscription revision used by paid status. Existing payment receipt
  identities remain compatible; current manager permission applies to both
  sources. Status alone creates no paid conversion or commission, and durable
  host collection/reporting still requires integration.

- Optional [pre-payment checkout lifecycle ownership](external/billing/README.md#pre-payment-checkout-lifecycle-ownership)
  validates acknowledged sessions against frozen intent and original price
  evidence, retaining encrypted immutable anchors and exact capture receipts.
  Lifecycle and paid checkout ownership cannot disagree. These anchors create
  neither paid revenue nor commissions; status collection and host scheduling
  remain separate integration work.

- [Checkout session evidence](external/paymentprovider/README.md#optional-checkout-evidence-for-revenue-identity)
  can be retrieved by a retained session ID before a subscription's first
  charge. The optional provider capability authenticates merchant scope and
  complete original lines; billing must still match its frozen authorization.
  This is lifecycle evidence, not paid revenue or an enabled status collector.

- Selected [operator preparation reads](external/partnermanager/README.md#withdrawal-admission-and-recovery)
  provide status revisions under partner policy authority, current withdrawal
  admission/destination/funds under on-behalf claim authority, and individual
  policy history with its publication revision under customer policy authority.
  Current permissions remain separate for each selected target; pauses preserve
  reads and original financial retries remain independent of preparation.

- [Withdrawal admission](external/partnermanager/README.md#withdrawal-admission-and-recovery)
  supports explicit minimums and customer-selected destination revisions while
  preserving authorized original-receipt recovery after admission or destination
  changes. Payment preparation reads use the separate recording capability;
  financial settlement and transfer execution remain separate responsibilities.

- An optional [active direct group membership read](external/group/README.md#active-direct-membership-capability)
  excludes pending invitations, inactive groups and inherited administrator
  access. Bounded native reference probes refuse truncation and retain failures;
  existing unbounded reads remain compatible. Membership alone confers no
  financial cohort or rate approval; hosts enforce that separately.

- Opt-in [paid-referral evidence](external/partnermanager/README.md#paid-referral-source-and-status-evidence)
  joins complete confirmed billing history and immutable attribution into safe
  visible-page customer/admin summaries and complete partner paid metrics from
  one owning relationship/binding snapshot, independently of list pagination.
  Distinct eligible paid and net-positive
  relationships remain separate from renewal allocation rows, commission and
  current funds. Fresh current status deduplicates scoped subscriptions, keeps
  trialing separate and withholds exact active totals when status is unknown.
  Complete source budgets, correction checks and independent revisions prevent
  partial reports and false delivery-completeness claims. Optional retained
  status reads are bounded; deferred candidates remain explicitly unknown.
  Original-payment cohorts never become signup/visit conversion denominators.
  Host adoption and durable refresh remain explicit integration work.

- Optional [original-cohort paid conversions](external/partnermanager/README.md#original-cohort-paid-conversions)
  join one complete traffic/ownership/binding snapshot and accepted billing
  history. Original signup and visit dates stay separate from payment dates;
  measured/unmeasured signups, manual initial acquisitions and correction events
  have matching denominators. Renewals and shared visit origins deduplicate;
  refunds preserve historical conversion with separate net-positive counts.
  Global totals precede link/administrator plan pages, private overlapping plan
  rows stay out of customer JSON, and missing origins withhold visit rates.
  Combined evidence and current authority are rechecked; provider delivery
  completeness and host UI adoption remain explicit limitations.

- Optional [scoped subscription lifecycle evidence](external/billing/README.md#scoped-current-subscription-status)
  uses authenticated provider reads, immutable billing payer binding and atomic
  encrypted head/receipt revision checks. Current scoped permission, original
  uncertain-save recovery and explicit freshness avoid false inactive results.
  Active status creates no financial entitlement; durable host refresh
  scheduling remains integration work.

- Optional [worker backlog reporting](external/partnermanager/README.md#worker-backlog-reporting)
  separates ready, unattempted delayed, attempted backoff and leased discovered jobs with attempted and
  decision subsets under dedicated program-scoped operations permission.
  Complete pending snapshots have explicit combined capacity and privacy
  boundaries; an empty queue does not certify source or maturity completeness.

- Optional [durable maturity work](external/partnermanager/README.md#owning-signup-revenue-and-maturity-worker)
  preserves original accrual deadlines across discovery, restarts and leases.
  Dedicated current maturity authority and the same scoped earnings owner
  recover actual financial receipts after lost acknowledgements; an old queue
  decision cannot manufacture acceptance. Hosts own scheduling and readiness.

- Opt-in [consented visit measurement](external/referral/README.md#consented-visit-measurement)
  uses separate purpose-bound signed cookies, keyed link-scoped digests and
  atomic first-visit deduplication. Optional analytics failures preserve signup
  evidence. Explicit approved retention expires raw observations/receipts through
  the prepared [record store](external/repository/recordstore/README.md#explicit-ephemeral-record-expiration),
  while financial/signup/ownership records remain persistent.

- [Referral analytics](external/referral/README.md#referral-analytics) derive
  distinct measured-visit conversions, independent signup periods and retained
  relationship counts from one complete bounded owning snapshot. Link pages
  preserve global totals. Anonymous UTC-day counts and signed original visit times
  preserve historical reporting after raw cleanup; incomplete sub-day boundaries
  return a granularity error. Checked counts, missing origins and capacity remain
  explicit.

- Required private [maturity source evidence](external/partnerearnings/README.md#durable-maturity-source-evidence)
  commits with accrual and maturity, retaining original deadlines and accepted
  journal receipts without expiry. Bounded scoped discovery verifies owning
  financial snapshots and supports lost-acknowledgement recovery. Host worker
  scheduling and source reconciliation remain explicit integration requirements.

- Opt-in [financial metrics](external/partnerearnings/README.md#financial-metrics)
  separate current original-payment cohorts, economic-date commission movements
  and unfiltered balances/claim exposure and overdue maturity. Due ledger rows
  include zero/refunded credit; net pending and overlapping holds remain separate
  from claim availability. Frozen billing-plan provenance supports
  bounded breakdown pages with complete selected totals. Authorized customer
  summaries omit billing identifiers/revenue; source/subscription completeness
  remains a separate owning integration.

- Opt-in grouped retained-referral commission summaries with exact original
  payout/return portions, independent snapshot revisions, safe page-scoped
  customer projections and explicit missing-acceptance/coverage states. See
  [partner reporting](external/partnermanager/README.md) for authorization and
  cross-domain snapshot limits.

- [Payment cohort reports](external/partnerearnings/README.md#payment-cohort-reports)
  attribute split manual payouts, returned backing and subsequent reclaims to
  immutable original payment/referral portions. Bounded pages retain complete
  cohort totals and separate current global balances. Scoped manager reads
  recheck permission; hosts still project safe customer/admin reporting views.

- [Retained partner relationships](external/referral/README.md#retained-partner-relationships)
  preserve former owners' referral visibility through prospective correction
  and reacquisition. Atomic lifetime membership and snapshot history reads
  support bounded pagination; customer manager projections omit private
  customer, replacement-owner and acquisition identifiers. Returned payout
  backing uses durable typed operation links and validates conserved portions,
  independently of display notes. Host reporting integration remains explicit.

- [Partner statements](external/partnerearnings/README.md#statements) provide
  bounded, filtered journal pages with running matured amounts and current
  balances from one owning snapshot. Revisions include claim changes, and paid
  rows present retained manual-record amendments without another debit.
  Customer and operator manager reads recheck current scoped permission;
  hosts must project permitted fields. Snapshot reads still grow with history.

- [Partner claims](external/partnermanager/README.md) support separately scoped,
  reasoned administrator creation on behalf of an eligible owner, using the
  owner's versioned destination. Immutable
  [financial request receipts](external/partnerearnings/README.md) recover an
  existing claim before new-admission pauses or mutable destination lookup.

- [Referral payment binding](external/referral/README.md) selects and freezes
  ownership in the same customer-scoped transaction boundary as attribution
  correction. Historical bindings remain immutable, and recorded cutover time
  cannot precede the previous ownership revision. Scoped operator previews and
  prospective corrections use reviewed source/snapshot preconditions and
  immutable actor/customer/key receipts. Lost-response recovery preserves the
  original terms through later policy/owner changes and admission pauses;
  existing bindings and paid earnings remain unchanged. Recurring windows stay
  anchored to signup and retain earlier frozen ends. Host routes/UI and
  historical compensation are separate from this owning workflow.

- An optional [durable partner worker](external/partnermanager/README.md)
  consumes owning signup, verified revenue and quarantined source feeds with
  atomic discovery pages, fenced leases and independent retry schedules. It
  records durable decisions before owning acknowledgements and recovers lost
  replies under current scoped authority. Only conclusive owning evidence can
  establish no entitlement; outages and unresolved evidence remain pending.
  Hosts must explicitly supply approved composition, authority and scheduling.

- Optional [immutable checkout billing identity](external/billing/README.md)
  saves authorized request parameters before submission, atomically binds
  authenticated session/subscription history, and preserves payer/plan/cost for
  renewals. [Billing manager](external/billingmanager/README.md) requires current
  owning-account authority on captured checkout retries and retrieves known
  sessions instead of repeating POSTs after provider key retention. Legacy and
  unmapped portal changes remain unresolved pending reviewed history recovery.

- [Partner earnings](external/partnerearnings/README.md) now records dispute
  hold/won/lost evidence, nonsettling partial/unknown/mismatched manual payment
  observations and capped returned-transfer adjustments with original backing.
  Revision-bound receipts, assigned-operator recording, strict unavailable-vs-
  absent handling and canonical destination snapshots guard financial replay.
  The package remains opt-in and is not automatically wired into host startup.

- A [partner-manager identity adapter](external/partnermanager/README.md) for
  current owning account eligibility and private immutable new-signup capture,
  with explicit account-type/status/region rules and typed-nil capability checks.
  Hosts must still install durable handoff workers and scoped transport.

- Optional [verified-revenue sweep paging](external/billing/revenuestore/README.md)
  reaches later pending facts and quarantined sources without acknowledging or
  resolving earlier failures; host attempt scheduling remains explicit.

- Add opt-in [authenticated Stripe paid-invoice, cumulative refund and dispute-reference evidence](external/paymentprovider/README.md#authenticated-paid-revenue-evidence), with explicit merchant/mode/currency configuration and complete fiscal allocation checks. Refund objects without a mode field remain bound to the authenticated original charge/payment; contradictory supplied modes are rejected. Ambiguous shapes remain quarantined; provider outages remain retryable. No provider charge, refund or payout is submitted.

- Add optional billing-manager financial reception through owning historical association and verified-feed ports, plus scoped authenticated source reconciliation retaining immutable quarantine history and committing recovered facts/resolution atomically. Host composition, workers and deployment remain separate adoption work.

- A [partner-earnings ledger](external/partnerearnings/README.md) owning the
  single financial ledger for a partner program: commission accrual and maturity,
  cumulative refund reversals, payout claims with oldest-first reservation,
  operator claim decisions and the sole privileged manual-payment debit, each
  guarded by an actor/use-case idempotency receipt. The service computes all
  rules itself and never converts currencies; it exposes a driver-free
  `Repository` port and requires host-owned write-guard wiring and index
  migrations. It is not yet wired into host startup or partner-manager routes.

- Explicit [partner program](external/partnerprogram/README.md) enrollment,
  immutable policy publication and versioned destinations, plus
  [referral](external/referral/README.md) share links, signed signup evidence and
  frozen economic-payment ownership. Opt-in
  [encrypted persistence adapters](external/partnerstore/README.md) require a
  transaction-capable store, additive index preparation and stable keys; host
  composition, authority and commercial launch approval remain explicit.

- [Selected operator claim reads](external/partnermanager/README.md#withdrawal-admission-and-recovery)
  use the same explicit processing, recording, amendment or return permission as
  the selected action, without requiring queue or unrelated recording authority.
  Hosts still bind current identity and project permitted fields; these reads
  perform no financial write and grant no broader access.

- An owning billing verified-revenue contract and
  [durable encrypted feed](external/billing/revenuestore/README.md) with economic
  deduplication, atomic delivery acceptance and independent consumer receipts.
  Provider allocation evidence and host workers remain separate; invoice totals
  and access grants are not accepted as revenue facts.

- Optional [immutable signup creation evidence](external/user/v2/README.md#optional-immutable-signup-attribution)
  and [browser cookie transport](external/accessmanager/README.md#optional-browser-signup-evidence).
  Evidence commits with the new account, survives profile edits and exposes an
  owning-service pending feed. The host opts in with explicit account types,
  cookie configuration, signing policy and an additive feed index; no historical
  attribution backfill or automatic commission entitlement is supplied.

### Fixed

- Complete safe HTTP error mappings for bounded direct-membership reads and
  immutable signup evidence, including unavailable, capacity and conflict outcomes.


- [Confirmed payment revenue history](external/billing/README.md#confirmed-payment-revenue-history)
  accepts authenticated quarantine resolutions with stable recovery fingerprints
  while preserving legacy resolution hashes. Altered recovery evidence stays
  rejected; original source receipts and financial history remain unchanged.

- Optional [original checkout status recovery](external/billingmanager/README.md#original-checkout-status-recovery)
  verifies the authenticated caller's retained session and frozen terms through
  fresh provider reads. It distinguishes paid, trial/no-payment, pending, unpaid
  and expired outcomes without creating another checkout or using existing
  access as proof of payment. Native encrypted ownership joins need no new index;
  missing evidence, revocation and cancellation withhold all output.

- [Private checkout lifecycle stages](external/billingmanager/README.md#private-acknowledged-checkout-completion-stages)
  preserve an observed uncertain owning result through later permission failure
  or cancellation while withholding all output. Recovery still requires the
  exact retained original input and current caller authority.

- [Stripe revenue recovery](external/paymentprovider/README.md#versioned-source-fingerprints-and-retained-snapshot-recovery)
  uses versioned source fingerprints that exclude only a refunded charge's
  rendered receipt URL. Unchanged legacy snapshots retain exact replay support;
  differing legacy representations require explicitly validated original
  snapshot evidence and matching authenticated event retrieval. Immutable
  resolutions retain a private stable fingerprint without rewriting the
  original quarantine, and verified redelivery cannot duplicate economics.

- Pre-payment checkout lifecycle lookup now stops after snapshot cancellation
  and withholds evidence after provider-read cancellation. Confirmed capture
  receipt recovery semantics remain unchanged.

- [Partner manual-payment handling](external/partnermanager/README.md#withdrawal-admission-and-recovery)
  pauses new or resumed claim processing while preserving authorized recording
  and original-receipt recovery of transfers already attempted. The existing
  `Controls.ManualRecording` switch now gates entering `processing`; it does not
  suppress financial attestation, review, amendments or returned transfers.
  Hosts must refresh their pause guidance; native assignment, revision and
  record-once settlement checks remain in force.

- [Partner payment observations](external/partnerearnings/README.md#commands)
  retain later partial or mismatched evidence after an unknown attempt without
  colliding with the earlier journal source. Original receipts and review-held
  funds remain intact; existing financial history is not rewritten.

- [Partner share-link rotation](external/referral/README.md#share-links-and-evidence)
  atomically replaces the selected original link and retains its actor/key
  receipt. Retries recover the originally issued result after uncertainty,
  later rotations or acquisition pauses, without invalidating a newer link.
  Current permission is still checked before every attempt and result disclosure.

- [Partner customer cancellation](external/partnerearnings/README.md#financial-evidence-and-recovery)
  retains an original-key receipt with the requested-only reservation release
  and terminal claim audit. Lost-response retries recover the completed result;
  changed intent and in-flight claims cannot release funds again. The manager
  rechecks current owner permission before mutation and result disclosure, while
  financial receipt reads reject mutable or expiring storage metadata.

### Security

- [Partner manager reads](external/partnermanager/README.md) validate selected
  ownership and currency, discard private partial results on failure, and
  recheck scoped customer/operator permission before returning data.

## [0.4.0] - Unreleased

### Added

- [Purpose-aware email routing](external/emailmanager/README.md#purpose-routing-and-submission-receipts)
  with named provider accounts, ordered mail-type preferences, round-robin ties,
  explicit transactional defaults, separate campaign capability and truthful
  skipped/captured/accepted/failed/uncertain submission receipts. Legacy single-
  provider constructors and error-only methods remain supported; routed generic
  sends require an explicit trusted purpose. No failure triggers provider failover.
  Provider compatibility adapters check the selected account readiness without
  advancing selection turns.
- A bounded [Postmark inline adapter](external/emailprovider/README.md#postmark-inline-email)
  for transactional and explicitly configured broadcast streams, preserving host
  instrumentation, HTML/plain text and Reply-To without automatic retries.
- Provider instance, vendor and purpose attribution on local inbox list/API/detail
  views. Local capture can intercept all routed providers into the same inbox.
  SparkPost now preserves plain-text bodies as well as HTML.

- [Waitlist host extensions](external/waitlist/README.md#host-policy-and-presentation)
  for configured consent, optional transactional enrollment sequences, CSV
  projections and persisted custom announcement data with recipient-specific
  rendering and preview variants. Sequenced signup requires a Mongo replica set
  or transaction-capable deployment; existing rows are not backfilled. Signup
  preflight now returns public 204 without invoking JSON decoding or rate limiting.

- A [user-domain batch lookup](external/user/v2/README.md#batch-user-lookup)
  resolves references in bounded, count-free repository queries, with exact
  returned identities, detached models, cancellation and native partial errors.
  Consumers retain their own authorization and field-level projections.
- A reusable lower-domain [voter service and repository](external/voter/README.md)
  stores atomic actor-target votes with shared Mongo helpers, explicit unique
  indexes and bounded count/viewer projections. Consumers retain permissions
  and target validation; no generic voting HTTP surface is added.
- An opt-in [prerelease waitlist](external/waitlist/README.md) with framework-owned
  deterministic signup identities, explicit consent, private CSV export and one
  previewed announcement. Host branding is independent of identity; durable
  claims prevent automatic retries of uncertain sends. Local capture remains
  explicit, and provider acceptance is not delivery confirmation.
- User Manager provides optional [conversation ownership and voting](external/usermanager/README.md#private-conversation-voting)
  through native admin-session routes, with live-authority
  checks and owner preconditions rejecting stale-account mutations. The contact
  service validates targets and delegates to the shared voter; UMS resolves
  user references and exposes a private email/role-free participant projection.
  Writes return an empty participant array. Hosts explicitly enable voting
  routes and apply the shared vote and conversation paging indexes.
- An opt-in [browser token-allowance approval bridge](external/accesspolicy/adminaccess/README.md)
  binds exact-origin cookie sessions to one reviewed, email-confirmed policy
  update. Proofs are short-lived and consumed before dispatch; the bearer-only
  API remains unchanged. This does not grant general permissions or provide MFA.
- A [Bird transactional email provider](external/emailprovider/README.md#bird-transactional-email)
  with regional Bearer authentication, inline HTML/text and Reply-To mapping,
  bounded requests/responses, redirect suppression and host HTTP/telemetry
  injection. Receipts distinguish API acceptance from delivery; sends are not
  automatically retried. Existing SparkPost and local capture providers are
  unchanged. Sender readiness and live delivery require separate host validation.
- [Private contact conversations](external/contacter/README.md#conversations-and-email-integration-hooks)
  support attributed, append-only administrator notes and recorded replies,
  bounded keyset history and atomic request/provider-message deduplication.
  Admin-session routes recheck live authority; trusted in-process email hooks
  preserve threading metadata without connecting a provider or sending mail.
  Existing contacts remain legacy snapshots and statistics are unchanged; the
  paging index is an explicit migration. Public creation keeps its direct 201
  receipt while explicitly excluding administrative notes, replies and links.
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
- [Native route error manifests](external/router/README.md#native-policy-error-manifests)
  extend router defaults with copied startup maps and ordered host overrides.
  Mapped native errors do not need conversion to router sentinels; supplied
  responses must be 4xx/5xx. Joined or unknown policy failures remain opaque 503s.
- Configurable [session-probe responses](external/accessmanager/middleware/README.md#session-probe-response-policy)
  through middleware and starter composition. Preserve the default `/me` 202
  error envelope, or explicitly select an empty 202 or structured 401 for a
  missing session. The private probe now sends no-store and noindex headers;
  other authentication failures and endpoints retain their existing contracts.
  An inconsistent identity returned as a successful verification is now a 503
  verification failure, not a missing-session response.
- [Explicit session-context authentication](external/accessmanager/middleware/README.md#explicitly-selected-sessions)
  for hosts with custom credential selection. It reuses live Access Manager
  verification, rejects incomplete or mixed credential results and clears
  inherited identity on failure while preserving diagnostic causes. Context
  publication also clears stale bearer-only transport markers. Verified
  claim metadata and separate session/API-credential context helpers are reusable
  without a second JWT parse or exposing raw credentials. Custom transports use
  `AuthenticateSession` and `ContextWithAuthentication`; account/resource policy
  remains explicit, and these helpers perform no refresh or cookie changes.
- Opt-in [request-local proof admission](external/accessproof/README.md) for
  exact alternative identity, assurance, capability, resource-binding and expiry
  requirements. Policies are immutable and default-deny; hosts supply freshly
  authenticated evidence and retain transactional ownership/replay checks. This
  does not add guest accounts, persisted grants or implicit quota subjects.
- Shared [strong-revision validation](external/router/README.md#shared-strong-revision-validation)
  for singular If-Match headers and opaque ETags. Explicit size, whitespace and
  byte-policy options let custom proof handlers reuse the standard parser while
  retaining their wire contract. The route guard uses the same implementation;
  syntax validation does not replace current resource checks or transactions.
- Opt-in [token-policy management endpoints](external/accesspolicymanager/README.md)
  for explicit stored-user preview and revision-checked provisioning. They reuse
  lower-domain policy/user/inventory repositories and shared reply manifests;
  no sign-up defaults, role translation or production migration is implied.
- Reusable bearer-only session middleware and live policy-management authorization.
  Management routes reject cookie-adapter miswiring, API credentials, stale updates
  and revoked or demoted administrator sessions. Cookies are never refreshed.
- Opt-in transactional API-token admission in Access Manager, using live policy
  limits, exact stored inventory and one fenced count-and-insert transaction.
  The threshold endpoint uses the same policy. Host rollout and explicit grant
  migration remain required; see [adoption boundaries](external/accessmanager/README.md#transactional-api-token-policy).
- Owner-wide token inventory fences shared across system grants, with explicit
  startup initialization and idempotent owner-lock preparation. Policy adapters
  must supply fenced counts; missing preparation or unsafe transaction context
  denies issuance. See [inventory setup](external/apitoken/README.md#transactional-inventory-setup).
- Explicit [token-limit migration](external/accesspolicy/README.md#explicit-token-limit-migration)
  planning, audited apply and revision-checked rollback. Existing permissions,
  scopes, enabled state, expiry and usage are preserved. Host source selection,
  inventory provisioning and rollout remain explicit; no automatic role seeding
  or production migration is performed.
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
- [Typed JWT identity claims](external/auth/README.md) carrying the persisted user type, token purpose and
  standard registered claims, with optional issuer/audience configuration. User
  type remains classification, not a permission or replacement for the stable
  user ID. See [authenticated context](docs/how-to/authenticated-session-context.md)
  for the separate middleware integration.
- Declarative route definitions, startup validation, defensive route inventories
  and structured policy errors. The opt-in
  [route-policy guard](external/accessmanager/middleware/README.md#route-policy-guard)
  connects verified identity to live requirements. Raw Mux routes remain outside
  the registry; host adoption and complete route coverage are not automatic.
- System-scoped grants and atomic usage through an opt-in managed
  [Mongo policy store](external/accesspolicy/README.md), with revision-CAS audit
  records, current-authority replay checks, fixed-window counters and fenced
  token-inventory callbacks. Business callbacks can commit changes with quota
  receipts while rechecking resource authority on replay. Startup initialization
  requires transaction-capable Mongo. Host wiring, legacy-tier migration and
  retention tooling are not automatic or complete.
- Contributor guidance establishing table-driven tests as the default and
  requiring changelog updates for notable changes. The whole-suite test-style
  audit remains separate work.
- Reusable [manifest-driven HTTP error helpers](external/errormanifest/README.md#wrapped-errors-at-http-boundaries)
  preserve mapped wrappers and validation joins while rejecting unknown independent
  causes. A separate strict resolver supports single-cause authentication
  boundaries. Handler adoption and dependency-map wiring remain explicit.

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
