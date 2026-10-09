# Partners integration helpers

`partnermanagerhelper` supplies optional referral, signup, identity and execution
composition over existing owning services. Its referral transport wraps
`partnermanager.Manager.PrepareVisit` and owns anonymous consent,
bounded form parsing, rate-admission integration and secure browser cookies.
The manager owns link eligibility, attribution, signed evidence and measurement.
The core manager does not import this package.

`AttachPartnersReferralRoutes(router, config, dependencies)` validates all
configuration before registering routes. Register it before a SPA fallback,
including when capture is disabled, so visitors can decline and continue to
ordinary signup. `NewPartnersReferralHandler` also supports explicit handler
composition. The returned handler's configuration is private and immutable.

## Host configuration

Supply `PartnersReferralConfig` with an explicit `PublicOrigin`, distinct
`SignupCookieName` and `VisitCookieName`, and the owning program's
`AttributionWindow` and optional `VisitWindow`. `ReferralPath` and `SignupPath`
default to `/ref` and `/auth/signup`; overrides must be canonical root-relative
paths. A signup redirect cannot select another site or enter the referral route.
Cookie names belong to the host and must match its signup evidence consumer.

`PublicOrigin` must be HTTPS. `AllowInsecureLoopback` permits HTTP only for
`localhost` or a loopback IP; the host must set this solely in its explicit
local environment. Cookies always use `HttpOnly`, `SameSite=Lax`, root path and
the owning issuer's absolute expiry. No arbitrary flag disables those protections.
Issuer output is validated as a complete pair before either cookie is written.

`TrustedProxyCIDRs` accepts up to 32 canonical, non-default peer networks and is
copied at construction. `CF-Connecting-IP` is trusted only from a matching peer,
with exactly one valid forwarded address. Arbitrary query parameters and public
link codes cannot choose admission keys. Admission suppresses the native
middleware's raw-IP logger without mutating the original domain request context.

## Required dependencies

`ReferralDependencies` contains:

- `Visits`: the narrow owning `VisitPreparer` port. A nil or typed-nil port
  explicitly disables capture, while decline and signup navigation remain usable.
- `Clock`: the owning clock; mandatory when capture is enabled.
- `Admission`: the host's rate-admission middleware; mandatory for capture.
  This is anonymous admission, not a human session or a worker invocation.
- `RenderPage`: a required host renderer accepting an `io.Writer` and a safe
  `ReferralPageView`. Hosts retain their logo, public styles and legal navigation.
  The view carries only availability and public duration text. The renderer
  cannot modify the handler's security headers through its writer interface.

The handler sets `no-store`, `nosniff`, `noindex, nofollow`, same-origin referrer
policy and a restrictive CSP. Host rendering must use same-origin styles and
images and cannot rely on inline scripts/styles. The GET page deliberately does
not validate or disclose referral ownership before consent. HEAD writes no body.

POST requires exactly one matching `Origin`, a bounded URL-encoded form and one
`choice=yes` or `choice=no` field. Decline clears both cookies without invoking
storage or rate admission. Opt-in requires a valid code, successful rate
admission and a valid owning result. Dependency failures and raw diagnostics are
never included in the page. Unsupported methods return 405 instead of falling
through to a successful SPA response.

## Composition example

```go
err := partnermanagerhelper.AttachPartnersReferralRoutes(router,
    partnermanagerhelper.PartnersReferralConfig{
        PublicOrigin:      "https://app.example.test",
        ReferralPath:      "/invite",
        SignupPath:        "/join",
        SignupCookieName:  "host-referral-signup",
        VisitCookieName:   "host-referral-visit",
        AttributionWindow: programConfig.AttributionWindow,
        VisitWindow:       visitWindow,
    },
    partnermanagerhelper.ReferralDependencies{
        Visits:     runtime.Manager,
        Clock:      runtime.Clock(),
        Admission:  rateAdmission,
        RenderPage: renderReferralPage,
    })
```

These variables are host-supplied owning services and presentation, not global
defaults. Construction performs no provider/storage calls and starts no work.
The referral helper supplies no customer/operator JSON routes, authentication
or grants. The separate helpers below compose identity/signup/execution; the host
still owns scheduling, shutdown and resource acquisition.

See [runtime composition](../runtime/README.md) and
[authority](../../partneraccess/README.md) for the separate owner boundaries.


## Selected account admission and signup

`NewAccountIdentity(owner, admission)` decorates the existing `Identity` port
with an explicit [AccountAdmission](../../partneraccess/README.md) callback.
Current selected-principal reads require matching owner IDs and successful host
admission; errors and late cancellation withhold the projection. Immutable
`GetSignupFact` recovery deliberately ignores current admission and delegates
only to its owning capture service. Account restrictions cannot erase historical
signup evidence. Neither port is optional and construction performs no I/O.

`ConfigureSignupCapture(ctx, SignupConfig, users, access)` binds the same user/v2
owner to password/browser OAuth capture. Supply `Enabled`, `EvidenceCookieName`
and the owning `Capture` configuration explicitly. Cookie names must match the
consented referral issuer, and capture types/program must match host eligibility.
Missing/different user ownership, invalid cookie names and missing capture
capabilities fail before enabling access. Cookie name validation precedes capture
installation. Disabled capture requires no owners and observes cancellation.
The helper issues no cookie, consent or attribution, retrofits no existing
account and creates no grant. Native OAuth without a browser cookie captures
empty evidence; the owning consumer independently verifies signed evidence.

```go
err := partnermanagerhelper.ConfigureSignupCapture(ctx,
    partnermanagerhelper.SignupConfig{
        Enabled: captureEnabled,
        EvidenceCookieName: signupCookieName,
        Capture: userv2.SignupAttributionConfig{
            ProgramID: programID,
            IndividualAccountTypes: individualAccountTypes,
        },
    }, users, access)
```

These are trusted host choices; they are not request/body fields. Signup evidence
cookies and browser CSRF cookies have separate purposes. For CSRF, pass the host's
configured guard and ordered member binding to [partnerhttp](../http/README.md).
Use [partneraccess](../../partneraccess/README.md)'s current member/session and
service-account adapters with the same explicit host admission hook.

## Billing capture and explicit worker execution

`NewExecution(ctx, ExecutionConfig, ExecutionDependencies)` composes an existing
prepared [runtime](../runtime/README.md), owning billing manager, provider registry,
user owner and explicit worker identity/current policy. Supply the payer's allowed
account types/statuses, policy system, `RevenueCapture` and optional native worker
configuration/cadence. Disabled capture plus no worker acquires nothing and returns
nil. Capture without a worker still attaches billing capabilities and returns nil.
An enabled worker returns `Execution`, with `RunOnce` and validated `Interval`.

The helper creates a separate worker manager over the identical financial owners.
It checks all three current signup/revenue/maturity worker grants before attaching
capabilities. `RunOnce` binds the same instance-private worker authority afresh;
human sessions cannot become worker invocations. It never replaces human manager
authority, seeds a grant, prepares history, discovers work or starts a goroutine.
Provider evidence dispatch uses the [billing helper](../../billingmanager/helper/README.md)
and retains native scope/intent verification. Billing capture/reconciliation
capabilities attach to the supplied billing manager during startup; complete this
configuration before admitting handlers or starting any work.

The host explicitly schedules sequential passes, applies cancellation/deadlines,
logs only approved bounded report fields and drains borrowers before closing
storage. Construction performs current identity/grant reads for an enabled
worker; it does not contact remote providers. Lifecycle pipeline composition is
separate in [billinglifecycle/helper](../../billinglifecycle/helper/README.md).

## Verification boundary

Named tables cover callback/owner refusal, identity mismatch, constructor limits,
late cancellation and immutable capture independence. The signup test uses a real
disconnected owner to prove installation requires no storage I/O. Provider dispatch
has its own table suite. Native host integration checks prepared owners, scoped
worker grants, startup/restart, revocation and retained financial recovery; these
are separate from routing fixtures and do not establish production deployment.
