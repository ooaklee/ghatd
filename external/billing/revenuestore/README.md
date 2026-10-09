# Verified billing revenue persistence

`revenuestore` implements billing's `RevenueRepository` over encrypted shared
recordstore transactions. The billing owning service accepts authenticated
provider facts and validates economic identities. This adapter never treats an
access grant, checkout, invoice total or current catalogue price as paid revenue.

Construct it from a prepared transaction-capable store and pass it to
`billing.NewRevenueService` with an explicit clock. Verified acceptance commits
facts, their monotonic sequence head and the delivery observation together.
Economic identities include provider account and live/test scope independently
of the delivery envelope. Duplicate deliveries add observations rather than
duplicating an economic fact; changed economics under the same identity conflict.
Quarantined observations persist their bounded reason without raw provider bodies.

Facts and observations are immutable. Internal economic fingerprints and worker
actors use explicit encrypted persistence envelopes even though public JSON
excludes those fields. Indexed record metadata contains no payer email or private
provider reference.

Pending reads examine facts and one consumer's acknowledgements in one snapshot.
Acknowledgement requires the durable owning decision ID and outcome. A later
acknowledgement cannot skip an earlier pending fact or one accepted late with an
earlier paid timestamp; each consumer advances independently. Read results are
bounded to 1–200 pending facts, but inputs are complete indexed history rather
than a silently truncated scan. Hosts supply operational deadlines and plan
projections for large histories.

Atomic failure leaves no partial observation/fact/sequence writes. An uncertain
commit replays the original economic/envelope identity. Provider parsing,
authenticated lookup/reconciliation, commission decisions and host workers are
separate owning capabilities; constructing this adapter does not enable them.
Retain the shared records and encryption keys on restore or rollback.

Source quarantine recovery uses an immutable resolution observation referencing
the original quarantine. Recovered facts, sequence allocation and resolution
commit atomically; current source decisions never overwrite history. Explicit
encrypted persistence envelopes retain private source fingerprints and
reconciliation actor identity excluded from public JSON. Pending source
observations are separate from consumer fact acknowledgements. Original payment
lookups collect complete owning history and isolate provider/account/mode;
future indexed projections may optimize this without returning partial data.

The encrypted observation envelope optionally retains the private
`RecoveryFingerprint` alongside the original `SourceFingerprint`. Existing
rows without this field continue to decode. A present recovery fingerprint is
bounded, trimmed and belongs only to a resolution; it is excluded from public
JSON and participates in the immutable owning resolution hash. No new index or
rewrite of existing quarantine observations is required.

`PendingRevenueFactsAfter` pages pending facts by durable acceptance sequence;
`UnresolvedRevenueObservationsAfter` pages unresolved sources lexically by
retained ID. These read positions are sweep positions, not acknowledgement or
resolution checkpoints. Workers can inspect later items while earlier failures
remain pending. Restart each complete sweep at zero/empty ID so newly inserted
sources below a prior read position are also visited. The compatibility methods
retain their existing first-page/accepted-time ordering. Hosts still own durable
attempt scheduling, backoff, worker authority and restart orchestration.

## Complete confirmed-source reporting

The same repository optionally supplies `billing.RevenueHistoryRepository`.
`ReadRevenueHistory` reads the economic head, accepted facts and all reception
and quarantine-resolution receipts in one owning snapshot. It pages the existing
encrypted global partitions with a combined 10,000 fact/receipt budget and an
extra-row capacity probe; filters do not reduce that budget. No extra projection,
financial write, schema or TTL is added. Late page failures, cancellation,
nonexecuted callbacks or malformed record metadata return no partial result.
The billing owner checks sequence completeness, canonical provenance and net
economics; handlers must use that owner rather than interpreting adapter rows.

## Checkout history

The same repository implements `billing.CheckoutRepository` for immutable
pre-submission intents, session acknowledgements and historical subscription
price associations. Requests and fingerprints excluded from public JSON are
retained explicitly inside encrypted persistence envelopes. Indexed record IDs
are opaque digests; personal contacts and provider identifiers are not indexed
in plaintext. Scope guards serialize competing linkage commands. A session can
belong to only one intent within merchant/live scope, and a subscription cannot
change its original paying principal through another checkout association.

Linkage commits the session reservation, acknowledgement, subscription principal
and original price mapping together. Ambiguous transaction completion returns
`ErrRevenueUncertain`; the owning service recovers the same immutable result.
No record is deleted or marked failed because a provider response was lost.
Use the owning `billing.CheckoutService` rather than calling adapter writes from
handlers. Explicit legacy/portal history remediation is not supplied here.

## Subscription lifecycle evidence

The same repository optionally implements `billing.SubscriptionStatusRepository`.
Opaque provider/account/live-mode/subscription identities use independent guards;
lifecycle captures do not allocate an economic sequence or change billing facts.
An explicit encrypted envelope retains private preparation, original author,
billing source fingerprint, expected head revision/fingerprint, lifecycle state,
scheduled cancellation and request/observation times. No raw provider body or
email is retained or indexed. Head and immutable receipt commit together and
remain persistent; lifecycle evidence is not an ephemeral analytics record.

Joined reads use one owning snapshot. Missing head is normal first absence;
missing linked receipts, inconsistent revisions, changed scope, expiry metadata
or corrupt evidence fail without a partial result. Capture writes are fenced
to the bound subscription and use head revision CAS. Use `billing.RevenueService`
for provenance, chronology, original-receipt recovery and freshness rules; this
adapter supplies no status eligibility, provider lookup or scheduler. Preserve
these records and their encryption keys in backup, restore and rollback.
Each refresh retains an immutable capture. Hosts must budget for that growth
and explicitly review retention/privacy policy; raw-analytics TTL is not applied
to lifecycle receipts automatically.

## Native lifecycle source records

New subscription checkout acknowledgements atomically retain an encrypted
source record under an opaque scope partition. Payment-mode and unacknowledged
intents do not create subscription candidates. The original intent fingerprint
and acknowledged session stay private.

New accepted subscription payments with authenticated customer evidence,
checkout associations and pre-payment lifecycle anchors share a native
subscription owner record. Financial and checkout transaction guards touch the
same record identity, so uniqueness and revision CAS prevent contradictory
payer/customer ownership from committing across those guards. Renewals and
later same-owner checkouts preserve the first payment and lifecycle pointers;
adding the other source type advances only the projection revision. Source
records do not allocate revenue, create a status observation or reset freshness.

Legacy payment facts without provider customer evidence remain financial
records and do not become authenticated lifecycle candidates. Existing retained
checkout ownership is still checked while adding a projection. These additive
records support the gated owning discovery read and explicit bounded preparation
described below. They do not install a collector or recurring refresh scheduling.
Older original receipts are not rewritten or silently reconstructed on replay.
Owning preparation must explicitly establish native history coverage before
discovery; hosts must not query these adapter records directly or treat an empty
projection as no subscriptions.

### Gated indexed source reads

`ReadLifecycleDiscovery` implements the optional billing-owned discovery port.
One `recordstore.Query` selects the source kind and opaque scope partition,
sorts by ID, and limits input to 1–200 rows using the existing partition index.
It never substitutes `ReadRevenueHistory` or `FindAll` for this bounded scan.
Original source pointers and joined receipt/intent/payment evidence, including
any paid checkout owner's original acknowledgement/session reservation, are
read in one snapshot; native billing validates their business provenance. Repeated
read callbacks reset both their page position and result, while late errors
return no partially joined page.

The read requires an immutable versioned scope-preparation record before
querying projections. Absence is `ErrLifecycleDiscoveryUnprepared`; invalid
metadata, authenticated decoding failure or missing joins is unavailable with
diagnostic causes retained. No read creates or repairs preparation. Normal read
fixtures now invoke owning preparation; dedicated replica-set cases restore
original native codecs without projections and verify reconstruction, resumable
progress, rollback, uncertain replies and concurrent completion fences. These
controlled native checks do not certify a host deployment or backup migration.

### Owning preparation transaction

`WithLifecyclePreparation` lends a narrow typed transaction to the billing
service. Each call reads one bounded original-source or projection-validation
page. Original global partitions require a bounded upgrade sweep; this is never
substituted for ordinary indexed scope discovery. Canonical business validation
belongs to billing, while this adapter owns encrypted codecs, original joins,
projection persistence, progress CAS and readiness admission.

Current native source writes also CAS an opaque per-scope source epoch in their
original transaction, across financial and checkout guards. Completion writes
that same epoch, progress and the immutable schema-1 preparation record together.
A concurrent source therefore conflicts or makes the next call reset its sweep;
a read of the epoch followed only by a separate marker write would not suffice.
Projection reconstruction alone does not advance the source epoch. The shared
epoch adds per-scope write contention; native transaction retries/failures remain
explicit, without treating a conclusive conflict as an uncertain commit.

Drain older writer instances before preparation, and exclude direct database
writes. Older software does not update this fence. Continuous current writes may
require a controlled quiet period to complete a sweep. Missing/corrupt original
records and owner conflicts must be remediated explicitly; preparation never
skips such evidence or rewrites original financial records. Host orchestration,
authority, upgrade/restore qualification and recurring collection remain required.
See [billing preparation](../README.md#explicit-bounded-preparation) for the owning
operation and the meaning of its private progress counts.


#### Acknowledged-checkout preparation joins

The native checkout transaction implements `CheckoutAcknowledgementTx`.
`ValidateCheckoutAcknowledgement` joins the retained original intent, forward
acknowledgement and scope/session reverse owner in the same owning snapshot.
Missing joined records are unavailable, and contradictory owner/session or
changed original creation/fingerprint conflicts. This join performs no writes.
New lifecycle preparation and per-intent receipt recovery require it; native
lookup/capture and anchor validation also use it. Receipt recovery joins the
first anchor and its original receipt rather than treating a later receipt as
an independent replacement. Existing encrypted receipt codecs/hashes remain
compatible. These joins do not prepare source-discovery coverage or install a
manager, host worker, recovery outbox or refresh schedule.


### Original status resolution snapshot

The optional `ReadSubscriptionStatusOriginal` capability reads the exact immutable
capture first, then joins current head/receipt only if the capture is conclusively
absent, in one encrypted `recordstore.Read`. It has no writes, provider calls or
financial sequence effects. Existing captured originals do not depend on later
head availability. Joined absence/outage and uncertain errors remain failures;
missing linked current receipts are unavailable rather than first absence.
The billing service validates provenance and decides pending/superseded; the
repository does not clear host execution state or choose refresh policy.

### Original session status reads

The native checkout transaction implements optional `billing.CheckoutSessionTx`
using the existing encrypted reverse-session reservation. The owning service
validates its joined intent and original forward acknowledgement before returning
a result. This read adds no document kind, migration or index, and does not write
checkout, revenue or lifecycle records. Missing or inconsistent joins fail closed.
See [original checkout status](../README.md#original-acknowledged-checkout-status).
