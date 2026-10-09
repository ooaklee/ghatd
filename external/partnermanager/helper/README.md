# Partners referral route helpers

`partnermanagerhelper` is an optional transport layer around the owning
`partnermanager.Manager.PrepareVisit` operation. It owns anonymous consent,
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
This helper supplies neither customer/admin JSON routes nor authentication,
grant provisioning, signup service composition or worker scheduling.

See [runtime composition](../runtime/README.md) and
[authority](../../partneraccess/README.md) for the separate owner boundaries.
