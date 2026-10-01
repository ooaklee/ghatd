# ADR0025: Share signed provider identities across web and native sign-in

- Status: accepted
- Date: 2026-09-30

## Context

Applications need Google and Apple as alternatives to passwordless email.
The earlier Google flow retrieved a profile and found accounts by email,
without durable issuer/subject ownership, nonce or PKCE protection. Apple also
requires a cross-site POST callback and supplies names only on first consent.

## Decision

Add optional secure provider, identity repository and signed authentication-time
capabilities while preserving existing host interfaces. The access manager
fails closed for legacy providers rather than invoking email-based login.

Persist provider identity and a verified ACTIVE account in one MongoDB insert.
An explicit migration enforces unique email and unique sparse identity keys.
Keys hash the unambiguous JSON pair of canonical issuer and subject. Duplicate
same-identity inserts return the winning account; matching email with another
identity requires explicit linking. Identity metadata is private BSON state
excluded from JSON. Stale full-user updates cannot replace it. Provider names
are optional without relaxing ordinary email signup requirements.

Use server-held single-use Redis transactions for state, nonce, Google S256
PKCE, completion mode, safe return paths and linking proof. Verify signed ID
tokens before persistence. The transient cookie is host-only, HttpOnly and
limited to the provider callback path: Lax for Google, None plus Secure for
Apple. Normal application session cookie policy remains unchanged.

Linking requires a same-origin POST and recent signed `auth_time` from the
initiating access session. Recheck that session in Redis and the account's
current status at callback, then atomically add the identity. A refresh carries
the original authentication time forward; another session's fresh login does
not upgrade it. Legacy sessions lacking this claim need reauthentication.
Multiple explicitly authorised identities may belong to one account; an
identity may never belong to two accounts.

Conditional provider-login writes update timestamps only for verified ACTIVE
accounts. Restricted accounts are never activated by a provider callback.
New-account billing and group association use the existing signup actions;
linking preserves account ID, profile, roles, memberships and subscriptions.

Browser mode is captured at initiation and completes with normal cookies and
a 303 redirect to a validated rooted path with query/fragment. API initiation
retains the token-metadata response. Failures select fixed codes and static
messages; credentials, codes, tokens and provider profiles are not logged.

## Consequences

Hosts must apply the identity migration, register exact provider callback URLs,
configure their trusted browser origin and keep Apple keys outside Git. All-empty
provider configuration disables that provider; incomplete configuration is an
operator error. Apple live tests need public HTTPS. Strict ID-token validation
allows 30 seconds of clock skew; transaction expiry is absolute. Operational
JWKS refresh throttling can cause a short fail-closed window during rotation.

The new HTTP lifecycle checks run against actual MongoDB and Redis with signed
provider fixtures and cover creation, repeat Apple login, linking, collision,
restricted statuses, refresh/logout and cancellation. Separate live-provider
checks are required when credentials and consent are available.

## Native handoff and host adoption

The native flow reuses provider verification, identity resolution/linking and
normal session issuance. A system authentication browser has a separate cookie
store from the app. GHATD therefore binds a short-lived start ticket and one-use
return grant to app-generated state and an independent S256 verifier. No access
or refresh token crosses a deep link. Account writes and linking happen only
at exchange, with the same signed-session and account-status checks.

The framework owns this protocol; hosts own environment parsing, secret loading,
provider registrations, migrations, public origins and platform callback setup.
Vue/Flutter views and native secure-cookie adapters remain in their clients.
A [shared adoption guide](../how-to/add-google-apple-sign-in.md) and
[compiled composition example](../../examples/oauth/README.md) carry the reusable
setup knowledge. No Bedrock-specific environment or UI API is added to core.

Live Google/Apple web and Android companion login have been confirmed against
the shared backend. iOS, broader native acceptance and actual relay email
receipt remain separate validation work, recorded in the adoption guide.
