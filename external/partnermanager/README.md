# Partner use-case orchestration

`partnermanager` composes the owning `partnerprogram`, `referral`,
`partnerearnings`, identity and verified billing services. It has no database
client, provider webhook parser, commission formula or payout sender. Construct
`NewManager` with every typed capability, an evidence signer, an explicit clock
and independent `Controls`; missing and typed-nil dependencies fail closed.
Commercial configuration and region eligibility are host-approved inputs.

Every customer, operator and worker operation requires current scoped authority,
including receipt replay. Verified `ActorID` is separate from the target partner,
customer or claim. Transport must bind it from authenticated context, enforce
origin/CSRF/rate policy and project only permitted response data. An ordinary
administrator role does not grant payout processing or recording authority.

## Withdrawal admission and recovery

`Dependencies.Claims.MinimumMinor` supplies an explicit host-approved minimum
in the program currency's minor units. Negative values reject construction;
zero preserves the existing positive-amount contract and implies no commercial
approval. The minimum applies to new self-service and on-behalf requests.

Interactive hosts use `RequestClaimWithDestination` with `SelfClaimRequest`:
amount, expected positive destination version and an intent's original
idempotency key. Actor identity remains a separate verified argument. The manager
selects the address from the owning destination; an unseen version returns
`partnerearnings.ErrStaleWrite`. Reusing an admitted key with a changed amount or
destination version returns `partnerearnings.ErrConflict`.

Original receipt recovery precedes new-admission pauses, a raised minimum and
mutable destination reads, with current scoped authority rechecked before
return. A later destination edit does not change the original snapshot. The
compatible `RequestClaim` API retains current-destination selection and also
uses the minimum for new admission; it does not bind the caller's observed
revision. These are sequential owner checks, not an atomic transaction spanning
destination and financial stores. Existing financial receipt and reservation
transactions remain authoritative; no new schema or ledger algorithm is added.

`AdminPaymentClaim` reads one selected obligation under the exact
`partner.admin.payments.record` capability before and after the owning read.
Reporting and claim-processing privileges cannot substitute. Hosts can prepare
a full manual attestation from its fixed amount and currency without accepting
those fields from a browser or borrowing another owner. The read preserves
existing obligations during a manual-handling pause. `Controls.ManualRecording`
admits new or resumed handling through `AdminDecideClaim` entering `processing`.
When false, that transition is denied; review and confirmed-unsent cancellation
remain available under their own authority. `AdminRecordPayment` still records
a transfer already attempted, including original-receipt recovery, with current
selected permission and the native assigned-actor/state/revision checks. It never
sends money. Amendments and returned-transfer recording remain independently
authorized. A pause never silently releases potentially paid reservations.
`ManualHandlingAdmitted` supplies display guidance, not permission or proof that
a transfer occurred. Hosts must communicate the pause and keep command checks
authoritative against stale browser state. Roll out a pause to all running
instances before relying on it; these controls are constructor configuration.
This private domain result needs an explicit host DTO and proves no transfer.

`AdminClaimForAction` also permits a selected-claim read under exactly one of
processing, payment recording, payment amendment or returned-payment authority.
It rejects other actions and empty list targets before consulting authority.
The caller-selected action grants nothing: current authority must admit that
exact action and ClaimID before and after the successful owning read. It cannot
borrow recording authority for an amendment, grant queue/reporting access or
write a reservation or payment. The recording-only `AdminPaymentClaim` keeps
its existing contract. Hosts must bind the verified actor, apply a closed action
selection, suppress caching and enumerate only the action's permitted DTO fields;
serializing the private domain claim directly is inappropriate.

`AdminPartnerStatus` reads the selected partner under policy authority at that
PartnerID. An explicit host status DTO includes the current revision, status,
reason, timestamps and three admission flags, omitting the domain record's
customer identity, payout email, destination version and accepted terms. This
grant cannot substitute for policy authority at the underlying CustomerID.

`AdminClaimPreparation` requires on-behalf claim-creation authority at exactly
the selected PartnerID. Its typed view supplies configured currency/exponent
and minimum, claim/partner admission flags, current active/verified/individual
eligibility, the current destination's ID/method/email/version or null, and only
the selected financial owner's available amount. The full destination email is
for explicit recipient review under this action, not general reporting. Only
sole canonical destination absence becomes null; owner failure, identity
absence and malformed owning output remain errors. Pauses are data and preserve
the read. These are separate owner reads, not an atomic admission snapshot or
proof of email ownership. The command still checks current admission, funds and
the observed destination version. Existing original-key request recovery must
remain independent of this preparation read.

`AdminIndividualPolicyVersions` requires policy authority at exactly the
selected CustomerID. It filters complete owning history to that program's
individual stream, excluding global, group and other-customer records. The
returned `IndividualPolicyHistory.Revision` is the maximum published scope
revision, including future and expired versions; neither list ordering nor
currently effective policy supplies the publication CAS head. Conclusive empty
history returns an empty slice and revision 0. Mutable version fields are copied.
Hosts enumerate permitted policy DTO fields without serializing owner audit IDs.

All three reads validate the selected target and check its exact current action
before and after successful owning reads. They grant no program-list access,
perform no owning write, and require a verified actor and uncached host response.

## Worker backlog reporting

`AdminWorkerBacklog` uses the separate `partner.admin.operations` capability,
scoped to the program before and after the owning read. Partner reporting or
claim-processing permission is insufficient. Its optional `WorkReporting`
dependency is a `WorkBacklogService`, normally the composed `WorkQueue`; an
unwired or typed-nil service makes only this report unavailable. Commercial
admission pauses preserve authorized backlog reads. The manager rejects zero or
future classification times against its explicit clock.

`WorkQueue.GetBacklog` reads the complete pending set and discovery cursors
through `WorkReportingRepository.ReadWorkSnapshot`. The combined budget across
signup, revenue, revenue-source and maturity jobs is 10,000 pending rows; capacity,
corruption, cancellation or a failed scope returns no partial counts. Completed
history is excluded from the pending query and budget. The supplied queue
adapter reads all four partitions and cursors in one owning read transaction.
This does not establish a snapshot across billing, identity or earnings owners.

`pending` equals the disjoint `ready`, `delayed`, `backoff` and `leased` counts.
Delayed jobs have never been attempted; backoff jobs have at least one attempt.
A scheduled future maturity is delayed, not proof of a failure or retry. A live
lease takes precedence over the next-attempt time; expired leases are classified
by that time. `attempted_pending` includes the first active attempt and is not a
retry or failure count. `decision_awaiting_completion` is an overlapping subset
with a committed decision but unfinished queue completion; its source
acknowledgement may already have committed. Never add either subset to pending.
Oldest times refer only to the matching pending, ready or decision subset.

The public report exposes counts, oldest times, work kinds and an opaque
persisted-state revision. It omits job, source, receipt, actor, lease and cursor
identities. The revision includes private persisted evidence and discovery
positions; advancing the owning clock can change scheduling classification
without changing that revision. `as_of` is the owning classification clock
sampled after the read; it can be later than the database snapshot whose retained
state is classified. It is not
a source-acceptance watermark, database snapshot timestamp or commit timestamp.

Coverage is explicitly `discovered_pending_work`. An empty queue does not prove
signup/billing discovery, provider delivery or financial maturity is caught up.
Those obligations require their own owning reports and host alert thresholds.

Self-service includes explicit enrollment consent, overview, stable sharing
links, referrals, journal and claims, versioned payout destinations and requested
claim cancellation. Operator operations include policy publication/history,
partner status, the processing queue and conditional claim decisions, manual
payment recording, append-only amendments and returned-transfer adjustments.
`AdminRequestClaim` uses the separate `partner.admin.claims.create` capability,
an explicit target partner, a reason and expected owning destination version.
Reporting or processing permission does not authorize claim creation. The
operator stays separate from the owner, and cannot supply another destination.
Existing request receipts recover before new-admission gates or destination
changes; current scoped permission is rechecked before returning them.
Claim mutations require positive expected revisions; the owning financial
service preserves immutable receipts, reservations and settlement rules.

`CancelClaimWithReceipt(ctx, actor, SelfCancellationRequest)` resolves the
currently authorized owner and selected claim, then passes the immutable claim
revision, reason and request key to the financial owner. Pausing new withdrawals
does not prevent requested-only cancellation or original-key recovery. Current
permission is checked immediately before the owning operation and before
returning its result. A late postcommit revocation returns
`partnerearnings.ErrUncertain` without a claim; the committed receipt remains
recoverable under the original key once current authority permits it. Permission
reads and the financial transaction are separate owner boundaries.
The earlier `CancelClaim` API performs a conditional one-shot decision; clients
requiring lost-response recovery should use `CancelClaimWithReceipt`.

`RotateLink(ctx, actor, referral.RotateLinkRequest)` binds actor and partner
through current owning identity and permission. Callers supply the observed
original code, reason and a stable request key; they cannot select a different
owner. The [referral owner](../referral/README.md#share-links-and-evidence) commits
one atomic replacement and immutable receipt. New acquisition pauses preserve
original-key recovery. Permission is checked immediately before the owning call
and again before disclosing its result. If the latter check fails after a commit,
the manager returns `referral.ErrUncertain` with no link: the committed receipt
must be reconciled using the original key once current permission allows it.
These permission reads and the referral write remain separate owner boundaries;
a late revocation does not undo an already committed rotation.

`Statement` resolves the currently authorized customer's owning partner;
`AdminStatement` requires the separate reporting capability scoped to an
explicit target partner. Both validate the returned owner and configured
currency, then recheck current permission before returning the financial
snapshot. See the [earnings statement contract](../partnerearnings/README.md#statements)
for filters, revision semantics, manual payment presentation and history costs.
These are domain views, not customer-safe transport DTOs; project provenance,
operator identifiers and references according to the caller's permission.

`ReferralHistory` returns a customer-safe projection of current and retained
partner relationships. It resolves the verified owner's enrollment, validates
owning membership/currency/period output and rechecks permission after the read.
It exposes partner-scoped opaque relationship/period IDs, owned cutover periods
and frozen commercial terms, without customer identity, other owners, internal
revision/policy/plan IDs, acquisition evidence or raw correction reasons.
`AdminReferralHistory` requires reporting permission scoped to an explicit
partner before and after its owning read; its domain result retains internal
customer identity for an independently authorized host projection. Neither
admission pause nor an ownership correction deletes retained relationships.
These reads do not calculate per-referral availability or assign a whole claim
payout to a referral. See [retained relationship semantics](../referral/README.md#retained-partner-relationships)
for snapshot, pagination and historical binding limits.

`Payments` and `AdminPayments` expose the owning
[payment cohort report](../partnerearnings/README.md#payment-cohort-reports)
under current self or partner-scoped reporting permission. They validate
enrollment/program/currency, visible row filters and cursors, then recheck the
same capability before returning. Commercial pause preserves this read path.
These remain private financial domain results; the host must compose safe
referral summaries and label original-payment cohorts separately from period
journal movements and current global availability. No whole claim payout is
copied onto every referral, and missing acceptance is not an entitlement zero.

`ReferralSummaries` and `AdminReferralSummaries` join a bounded retained
relationship page to [grouped owning financial amounts](../partnerearnings/README.md#grouped-retained-referral-amounts).
They accept only relationship pagination and an optional original-payment date
cohort; the manager builds the financial groups from all owning period IDs.
Public period digests and client-supplied revision lists establish no authority.
Both return the same safe aggregate DTO; broader operator identity disclosure
requires a separate explicit permission and projection.

`FinancialSummary` returns a customer-safe aggregate of the owning
[financial metrics](../partnerearnings/README.md#financial-metrics). It accepts
only the original-payment/economic-movement date range. It exposes aggregate
commission, paid/returned movements and explicitly unfiltered current balances,
claim states and remaining commission. It omits billing revenue and private
plan/provider/payment identifiers and payout references. Missing cohort
acceptance omits `cohort_commission`, even if a paid movement exists in the
selected period; accepted zero commission remains explicit. Allocation-row
counts never stand in for subscribers, paying referrals or click conversions.
Source and subscription coverage remain `not_evaluated`.

The same authorized partner's `current_maturity` exposes only aggregate due
ledger rows, net pending credit, its overlapping dispute hold and oldest due
deadline. It stays unfiltered even when the selected date cohort has no accepted
rows. These describe the caller's retained earned obligations, without source,
payment, referral, provider or operator identities. They are not extra available
funds, subscriber counts or proof that a worker processed the deadline.
The manager rejects malformed due-row/time coupling and amounts exceeding the
corresponding complete owning pending balances. See
[maturity semantics](../partnerearnings/README.md#financial-metrics).

`AdminFinancialMetrics` is a private domain report under selected-partner
reporting permission, with bounded plan breakdowns and original plan/date
filters. The host must project permitted catalogue labels and financial fields
for operators; it is not a customer response or an unrestricted export. Both
methods validate owner/program/currency and result pagination, then recheck
current capability and enrollment. Customer reads also recheck current identity.
Paused new commercial admission preserves authorized reporting. Current claim
exposure and remaining commission are separate overlapping measures, never
additional available funds or a source-completeness guarantee.

The result's `scope` is `visible_relationships`, never a whole-program total.
`attribution_revision` fingerprints this page, not the global relationship set;
`attribution_observed_at` records when the manager completed that owning read.
`financial_revision` and `financial_as_of` describe the independent complete
financial snapshot. A second attribution read detects an on-page correction or
reacquisition; changed page evidence returns `referral.ErrStaleWrite` and no
partial result, so a caller can refresh rather than retry indefinitely. A new
membership outside the page window is not certified by this check. The reads
are not a globally atomic transaction, and later changes can invalidate either
snapshot. Self-service rechecks current identity/enrollment and capability;
operator reads recheck explicit scoped reporting and selected enrollment.

Rows expose only opaque relationship/period IDs, frozen public commercial terms,
accepted payment-row count, review flag and aggregate commission/backing. They
omit raw customer, attribution, policy, plan and payment/provider IDs, per-invoice
revenue, private correction reasons and first/last purchase times. No accepted
row means `acceptance: no_accepted_accrual` and an omitted `commission`; an
accepted zero commission means `acceptance: accepted_accruals` with explicit
zero amounts. Neither implies complete source processing. `source_coverage`
and `subscription_coverage` remain `not_evaluated` without the explicit
[revenue reporting opt-in](#paid-referral-source-and-status-evidence).
Reserved/review/payout backing is provenance;
only `current_partner_balances` supplies global availability and debt. A grouped
history over 10,000 revisions returns `partnerearnings.ErrReportTooLarge`; a
smaller page helps only if its individual relationships fit that bound. The
host must preserve that capacity error rather than turn it into no earnings.
A commercial admission pause does not erase retained earnings or this read path.

## Paid referral source and status evidence

`WithRevenueReporting` opts retained referral summaries into capabilities on
the manager's **same configured Revenue owner**. The host explicitly configures
provider/account/test-live scopes and a freshness budget of 1 second–24 hours;
the manager copies the scope list and rejects missing capabilities. No request
accepts a payer ID, merchant scope or freshness override. Without opt-in the
existing coverage remains `not_evaluated` and paid evidence is omitted.

The join reads each visible relationship's owning attribution snapshot and
[complete confirmed billing history](../billing/README.md#confirmed-payment-revenue-history).
Only an immutable payment binding to that partner's retained period, with
matching frozen terms and economic time, establishes ownership. Reads never
create bindings or accept revenue. Eligible positive original allocations set
`paid`; positive current net revenue sets `net_positive`. Each relationship
counts once in the corresponding page totals, regardless of renewal/allocation
count. Zero payments, ineligible frozen plans and payments at or after the
frozen recurrence endpoint do not establish paid eligibility. An accepted
positive payment without binding is an `awaiting_attribution_rows` processing
count, not a person or commission entitlement. Later prospective corrections
cannot transfer an existing binding or its earned history.

Original-payment dates use From inclusive / To exclusive. Later refunds and
disputes attach to their original cohort, independently of adjustment date.
Commission acceptance and balances still come from the earnings owner; source
paid history does not manufacture a missing accrual or recompute commission.

Only currently owned relationships with eligible positive-net revenue supply
status candidates, deduplicated by provider/account/mode/subscription. A scoped
subscription cannot belong to two paying principals. Fresh retained
`active` contributes to confirmed active subscriptions; `trialing` has its own
count. Missing, stale or failed lifecycle reads increment unknown subscriptions
and leave exact `active_paid_subscriptions` null. Confirmed active counts remain
visible as a lower bound. Former owners receive `retained_history_only`, no live
status lookup or per-row exact current subscription count. Status reads perform
no provider I/O. At most `SubscriptionStatusReadCapacity` (200) optional retained
status reads run per report; remaining candidates increment unknown subscriptions
and `deferred_status_reads`. Paid-source counts remain complete for the report
scope, while an exact active count stays absent. Observed time ranges contain only successful fresh evidence.

`paid_coverage` is explicitly for `visible_relationships`, never an Overview or
whole-program conversion denominator. It retains independent source as-of and
revision, lifecycle observed times and opaque revision, alongside the existing
financial/attribution metadata. Unreceived and unassigned quarantined deliveries
are not assessed for relationship completeness. Their counts **and presence**
are not disclosed through customer or partner-admin reports. Empty visible pages
carry no invented source timestamp/revision. Page membership and attribution
fingerprints are rechecked after the join; observed changes return
`referral.ErrStaleWrite` with no partial response. These checks do not create a
cross-owner atomic snapshot.

The combined ownership-history/payment-binding budget for one page is
`ReferralEvidenceCapacity` (10,000 rows), independently of billing's complete
global fact/receipt budget. `ErrReferralEvidenceTooLarge` and
`billing.ErrRevenueHistoryTooLarge` are capacity failures, not invalid customer
input or zero earnings. Scope failures, malformed source/attribution evidence
and cancellation return no partial report. Host transport and UI adoption,
durable status refresh and coverage alerts remain explicit integration work.

### Complete partner paid metrics

`PaidReferralMetrics` and selected-partner `AdminPaidReferralMetrics` return
aggregate paid evidence for **complete_partner_relationships**, independently
of referral list pagination. The optional same referral owner supplies
[complete relationship evidence](../referral/README.md#complete-relationship-evidence).
The manager derives all payer identities from that owning set and makes ONE
complete billing-history read. It never batches 100-person reads into global
sums. Billing's 10,000 facts-plus-receipts budget remains global before filtering;
a small partner set cannot hide an exceeded owning history budget.

Lifetime/current/retained relationship counts are unfiltered. From inclusive /
To exclusive selects **original payments**, not signup or visit cohorts. Paid
and net-positive relationships, allocation rows, awaiting-attribution processing
rows, fresh active/trialing counts and unknown/deferred status remain distinct.
Renewals cannot increase people counts. The aggregate exposes no payer, provider,
payment or replacement-owner identities and creates no commission entitlement.
Financial balances/movements come separately from `FinancialSummary`.

Independent attribution/source/status clocks and revisions remain explicit.
ONE complete owning-evidence recheck detects observed new memberships, bindings
and corrections during the join; current self or selected-partner admin
permission and enrollment are checked again before returning. This is not a
multi-owner transaction or certification of unreceived/unassigned deliveries.
An empty complete set carries no invented billing timestamp. Missing capability,
capacity, malformed evidence or cancellation returns no partial aggregate.

These global paid counts use original-payment dates. Use the separate original
cohort report below for signup-to-paid or visit-to-paid conversion fractions.

Overview, referral/journal/claim/destination reads, the operator queue and policy
history also recheck their original capability before returning data. Owner
views validate scoped dependency results; failures discard partial data instead
of returning it beside an error. Payout destinations must belong to the resolved
customer. Permission to read the global processing queue is explicitly distinct
from permission to read a selected owner's reporting statement.

### Original cohort paid conversions

`ConversionMetrics` and selected-partner `AdminConversionMetrics` require the
same revenue-reporting opt-in and the referral owner's optional
`GetConversionEvidence` capability. ONE native snapshot combines complete
traffic, ownership and immutable bindings. ALL selected relationship principals
feed ONE complete billing-history read; ONE combined revision recheck detects
observed traffic or attribution changes before current authority and enrollment
are rechecked. Missing capability, granularity, capacity, corrupt evidence,
cancellation or revocation returns no partial report. No provider/status calls,
binding creation or financial writes occur.

The inclusive `From`, exclusive `To` range selects ORIGINAL signup/acquisition
times and independently original measured visit times, never payment times.
Eligible payments after `To` can convert those cohorts. Revision-one measured
and unmeasured signups, manual initial assignments and later correction
acquisition events remain separate. Only positive original payments with the
selected immutable period binding and eligible frozen plan, currency/exponent
and recurrence establish conversion. Later ownership never inherits the original
owner's signup or visit credit. Renewals deduplicate per signup/period; multiple
accounts from one measured origin convert one visit.

Each fraction exposes `confirmed_converted`, its matching `denominator`,
`net_positive` and an observed retained-evidence rate. Full refunds preserve
historical conversion while net-positive counts can fall. Zero denominators
omit the rate; missing original visit times withhold visit rates rather than
guessing. Source coverage never certifies unreceived or unassigned provider
deliveries, and conversion is not current subscription activity. Independent
traffic/attribution and billing clocks/revisions remain explicit. A successfully
empty relationship set creates no billing clock.

Global totals precede link pagination. Private administrator plan rows page
independently and use ALL selected automatic signups as their denominator;
manual and correction denominators stay separate. A signup can convert under
multiple eligible plans, so `MultiPlanSignups` exposes overlap and plan people
must not be summed. Plan IDs and the entire administrator wrapper are excluded
from JSON; hosts must project permitted catalogue labels. Customer output
contains only safe aggregates and the partner's own links, no customer, signup,
measured-origin, provider, payment or raw plan identities. Hosts serialize large
counts losslessly and compose these reports into their Overview and filters.
Transport/UI adoption and durable refresh/coverage alerts remain integration
work.

`UserIdentityAdapter` projects current identity and immutable new-account
creation capture from the owning `user/v2` service. Its explicit program,
individual account types and active statuses are copied at construction. A
separate `RegionEligibility` capability supplies approved host rules; there is no
GeoIP, email or role heuristic. Current inactive, unverified or nonindividual
accounts remain ineligible. A legacy account without atomic signup capture never
becomes a new signup fact, and current profile changes cannot rewrite captured
creation time, type or signed evidence. Within one program, the creation fact ID
is the owning account ID; it is not a login or organization-seat event.

`ConsumeSignup` verifies the captured token at the owning creation instant and
locks attribution with event-time policy terms. Immutable successful acceptance
can replay after link retirement or commercial pause, but current worker
permission is still required. `ProcessRevenueFact` reads the billing owner's
scoped immutable allocation and original paid source, freezes its attribution
and terms, and calls only the earnings owning service. Accepted accruals recover
before new-admission gates; fresh admission still obeys accrual eligibility.
Refunds and disputes use original economics independently of new-accrual pause.
`MaturePartner` and returned-transfer recovery similarly preserve existing
obligations rather than creating a new commercial entitlement.

## Visit preparation and aggregate analytics

`PrepareVisit` takes server-bound consent, known-bot classification, link code
and prior secure cookie values. The host owns same-origin navigation, consent,
rate limits and collection/retention admission. `PreparedVisit` exposes only a
measurement status to JSON: tokens and absolute expiries are server-only fields.
Set separate Secure/HttpOnly/SameSite signup and visit cookies to those absolute
expiries, and clear empty values. Do not extend a token's lifetime on reload.

A valid same-link signup token is reused verbatim. The shorter signed visit
cookie handles measured reload deduplication; a new visit cookie does not replace
otherwise-valid signup evidence. Known bots and non-consenting requests do not
seed attribution. Optional analytics failure preserves the existing signup token
or issues plain unmeasured evidence; cancellation remains an error. Current link
and partner admission still gate new visit preparation. See the
[measurement contract](../referral/README.md#consented-visit-measurement).

`ReferralAnalytics` and `AdminReferralAnalytics` expose the
[owning aggregate report](../referral/README.md#referral-analytics) under current
self or selected-partner reporting authority, rechecked after the read. Admission
pauses preserve authorized history reads. Results contain only aggregate counts
and the selected partner's own share links, never referred customer, signup,
cookie, measured-click, provider or payout identity. Count dates, lifetime
memberships and incomplete-origin coverage remain explicit; an empty report
is not proof of paid conversions, active subscriptions or complete source feeds.
Raw analytics require explicit approved service retention. All-time/whole UTC-day
reports survive raw cleanup; sub-day boundaries require fully retained raw rows
and otherwise return `referral.ErrGranularity`. Hosts must present that failure
separately from zero and serialize large `int64` counts losslessly.

## Prospective attribution review

`PreviewAttribution` and `ApplyAttribution` require the separate
`partner.admin.attribution` capability scoped to the referred customer. Reporting
or claim-processing permission cannot authorize an ownership change. The host
binds verified `ActorID`; the request selects customer, proposed owning partner,
owning signup ID, reason and explicit `prospective` mode. New admission resolves
the immutable eligible individual creation capture, current target and proposed
partner owner, enrollment and approved region/policy/group capabilities. A
missing legacy capture cannot become signup evidence. Current region checks
do not assert historical geography. Assisted assignment records its operator
and reason and begins prospectively; it is not an automatic signed-click claim.

The preview presents the original/proposed ownership and terms, retained
history, and every unchanged payment binding, including future-effective
bindings. Historical commission and payout deltas are explicitly zero; this
workflow neither transfers old entitlement nor rewrites paid history. Future
commissions depend on verified eligible revenue, so the preview is not a money
forecast or a per-referral payout report. Claims can cover multiple referrals.
Finite recurrence is anchored to signup and clamped to prior frozen ends.

Apply requires the original head ID/revision and both reviewed fingerprints.
The manager recomputes current owning source and terms; the referral owner checks
the complete snapshot inside the transaction that appends its receipt. Changed
source, policy, ownership or bindings require a new review. Fingerprints are
preconditions, not authority. Permission is rechecked before append/return; an
in-flight revocation can hide a committed result, so the original key remains
the recovery path under renewed current authority.

An exact immutable actor/customer/key receipt is recovered before current
policy, identity or new-admission controls. Changed submitted targets, reason,
mode or reviewed preconditions conflict. A later head or paused program cannot
replace the original response. Preview and result are private owning operator
models; host DTOs must omit internal source/policy/economic references from
customer responses. Full snapshot reads grow with history, and cross-domain
policy/identity checks are not one database transaction with the referral store.

Construction alone does not install host routes or background workers. Hosts
must compose the optional durable signup/billing/maturity worker, authenticated billing
source reconciliation and bounded scheduling, current identity/group and
permission adapters, additive index/readiness/restore procedures and complete
customer/operator interfaces. Historical compensation is outside the prospective
correction workflow. Local package
and datastore tests do not establish full application adoption or deployment.

## Durable work queue

`WorkQueue` supplies an optional owning workflow boundary for signup, verified
revenue, quarantined source and financial maturity work. Explicit configuration bounds leases and
exponential retry delays. `partnerstore.WorkRepository` persists it through the
prepared encrypted transaction-capable store.

A bounded discovery page and its read cursor commit together. The cursor is a
read position, not a source acknowledgement. Immutable page receipts support
lost-ack replay even after a later page has advanced; completed sweeps reset the
position so late lower IDs are discovered. Queued work has independent durable
attempt times, so a failing prefix cannot hide later work. A changed immutable
source fingerprint conflicts instead of overwriting a completed decision.
Maturity candidates require the earnings owner's original `DueAt`; other kinds
require zero. `InitialDueAt` remains immutable in jobs, page receipts and decision
fingerprints, with the first attempt scheduled at the later of queue creation
and that deadline. Currency selection, refunds, retries and restarts cannot
retime an accepted accrual. Due times retain UTC nanosecond precision.
Retry scheduling cannot move before the original deadline or queue creation
even after a clock rollback. Deadline gates and decision timestamps use the
queue's scheduling clock; the earnings owner still decides what can mature.

Leases use opaque fencing tokens. Competing workers cannot lease the same item;
after expiry, a stale token cannot write a decision or retry state. `Decide`
records the financial/referral owner's acceptance reference or a conclusive
no-entitlement/quarantine decision before the source can be acknowledged.
`Complete` requires that decision and is called only after the owning source
confirms acceptance. A lost acknowledgement preserves the decision for replay;
retry state never discards or reverses it. Public JSON omits source identities,
fingerprints, lease tokens and decision actors; encrypted envelopes retain them.

These are in-process workflow primitives. No automatic startup, HTTP route or
authority bypass is supplied by this queue. The host/manager must
check current source-scoped authority before every operation, including receipt
replay. Fingerprint private immutable source fields explicitly; serializing a
DTO that excludes evidence from JSON would omit necessary replay identity.

## Owning signup, revenue and maturity worker

`NewWorker` requires a manager, prepared work queue, owning `user/v2` signup
feed, owning `billing.RevenueService` feed and authenticated billing-manager
source reconciler. It also requires `MaturityFeed` from the same manager earnings
dependency used for mutation, bound to the queue's program and the program's
single approved currency. Missing, typed-nil or mismatched ownership fails
construction; a separate feed cannot acknowledge another financial ledger.
Configure a verified service `ActorID`, a stable billing
consumer ID and page/batch sizes between 1 and 200. The initial program is
`partners-v1`. Construction does not schedule work or enable commercial admission.
The host calls `RunOnce` on its bounded, cancellable schedule.

Each run discovers one page per signup, source, revenue and maturity kind, then attempts
one due batch per kind. Discovery moves separately from owning acceptance;
completed sweeps reset to include late lower IDs. Failed jobs have independent
durable backoff, so an unresolved first 200 sources cannot hide later work.
Reports contain operational counts and structured issue codes, without source
identities or raw dependency errors. A run may process later items while
returning earlier dependency errors; inspect both report and error.

Signup success means a durable referral lock, followed by an immutable queue
decision and only then owning identity consumption. Missing cookie or immutable
ineligible account type has an explicit no-attribution decision. Invalid or
unverifiable evidence remains pending for review, including unavailable old
signing keys. A legacy account without capture is not guessed to be a new signup.

Revenue success means the earnings owner committed its idempotent operation,
including zero-delta adjustment receipts. The queue decision precedes the billing
consumer acknowledgement. Frozen plan exclusion, zero net revenue or expired
recurrence produces a typed `NoEntitlementError`; generic absence does not.
Missing referral ownership is conclusive only with an owning consumed
no-evidence/ineligible receipt or proof that payment predates account creation.
Adjustments may inherit the original consumer's durable no-entitlement receipt.
Outages, pending attribution, currency review and commercial pauses remain pending.

Source recovery authenticates evidence through the billing manager and records
the billing owner's immutable resolution. An existing resolution is recovered
before calling the provider again. It does not acknowledge any consumer fact;
newly recovered facts enter the separate revenue feed.

Current source-scoped worker authority is checked throughout, including before
decisions, acknowledgements and completion. Restart can use a newly authorized
actor; stored receipt authorship never supplies current authority. Lost replies
after financial commit, source resolution or owning acknowledgement retain
enough information to recover without another accrual or provider call. Paused
new admission preserves existing accepted obligations and recovery.

Maturity uses the separate `partner.worker.maturity` capability. Program-scoped
permission precedes discovery, leases and the initial private source read; the
verified source supplies the partner target for mutation, decisions, completion
and retries. This grant does not permit revenue accrual, claims or payment
recording. `MaturePartner` rechecks it after the owning call, independently of
commercial admission. A revoked caller cannot decide, complete or retry work.

Pending due sources invoke idempotent owning maturity, then re-read the exact
source through a complete financial snapshot. Only its retained accepted
`MaturedEntryID` permits a decision. A concurrent completion seen during
discovery keeps the same candidate/deadline and recovers that receipt. An old
decision requires the same actual completed receipt before any mutation; absent,
pending or contradictory proof never manufactures acceptance or no entitlement.
There is no additional billing/signup acknowledgement for maturity.
Queue acceptance cannot precede the owning receipt time. A clock behind that
receipt waits for recovery; an old decision predating it conflicts. Fenced
workers report `lease_fenced` without another stale-token retry write.

Failed verified sources retain independent retry schedules. If a source read
cannot establish a verified partner target, the worker retains the lease until
expiry and continues later jobs; program authority cannot substitute for partner
retry authority. Financial source reads validate complete bounded partner history
per selected source, so hosts must pace discovery and attempts and alert on
capacity, unavailable source evidence and overdue financial obligations. Queue
counts alone do not prove the source index covers the ledger. Hosts still own
startup preflight, source reconciliation, scheduling and restore/rebuild checks.
Maturity preserves refund/debt/dispute-hold rules and never sends a payout.

## Optional HTTP boundary

Use [partnerhttp](http/README.md) for independently mounted customer/operator JSON
routes, safe projections and configurable browser protection over the same human
authority. It owns no financial transactions or additional receipt store.
