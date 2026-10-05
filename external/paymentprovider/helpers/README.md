# Shared Stripe settings

`StripeSettings` lets a host share Stripe defaults, startup validation and
provider construction while retaining ownership of credentials and frontend
routes. Embed it in the host's settings or populate it directly. Its
`envconfig` tags support environment loading, but the helper does not load the
environment itself.

Call `Configure` after loading settings and before `NewProvider` or
`AppendProvider`. This complete helper function uses explicit example routes;
its caller supplies settings from the host's normal configuration source:

```go
package bootstrap

import (
    "github.com/ooaklee/ghatd/external/paymentprovider"
    paymenthelpers "github.com/ooaklee/ghatd/external/paymentprovider/helpers"
)

func PaymentProviders(settings *paymenthelpers.StripeSettings, environment, frontendURL string) ([]paymentprovider.Provider, error) {
    if err := settings.Configure(paymenthelpers.StripeConfiguration{
        Environment:              environment,
        FrontendBaseURL:          frontendURL,
        CheckoutReturnPath:       "/checkout/complete?session_id={CHECKOUT_SESSION_ID}",
        CustomerPortalReturnPath: "/settings/billing",
    }); err != nil {
        return nil, err
    }
    return settings.AppendProvider(nil)
}
```

Pass the returned slice to `starter.NewServicesRequest.PaymentProviders` as
described in the [Starter payment guide](../../starter/v0/README.md#payment-checkout-and-customer-portal-capabilities).
`AppendProvider(existing)` retains other providers. When all three credentials
are absent and configuration is otherwise valid, Stripe is disabled and the
slice is unchanged; `NewProvider` returns `(nil, nil)` in that case. Do not
append that nil provider manually.

## Settings and validation

| Field | Purpose |
| --- | --- |
| `StripeAPIKey`, `StripePublishableKey`, `StripeWebhookSecret` | Complete credential set required when Stripe is enabled; partial sets fail |
| `StripeAPIBaseURL` | Defaults to `paymentprovider.StripeDefaultAPIBaseURL` (`https://api.stripe.com`) |
| `StripeAPIVersion` | Defaults to `paymentprovider.StripeDefaultAPIVersion`; see [the pinned contract](../const.go) |
| `StripeSuccessURL` | Explicit checkout return URL, or one resolved from `CheckoutReturnPath` |
| `StripePortalReturnURL` | Explicit portal return URL, or one resolved from `CustomerPortalReturnPath` |
| `StripePortalConfigurationID` | Optional Stripe portal configuration; requires complete credentials |

An enabled integration requires a nonblank environment and matching recognized
test/live prefixes on the secret/restricted API key and publishable key. This
checks configuration, not credential validity against Stripe, and does not
force live-mode keys in a production-named environment.

Return URLs must use the configured frontend origin. Outside `local`, `dev`,
`test` and `development`, they must use HTTPS and the API base must be Stripe's
public HTTPS origin with no custom path, query or fragment. Environment matching
ignores case and surrounding whitespace. The relaxed local API-base policy
supports isolated test servers; it does not relax the same-origin return rule.

Empty default return paths leave those capabilities disabled unless explicit
URLs are configured. `DefaultStripeConfiguration(environment, frontendURL)`
selects the built-in checkout and portal paths;
`StripeCheckoutConfiguration` selects only the built-in checkout path. Use an
explicit `StripeConfiguration` when your frontend routes differ. Helper
construction also validates the provider's checkout and portal configuration.

## Catalogue and operator verification

Successful settings validation does not provision Stripe objects or prove that
a Product/Price belongs to the intended account and mode. Retrieve and verify
objects with that environment's credentials before saving their references.
Keep account/mode-specific IDs out of shared template defaults and never treat
fake fixture IDs as real Stripe test-mode objects.

When Billing Manager calls Stripe Checkout with trusted catalogue expectations,
the adapter rechecks the exact Price ID, active state, amount, currency,
one-time/recurring type and recurring interval/count. It does **not** compare
the Price's Product ID, tax behaviour, lookup key or provisioning metadata with
the catalogue. Those remain operator checks; the shared publish validator also
does not enforce Product-to-Price correspondence. An existing Product ref is
not evidence that those checks passed.

Follow [Pricer's catalogue workflow](../../pricer/README.md#bind-save-and-publish-a-stripe-catalogue)
and [Billing Manager checkout ownership](../../billingmanager/README.md#checkout-ownership-and-fulfilment).
Saving refs, publishing a plan, creating a Checkout session and confirming
webhook-derived paid access are separate outcomes. This guidance does not
change provider validation or perform provider mutations.

## Telemetry and ownership

Start the [observability runtime](../../observability/README.md#bootstrap)
before constructing providers. Stripe's default HTTP client is instrumented and
retains its ten-second timeout. Application calls must carry the active context
for parentage. The lower-level `paymentprovider.Config.HTTPClient` option remains
available for hosts with custom client policies; supplying a client leaves its
transport instrumentation and timeouts to the host.

Helper failures have stable `PPH0` error-manifest codes through
`PaymentProviderHelperErrorMap`, included in GHATD's default error bundles.
The helper is optional: existing low-level provider construction remains
available. Never log the settings struct or commit loaded credentials.
