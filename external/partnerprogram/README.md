# Partner program

`partnerprogram` owns enrollment, partner lifecycle, immutable commercial-policy
versions, policy resolution and versioned payout destinations. It has no Mongo,
HTTP, identity, billing or group-repository dependency. A host composes it through
the shared partner manager, which obtains verified identity and current scoped
authority before invoking an owning use case.

## Construction and admission

Supply a typed `Repository`, an ID generator and explicit `Config`. A nil clock
uses the UTC real clock. Configuration never invents approved commercial terms:
the rate must be 0–10000 basis points, the hold within the configured maximum, the attribution window
positive and at most 180 days, and the single currency three uppercase letters
with exponent 0–3. Zero hold additionally requires `AllowZeroHold`. Supply the
accepted terms version and eligible plan IDs; fixture values are not approval to
launch a program. The current program identity is `partners-v1`.

`MaxHoldDays` limits new defaults and published policies. Omitting it selects
30 days; an explicit maximum may be 1–365 days. Holds are elapsed 24-hour days,
not a calendar payout schedule. `DefaultHoldDays` must fit that maximum; zero
still requires `AllowZeroHold`. Existing 28-day configurations remain valid.

The stable `MaxSupportedHoldDays`/`MaxHoldDuration` bounds validate retained
referral and financial evidence up to 365 days. Lowering `MaxHoldDays` does not
rewrite or invalidate an already-published policy or frozen commission. Publish
a new prospective policy to change commercial holds; changing this validation
limit alone does not replace the active published policy.

Enrollment requires explicit acceptance of the configured terms. Repository
uniqueness makes repeated or competing enrollment return the same participant.
Approval-required enrollment starts pending. Status changes require an actor,
reason and expected revision. Acquisition, accrual and payout requests are
independent gates; pending/closed participants cannot acquire or accrue. A host
may preserve payout recovery while acquisition and accrual are paused.

## Published policy

Publication appends an immutable version while atomically advancing the exact
global/group/individual scope head from `ExpectedRevision`. A stale writer is
rejected. Effective intervals are inclusive at their start and exclusive at
their end. A version cannot affect an event before it was published.

Resolution selects the event-time version of each scope, then applies global,
highest-priority matching group and individual fields. Equal winning group
priorities are ambiguous and fail closed. Group IDs must come from the owning
group service. Overrides use `-1` for inherited rate/hold, empty currency/terms
for inheritance and a nil plan slice for inheritance; an explicit empty plan
slice excludes all plans. The resolved result includes field provenance,
version IDs, currency exponent, eligible plans and the recurrence end.

Referrals and payments freeze their resolved terms; reading a newer policy must
not reprice an older payment. A payout-destination update appends a new version
with an expected-version precondition. Existing claims retain their original
destination snapshot.

`ResolveReferralTerms` selects policy at a current review instant while deriving
a finite recurrence end from the immutable owning signup time. This differs
from `ResolveTerms`, which uses its event instant for both purposes. A manager
changing prospective ownership must also retain any earlier frozen recurrence
end; a new owner or unlimited policy cannot restart an existing referral window.
Committed correction replay uses the stored terms rather than resolving current
group membership or policy again.

## Persistence and failures

`partnerstore.ProgramRepository` implements this port with encrypted shared
records, guarded transactions and immutable status/policy/destination history.
Callers must retain encryption keys and history across upgrades and restores.
Expected absence, enrollment uniqueness, stale CAS, ambiguous policy,
validation, dependency failure and an uncertain commit are distinct outcomes.
An uncertain result is not proof that a write failed. Retry enrollment by its
stable customer identity or inspect the observed immutable revision; do not
overwrite or delete history to resolve uncertainty.

Use bounded caller contexts. Neither service nor repository performs external
effects from transaction callbacks. The service does not approve commercial
launch, authorize an operator or send money.
