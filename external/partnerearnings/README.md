# Partner Earnings

`external/partnerearnings` is an opt-in package: it is wired up by a host only
when a partner program with a commission ledger is needed; it is not enabled by
default host functionality. It owns the single financial ledger for a partner
program: commission accrual and maturity, refund reversals, payout claims and
manual payment recording. It follows the blueprint service/repository separation
without adding handlers, routes or fenders that its consumers do not need.

## Composition

The repository implementation lives in `external/partnerstore`; the service and
types (including `Config`) live here.

```go
// store is a prepared recordstore.Store; config contains approved host terms.
config := partnerearnings.Config{ProgramID: programID, Currency: approvedCurrency}
repo, err := partnerstore.NewEarningsRepository(store, config)
if err != nil {
    return err
}
svc, err := partnerearnings.NewService(repo, clock, idGenerator, config)
if err != nil {
    return err
}
```

The `recordstore.Store` passed to `partnerstore.NewEarningsRepository` is the
existing managed store; its client, connection lifetime and configuration remain
host-owned. The repository adapter obtains a write guard per program, partner and currency
through the store transaction and owns safe callback retry. `partnerearnings`
never issues datastore queries itself. Custom persistence adapters implement the driver-free
`Repository` port; there is no silent in-memory fallback.

Run the host-owned migration that creates the ledger's indexes and uniqueness
constraints (entries, claims and receipts) before the first write. There are no
implicit migrations, collection drops or index repairs.

## Responsibilities and identity

- `Service` computes every rule itself: commission, maturity, availability,
  claim transitions and refund recomputation. It never reads another domain's
  store.
- `Repository` owns schemas, deterministic keys, atomic writes and the write
  guard; datastore types stay out of service requests/ports.
- Actor identity is bound from verified context, never decoded from a transport
  payload. `RecordPaymentRequest.ActorID` and the recorded time are server-bound.

The ledger is scoped by program, partner and currency. There is exactly one
currency per program; a source event or claim in any other currency fails closed
with `ErrCurrencyMismatch`. The ledger never converts currencies.

## Accounting model

The journal is append-only. Balances are derived from journal entries plus claim
states; they are never stored as running totals.

- Pending (`P`) holds accrued commission still inside its hold. It is reported
  separately and cannot be claimed.
- Matched (`M`) is the net matured balance: matured commission minus refund/
  dispute-loss reversals and payouts, plus returned payouts. It may be negative.
- Reserved (`R`) is the sum of claims in `requested`/`processing`; review hold
  (`H`) is the sum of claims in `needs_review`.
- Dispute hold (`D`) freezes matured credit; pending dispute holds are reported
  separately and never reduce unrelated matured credit.
- Available is `max(0, M - R - H - D)`. Debt is `max(0, -M)`.

Journal kinds (append-only): `accrued`, `matured`, `reversed`, `allocated`,
`allocation-released`, `allocation-settled`, `paid`, `returned`,
`payment-observed` and dispute hold/release/won/lost/decision entries.

A claim reserves matured funds by allocating them against specific payments,
oldest available credit first, in partial portions. Concurrent claims cannot
spend the same funds: allocation and availability are recomputed inside one
`WithTransaction` under the repository adapter’s write guard. Paying a claim settles its
reservation into one `paid` debit; the payout's payment metadata is recorded on
the claim.

Refunds are cumulative: `ReversalRequest.CumulativeRefundedMinor` is the total
eligible refund in payment units after the event, so split refunds yield the
same cumulative reversal as one refund. The reversal is computed as
`roundHalfUp(commission * cumulativeRefunded / payment)` capped at the original
commission, using overflow-safe integer math.

## Commands

- `Accrue`: books one commission. Idempotent by payment id; an exact replay
  returns the original entry, a replay with any changed frozen field (amount,
  rate, hold, provider-effective timestamp, referral, terms, policy or source
  metadata) conflicts. A hold-free commission matures in the same transaction.
- `Mature`: journals the explicit maturity movement for every pending accrual
  whose hold has elapsed. Idempotent per payment.
- `Reverse`: books one refund. A refund received before its payment is
  `ErrUnresolved` and must be replayed once the accrual exists. Idempotent by
  refund id: an exact replay returns the original reversal, a replay with a
  changed payload conflicts, and a reversal whose delta rounds to zero (or that
  reports lower cumulative evidence than already recorded) still journals a
  zero-debit observation so the event stays durably anchored. Reversing a
  matured credit atomically releases reservations of affected `requested` claims
  and flags affected `processing`/`needs_review` claims for review.
- `Balances`: derives `PendingMinor`, `MatchedMinor`, `ReservedMinor`,
  `ReviewHoldMinor`, `AvailableMinor`, `DebtMinor`, `PaidOutMinor`.
- `RequestClaim`: reserves matured funds. Idempotent by
  (actor, use case, partner, currency, key): the same request replays the
  original claim, a changed payload conflicts. Requires an idempotency key and a
  destination snapshot.
- `GetClaim` / `ListClaims`: read claims. `ListClaims` with empty states returns
  all states; empty partner returns all partners (an administrator queue);
  `limit <= 0` means no limit.
- `DecideClaim`: operator queue transitions among `requested`, `processing`,
  `needs_review`, `rejected`, `cancelled`. It can never reach `paid`. Rejecting
  or cancelling a `processing`/`needs_review` claim requires `ConfirmedUnsent`
  (the transfer may already be in flight). A claim already owned by another
  actor conflicts. `processing`/`needs_review` retains its reservation and flags
  review. `ExpectedRevision`, when non-zero, is an optimistic-concurrency
  precondition (`ErrStaleWrite`).
- `RecordPayment`: the single privileged debit that pays a claim. Idempotent by
  (actor, use case, partner, currency, key) with a fingerprint binding the claim
  identity and its frozen amount/currency, so the same key cannot replay a
  different claim; a changed payment payload conflicts. Metadata is bounded,
  plain, and the paid date is not in the future. Only `RecordPayment` may
  transition a claim to `paid`. `ExpectedRevision` is a precondition on the fresh
  write.
- `AmendPayment`: corrects a paid claim's method, reference or date with a
  required reason. It appends an amendment and never creates a second debit.
  `ExpectedRevision` is a precondition.

All mutating commands that act on a verified actor require a non-empty
`ActorID`, which is server-bound (`json:"-"`), never trusted from a payload.

## Durable maturity source evidence

`Repository` includes the required `MaturityRepository` port. `Accrue` inserts
one immutable source in the same guarded transaction as its journal entry.
`Mature` commits source completion and the original gross maturity entry
together. Hold-free accrual is completed in that transaction and never appears
in the pending feed. Source identity freezes program, currency, partner and
payment; its fingerprint also binds the original accrued entry, order, amount,
deadline and recording time. Refunds, holds, retries and completion cannot change
that input. Zero-rate and fully reversed accruals still require journal work.

`PendingMaturitySourcesAfter` selects up to 200 indexed pending sources by
opaque ID. `GetMaturitySource` re-fetches source and scope inside a complete
validated owning financial snapshot. A selected pending source may complete
before that refresh; discovery then returns its unchanged input and the verified
completed receipt. Completion does not discard the source. Both APIs expose
private service evidence: source JSON omits every field, and hosts must never
return the evidence as customer data. The adapter retains it in encrypted
storage with no expiry.

Missing required sources fail accrual replay or maturity; contradictory
identity, deadline or receipt fails validation. An absent source or empty feed
does not prove no entitlement or a complete ledger. Install the shared indexes
and run host reconciliation/preflight before admission. These are new schemas
introduced with this package, not a backfill of previously shipped records.

Discovery is bounded by source count, but each selected source currently reads
and validates its complete partner history, with the same financial history
capacity limits as reports. Allow for that cost when pacing the worker. This
source port does not schedule, lease or execute work automatically. The optional
[owning worker](../partnermanager/README.md#owning-signup-revenue-and-maturity-worker)
can retain deadlines and recover accepted receipts through a durable queue;
a host must wire scheduling and current scoped authority independently of
customer views. A lost transaction acknowledgement is recovered using the original
accrual identity or retained accepted maturity receipt, without a second journal
movement. Maturity does not release a dispute hold or initiate a payout.

## Errors

Typed outcomes include `ErrNotFound`, `ErrInvalid`, `ErrDenied`,
`ErrUnavailable`, `ErrUncertain`, `ErrConflict`, `ErrStaleWrite`,
`ErrInsufficient`, `ErrInvalidState`, `ErrDuplicateEvent`, `ErrUnresolved` and
`ErrCurrencyMismatch` and reporting capacity `ErrReportTooLarge`. `ErrUncertain` is produced by the host adapter when a
commit outcome cannot be confirmed; retry the same identity, never invent a new
one. Refund and payment replays return their original result rather than
`ErrDuplicateEvent`; that sentinel is reserved for adapters that journal a
source event directly.

## Testing

`service_test.go` covers rate math (exact half-up, bounds, overflow), accrual
and maturity idempotency, frozen-field fingerprint conflicts, cumulative refund
reversal with replay/conflict/zero-delta anchoring, the claim lifecycle with
oldest-first allocation and reservation release-once, idempotent claim creation
and manual payment (replay vs changed-payload and cross-claim conflict),
`ExpectedRevision` preconditions, audit provenance, overflow-safe derivation,
driver-callback retry isolation, and the concurrent claims no-double-spend
guard. A `fakeRepository` implements `Repository` with a
transactional memory store so the service's guarantees are tested without a
database.

## Financial evidence and recovery

Positive expected claim revisions are mandatory for decisions, manual recording,
amendments and returned transfers. Payment/amendment receipts bind the original
revision and full canonical payload; exact stale-revision replay reads its
receipt first, while changed payload conflicts. Destination snapshots use
canonical JSON so delimiter-bearing values cannot alias a different map.
Joined absence/unavailability is an error and never permission to write. A
manual recorder must own the claim's processing assignment.

Only a full exact-amount, exact-currency observation settles a manual claim.
Partial, unknown and mismatched observations are retained in review without a
paid debit or release of the reservation. A later verified full record preserves
those observations and settles once. Potentially sent transfers cannot be
canceled without explicit confirmed-unsent evidence and current scoped host
permission. Amendments preserve the original paid record and never debit again.

`PendingMinor` is net outstanding unmatured commission. Its frozen portion is
`PendingDisputeHoldMinor`; that hold does not reduce unrelated matured money.
`DisputeHoldMinor` is the matured frozen portion. Availability is
`max(0, MatchedMinor - ReservedMinor - ReviewHoldMinor - DisputeHoldMinor)`;
debt is `max(0, -MatchedMinor)`. A hold does not reverse revenue. Won releases its
remaining hold, lost reverses only commission not already refunded/lost, and
late holds cannot revive a terminal dispute. Refunds before maturity reduce the
same original obligation exactly once when it later matures. Holds are bounded
to 28 elapsed days; zero hold requires explicit approval in program composition.

`RecordReturnedTransfer` preserves original paid evidence and appends a reasoned
adjustment, capped by original settlement minus prior returns. It credits the
outstanding obligation and restores the oldest original payment backing so a
future claim retains provenance. Its receipt keeps the claim and operation IDs
separate. Returned money does not erase refund debt or rewrite payout history.

Returned backing uses typed journal linkage: `returned.SourceEventID` identifies
the return operation, `returned.SourceRef` identifies its paid claim, and matching
`allocation-released.SourceEventID` rows identify that operation's restored
payment portions through their `SourceRef`. Human-readable notes establish no
financial authority. Before another return, complete prior release totals must
equal each returned amount and remain within the original payment allocations;
malformed linkage fails closed. Terminal unpaid reservation releases instead
use their claim ID as `SourceEventID`. Paid claims cannot be cancelled/rejected,
and a later revenue refund does not release their allocations a second time.
All mutation results are checked for representable integer aggregates before
commit; invalid or overflowing transactions roll back.

Each recorded return adjustment also retains its immutable `OperationID`.
Financial snapshot validation matches that operation's amount, currency,
economic return date, recording time and actor to the claim evidence. Aggregate
amount equality alone cannot silently move a return between reporting periods.
These operation/audit identifiers remain private domain provenance, not a
customer disclosure contract.

`FindClaimRequest` recovers the original actor/partner/key receipt and current
claim together, checking its immutable request fingerprint. It does not consult
today's destination or create a second reservation. A manager still requires
current authority before replay. `ClaimRequest.Reason` is optional for customer
requests and retained as immutable `Claim.RequestedReason`; later review reasons
do not replace it. Changed request reasons conflict under the same key.

## Payment cohort reports

`GetPaymentReport` reads the complete owning journal and current claim heads in
one program/partner/currency transaction, validates conserved provenance, and
returns a newest-first page of 1–100 original accruals. It preserves each
original payment's frozen referral and commercial provenance. A claim spanning
several referrals is attributed through its exact per-payment allocation
settlements, which must conserve the single original paid debit. Returned
operations restore only their proven original portions, including later
reclaim/repayment cycles; amended/returned claim heads do not change original
settlement evidence. Malformed scopes, chains, source identities, settlements,
return linkage or dispute-hold linkage fail without a partial report.

Each payment exposes original eligible revenue, maximum accepted cumulative
refund, accrued commission, current pending/matured earned amounts, reversals,
dispute loss/hold, active reserved/review backing and gross/returned/net payout
backing. A processing claim flagged for reversal review retains its reservation
and sets `ReviewRequired`. These backing amounts are provenance, **not funds
available to claim for that referral**. Canonical unfiltered `Balances` supplies
current partner debt/availability, including offsets between referrals.

`From`/`To` filter original payment `OccurredAt` (inclusive/exclusive);
`ReferralID` selects its immutable accrual revision. `CohortAmounts` and
`CohortPayments` include the entire selected cohort, independently of the page
cursor, and describe its current status at the snapshot. A payout or refund
outside those original payment dates still affects that cohort's current
status. For economic movements within a date period, use `GetStatement` instead.
The cursor is an exclusive accrual sequence; each page is a fresh snapshot.
Revision hashes the complete ordered journal and sorted current claim evidence,
so claim changes without journal appends invalidate it.

An accepted zero commission has a row; absence of an accepted accrual has no
row. No row does not prove an unresolved payment binding has zero entitlement.
The host must retain unresolved source/worker status separately. Results are
private domain views: customer responses must aggregate/project permitted
referral amounts and omit internal payment/policy/provider identifiers and
per-invoice details. Complete snapshot reads and validation grow with history;
cohort totals also fail closed if their integer representation overflows.

## Grouped retained-referral amounts

`GetReferralAmounts` groups immutable accrual referral revisions supplied by
an owning-service manager, reading the same complete financial snapshot as the
payment report. A query selects at most 100 groups and 10,000 unique revision
IDs in total. Group IDs and revision IDs must be canonical, bounded and unique;
overlap across groups fails closed. Exceeding the revision bound returns the
distinct `ErrReportTooLarge`, never partial history or zero earnings. Reducing
the relationship page size can help when several groups exceed it together;
a single relationship over 10,000 owned revisions needs an explicit capacity
extension and cannot currently be queried. The host must show that operational
reporting error honestly. Never truncate retained history to satisfy the bound. These IDs are
internal domain inputs, not a customer request or evidence of ownership.

Each requested group is returned in query order. Exact original per-payment
payout/return portions are summed once, including reacquisition and payments
already frozen to an older owner whose payment dates follow its cutover. Do not
join on current ownership or ownership dates. `From`/`To` select original
payment cohorts, independently of when their later reversals or payouts occur.
All groups share one financial revision and timestamp; unfiltered partner
balances stay separate from the groups and their date cohort.

`AcceptedPayments` counts accepted original accrual rows, including accepted
zero commission. It does not count people, subscriptions or renewals. A group
without such rows is returned with zero accepted count and no payment times;
this is **not** a complete source/worker entitlement decision. An outage or
uncertain transaction returns an error and no partial report. Customer
projections must omit amounts for missing acceptance, distinguish that state
from accepted zero commission, and label source/subscription coverage honestly.
First/last original payment times and eligible revenue remain internal domain
information; customer disclosure needs a separate explicit projection.

The earnings owner can validate only its own financial scope and provenance.
The manager must obtain each group's full owned revision list from the referral
owner. Earnings does not read referral storage to invent membership or turn
absence of an accepted accrual into an attribution decision. Complete journal
reads still grow with financial history; aggregate integer overflow fails closed.

## Financial metrics

`GetFinancialMetrics` uses the same complete validated financial snapshot as
payment/referral reports. It keeps three distinct views rather than treating a
date-filtered sum as current funds:

| View | Date/plan basis | Meaning |
| --- | --- | --- |
| `AcceptedAllocationRows`, `CohortAmounts` | Original accepted payment's `OccurredAt` and frozen `PlanID` | Current status of the selected original-payment cohort, including later refunds, payouts and returns. Rows count accepted economic allocations, not unique invoices, people, subscriptions or renewals. |
| `PeriodMovements` | Original economic movement time and original payment's frozen plan | Gross accrued/matured commission, refund/loss movements, hold/release movements and exact paid/returned allocation portions within the period. These are movements, not current balances. |
| `CurrentPartnerBalances`, `CurrentClaims`, `CurrentMaturity`, `RemainingCommissionMinor` | Complete current partner/currency snapshot; unfiltered | Canonical current balances, claim states, due maturity work and obligations, independently of all date/plan filters and breakdown pagination. |

Dates are inclusive `From` and exclusive `To`. Paid portions use the immutable
`EntryPaid.OccurredAt`, not the allocation settlement's later recording time;
returned portions use their typed return operation's `OccurredAt`. A transfer
spanning plans contributes only its original portion to each plan. Amended
payment presentation dates do not rebook original movements. Ordinary unpaid
reservation releases after cancellation/rejection are neither paid transfers
nor returns, so they are excluded from commission movements.
Maturity movements use the time maturity was durably recorded, including late
worker runs; they are not backdated to the earlier `AvailableAt` deadline.

`AccrualRequest.PlanID` is optional immutable provenance and participates in
exact replay identity. The manager supplies the verified owning billing fact's
original plan. Standalone accruals without it remain explicitly unspecified;
no current catalogue lookup invents their plan. Changed plan evidence conflicts
under the same economic payment. A known plan key is `plan:<PlanID>`; an absent
plan uses `unspecified`, which cannot collide with a real plan of that name.
`PlanID` and `UnspecifiedPlanOnly` are mutually exclusive query filters.

`Plans` is sorted by key and limited to 1–100 rows. `AfterPlanKey` is exclusive
and must exist in the selected plan/date grouping. Full-scope cohort/movement
totals do not change with its cursor. Each page is a new snapshot; compare
`Revision` to detect financial/claim changes, including claim-only transitions.
Changing a filter requires a fresh first page: a cursor outside the new grouping
returns `ErrInvalid`, even if that plan exists in a different scope.

Remaining commission is **pending plus positive matched credit**, with checked
integer addition. It includes held pending commission and is not availability
or a net forecast after future debt offsets. Current debt stays separate, even
when pending commission is positive. Processing exposure sums the full amounts
of current Processing/NeedsReview claims that may already have been sent,
including reservations whose credit was later refunded. Requested claims are
reserved but excluded from that exposure. Do not add exposure to remaining
commission: they can overlap. Claim counts and oldest open request time describe
current heads, not claims created/paid within the selected date range.

`CurrentMaturity` counts unjournaled accrual allocations whose original
`AvailableAt` is at or before the snapshot's `AsOf`. It matches the owning
`Mature` selection, including zero-rate, fully refunded or dispute-lost credit.
Positive `DueAllocationRows` can therefore have zero `DuePendingMinor`; rows
measure remaining ledger work, not people, revenue or additional earned funds.
`DuePendingMinor` is the validated current net pending credit in those rows,
which can differ from the original gross amount a maturity journal records.
`DueDisputeHoldMinor` is an overlapping subset of that net credit. Maturity does
not release dispute holds or promise claim availability. Both amounts are
bounded by their corresponding complete current pending balances.

`OldestAvailableAt` is present only when due rows exist and describes their
oldest original deadline. Future and already-matured lots are excluded. Invalid
or missing original horizons remain provenance errors, never a guessed legacy
schedule. Date/plan filters and plan pagination cannot hide current due work.
Clock passage can change these counts without a new journal revision; hosts
must not cache due status using `Revision` alone. The snapshot clock describes
classification of the owning read, not a worker acknowledgement or database
commit timestamp. `Mature` samples its own clock when actually invoked.

This is a pure report: it does not schedule or execute a maturity transition.
The host must compose durable bounded maturity discovery/processing independent
of customer views and claim requests, with overdue alerts and recovery; the
optional [owning worker](../partnermanager/README.md#owning-signup-revenue-and-maturity-worker)
provides the queue/receipt orchestration, without installing a host schedule.

No accepted rows means no accepted journal cohort, not complete billing-source
coverage or zero entitlement. Worker backlog, source completeness, matched
click conversion and active subscriptions require their separate owning
measures. Financial metrics are private domain models; customer transport must
omit billing revenue and raw plan/provider/payment identifiers. Complete reads
grow with history. Malformed provenance, unavailable/uncertain snapshots or
integer overflow fail without a partial report.

## Statements

`GetStatement` returns a newest-first page of 1–100 journal lines and current
balances from one owning transaction snapshot. Optional kind and economic-date
filters use inclusive `From` and exclusive `To`; pagination uses an exclusive
`BeforeSequence`. Filters do not change balances or a line's running matured
amount: that amount is derived from the complete journal prefix up to the line.

The snapshot revision includes journal sequence and claim revisions/states.
Claim processing and review can change available balances without appending a
journal line, so journal sequence alone is not a balance revision. Each page is
a fresh snapshot; callers can detect a changed revision between pages rather
than assume pagination freezes the ledger.

Paid lines include the current manual payment presentation, applying retained
amendments without changing the original debit or recorder attribution.
`PaymentVersion` is the presentation ordinal (original record plus amendments),
not authenticated provider delivery or settlement evidence. Manual records are
operator attestations; the package does not initiate or verify a payout.

The service currently reads the complete journal and claim history to derive
the snapshot, and reuses canonical derivation for each returned running amount.
The response page is bounded; datastore reads and computation still grow with
history. Repository failures return no partial statement. These domain results
include internal provenance and must be explicitly projected for customer
transport; do not serialize them directly into a public response.
