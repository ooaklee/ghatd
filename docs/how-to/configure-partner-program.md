# Configure a partner program

Use the optional partner packages to add referral attribution, commission records
and withdrawal workflows to a host application. The host supplies commercial
policy, credentials, current identity and worker scheduling. Start with the
interfaces below; there is no automatically enabled partner program.

## Choose the owning packages

| Responsibility | Guide |
| --- | --- |
| Enrollment, terms and partner policy | [partnerprogram](../../external/partnerprogram/README.md) |
| Referral links, consented visits and attribution | [referral](../../external/referral/README.md) |
| Earnings, statements, claims and payment records | [partnerearnings](../../external/partnerearnings/README.md) |
| Use-case orchestration | [partnermanager](../../external/partnermanager/README.md) |
| Current member, operator and worker authority | [partneraccess](../../external/partneraccess/README.md) |
| Borrowed database and owner composition | [runtime](../../external/partnermanager/runtime/README.md) |
| Optional referral, signup, identity and execution wiring | [helper](../../external/partnermanager/helper/README.md) |
| Optional member/operator JSON routes | [http](../../external/partnermanager/http/README.md) |
| Retained billing inputs and recovery workers | [billinglifecycle](../../external/billinglifecycle/README.md) |

## Prepare storage and configuration

1. Select a transaction-capable MongoDB deployment and a host-owned database.
   Supply stable payload-encryption and retained referral-signing keys. Keep them
   independent of the other purpose keys required by the runtime configuration.
2. Define the program's terms, currency, commission policy, attribution windows,
   claim minimum and admission controls explicitly. Load secrets and environment
   values in the host; do not derive these choices from browser requests.
3. Construct `partnerruntime.NewRuntime(database, config, dependencies)`, then call
   `runtime.Prepare(ctx)` before admitting handlers or workers. Preparation installs
   indexes and probes a real transaction. A failed preparation blocks startup.
   The host owns the database pool and must drain borrowers before closing it.

See the [runtime configuration](../../external/partnermanager/runtime/README.md#explicit-configuration)
for the complete typed inputs. Changing import paths does not change storage
identifiers; retain the owning record and encryption contracts across restarts.

## Install member and referral routes

1. Compose the current member/session and account-admission ports in
   [partneraccess](../../external/partneraccess/README.md). Bind `ActorID` from verified
   identity; account and claim selectors identify targets, not callers.
2. Construct the [HTTP principal resolver](../../external/partnermanager/helper/README.md#member-http-admission-and-observation)
   with the authentication-cookie name, explicit trusted native audiences and the
   host's transport-identity binding. Pass the same configured
   [browser-security guard](../../external/http/browsersecurity/README.md) to the
   transport when reusing an existing CSRF cookie. Cookie possession alone is
   neither authentication nor permission.
3. Mount [partnerhttp](../../external/partnermanager/http/README.md) routes with the
   configured manager and enforcing admission. Mount
   `AttachPartnersReferralRoutes` before the SPA fallback, using host-owned signup
   and visit-cookie names, public origin and consent-page renderer.
4. Configure `ConfigureSignupCapture` with the evidence-cookie name used by the
   referral issuer. The access manager must hold the exact same `*userv2.Service`
   instance passed to the helper; a separately constructed service is rejected.
   This captures immutable signup evidence; it does not manufacture attribution
   for older accounts.

The host retains page branding, legal copy, CORS, cache exclusions and client
handling of the documented error codes. An empty operator screen grants no access
to selected accounts; owning reads and commands still require current scoped grants.

## Configure workers explicitly

Create the host's service account through its existing identity administration.
Provision only the documented worker capabilities and exact native scopes through
[capability administration](../../external/accesspolicy/README.md#explicit-capability-administration).
Runtime construction neither creates the account nor seeds permissions. Use
instance-bound worker authority; a human session cannot become a worker invocation.

Prepare the billing manager before calling `partnermanagerhelper.NewExecution`.
Supply its `ExecutionDependencies` with the prepared partner runtime, billing
manager, a `CheckoutPayerUserService`, and a provider registry implementing both
`RevenueProviderRegistry` and `CheckoutProviderRegistry`. Worker execution also
requires the user owner to implement `SignupFeed`, plus current worker identity
and policy ports. Construction installs revenue services and, when selected,
checkout capture and reconciliation authority into the supplied billing manager;
complete this wiring before admitting requests.

Revenue capture and worker execution are independently optional. With no worker,
`NewExecution` can configure capture and return a nil execution without an error.
With neither selected, it returns nil without wiring dependencies. A configured
execution composes signup, revenue and maturity work over the prepared owners.
Billing lifecycle recovery uses its separate
[composition helper](../../external/billinglifecycle/helper/README.md): perform
explicit native preparation before constructing the enabled runtime. Neither
helper starts a background loop. Schedule sequential `RunOnce` passes with their
validated intervals/deadlines, apply cancellation and drain them on shutdown.

After an uncertain outcome, inspect retained inputs and receipts under current
authority. Do not create a new provider submission or erase the original command
to make a retry succeed. Provider acceptance, local capture and recorded manual
payment are different outcomes; use the owning guide's recovery contract.

## Verify host adoption

Check ordinary-user denial, selected partner/operator permissions, CSRF/origin
refusal, worker-grant revocation, stable-key restart and retained-receipt recovery.
Run native package tests with isolated MongoDB/Redis fixtures as specified by each
guide, then test the host's actual authentication and provider configuration.
Keep test databases and provider test credentials separate from production.
