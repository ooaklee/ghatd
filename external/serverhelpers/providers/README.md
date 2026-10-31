# Provider composition

This package combines existing provider owners using resolved typed configuration.
It does not read environment variables, discover secrets, open storage or send
provider requests. Hosts still choose their enabled capabilities explicitly.

`BuildPayments(PaymentConfig)` adds optional Ko-fi then optional Stripe. A blank
or whitespace-only Ko-fi secret omits that provider. A configured secret is kept
byte-for-byte for webhook verification. `Stripe: nil` omits Stripe; a supplied
`paymentprovider/helpers.StripeSettings` must have passed `Configure`, and its
owner revalidates current values on construction. `StripeOptions` carries the
borrowed HTTP client, optional revenue policy and explicit promotion-code choice.
The helper adds no commercial defaults or entitlement reconciliation.

Payment errors retain already constructed providers, matching the owner append
contract. **Abort startup on error** rather than serving a partial registry.
The error identifies the construction phase without echoing configured keys.

`BuildOAuth(OAuthConfig)` constructs configured Google then Apple providers using
[`oauth/helper`](../../oauth/helper/README.md) and one borrowed secure transaction
store. All-blank credentials omit a provider; partially configured credentials
fail construction. Failure returns no partial provider list. Apple retains the
owning helper's encoded-key-before-file precedence and may read the explicitly
configured local key file. Callback, signing-key and transaction validation stay
with the secure providers. Mobile OAuth configuration remains a direct call to
Access Manager and is not enabled implicitly.

Email already has a complete builder at
[`emailmanager/helper`](../../emailmanager/helper/README.md); use it directly.
Its registry-free remote default is Bird. Explicit registry providers, including
SparkPost, retain their selected behavior. Notifier sender composition likewise
stays with `notifier.NewStandardSenders`.

`go test ./external/serverhelpers/providers` covers optional ordering, exact Ko-fi
authentication, disabled inputs, Stripe error preservation and OAuth partial
configuration without network or transaction-store requests. Provider-specific
transport, financial replay and signature contracts remain in their owning suites.
