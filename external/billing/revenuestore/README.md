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
