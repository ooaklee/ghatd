# Referral attribution

`referral` owns stable share links, signed browser evidence, immutable signup
attribution revisions and event-time payment attribution. It does not create
accounts, determine whether a subscription was paid or calculate commission.
The partner manager supplies verified facts from the identity, program, group
and billing owning services.

## Share links and evidence

Supply a repository, non-enumerable ID generator and explicit positive window
of at most 180 days. A nil clock uses the UTC real clock. Codes derive from the
whole generated identifier and remain case sensitive; normalization trims only
whitespace. The repository enforces one active link per partner and global code
uniqueness including retired links. Retirement records an actor and reason and
does not free the old code for reuse.

`RotateLink` requires the selected original code, a non-empty reason and one
stable request key, plus verified actor and partner input from the manager.
The optional `LinkRotationRepository` capability must retire that exact original,
issue its replacement and commit the private actor/key receipt in one partner
transaction. There is no separate retire/issue fallback. A changed payload under
the same actor/partner/key returns `ErrStaleWrite`. An identical request returns
its originally issued link even after a later rotation retires that result or
new acquisition is paused. Hosts must preserve the original key and payload
after `ErrUncertain`, then refresh the current link after recovering the result.
Current identity and permission remain required on every attempt. Receipt
integrity failures return `ErrUnavailable`; receipt contents are never customer
response fields. See [storage guarantees](../partnerstore/README.md).

`EvidenceSigner` requires an explicit program, active signing-key ID and a
keyring with keys of at least 32 bytes. The token is HMAC authenticated and bound
to program, signup audience, link/code, issued time, expiry and an optional
measured-visit origin.
Hosts persist keys, retain still-valid old keys during rotation, issue cookies
only under their consent policy, and use safe same-site navigation. Never
generate a new key on each process restart. Tokens are bounded to 2048 bytes.

Signature validation uses the owning account-creation time, not a browser
timestamp or the later worker processing time. A host may transport this token
through a configured browser cookie to the owning identity service. Native or
cross-device sessions without that evidence remain unattributed; email matches
do not establish ownership. Analytics are optional, non-authoritative and
bounded; raw IP addresses are not part of the click model.

## Consented visit measurement

Measurement is separately opt-in: configure `EvidenceConfig.VisitWindow` between
one second and 24 hours and inject non-enumerable `VisitIDs`. Configure the
referral service with `WithAnalytics(AnalyticsConfig{ObservationRetention: ...})`
using a host-approved lifetime between 24 hours and 365 days. The builder returns
a separate service; collection has no implicit default. The minimum preserves
the first observation throughout every supported visit-cookie lifetime.
A zero visit window
keeps signup attribution available without measurement. Wire the signer and
referral service to the same owning UTC clock; future or expired identities are
rejected without clock-skew allowances.

`IssueVisit` creates a signed `partner-visit` cookie, separate from the longer
`partner-signup` token. `VerifyVisit` authenticates it at the current clock and
binds it to one link. Storage retains only a keyed, link-scoped digest; neither
raw nonce, cookie, IP nor user-agent is collected by `ObserveVisit`. The host
binds consent and known-bot classification after its rate/abuse policy. Request
fields are excluded from JSON decoding; browser flags are not admission evidence.

`WithVisitTransaction` commits each observation and its anonymous UTC-day
classification counts together with the unique first-visit receipt. Reloads and concurrent uses of the same signed nonce record duplicates
pointing to that first observation. A lost acknowledgement followed by another request records a duplicate
observation; it does not create another eligible visit. These are request
observations, not exactly-once HTTP request counts. A fresh cookie after expiry
starts a new visit, not a new person. Known bots remain excluded from measured visits and
cannot seed signup evidence. No consent means no observation or cookie.

`IssueMeasured` accepts the owning observation returned for that link, not
browser-supplied click fields. It performs signature policy, not repository I/O.
The optional `Evidence.MeasuredClickID` and `MeasuredOccurredAt` are appended
with `omitempty`; plain evidence retains identical JSON bytes and signup digest.
The authenticated observation time cannot follow issuance, and the owning
attribution service also checks the link-creation floor. Attribution locks
freeze a copy of the optional origin/time but never depend on a current analytics
lookup. This preserves the original conversion cohort after raw cleanup.
A missing or failed analytics store cannot invalidate an accepted signup or
financial replay. Existing plain tokens are not upgraded retroactively.

The storage adapter sets absolute expiration only on raw observations and visit
receipts, using the prepared shared record store's single-field partial TTL
index. Expiry is asynchronous physical cleanup, not consent or admission
permission. Cookies still expire at their authenticated absolute time even if
cleanup is delayed. Financial, signup, ownership and anonymous daily records
have no expiry metadata. Hosts must approve their privacy/retention and consent
policy before collection and monitor the database cleanup behavior.

`ObserveClick` remains an unmeasured observation API; its user-agent argument is
not stored. `CountClicks` counts **currently retained raw rows**, including
duplicates, bots and unmeasured observations. It is neither an all-time total nor
a conversion denominator. Use the owning aggregate report instead.

## Referral analytics

`GetAnalytics` uses `ReadAnalyticsSnapshot` to read all selected-partner links,
anonymous day counts and lifetime memberships with complete heads/histories in
one owning transaction. Independent 10,000-record budgets cover (1) links,
memberships and ownership revisions, (2) anonymous day buckets and (3) retained
raw boundary rows. Counts themselves may exceed 10,000; checked `int64` additions
reject overflow. `ErrCapacity`, cancellation or any failed scope discards the
entire result. Custom adapters must bound complete reads; a partial page cannot
stand in for a complete snapshot.

`AnalyticsQuery` pages 1–100 links. Full totals and the persistent owning revision
are independent of the page cursor and date filter. Raw cleanup does not change
that revision. A cursor must belong to the selected partner. Pages are separate
snapshots: refresh after changes rather than treating several requests as a
single transaction. `AsOf` is generation time after the read, not an identity,
billing or provider source watermark.

The optional date range is inclusive `From`, exclusive `To`:

- All-time and whole UTC-day observation totals use the persistent anonymous
  classification counts. Ranges cutting a UTC day require that complete day's
  raw rows in the same snapshot. Their counts must match its bucket before
  filtering timestamps. Expired or partially missing boundary rows return
  `ErrGranularity` with no report; there is no prorating or false zero. Hosts can
  offer UTC calendar-day ranges for durable 30/60/90-day and custom reporting.
- The conversion numerator counts distinct signed measured origins with at least
  one accepted new signup at or after the original observation, including later
  signups outside the visit range. Multiple accounts from one visit increase
  `SignupsFromVisitCohort`, never converted visits. Equal instants are valid.
- Signup metrics independently select original immutable account-creation time.
  Measured and unmeasured signup origins are separate. An `admin` initial
  assignment uses its cutover time and counts as manual acquisition; later
  prospective corrections count acquisition events, not new signups.
- Lifetime/current/retained relationships are distinct, unfiltered memberships.
  Corrections and reacquisition never move the original owner's signup
  conversion or duplicate a lifetime membership.

Coverage describes owning evidence only. Unmeasured observations and signup
origins remain explicit. A stored measured signup without its authenticated
observation time reports `incomplete_measured_origins`; the missing time is never
reconstructed from current ownership. Raw first observations outside requested boundary days are intentionally not
loaded; they are not missing conversion evidence.
Even complete measured evidence does not identify unique people, all traffic,
paying referrals, active subscriptions or financial/source-feed completeness.
Transports display unavailable, capacity and granularity errors separately from
a successfully observed zero, and encode large counts losslessly for clients.

## Signup locking and corrections

`LockAttribution` requires a trusted new individual account, matching authenticated
link evidence inside the window, a partner eligible to acquire referrals and
complete frozen policy terms. Self-referral, old accounts and competing ownership
are rejected. An exact same-signup/evidence/time retry returns its original
immutable revision.

`AssignAttribution` supports explicit `prospective` mode only. The privileged
manager supplies an eligible owning creation fact, proposed owner and frozen
approved terms after a reviewed impact preview. Every request requires actor,
customer, reason, stable idempotency key, expected head ID/revision and snapshot
and preview fingerprints. It appends a new ownership revision and never edits
an earlier record, existing payment binding or earning. Initial assisted
assignment starts at its recorded cutover; it does not backdate signup credit.
Historical compensation or financial transfer is not an available mode.

`GetAttributionSnapshot` reads and validates the complete head, immutable history
and customer payment bindings in one owning transaction. Its fingerprint
detects a binding or correction added after review; apply checks it under that
same customer guard. Full-history reads grow with account age. Fingerprints
are compare-and-swap preconditions, not permission or trusted identity evidence.

An immutable correction revision is also its receipt, keyed by program,
customer, actor and request key. Exact original payloads replay before current
head, acquisition or cutover-clock checks, even after another correction.
Changed payloads conflict; a different actor/key cannot read that receipt.
`FindCorrection` supports manager recovery before today's policy/admission
lookups, while current manager authority remains mandatory. The private
`Correction` field is omitted from domain JSON; storage adapters must explicitly
retain it in an encrypted envelope. Hosts still project customer-safe views.

`BindPayment` selects the highest ownership revision effective at the provider's
paid time and durably freezes it under the globally scoped economic payment ID.
Retries and refunds reuse that binding even if current ownership or policy has
changed. A future payment may bind a later ownership revision. A binding replay
with a different payer or paid time fails rather than silently changing owner.

Ownership correction and event-time payment selection run inside the same
customer-scoped `WithAttributionTransaction`. Custom repositories must provide
this driver-free capability and pass a bound repository to its retryable
callback. Selection, original-binding lookup and insertion use that snapshot;
no external service or provider call occurs inside it. Payment identity remains
globally unique, even when different customers compete for it.

The correction clock is sampled within the guarded transaction callback, and
the recorded cutover cannot precede the prior revision's `LockedAt`. Equal
instants use increasing revision order. `LockedAt` is the economic cutover
instant, not a database commit timestamp. A previously frozen payment binding
always remains authoritative, including one with a future-effective timestamp;
such bindings must be explicitly excluded from any correction impact preview.
The impact preview must explicitly retain every existing binding, including
future-effective allocations. Unbound late payments before the cutover select
the old revision; unbound payments at/after it select the newer revision.

## Retained partner relationships

`ListByPartner` lists current ownership heads only. Use `ListRelationships` for
customer and operator referral history: a former owner must retain visibility
of the relationship underlying its existing earnings. The repository stores one
immutable membership per program/partner/customer, referencing the first owned
revision, in the same transaction as the revision and conditional head write.
Reacquisition retains that reference instead of creating another relationship.

`ListRelationshipSnapshots` reads a bounded membership page and the complete
head/history of each selected customer in one owning read snapshot. The service
validates the full chain before returning only the selected partner's periods.
Period ends are exclusive cutovers; `Current` describes ownership at that
snapshot. It discloses no replacement owner's identity, raw correction reason
or acquisition evidence. Already-frozen payment bindings may still belong to a
period after its recorded end, so economic reporting must join the immutable
binding/accrual revision rather than infer ownership from dates.

Pages contain at most 100 relationships. `NextAfter` is an immutable,
partner-scoped membership ID; a cursor from another partner is rejected. Each
page is a new snapshot, so refresh to discover new memberships whose IDs sort
before a previously used cursor. Complete per-customer history can grow with
age; the service never truncates it to fit response pagination. Dependency
failures return no partial page. Full raw ownership history remains privileged.

## Complete relationship evidence

`GetRelationshipEvidence` derives an optional capability from the **same referral
repository**. `ReadRelationshipEvidence` supplies all selected-partner lifetime
memberships, current heads, complete ownership histories and immutable customer
payment bindings in one native read snapshot. It performs no correction guard
writes, billing/provider calls or payment binding. Unlike a list page, this
complete set can support global partner reporting without adding independent
pages. It does not require raw visit retention or analytics collection.

A combined 10,000-record budget counts each membership, head, ownership revision
and binding before report filters. `ErrCapacity`, cancellation, missing expected
records, corruption and late read failure discard the entire set. Both the
service and native adapter enforce the bound; a nil result is not an empty
complete set. Callback reentry resets accumulated evidence.

The service validates lifetime references, complete chains and binding ownership,
terms and economic times using the same canonical attribution validator as
correction reads. Persisted correction snapshot fingerprints remain unchanged.
Former owners retain their periods while bindings to other owners remain private
validation evidence. Future-effective frozen allocations remain immutable;
future recorded evidence cannot be certified by a clock that precedes it.

Every field of `RelationshipEvidence` and its rows is excluded from transport
JSON. Its revision identifies the complete retained ownership/binding set and
includes private correction receipts. `AsOf` is generation time after the read,
not a cross-owner transaction or billing/status watermark. Managers must project
permitted aggregates and recheck current authority before returning them.

## Combined conversion evidence

`GetConversionEvidence` derives the optional `ConversionSnapshotRepository`
capability from the **same referral repository**. `ReadConversionSnapshot` reads
traffic, all retained memberships, current heads, complete histories and every
selected customer's immutable payment bindings in one native snapshot. Its
combined 10,000-record budget counts links, memberships, heads, revisions and
bindings before filtering; day buckets and raw boundary rows retain independent
10,000-record budgets. Nil or missing binding partitions are not complete empty
sets. Capacity, corruption, cancellation and late failure discard all evidence.

The existing analytics snapshot shape, traffic revision and canonical attribution
fingerprints stay unchanged. The combined revision identifies both traffic and
ownership/bindings; changing either invalidates a manager's observed recheck.
Private link evidence is complete before public link pagination. Every field of
the combined snapshot and evidence wrapper is excluded from transport JSON.
The service performs no billing calls or business writes; the
[partner manager](../partnermanager/README.md#original-cohort-paid-conversions)
joins independent accepted billing history and projects permitted aggregates.
Raw cleanup preserves all-time and whole-day evidence; incomplete sub-day
boundaries still return `ErrGranularity`.

## Storage and recovery

`partnerstore.ReferralRepository` uses encrypted shared records and guarded
transactions for active links, reserved codes, immutable referral history/current
heads, retained lifetime memberships and payment bindings. A failed transaction has no partial references or
history. Context cancellation, dependency failure and uncertain commit are
separate from expected absence. Reconcile a lost acknowledgement by the stable
code/customer/payment identity; never delete the evidence or reuse a retired
code. Use bounded caller contexts and retain signing/encryption keys in restores.
