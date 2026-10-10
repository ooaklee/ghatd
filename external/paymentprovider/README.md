# Payment provider


## Stripe promotion-code entry

`Config.AllowPromotionCodes` is a trusted host opt-in, captured at construction,
for Stripe payment and subscription checkout. Its default omits the parameter;
enabling it emits `allow_promotion_codes=true` when building the checkout form.
It is not a checkout-request field and must never come from browser input.
Provider coupon eligibility and host commercial policy remain separate from
paid-access and revenue evidence. The option changes neither Price validation
nor request metadata, idempotency or other provider endpoints. Hosts can use
the [construction helper](helpers/README.md#provider-construction-options) to
bind this option, an HTTP client and revenue configuration to one instance.

## Authenticated paid-revenue evidence

`RevenueProvider` is an optional capability separate from webhook/access,
checkout, portal and invoice-preview contracts. Stripe opts in only when
`Config.Revenue` supplies an explicit merchant account, live/test mode, allowed
connected accounts and approved currency exponents. The provider copies this
configuration at construction. `Environment`, API-key prefixes, checkout totals,
current prices and an active subscription are not financial evidence.

Optional `RevenueSubscriptionProvider.LookupRevenueSubscription` supplies
minimal current lifecycle evidence for an exact subscription. Stripe reuses the
authenticated merchant lookup, explicit account allowlist/mode and pinned API
version, then checks the returned subscription ID, object, mode, customer,
recognized status and explicit scheduled-cancellation flag. Unknown/missing or
contradictory evidence is unassessable; outages remain errors. Redirects are
not followed and response size is bounded. No metadata/email search, legacy
access flag or catalogue-price fallback establishes the result. These private
fields are excluded from public JSON. The capability neither proves paid
revenue nor enables a scheduler. See Stripe's
[retrieve subscription](https://docs.stripe.com/api/subscriptions/retrieve) and
[subscription object](https://docs.stripe.com/api/subscriptions/object) contracts.

`ResolveRevenueWebhook` verifies the delivery signature before returning scope
or making an API request. Invoice evidence is retrieved with authenticated,
pinned-version calls and checked against the authenticated account identity.
The signed event mode must agree with every retrieved financial object. Complete
invoice-payment and line lists are collected before returning a result; failed,
duplicate, stalled or oversized pagination returns no partial allocation.

`RevenueDeliveryVerifier.VerifyRevenueDelivery` authenticates the native scope,
envelope ID and canonical source digest without querying the provider API. An
owning billing manager can use that identity to acknowledge an already committed
delivery during an API outage. Fresh signature verification remains required;
changed source evidence conflicts, and a quarantined receipt stays unresolved
until explicit source reconciliation. New deliveries still require economic
lookup and durable acceptance.

### Versioned source fingerprints and retained snapshot recovery

New Stripe revenue identities use `stripe-revenue-v2:` followed by a canonical
SHA-256 digest. For a `charge.refunded` event whose snapshot object is a charge,
only the top-level `data.object.receipt_url` is excluded. Stripe documents this
as a URL to view the current [charge receipt](https://docs.stripe.com/api/charges/object#charge_object-receipt_url),
rather than financial allocation evidence. Rendered receipt URLs can differ
between an original delivery and authenticated event retrieval. Every other
field retained by the existing snapshot contract remains bound, including
amounts, currency, payment references, merchant/mode, event identity and time.
Nested fields named `receipt_url`, other URLs, invoice and dispute snapshots
are not excluded. Canonicalization does not modify the supplied snapshot.

The verifier and financial resolver also return a private
`LegacySourceFingerprint` for exact replay/reconciliation of an unchanged
pre-versioned snapshot. A changed legacy hash is still a conflict; it is never
silently converted or substituted for the original source.

For a legacy quarantine whose authenticated event representation differs,
`RevenueSnapshotVerifier.VerifyRetainedRevenueSnapshot` validates an explicitly
retained original snapshot against the **exact original legacy hash**, selected
scope and envelope. It derives the versioned hash locally, without provider
requests or a fabricated signature. The billing manager separately retrieves
the authenticated original event and requires its versioned hash to match that
derived identity before resolving economics. Missing/mismatched original bytes
cannot establish the bridge. This optional native recovery input is private,
bounded by the webhook-body limit and excluded from JSON; the revenue feed does
not retain the raw snapshot. Hosts must supply a trusted recovery procedure,
not a customer/operator HTTP payload shortcut. Preserve the original source and
use its owning immutable resolution for retry recovery.

Supported paid-invoice evidence requires a fully paid invoice, one authenticated
successful payment intent, matching customer/currency, explicit ex-tax line
bases and complete discount/pretax-credit data reconciled to the invoice's
`total_excluding_tax`. Pretax credits already include discounts and are
subtracted once. Each line retains its historical provider price and subscription
reference; the owning billing boundary must independently resolve the paying
principal and immutable plan/cost mapping.

A `charge.refunded` delivery supplies an immutable signed cumulative snapshot.
Full refunds reverse the original verified net, not its tax-inclusive gross.
Stripe's [Refund objects](https://docs.stripe.com/api/refunds/object) omit
`livemode`. Refund evidence instead requires an authenticated charge-scoped
list, the parent charge's verified mode, and exact original charge/payment and
currency references. A supplied contradictory or malformed refund mode is
rejected. Invoices, charges, credit notes and credit-note lines still require
their own matching mode fields.
Partial refunds require complete successful refund references and matching
credit-note line allocations whose net totals reconcile. Changed cumulative
snapshots, refunds without exact line evidence, partial disputes without
allocation evidence, multiple successful invoice payments, customer-balance
settlement, pre-payment credit notes and other unsupported fiscal shapes remain
quarantined for owning-service reconciliation. Individual refund lifecycle
webhooks retain existing ledger handling; they do not independently establish a
cumulative commission reversal. API outages are retryable errors, not no-revenue
decisions. This capability does not submit charges, refunds or partner payouts.

`RevenueReconciliationProvider.ReconcileRevenueEvent` retrieves the original
provider event through the authenticated API. It accepts only persisted source
scope/identity supplied after billing's current reconciliation authority check;
it does not fabricate a webhook signature. The canonical signed-source digest
binds recovery to the original event without storing raw payload or email in the
revenue feed. Providers without this optional capability remain supported for
existing payment/access use cases.

The evidence contract follows the provider's [event](https://docs.stripe.com/api/events/object),
[invoice](https://docs.stripe.com/api/invoices/object),
[invoice payment](https://docs.stripe.com/api/invoice-payment/object),
[invoice line](https://docs.stripe.com/api/invoice-line-item/object) and
[credit note](https://docs.stripe.com/api/credit_notes/object) definitions.
Provider tests use controlled HTTP fixtures; they do not establish live-provider
operation. Applications must configure webhook event coverage and current
scoped reconciliation authority explicitly before opting in.

## Optional checkout evidence for revenue identity

`RevenueCheckoutProvider` adds authenticated merchant scope, acknowledged
session retrieval and original subscription-checkout evidence. Stripe uses the
documented subscription filter, fetches every session and line-item page, and
requires one completed subscription session and one original recurring line.
Customer/subscription IDs, merchant scope, explicit live mode, original price
amount/cadence/currency and client reference are retained for owning comparison.
Missing, multiple or contradictory evidence is unassessable; API/pagination
outages remain errors. No current catalogue, email or entitlement lookup occurs.

The separate optional `RevenueCheckoutSessionEvidenceProvider` retrieves that
same complete evidence using an already retained checkout session ID, before a
subscription payment is required. Stripe uses authenticated
[session retrieval](https://docs.stripe.com/api/checkout/sessions/retrieve) and
[complete line-item pagination](https://docs.stripe.com/api/checkout/sessions/line_items).
An owning billing integration must match the original frozen intent before
using this evidence for a lifecycle association. A completed unpaid or trial
checkout and its original recurring price do not establish paid revenue or
commission. This capability supplies no association write or status collector.

An opaque `checkout_intent_id` metadata pointer is useful only with an already
persisted billing authorization. The owning billing service supplies payer and
plan/cost from that record; provider metadata does not establish them. New
captured checkout submissions also check price/session live mode and returned
intent/reference correlation. Managers retrieve known sessions via GET instead
of recreating a POST after idempotency retention expires.

## Optional original checkout status

`CheckoutStatusProvider` supplies a fresh read for an already retained session;
its `CheckoutStatusEvidence` is separate from persisted revenue and lifecycle
evidence. Stripe authenticates the configured merchant and uses GET-only
[session retrieval](https://docs.stripe.com/api/checkout/sessions/retrieve) and
[complete line-item pagination](https://docs.stripe.com/api/checkout/sessions/line_items).
It checks explicit live mode, original intent/reference, one quantity-one price,
unit amount, currency, mode and cadence. The owning billing service must then
match every field against the immutable original authorization.

The session/payment status pair produces `paid`, `no_payment_required`, `pending`,
`unpaid` or `expired`; unknown or contradictory pairs return no evidence. A
completed trial is not a payment, and this read does not establish paid revenue
or commission. No checkout submission, metadata update, access grant or revenue
capture occurs. Controlled HTTP tests verify this contract; they do not replace
qualification of the host's authenticated recovery flow and webhook fulfilment.

Stripe may label a zero-total trial invoice `paid`. The status read requires an
explicit nonnegative session total and reports zero-total completion as
`no_payment_required`, only when the frozen intent authorizes a subscription
trial. A positive total is required for `paid`; an unknown total fails closed.
This completion read is not a refund-adjusted balance or a revenue receipt.
