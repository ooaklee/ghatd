# Partners authority

This reusable adapter uses the existing GHATD access-policy service and a live session
verifier. It stores no grants, reconstructs no account roles and accesses no policy
repository. Bind the verified principal and credential with `WithVerifiedSession`
at the trusted transport boundary; `CheckPartners` independently rechecks that
credential before and after current policy enforcement, including retries.

Customer operations require `partners.self` and the exact shared manager action
permission, with target equal to the verified actor. An operator needs the exact
action permission and either `TargetScope(action, selectedResource)` or an explicit
`partners.program` scope. Program authority intentionally covers that action's
resources; it grants no additional action. Missing grants and generic admin roles
do not confer recording, reporting, policy or attribution authority. Joined denial
and storage failure cannot trigger a program-scope retry.

A host can explicitly attach the existing access-policy manager's capability
management routes to provision grants. Those audited APIs retain policy ownership;
this adapter never assigns permissions at signup or startup. See the
[capability management guide](../accesspolicymanager/README.md#optional-capability-management).
Token-limit endpoints do not assign Partners permissions.

Operations reporting is bound to `partners-v1`. Browser sessions cannot satisfy
worker capabilities. `WorkerAuthority` binds an instance-private scheduler
context to one real configured API-service account and checks current identity
before and after native policy enforcement. It rejects human session contexts,
other authority instances, customer/operator actions and missing exact worker
grants. Program discovery uses an empty target; selected processing validates a
bounded source or partner ID. Reconciliation uses the same revenue permission.
The host's identity verifier also applies durable account-deletion admission.
The adapter creates no implicit grants and performs no financial or policy writes.

`OperatorTargetValid` is the common shape check used by enforcement and the
host's operator-access projection. It recognizes no customer or worker action.
Policy history and the processing queue use an empty target; selected policy
actions use the partner or customer ID and processing actions use the claim ID.
Reporting and on-behalf creation select a partner; attribution selects the
referred customer; recording, amendment and return select a claim. Operations
selects the owning program. These are opaque ID shapes, not existence checks.
Program-wide reporting is checked through the explicit program-scope alternative
on a selected-partner probe; an empty reporting target is invalid. A denied
program-list probe does not establish that all selected-resource grants are absent.

`Cohorts` supplies the manager's rate-bearing group IDs through a separate
intersection. It requires the owning group's complete, sorted active direct
membership read plus the selected customer's current `CohortScope(groupID)` and
`partner.cohort.approved` permission. These are independently administered through
the existing privileged access-policy API. Group ownership, ordinary joining,
inherited administrator access and program/operator scopes do not confer cohort
approval. A cohort grant neither approves a region nor authorizes policy changes.

The adapter validates all membership evidence before grant I/O, rereads membership
and confirms every selected scope together in one current owning grant read.
Unknown/joined denial, outage, cancellation or changing evidence discards all
output; only sole known denial excludes an unapproved membership. The host bounds
selection to 10,000 candidates, with at most 10,001 policy reads and two membership
reads. Churn or final grant denial requires caller backoff and a complete fresh
resolve. These rechecks are current evidence, not an atomic snapshot across group
and policy domains or a historical membership fact. No group rules are copied into
the host, no grants are created and no group IDs are exposed as customer DTOs.

`Regions` supplies explicit current participation admission to the shared user
identity adapter. A privileged reviewer must first apply the approved region
rules and use the existing audited policy-management API to grant the selected
customer `partners.region.reviewed` AND `partner.region.eligible`. This is an
administrative attestation, not automatic country verification or consent.
There is no trusted country field in the owning profile; editable extensions,
phone, locale, GeoIP, cohort approval and operator roles cannot supply admission.
Known missing/revoked approval returns false; unknown or joined failures remain
unavailable. No grant or commercial launch decision is created by this read.

The host installs this adapter with the published Partners runtime. Its operator
access endpoint projects one current action/target result after live session
checks; it is transient UI guidance, not a reusable grant. Known permission
denial returns `allowed=false` only with a still-live credential. Revoked sessions
and dependency/unknown/joined failures remain errors. Every owning read and
mutation still authorizes independently. Native fixture tests do not certify
authentication cryptography, customer/operator browser flows or platform E2E.


`NewLifecycleWorkerAuthority` is a separate optional configuration path for the
private billing discovery/read/refresh capabilities. It copies an explicit
native scope set (one to ten provider/account/live-mode triplets). Grant scope
names come from `LifecycleScope`, a versioned canonical hash; grant permissions
are `billing.subscription-status.discover`, `.read` and `.refresh` independently.
The normal financial worker constructor installs no lifecycle scopes. Neither
financial program grants nor human operator grants confer these capabilities.
No grants or accounts are created by either constructor.

Every discovery target checks the exact configured scope and source kind, with
a selected acknowledged intent or payer/subscription shape. Every status stage
checks read or refresh independently. Empty pre-lookup status targets require
ALL configured scope grants in one native current-policy read; a selected target
requires its exact scope grant. Selected payer and source IDs use the host's
bounded, whitespace/control-free opaque ID contract. Native billing still owns
canonical evidence validation; this adapter establishes current authority only.
Real API-service identity, an instance-bound private invocation, cancellation
and policy are checked for every stage. Identity is rechecked after policy I/O;
joined denials retain operational causes. These checks do not lock identity or
policy across domains.

This adapter does not install the collector, migration orchestration, durable
original-input recovery or the recurring refresh scheduler. It is not an HTTP
authenticator and cannot grant a human session worker access. Runtime composition
and full authenticated platform verification remain required.


`AuthorizeCheckoutLifecycle` checks the same scoped refresh permission for the
manager's acknowledged-checkout preparation, provider lookup, capture and receipt
recovery stages. Selected payer and intent are independent of the current service
actor; both are required. Read/discovery permissions, a human session or another
authority's invocation cannot complete a checkout. Installation on the owning
billing manager and durable original-input retention remain runtime requirements.


## Owning identity and explicit host admission

`NewMemberSessionVerifier(authenticator, admission, activeStatus)` supplies the
live session port from the owning `AuthenticateSession` operation. It requires
current authentication, matching response/user actor IDs, the configured active
status and verified email, then calls the explicit host `AccountAdmission` hook.
Invalid credentials/actors, nil or ambiguous evidence and late cancellation fail
closed. It creates no action permission; authority independently rereads live
sessions around each current policy check, including receipt replay.

`NewUserWorkerIdentity(users, admission, accountType, activeStatus)` supplies the
current service identity port from user/v2. Hosts explicitly select the registered
API-service account type and active status, and retain their durable account
restrictions in `AccountAdmission`. Missing users/admission cannot become implicit
unrestricted access. Neither constructor performs I/O or binds a worker context;
only the existing instance-private `WorkerAuthority.Bind` does that.

Callbacks preserve host denial/outage errors and must never erase historical
financial evidence. Configuration is trusted startup input, never an HTTP actor,
role or body. These adapters provide current identity, not grants, signatures or
an atomic snapshot across identity and policy owners.
