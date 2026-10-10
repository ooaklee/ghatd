# Partner persistence adapters

`partnerstore` implements the program, referral and earnings typed repository
ports over `repository/recordstore`. It owns schema identities, encryption
envelopes, unique references, transaction guards and compare-and-swap writes.
Domain services own eligibility, policy precedence, balances and transitions.
The adapters do not query another domain's repository or call a payment provider.

Prepare one caller-owned Mongo database and an explicit stable payload cipher
through `recordstore.NewMongoStoreFromDatabase`. Run its additive `EnsureIndexes`
and actual read/write/commit `Probe` before admitting financial mutations. A
transaction-capable replica set is required; there is no standalone-Mongo or
nontransactional fallback. Hosts register these preparations in their additive
migration/startup lifecycle and retain records, guards and keys on rollback.

Program enrollment commits its participant, immutable revision and unique
customer reference together. Policy and destination publication advance a scope
or customer head in the same transaction as an immutable version. Status updates
commit their new head and immutable audit snapshot together.

Referral creation commits the active-link head, link/audit records and reserved
code together. Retired codes remain reserved. Signup correction appends history
and advances the customer head conditionally. Economic payment bindings are
immutable and globally unique within the program.

`WithLinkTransaction` uses the same partner guard as issuance and retirement.
Its bound adapter commits the original retirement/audit, globally reserved new
code, replacement/audit, active head and immutable `partner_referral_link_rotation`
receipt together. The storage transaction may touch the global code index while
its serialization guard remains partner-scoped; unique record identity also
rejects competing partners reserving one code. Nested guards and receipt writes
outside the bound transaction are rejected. Receipts explicitly retain the
verified actor in their encrypted envelope, have no expiry, and replay their
frozen result rather than whichever link is currently active. They use the
existing record indexes and require no backfill of prior links.

`ReferralRepository.WithAttributionTransaction` uses the same customer guard as
correction CAS. Its callback reads and writes through a bound store, including
historical payment selection and original-binding replay. Nested owning
transactions and another customer's writes are rejected. Payment uniqueness is
still global; customer guard scope does not permit two payers to claim one
economic payment identity. Correction time is sampled inside this transaction
and cannot precede the prior recorded cutover. It is not a commit timestamp.
Correction revisions retain private original-request receipts in an explicit
encrypted envelope, independently of the domain's JSON omission. Payment
records project their customer into the indexed state selector for complete
customer-scoped binding history; economic payment identity remains global.
These new referral schemas are introduced together and require no legacy
referral backfill. A host must install the shared indexes before admission.

Every ownership revision also retains an immutable partner/customer relationship
reference before advancing the head in that same transaction. Unique record
identity enforces one lifetime reference; reacquisition preserves the first
owned revision. `ListRelationshipSnapshots` reads a bounded partner-membership
page, its current heads and complete selected customer histories in one owning
read transaction. Thus a concurrent correction cannot mix an earlier membership
page with a later head. Former owners remain discoverable, and failed reads
discard all accumulated results. Domain services validate ownership-chain and
first-reference provenance before projecting a customer's permitted periods.

Earnings construction pins one program/currency. All competing financial
commands for a partner/currency share one durable guard, and callbacks receive
only the transaction-bound repository. Journal sequence allocation, source
anchors, claim head, immutable claim revision and idempotency receipts share the
same transaction. Complete financial history is read through bounded pages;
public pagination limits must never silently truncate balance inputs. Result
accumulators reset on transaction retries.

Customer cancellation uses the existing encrypted `partner_earnings_receipt`
kind and ledger guard. Its release, claim head, immutable audit and original-key
receipt commit or roll back together; no new index or backfill is required.
All financial receipt reads require the canonical kind/ID/partition, immutable
revision 1, and no sequence, state or expiry metadata. Financial receipts are
retained evidence and must not enter ephemeral-record cleanup.

The required earnings maturity port retains one encrypted source in a global
program/currency partition for bounded pending-state discovery. Source insert
and completion use the same transaction and partner-ledger guard as their
accrual/maturity entries, even though their query partition differs. The generic
store's transaction spans those writes; the partition is an index scope, not a
second global guard. Different partners keep distinct ledger guards and source
documents. The adapter computes no deadlines, eligibility or receipt acceptance.
Completion fences revision 1 to 2 and unchanged immutable input; the source and
actual receipt remain indefinitely. Decoding rejects wrong scope, state,
revision, sequence or expiry. No financial maturity source receives TTL.

An aborted transaction leaves no partial financial or audit writes. An uncertain
commit must be reconciled with the original idempotency identity; it must not
trigger a new transfer or a fresh request key. Native operational causes remain
available internally, and expected absence is never inferred from a joined
absence-plus-outage error. Public handlers must redact driver diagnostics and
mask destination/provider references for their audience.

Real datastore tests require `GHATD_TEST_MONGO_URI` pointing to an isolated
replica set. They create uniquely named databases and drop only their own
fixtures. Production migration, restore, provider evidence and host UI acceptance
are separate from these repository tests.

Native financial tests also overlap a claim request or processing assignment
with a full refund of its backing payment. They verify coherent cancellation or
retained in-flight reservation, one reversal and unchanged replay. Cumulative
split-refund cases retain zero-debit receipt anchors and never reverse more than
the original rounded commission.

## Complete relationship reporting snapshot

`ReadRelationshipEvidence` reads every selected-partner lifetime membership,
current head, complete ownership history and immutable customer binding in ONE
native read. It uses the existing encrypted ownership/binding schemas and indexed
customer binding projection; no new schema or financial write is introduced.
The combined 10,000-record capacity includes membership, head, revision and
binding rows before filtering. An extra-row probe refuses truncation, callback
reentry resets all accumulated results, and any late failure discards them.
Expected head absence, malformed metadata, expiry on durable evidence and
underlying encryption/driver failures remain unavailable with retained internal
causes. Cancellation and explicit capacity stay distinct. The owning
[referral service](../referral/README.md#complete-relationship-evidence) validates
chains, frozen terms and canonical attribution fingerprints before reporting.

## Visit and analytics snapshots

`WithVisitTransaction` binds one link's partition. It atomically writes an
observation, anonymous UTC-day classification counts and the unique keyed-digest
first-visit receipt. Concurrent reloads reuse that origin. Nested guards and
foreign-partition writes are rejected. Lost acknowledgements preserve
`ErrUncertain`; another request records a duplicate rather than another eligible
visit. All payloads use the same encrypted records. Signed cookies, raw nonces,
IPs and user-agent strings are absent from the observation path.

`RecordClick` requires an explicit approved absolute expiry and indexes its UTC
day in `State`. Visit receipts use their authenticated nonce expiry. The generic
store rounds expiry upward to BSON millisecond precision and authenticates it;
its single-field partial TTL index removes these records asynchronously. Receipt
expiry is still enforced by business code before observation, independently of
cleanup. Anonymous day counts have conditional revisions and no expiry.

`ReadAnalyticsSnapshot` accepts the owning query and reads complete selected
links, day counts and lifetime memberships/histories in one transaction. Only
queries cutting UTC days load complete indexed raw boundary-day rows. Ownership,
day buckets and raw rows each have independent 10,000-record budgets; an extra
row returns `referral.ErrCapacity`. Immutable ownership records have storage
revision 1 and increasing domain revision in their payload. Any query, decoding
or cancellation failure discards earlier scopes; repeated callbacks reset output.
The service checks boundary completeness and computes global totals before link
pagination. Physical cleanup cannot change its persistent snapshot revision.

The host must approve explicit collection retention, consent and abuse admission
before enabling measurement. Financial, signup and ownership evidence never
receive analytics expiry. See [analytics semantics](../referral/README.md#referral-analytics).

`ReadConversionSnapshot` reuses the traffic/history walker and immutable binding
walker within ONE native read. A shared 10,000-record budget includes links,
memberships, heads, revisions and bindings; independent day and raw budgets
remain unchanged. Complete bindings use the same indexed customer selector as
relationship evidence. Callback reentry resets every result and budget; late
failure, skipped callbacks and late cancellation cannot return partial traffic
or fabricated empty evidence. Underlying availability causes are retained, with
capacity and cancellation distinct. No new schema or business write is needed.
See [combined evidence](../referral/README.md#combined-conversion-evidence) and
[original cohort semantics](../partnermanager/README.md#original-cohort-paid-conversions).

## Workflow discovery and decisions

`NewWorkRepository` supplies the `partnermanager.WorkRepository` port over the
same prepared store. Bounded page insertion, discovery cursor revision and page
receipt commit together. Jobs retain immutable source identity/fingerprint and
the original financial maturity deadline in explicit encrypted envelopes. Page
receipts and replay comparisons include that deadline; future obligations
survive discovery advancement and reset. Initial scheduling is supplied by
the owning queue, while lease/retry writes preserve the original deadline and
reject a next-attempt time before it. No maturity source or job receives TTL.
Jobs also retain
independent due time, attempt count, fencing token and decision. Discovery does
not acknowledge signup or revenue sources. Later input remains discoverable
while earlier queued failures stay pending.

Lease, decision, retry and completion mutations use scope guards and revisioned
records. A decision is preserved during retry; completion cannot occur without
its exact committed receipt. Expired/superseded leases cannot mutate another
worker's state. Ambiguous commits return `ErrWorkUncertain` for owning replay.
Private source and actor fields excluded from public JSON are retained inside
encrypted persistence envelopes. The adapter does not establish financial
entitlement, execute provider calls or install a worker loop.

`ReadWorkSnapshot` implements the optional work-reporting port. One read
transaction loads the four kind/partition/pending-state selectors and their
discovery cursors. Complete bounded pages share a 10,000-pending-row budget;
an extra row returns `partnermanager.ErrReportCapacity`. Completed history does
not consume this budget. Missing cursors are zero only when the corresponding
pending scope is empty; joined absence/outage is never interpreted as absence.
Pending work and cursors cannot have expiry metadata. Query order, scope and
payload checks fail closed, and repeated callbacks reset both output and budget.
See [backlog semantics](../partnermanager/README.md#worker-backlog-reporting)
for scheduling subsets, privacy, authorization and coverage limitations.
