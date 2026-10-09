# Billing lifecycle recovery

`CheckoutOutbox` retains the original acknowledged subscription checkout before
provider lookup, then the original authenticated lookup response before billing
capture. It borrows the host's prepared encrypted GHATD `recordstore.Store` and
requires the configured billing manager's `ValidateCheckoutLifecycle` method.
It creates no authority, provider evidence, paid revenue, status or commission.

Every operation validates current selected refresh authority and native original
joins before storage and after the outcome, including errors and absence. These
checks happen outside retryable database callbacks. The double validation costs
two owning preparation reads and their authority checks per operation. An old
preparing actor cannot confer permission to a replacement worker.

The explicit private schema preserves `CheckoutIntent.Request` and
`Fingerprint`, which public JSON intentionally omits. Payload encoding retains
nanosecond timestamps, request metadata and typed provider evidence. Reads
revalidate the decoded canonical billing input, schema, stage and metadata.
Indexed IDs and partitions use SHA256 over versioned JSON tuples:

- ID: `["partners.lifecycle.checkout.v1", scope, intentID]`.
- Partition: `["partners.lifecycle.scope.v1", scope]`.
- Transaction guard: `partners_lifecycle_checkout_input:` followed by the ID.

The persistent record has no expiration or sequence. Its only states are
`prepared` (revision 1, no evidence) and `evidence` (revision 2, original evidence).
Preparation retries return the existing stage, including its evidence pointer;
they never reset an evidenced record. Evidence requires existing preparation.
Exact evidence retries do not change revision; changed evidence conflicts.
Corruption or missing preparation does not become a new authorized lookup.

When final current checks succeed, storage errors retain their original causes.
A post-operation authority failure
withholds every payload. If the storage operation already reported an uncertain
commit, its uncertainty cause is retained alongside a later authority failure or
cancellation. Every write error requires reading/retrying the original operation
under current authority before proceeding; neither denial nor cancellation proves
rollback. Only genuine single wrapped store absence permits initial insertion;
joined outages and custom absence aliases do not.

These outbox adapters implement the private original-input handoff. They are
composed explicitly by an opting host. The execution collector must use the separate fenced binding ports below;
standalone outbox writes are not a substitute for those stages. It must
inspect retained state before provider lookup, avoid lookup when evidence is
already retained, and use billing's receipt recovery or exact retained capture
after uncertainty. Provider calls and financial capture never run in these
storage callbacks. Authenticated lookup output is trusted internal input, never
HTTP-decoded payload.

The isolated named test cases verify round trips, stage conservation, detached
maps, uncertain replay, current authority and corrupt-record refusal. The native
suite uses encrypted Mongo, real billing and manager services, native policy
grants and the instance-bound worker authority. It checks replacement-worker
recovery, key/metadata tampering, grant revocation, concurrent identical and
different evidence writes, and receipt/capture recovery without another lookup.
Provider responses and active service identities are controlled fixtures.
Lost-reply tests return uncertainty after a real successful commit; they do not
simulate Mongo server unknown-commit labels or production network failures.

Neither suite certifies scheduler execution fences, actual service-account
admission, migration readiness, runtime installation or authenticated platform
end-to-end behavior. Those remain integration qualification requirements.

## Original subscription status inputs

`StatusOutbox` retains billing's original `SubscriptionStatusPreparation`, then
the authenticated `VerifiedSubscriptionStatusEvidence` returned by its manager.
It supports payment and pre-payment checkout provenance. All fields in these
billing types are excluded from public JSON, so a separate private payload codec
preserves the source IDs, native capture identity, frozen original author,
scope/payer/customer/subscription, expected revision/fingerprint and nanosecond
`RequestedAt`, plus status and scheduled cancellation evidence. The codec does
not reconstruct billing's capture identity or business fingerprints.

Its validator must be the configured billing manager's
`ValidateSubscriptionStatusPreparation(ctx, currentActor, original)` method.
The raw revenue owner does not supply current caller authority. Native tests bind this interface to the actual configured manager. Neither
module adoption nor native interoperability proves runtime installation.

Status input records use kind `partners_lifecycle_status_input` and ID
SHA256 of `["partners.lifecycle.status.v1", scope, captureID]`. Partition uses
the same versioned lifecycle scope tuple as checkout inputs. Each preparation
gets its own record so an unresolved observation remains recoverable after
later observations. The transaction guard is the kind followed by `:` and ID.
Only `prepared` revision 1 and `evidence` revision 2 are permitted, with no
sequence or expiration. Indexed state exposes the handoff stage against an
opaque ID; private identities and evidence remain encrypted payloads.

Preparation replay returns existing evidence. Evidence requires a retained
preparation and cannot replace a previously retained different response. Find
and writes validate provenance and current authority before storage and after
every outcome, outside retryable callbacks. Data is withheld on error, while an
observed uncertain storage commit survives later denial or cancellation.
Outage, corruption and joined absence errors never permit treating a job as new.

Owning validation proves provenance, not that the expected head revision is
still current. If an original capture already committed, exact input replay
recovers its immutable receipt even after a later head. If it did not commit,
the owning capture can reject its outdated revision. A generic conflict does
not prove supersession: resolve the exact original through the configured
manager first. Only a validated native `superseded` result permits fenced
disposition and then a new observation. Uncertainty never authorizes discarding
the original.

This adapter stores inputs, not status heads or capture receipts. The scheduler
must durably retain an active original/job pointer before provider work, fence
execution, acknowledge confirmed outcomes and implement qualified retention and
cleanup. With no expiration, these records grow with observation count. Capacity
and safe cleanup remain collector integration requirements. No provider call,
capture, financial fact or commission is created by an outbox operation.

Status tests use actual billing preparation/capture and native codecs over
isolated records, with controlled current-authority validation. They verify both
sources, later revisions, exact original replay after later heads, a stale
uncaptured conflict, uncertainty, cancellation, revocation and private codec
integrity. The native status suite uses encrypted Mongo, configured billing
manager and current native policy grants. It checks both payment and checkout
provenance, lost preparation/evidence/capture replies, current and post-commit
revocation/cancellation, replacement-worker recovery, wrong-key and AAD tamper
refusal, concurrent evidence conservation and original receipt replay after a
later head without another provider lookup. Provider evidence and active service
identities remain controlled fixtures; lost replies are injected after real
successful commits, not Mongo server commit labels or production network faults.
Runtime fences, retention/cleanup, actual service-account admission, migration
readiness and authenticated complete platform flows require separate proof.

## Scheduling storage boundary

`ScheduleRepository` is the private typed port for recurring lifecycle work.
`RecordScheduleRepository` borrows the prepared encrypted recordstore and reads
an ordered bounded job page plus its fair-scan cursor in one snapshot. Its
`CommitScan` writes never-executed job insertions/revisions and the cursor in one
native transaction, checking expected cursor and job revisions. Once a job has
an execution epoch or attempt, this bootstrap primitive refuses job writes;
execution-specific operations must enforce the active fence. No partial page
insertion or cursor advancement survives a known transaction failure.

Cold, refresh and retired lanes have separate opaque partitions and cursors.
An existing job can move lanes while retaining its immutable source and creation
time. The owning scheduler must decide when to wrap/reset a scan and advance
past future-due or failed jobs; a stored cursor is only a position, never proof
of source completeness, permission, provider freshness or successful capture.
Storage does not decide commercial eligibility, lease duration or retry policy.
Its execution adapter enforces due/expiry predicates using the service's choices.

Private codecs preserve acknowledged checkout fields and native original
status preparations, including nanosecond times and frozen authorship. Job and
cursor records have no TTL or sequence. Their indexed IDs and partitions are
versioned SHA256 tuples; their indexed state is the lane. Corrupt schema, private
input, joined absence or metadata fails closed. An indexed partition change can
hide a row from its old query; direct access rejects the altered ciphertext, but
an ordinary scan cannot prove exhaustive coverage. Reconciliation is required.

The guard selects serialization, not a single-kind restriction: the existing
`recordstore.Tx` supports job and cursor kinds in the same transaction. No new
Mongo transaction infrastructure or direct driver calls are needed here.
Native named cases verify atomic cursor/jobs, rollback, stale CAS, uncertain
reply recovery by reading persisted state, concurrent competing pages, private
codec round trips and tamper refusal. Source descriptors in adapter tests are
fixtures; those tests do not prove owning discovery or current queue authority.

An opting host explicitly composes this storage boundary. The owning scheduling service
must still enforce current global/selected worker authority outside callbacks,
due-work selection, fair lane budgets, lease/stage fencing and backoff, and bind
original inputs plus active job pointers atomically before lookup/capture.
An unknown commit requires reading current durable state before deciding whether
to replay a page. Full collector, retention, migration and platform verification
remain required; financial `WorkComplete` does not retire lifecycle observations.

## Atomic discovery admission

`DiscoveryRepository` retains a separate billing-native continuation for each
scope and source kind. `ReadDiscovery` returns its revision and cursor;
`CommitDiscovery` validates the canonical billing page, checks that the query
starts at the expected checkpoint, and admits every visible source together with
checkpoint advancement in one encrypted transaction. An empty binding-only page
can still advance the native cursor. A reached-end page stores an empty cursor
so the next sweep can discover late inserts behind an earlier position.

Discovery checkpoints use kind `partners_lifecycle_discovery`. Their ID,
partition and transaction guard derive from the versioned JSON tuple
`["partners.lifecycle.discovery.v1", scope, sourceKind]`. They have no state,
TTL or sequence. Billing's native cursor validation binds scope and source kind;
the host fair-scan cursor cannot be used in its place. Corrupt checkpoint metadata
or payloads fail closed; only sole genuine absence starts revision zero.

New jobs enter the cold lane. Existing jobs are read and validated but remain
unchanged: lane, original status preparation, due times, lease, retry count,
creation time and revision survive repeated admission. A subscription gaining
its first paid fact does not retarget a checkout-derived original. Different
nonempty fact identities, owners or acknowledged checkout originals conflict.
Discovery is admission, not permission to retire or reschedule a job.

Inputs are encoded before retryable callbacks. Known failures roll back all jobs
and the checkpoint. An uncertain result requires reading durable checkpoint and
job state; it neither proves rollback nor authorizes a new provider lookup.
Named encrypted Mongo cases cover competing pages, late conflicts/corruption,
lost replies, empty-page progress, independent cursors, and preservation of
native original status inputs after a first paid fact. Owning preparation/read
fixtures prove native page interoperability, not a qualified host migration.

This repository does not authenticate page provenance by itself. The owning
collector must obtain the page from its configured billing manager under current
discovery authority, recheck global and selected authority around admission,
and retain uncertainty through later denial/cancellation. `DiscoveryCollector`
below supplies that admission boundary. Atomic input/job binding, execution
fencing and runtime installation remain required.

## Current worker discovery collector

`DiscoveryCollector` implements one bounded admission invocation over the
configured billing manager, discovery repository and the same current native
worker authority. Configuration freezes the service actor, allowed native
scopes and page limit; the caller chooses only a configured scope/source kind,
never an actor or cursor. Construction creates no identity, grant, migration or
background worker. `Admit` uses the persisted native checkpoint for its query.

Scope authority is checked before checkpoint storage and after every outcome.
Malformed checkpoint revisions/cursors fail before manager lookup. The manager
owns canonical discovery joins and its current selected checks; the collector
also validates the returned page and checks scope plus every selected original
before committing it. Current scope/selected permission and cancellation are
checked again after every admission result, outside retryable callbacks. Its
private result counts visible handed-off sources, including already admitted
jobs; it does not count newly inserted jobs or certify source completeness.

Every error withholds the result. An observed uncertain storage commit remains
joined with a later permission failure or cancellation. The collector does not
retry that commit automatically; a subsequent invocation reads actual durable
checkpoint state before obtaining the next owning page. Reached-end remains a
repeatable sweep position, not permission to retire lifecycle work.

Isolated named cases cover dependency/config boundaries, checkpoint validation,
all storage/manager outcomes, current selected denial, late scope revocation,
cancellation and uncertainty conservation. Native cases use actual owning
preparation/discovery, the configured billing manager, native policy grants,
instance-bound worker context and encrypted Mongo. Discovery-only grants suffice;
read/refresh grants, disabled/wrong-scope grants and another instance's context
do not. Post-commit revoke/cancel withhold results while durable admission remains
inspectable, and lost reply recovery advances from the committed native cursor
without another provider lookup. Active API-service identity and provider
responses remain controlled fixtures; actual UMS/platform admission is separate.

An opting host explicitly composes this service. The fair scheduler and atomic binding
services below supply separate private boundaries. Owning execution/recovery
orchestration, confirmed recurring status refresh, validated runtime
configuration, host migration/retention/ops and complete authenticated platform
flows remain required. It does not use financial
`WorkComplete`, look up a provider, capture status or create financial facts.


## Execution repository fences

`ExecutionRepository` supplies private `ReadJob`, `Acquire`, `CheckLease` and
`Release` operations over the borrowed encrypted store. Current worker authority
remains the composing service's responsibility. The adapter's clock must be a
pure local clock: callbacks read it without remote I/O or authorization effects.
Production instances require qualified clock synchronization and bounded skew.
No claim is made that local time proves one distributed wall-clock instant.

Acquisition joins the exact immutable source and expected job revision inside a
native transaction. Only due cold/refresh jobs with no unexpired lease can be
acquired. The adapter increments job revision, attempt count and execution epoch,
then retains the proposed actor, fresh token and absolute expiry. It rechecks
time before conditional replacement and refuses a backward clock step during
that callback. The scheduler must choose bounded lease durations and generate
cryptographically random tokens with at least 128 bits of entropy. Counter
exhaustion conflicts and requires operational investigation, not silent retirement.

Every disposition checks the exact revision, actor, token, epoch, lane and expiry
inside its transaction and again checks time before replacement. It can release
the lease to a future retry/refresh date and cold/refresh lane, preserving source,
creation time, attempts, epoch and unresolved original status preparation. It
cannot retire a job or clear an original. `CheckLease` is a read snapshot, not a
fence for a later write or a billing/provider operation. An expired or replaced
handle cannot acknowledge, clear or retime a job through this repository.

Transaction errors return no job. An unknown reply requires reading current
durable state under current authority before proceeding; it does not prove
rollback. An acknowledged write is evidence of that transaction only, not proof
that the lease remains live when its caller receives the reply. Services must
check current permission and the persisted lease around each subsequent stage.
The checks establish the conditional-write boundary, not the network commit
instant or atomic fencing of billing's separate capture transaction. Exact billing
inputs, native capture receipts and head CAS remain required after in-flight
lease loss. Long-running operations need an explicit bounded execution policy;
no heartbeat or forced-expiry administrative override is implemented here.

Named encrypted Mongo tests cover acquisition races, stale/replaced/expired
handles, clock changes, overflow, lost acquisition/disposition replies, lane
changes, bootstrap-write bypass refusal and native original-input preservation.
Most storage protocol cases use descriptor fixtures; they do not prove native
discovery, current worker authority or runtime. Native original cases use real
billing preparation but do not implement atomic input/job binding. The fair scheduler and fenced binding services below supply their separate
private boundaries; execution orchestration, runtime and platform qualification
remain required.

## Fair scheduler service

`Scheduler` composes the typed scheduling/execution port with the same bound
native worker authority and local clock used by its execution repository. Its
configuration freezes the actor and native scopes, page limit, independent cold
and refresh acquisition budgets, a lease duration from one second to 15 minutes,
and bounded exponential retry delays. It creates no grant or background loop.
Current global refresh permission requires all scopes configured in that worker
authority; selected checkout or subscription refresh permission is also checked.
Discovery and public status-read permissions remain independent capabilities.

`Scan` checks current global authority around every bounded page read, validates
the stored job/cursor contract, and checks selected authority before examining
each job. Future-due and actively leased jobs advance the scan without consuming
an acquisition attempt. The lane budget counts acquisition attempts, including
known competing claims. When it is spent, the cursor stops at the last examined
job, leaving the untouched suffix reachable. Empty/short pages reset a completed
sweep; a full last page is followed by an empty page that resets it. Repeated
full sweeps revisit late inserts behind the prior cursor.

Each acquisition uses a fresh cryptographically random 32-byte token. Current
authority is checked again after its outcome. A sole native conflict is inspected
under current authority before being treated as contention; changed source or
joined outage errors do not become quiet skips. A known failed acquisition or
exhausted counter advances past the failing source and returns its cause so a
healthy suffix is reachable on the next scan. The job is never retired. Operators
must surface these failures and investigate exhausted/corrupt records. An
uncertain acquisition withholds the batch without advancing its cursor. Current
durable state on the next scan distinguishes an active committed lease from work
that can be attempted again; there is no automatic uncertain-commit replay.

Cursor advancement uses its own acknowledged CAS with no job writes. It is not
atomic with earlier acquisitions. Any cursor/authority/lease-check error returns
no batch; previously acquired jobs can remain held until their lease expires.
The final batch validates exact source/original conservation and checks persisted
leases plus current global/selected authority. It is an advisory stage handoff,
not permission for later unfenced writes or billing capture. Dispatch must check
current permission and the exact lease again around each subsequent stage.

`Check` requires the configured actor's current permission and exact lease.
`Retry` uses the durable acquisition count to choose exponential delay capped by
the configured maximum, releases only that execution and preserves unresolved
originals. Lost release replies require inspecting durable state; the old handle
cannot release again. Confirmed status-cycle completion and rescheduling from
the original observation time remain separate work. No provider calls, captures,
financial completion checks or public routes run through this service.

Named isolated service cases cover dependency/configuration boundaries, source
and original conservation, current authority, corrupt responses, conflict
classification, cursor/lease failure, cancellation and uncertainty. Native cases
verify budget suffix progress, future/active prefixes, full-page wrap, independent
lanes, known failed prefixes, counter reporting, unknown acquisition/cursor
recovery and retry caps using encrypted transactions and actual native grants.
Additional native cases compose actual billing preparation, manager discovery,
atomic queue admission and scheduler acquisition for checkout/subscription inputs;
discovery permission does not confer execution permission. API-service identity
and provider responses remain controlled fixtures. These are package flows, not
actual UMS admission, runtime installation or authenticated platform E2E.

Atomic fenced original/evidence plus active-job handoffs, confirmed recurring
completion, runtime/configuration, migration/retention/clock/capacity/operations,
exact host CI and complete authenticated platform/browser qualification remain
required. Source admission and scheduling alone do not finish issue #14.

## Atomic active status-input binding

`StatusBindingRepository.BindStatus` retains the status outbox stage and the
active job's `OriginalStatus` in one encrypted native transaction. The job source
must be a subscription descriptor; checkout lifecycle jobs use acknowledged
checkout input and require their separate binding operation. Both payment and
pre-payment checkout-derived **status preparations** are supported for a
subscription job. A later first paid fact does not invalidate a still-unresolved
checkout-derived status preparation; the owning manager validates its native
provenance, while the binding joins scope, payer and subscription.

Every bind reads the current job and checks its exact revision, actor, token,
execution epoch, lane and unexpired lease inside the transaction. It refuses
changing an existing original, attaching evidence without a pointer, or repairing
an attached pointer whose original record is missing. The explicit status codec
is reused. A first preparation can attach an already retained exact input,
including its evidence, without resetting that stage. Evidence requires both
the matching active job pointer and original outbox. A different response cannot
replace already retained evidence. Each accepted bind, including an exact replay,
advances job revision and preserves all other execution fields and due times.
Callers must use the returned successor handle; the consumed handle is stale.

The same job guard used by acquisition/disposition serializes job operations.
The outbox row also participates in the transaction and its native revision CAS;
overlap with a standalone outbox writer under a different guard is resolved by
native overlapping-record transaction conflicts. Named native tests cover same
and different evidence across those writers. Runtime execution must use the
binding port for writes, rather than a separate `StatusOutbox` write followed by
an advisory lease check. The standalone outbox remains a current-authority read
and explicitly separate adapter boundary, not an alternative fenced stage.

A local clock is checked again immediately before job replacement. A known late
failure rolls back the input write and job pointer together. This does not claim
fencing at the network commit instant or atomicity with billing capture. Unknown
replies return no payload. Recovery must read the actual job and its exact retained
original under current authority; an advanced revision with unchanged lease fields
alone does not prove which stage committed. Never substitute a newly prepared
observation, repeat provider GET when evidence is already retained, or infer
rollback from denial/cancellation. Replacement acquisition preserves the original.

`StatusBinder` composes that typed port with the owning scheduler and configured
billing validator. It checks current global/selected worker authority, current
live lease and native original provenance before storage, rechecks authority and
provenance after every outcome, and verifies the acknowledged successor against
the current durable lease before returning it. All authority/provenance calls
remain outside retryable callbacks. An observed uncertain write remains joined
with later denial/cancellation. Every error withholds job and input data. Any error after storage was attempted
requires current durable-state inspection before proceeding. A permission or
lost-lease error after an acknowledged write does not mean rollback; it must not
be mislabeled an unknown commit. Native billing validation errors and private
recordstore errors retain their owning cause vocabularies.
Construction creates no identity, grant, migration, provider work or goroutine.

Isolated named cases cover service dependencies, current checks, all storage
outcomes, malformed successor responses and private JSON. Encrypted Mongo cases
use actual native status originals, the configured billing manager, native grants
and instance-bound context. They verify atomic preparation/evidence, immutable
originals, consumed/stale/replaced handles, clock expiry/backward steps, competing
stages, late rollback, joined absence failures, outbox overlap, lost replies,
post-commit revocation/cancellation, replacement-worker reuse and exact owning
capture recovery without another provider lookup. API-service identity/provider
responses remain controlled. Lost replies are injected after real commits; they
are not Mongo server unknown-commit labels. Source admission in these binding
fixtures is explicit fixture setup, not owning discovery or host migration proof.

An opting host explicitly composes this boundary. Complete
execution/recovery orchestration, confirmed recurring observation completion,
original-pointer clearing under confirmed native evidence, retention/cleanup,
actual service-account admission, migration/operations and authenticated full
platform E2E remain required. Financial `WorkComplete` cannot retire status work.


## Atomic active checkout-input binding

`CheckoutBindingRepository.BindCheckout` retains the acknowledged checkout
outbox stage and its active job binding marker in one encrypted native
transaction. The immutable acknowledged intent already lives in
`ScheduledJob.Source.Checkout`; `CheckoutPrepared` records that its exact outbox
input has been bound. No second intent copy or business fingerprint is invented.
The private schema-1 job codec preserves the marker; earlier local records
without that field decode it as false. This is pre-runtime compatibility, not a
qualified deployed migration. A true marker is allowed only on checkout jobs
with execution history. Bootstrap writes cannot fabricate it. Discovery replay,
acquisition and retry conserve it.

Every bind joins the exact acknowledged source and checks the active job
revision, actor, token, epoch, lane and expiry inside the transaction. It rechecks
the local clock before replacing the job. First preparation can insert or reuse
an exact existing outbox, preserving any evidence, and sets the marker. Evidence
requires that marker and the matching prepared outbox. Missing bound input cannot
be repaired as fresh work; changed intent or evidence fails closed. Each accepted
bind consumes job revision while preserving all other execution fields and due
times. Known late failures roll back both writes; lost replies return zero data
and require inspection of current job and exact retained stage before continuing.
The current stage cannot be inferred from revision alone.

`CheckoutBinder` composes this typed repository with the same owning scheduler
and configured billing manager `ValidateCheckoutLifecycle` boundary. It checks
current global/selected authority, native provenance and live lease around the
stage, and verifies the acknowledged successor against durable state. All
permission/provenance calls stay outside retryable callbacks. Every error
withholds data; observed uncertainty stays joined with later denial/cancellation.
An acknowledged write followed by denial or lost lease does not prove rollback
and must not be mislabeled uncertain. Callers must inspect durable state after any
failure once storage was attempted. Provider lookup, billing capture and confirmed
job completion remain separate stages. Construction creates no identity/grants,
provider calls, indexes or background worker.

Named isolated tests cover current service checks, private JSON, dependencies,
malformed successor/input evidence and marker conservation in scheduler
acquisition, final lease response and retry. Native cases use acknowledged billing
intents, current grants/instance-bound worker contexts and encrypted multi-kind
transactions. They cover preparation/evidence, sequential exact replay,
preparation replay without resetting evidence, replacement-worker reuse,
consumed/stale handles, clock changes, joined absence errors, competing stages,
late rollback, older codec shape, bootstrap refusal and release/reacquisition
conservation. A controlled owning checkout capture loses its real committed reply
and recovers the exact retained evidence without another provider lookup.
API-service identity and provider responses remain controlled, and source
admission here is fixture setup. These are package flows, not runtime or full
platform E2E. No financial fact or commission is created by binding.

## Owning execution stages

`CheckoutExecution.Observe` and `StatusExecution.Observe` compose the same
scheduler, atomic binders, private outboxes and configured billing manager.
Constructors derive the owning capability from the outbox validator and create
the binder with that same manager. The caller supplies an exact scheduler lease;
these services create no identity, grant, provider instance or background task.

Every foreign stage is surrounded by current global/selected authority and an
exact persisted lease check. Preparation is bound before provider lookup, and
evidence is bound before native capture. Each acknowledged bind consumes the
job revision; subsequent stages use its successor handle. Failure stops the
sequence and returns zero data. Observed native billing or host storage
uncertainty stays joined with later denial, cancellation or lease loss. Known
commits followed by denial remain known commits. Snapshot checks cannot lock
billing's separate transaction; native receipts/head CAS and post-stage checks
remain required.

Checkout receipt recovery comes before any fresh provider lookup. Only a sole
wrapped owning absence permits continuing; joined absence/outage or unknown
results fail closed. Existing evidence is reused exactly. A native receipt found
before host binding can supply its original evidence without a provider GET,
but cannot override contradictory retained input. A bound marker whose outbox
is missing is unavailable, not an invitation to reconstruct fresh work.

Status execution reuses the active original preparation, including its exact
`RequestedAt` and preparing author, under the current caller's permission.
Payment-backed sources prepare from their retained fact; pre-payment sources
use the native checkout preparation capability. Exact capture of retained
evidence recovers an old receipt even after a later head. A conflict or uncertain
result never authorizes dropping the unresolved original. Conclusive uncaptured
supersession still needs an explicit owning resolution boundary; a generic
conflict is insufficient to advance to a new preparation.

Returned observations contain the confirmed native receipt and current job
handle, all private to in-process orchestration. They are **not** completion
receipts: jobs remain leased, status pointers remain attached, and neither
retirement nor recurring rescheduling occurs in `Observe`. The separate completion
services below perform those transitions. Financial `WorkComplete` cannot retire
lifecycle status work.

Named native cases exercise the complete retained-input/provider/capture stage
sequence, committed lost replies at each write boundary, replacement-worker
recovery, current revocation/cancellation/lease loss, receipt-first recovery,
missing bound inputs, joined absence errors, malformed owning output and old
receipt recovery after a later head. Paid-source and pre-payment trial status
remain separate from creating paid facts. Native billing, configured managers,
current grants, instance-bound worker contexts and encrypted Mongo transactions
are real; provider and API-service identity responses remain controlled. Source
admission is fixture setup. These are package flows, not runtime or platform E2E.

Installation of recurring observation/completion orchestration, conclusive supersession,
runtime/default-off configuration, actual service admission, migration/readiness,
retention/cleanup, capacity/ops and full authenticated platform/browser
qualification remain required.


## Confirmed fenced completion

`CheckoutCompletion` and `StatusCompletionService` reuse their execution service's
configured manager, scheduler and outbox. Before completion, they join the actual
retained original/evidence with the native receipt under current worker authority
and the exact live lease. Checkout confirmation reads the native anchor; status
confirmation replays the exact original capture. Neither confirmation performs a
fresh provider lookup. Missing bound inputs are unavailable and cannot be repaired
as fresh work.

`CompletionRepository` reads the exact job and retained outbox inside one encrypted
job-guard transaction, inserts an immutable private completion record and replaces
the job atomically. It checks revision, actor, token, execution epoch, lane and
expiry, then rechecks the local clock before the final write. Native fingerprints
are opaque references; host storage does not recreate native business evidence.
Permission and provider calls remain outside retryable callbacks.

Confirmed checkout work becomes retired with its bound marker and positive
historical attempt count preserved. Confirmed status work clears the active
original, resets cycle attempts, clears the lease and enters the refresh lane.
The execution epoch remains monotonic. Its first confirmed preparation establishes
a durable cadence anchor. Later cycles retain that anchor and coalesce missed
slots into the first strictly future aligned slot, rather than accumulating delay
or running an unbounded catch-up loop. Due times must remain future at commit.

Each job retains `LastCompletionID`; the immutable record keeps the completed job
snapshot and, for status, the original preparation needed to authenticate its
native receipt later. Acquisition, binding, retry and discovery preserve this
reference and cadence anchor. Bootstrap cannot fabricate them. Old private job
rows default absent fields to empty/zero; this is pre-runtime codec compatibility,
not deployment migration evidence.

Any failed completion returns zero data. Uncertain storage outcomes remain joined
with later denial or cancellation; a known commit followed by denial remains known.
Consumed or expired handles cannot replay completion writes. `InspectLast` reads
the durable reference and rejoins the original/native receipt under current
identity and grants without a live lease, since completion deliberately cleared
it. It performs no provider GET and works after later native heads or a newer
active host cycle. Its historical snapshot grants no execution authority.

Named isolated cadence and native encrypted cases cover atomic transitions,
rollback, committed lost replies, current revocation, stale and expired handles,
missing/changed retained evidence, private codec/ciphertext checks, recurring
cycles, retry-counter reset, anchor conservation and historical receipt inspection
after a later native head. Identity/provider responses and source admission remain
controlled fixtures. These are package flows, not installed runtime/platform E2E.
Conclusive uncaptured supersession, runtime composition and the rollout/operational
qualifications above remain required before merge readiness.

## Retained status resolution and supersession

`StatusOriginalResolver` derives optional native resolution from the same
configured manager used by `StatusExecution` and its outbox. It checks current
scope and selected-source authority plus the exact live lease before and after
foreign operations. Pending resolution preserves the original. Captured
resolution requires actual retained evidence that matches the native receipt;
it returns an observation for confirmed completion without a provider lookup.
Missing bound input or evidence cannot be repaired by a fresh lookup.

Only a validated native superseded result permits `SupersedeStatus`. One
job-guarded encrypted transaction checks the exact current job, lease, fence,
original pointer and actual outbox input; inserts immutable history; clears the
pointer; advances the job revision; and sets `LastSupersessionID`. It preserves
lease, lane, due time, attempts, fence, cadence anchor and completion reference.
The final local clock check refuses expiry or backwards time before job CAS.
Authority checks and native resolution remain outside transaction callbacks.
Supersession does not replenish attempt credit or establish a confirmed cadence.

History uses kind `partners_lifecycle_status_supersession`, immutable revision 1,
state `superseded`, no sequence and no expiration. Its opaque ID hashes
`["partners.lifecycle.status-supersession.v1", scope, sourceKind, sourceID,
originalCaptureID]`; its partition uses the lifecycle scope tuple. Private schema
1 retains the complete original input and native newer receipt, including the
owning revision and fingerprint, without recreating billing identities. The
optional job reference is compatible with earlier private schema-1 records;
missing fields decode empty. It is conserved by subsequent execution stages.
This codec compatibility does not certify a deployed upgrade.

A failed write or disclosure requires inspecting the current job and immutable
history under current authority. A consumed old handle must not be replayed.
`InspectLast` rejoins the actual outbox and native resolution without requiring a
live lease or performing a provider lookup. The native head may advance further,
but the original must remain conclusively superseded. A historical job snapshot
is never permission to execute; obtain and check a current lease before resuming.
Missing referenced history fails unavailable. Retained rows still need qualified
capacity, reconciliation and safe cleanup; no TTL is introduced.

These services are composed explicitly by an opting host.
Operational admission, migration/readiness, operations,
exact host CI and full authenticated customer/admin platform flows remain
required. Native Mongo tests use controlled source admission, identity and
provider responses; injected lost replies follow real successful transactions,
not Mongo server commit labels or production provider traffic.

## Bounded lifecycle worker pass

`Worker.RunOnce` composes discovery, scheduler, checkout/status execution,
confirmed completion and retained-original resolution over the same configured
manager, worker authority, scheduler and local clock. Its constructor starts
nothing and requires the optional native resolver; subscription originals must
not silently bypass unavailable resolution. Shared injected pointers are compared
by instance, including pointers whose underlying services contain maps or slices.
Noncomparable value adapters cannot establish this composition invariant.

Each pass admits one bounded owning page for each source kind and configured
scope, then scans one bounded cold and refresh page with independent budgets.
Scope start rotates after each admitted pass attempt so a failed scope cannot
permanently monopolize the first position. An overlapping pass fails promptly;
the server integration supplies one sequential, cancellable pass loop and timeout.
Native preparation/readiness and draining older writers are explicit operational
prerequisites, never automatic worker preparation.

Pending originals progress through `Observe` with their exact retained
preparation; evidence already retained avoids another lookup. Captured originals
complete from exact receipt/evidence. Supersession occurs at most once per job
per pass, then the acknowledged successor may prepare and observe fresh work.
Existing completion and supersession references are inspected under current
native authority before new status work, so a crash after pointer clearing cannot
turn missing history into fresh authorization. Checkout completion always retires
its job atomically; retired jobs are not recurring status jobs.

Any observed native or store uncertainty stops the entire pass without retry or
new provider work. A discovery checkpoint, stage or completion may already have
committed despite a withheld acknowledgement. The next pass reads current state;
held acquisitions await lease expiry. Known failures may use bounded backoff only
after `Scheduler.Inspect` reads the actual job under current global and selected
authority. Binding can have advanced the revision within the same execution;
recovery requires exact source, actor/token/fence/expiry, conserved counters and
schedule, actual retained outbox provenance and relevant immutable history before
checking and releasing that exact current handle. Changed or cleared/expired
leases authorize no retry. Missing bound inputs are not repaired.

Every error withholds all report counts, including otherwise acknowledged earlier
work or successful retry disposition. Errors describe the need for attention;
they do not imply rollback. Final current global and attempted-source permission
is checked before report disclosure, preserving observed uncertainty after late
denial. Reports contain no source IDs, destinations or provider payloads.

Native worker tests prepare owning discovery explicitly and exercise both source
kinds across passes, retained original states, committed lost replies, replacement
leases, known provider failures after binding revisions, final authority and
constructor conservation. Source admission now comes from native discovery in
these worker flows; provider responses and active API-service identity remain
controlled. These tests do not prove
deployment, migration/readiness qualification, operational retention or
authenticated full-platform journeys. Actual service identity/current native
grants, default-off settings, startup readiness, restart, deadlines and shutdown
drain have separate server tests; empty startup discovery makes no provider calls.

## Explicit bounded preparation orchestration

`Preparation` composes the native `billing.RevenueService.PrepareLifecycleDiscovery`
port with separate current preparation authority. It owns no projections, BSON,
transactions or duplicate progress state. Constructor copies explicit scopes,
validates page size 1..200, total pages 1..10000 and timeout 1s..1h, and starts
nothing. One invocation processes scopes round-robin under its deadline and
rejects same-instance overlapping invocation promptly.

Current global and selected preparation permission applies before and after every
owning page, including errors. Late denial/cancellation preserves all owning
error causes, including uncertainty. Ordinary failures withhold the whole report;
standalone budget exhaustion returns only authorized bounded progress and never
`Complete=true`. A subsequent invocation resumes native state; no reset or
assumption of rollback follows a withheld acknowledgement. Native epoch restarts
are counted and bounded by the same page budget, not described as legacy-writer
drain proof. Completion is disclosed only after final global permission.

The host provides an explicit operator command for preparation; ordinary server
startup must not run it automatically. The distinct
`partneraccess.LifecyclePreparation` capability does not reuse discovery/read/
refresh grants. Its service identity and current account-admission checks are the
same as those required by recurring work. Before preparation, operators must
attest old-writer drain, database/schema version, backup and retention readiness.
Package tests do not establish those operational conditions.

## Host adoption

`RuntimeConfig` supplies the explicit service actor, provider/account/live-mode
scopes and bounded scheduling limits. `Validate` checks all required inputs;
`ValidBounds` also supports checking supplied limits while the feature is disabled.
The host owns enablement, environment parsing, startup order and cancellation.

Compose the execution repository, current worker authority and owning billing
manager against the same prepared encrypted substrate and exact clock instance.
Constructors start no goroutines and create no service accounts or grants. Run one
bounded worker pass at a time and drain it before closing borrowed database pools.
Retain the original input, lease fences and recovery receipts across restart.

The durable `partners_lifecycle_*` kinds and `partners.lifecycle.*.v1` hash domains
are storage identifiers, independent of the Go import path. This package uses
those identifiers directly; it does not ship legacy dual reads or compatibility
copies. Hosts changing storage identifiers need an explicit reviewed data migration.

Run the native encrypted transaction suite with `GHATD_TEST_MONGO_URI` pointing
to an isolated replica set. Each case creates and drops its own test database;
never point fixture tools at a production database. Controlled provider responses
and service identities do not certify authenticated provider or production flows.
