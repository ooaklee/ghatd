# Add Google and Apple sign-in to a GHATD application

Use this guide to add browser and native sign-in alongside the existing email
magic-link/code flow. It applies to any GHATD host. The
[OAuth package](../../external/oauth/README.md) owns provider
verification; [Access Manager](../../external/accessmanager/README.md#secure-google-and-apple-sign-in)
owns the browser/native HTTP contract. A [compile-checked composition example](../../examples/oauth/README.md)
shows how to connect the existing APIs, with [placeholder configuration](../../examples/oauth/.env.example).

## Prerequisites and ownership

Use a GHATD revision containing secure providers and native handoff, MongoDB
with the OAuth identity indexes, Redis, your normal email/auth services and
host-controlled session-cookie settings. Browser and native clients share the
same backend accounts and normal access/refresh sessions.

| GHATD supplies | Your application supplies |
| --- | --- |
| Google/Apple code exchange and signed identity validation | Provider registrations, client IDs and backend-only secrets |
| Atomic state/nonce/PKCE transactions and native grants | Redis client, app/environment namespace and lifecycle |
| Stable issuer/subject identity lookup, explicit linking, session issuance | Database, migration registration and rollout |
| Browser/native endpoints and fixed errors | Public origin, proxy, cookie/CORS policy and exact app callback allowlist |
| Existing refresh/logout and email authentication | UI, system auth browser, native secure cookie persistence and platform registrations |

The Vue/Flutter screens, environment loader and Compose key mounts belong in
the host. They do not need to move into the Go framework. Hosts may use
[starter/v0](../../external/starter/v0/README.md#google-and-apple-sign-in) or compose
the packages directly.

## 1. Register the providers

Pick the public origin first. These are different kinds of callback:

| Purpose | Example | Where to configure it |
| --- | --- | --- |
| Google provider callback | `https://app.example.com/api/v1/ams/oauth/google/callback` | Google web OAuth client and backend provider constructor |
| Apple provider callback | `https://app.example.com/api/v1/ams/oauth/apple/callback` | Apple Services ID return URLs and backend provider constructor |
| Native app return | `com.example.yourapp:/oauth/callback` | GHATD allowlist and Android/iOS client registration |

Google: configure the project's consent/branding and intended audience (including
test users where required), then create a **Web application** OAuth client. Add
each exact authorised redirect URI. Keep the client secret on the backend. This
flow uses server redirects/code exchange; it does not require embedding a Google
JavaScript SDK in your app. See Google's [web-server setup](https://developers.google.com/identity/protocols/oauth2/web-server).

Apple: enable Sign in with Apple on a primary App ID, associate a Services ID
with it, and register the web domain plus exact HTTPS return URL. Use the
**Services ID** as GHATD's Apple client ID. Create a Sign in with Apple key and
supply its team ID, key ID and PKCS#8 P-256 `.p8` contents to the backend.
Follow Apple's [web configuration](https://developer.apple.com/help/account/capabilities/configure-sign-in-with-apple-for-the-web/)
and [key setup](https://developer.apple.com/help/account/capabilities/create-a-sign-in-with-apple-private-key/).
GHATD signs short-lived client secrets automatically; operators still own
rotation/revocation of the underlying key.

For this system-browser mobile flow, reuse the same Google web client and Apple
Services ID as the web host. Do not register the private app URI with Google or
Apple. A different provider client/team or a separately implemented native SDK
flow is not automatically interchangeable with these verified audiences and
provider subjects.

### Apple signing keys in containers and secret stores

`oauth.NewAppleProviderRequest.PrivateKeyPEM` accepts the decoded PKCS#8 P-256
PEM. Your host can load it from a narrowly mounted file or decode a standard-base64
secret in memory. GHATD does not parse environment variables or create temporary
key files. The [key-loading example](../../examples/oauth/apple_key.go) can be
copied into a host adapter alongside the [environment placeholders](../../examples/oauth/.env.example).

For the optional `APPLE_OAUTH_PRIVATE_KEY_B64` convention, a non-empty value takes
precedence over `APPLE_OAUTH_PRIVATE_KEY_PATH`. Use strict standard-base64 decoding
of the complete `.p8` PEM, including its original line breaks. Invalid encoding
must fail startup without falling back to an older file; the provider constructor
then rejects invalid PEM or a key on another curve. Return fixed configuration
errors without logging key contents, encoded values or secret file paths. Keep
providers disabled when all their settings are empty, and reject partial settings.

Base64 is encoding, not encryption. Store both raw PEM and encoded values as
secrets, such as AWS SSM `SecureString` parameters under your service/environment
prefix. One explicit deployment source selector should request either
`APPLE_OAUTH_PRIVATE_KEY` for a file or `APPLE_OAUTH_PRIVATE_KEY_B64` for an
environment value. Provision and verify the separate encoded parameter before
switching sources, deploy a compatible application image, and omit the Apple
volume and mount in encoded mode. Retain the raw parameter until rollback is no
longer needed. Never commit either value, expose it through frontend build
variables, or inject it into migrations, sidekicks or unrelated workers.

For file mode, project only the Apple key, read-only and readable only by the
server's user (for example `0400`), into a dedicated directory such as
`/run/your-service-apple-oauth`. Match the owner or group to the container's user.
Avoid mounting an entire read-only Secret at `/run`, `/var/run`, `/run/secrets`
or `/var/run/secrets`: `/var/run` may be a symlink to `/run`, and Kubernetes must
still create its nested `/var/run/secrets/kubernetes.io/serviceaccount` mount.
A dedicated Apple directory keeps these mounts disjoint. An individually mounted
file does not have the same parent-directory conflict, but the dedicated path
remains a clear deployment convention.

## 2. Apply the identity migration

Register `user/v2/migrations.InitUsersOAuthIndexesUp(ctx, db)` through your
[host migrator](manage-mongodb-migrations.md), before enabling providers. It
creates the unique email index and sparse unique private identity-key index on
`users`. The provider write path fails closed if its required indexes are missing.
Resolve existing duplicate emails/identities deliberately; the migration does
not merge accounts. `InitUsersOAuthIndexesDown` removes only the identity index
and preserves email uniqueness. Disable provider entry points before rollback.

Accounts are resolved by signed issuer/subject, not by email. A matching email
on another account requires the person to sign in to that account and explicitly
connect the provider. Existing users from the legacy email-only Google helper
have no guaranteed stored provider identity; plan this linking path for them.

## 3. Wire the existing GHATD APIs

The [composition example](../../examples/oauth/setup.go) performs these steps:

1. Reuse the host Redis client with
   `oauth.NewRedisTransactionStore(client, "your-service:development")`.
2. Construct `oauth.NewGoogleSecureProvider` and/or `oauth.NewAppleProvider`.
   Pass complete settings and the shared transaction store. Supply the Apple key
   bytes from your secret-store adapter; GHATD does not read secret files or `.env`.
3. Pass providers to `starter.NewServicesRequest.OAuthServices`, or directly to
   `accessmanager.NewServiceRequest.OauthServices`. Keep the normal `user/v2`,
   auth, email, ephemeral-store and audit dependencies.
4. Set `OAuthOrigin` on `starter.NewHandlersRequest` or
   `accessmanager.NewHandlerRequest` to the exact trusted frontend origin.
   Configure the usual access/refresh cookie names, domain and environment.
5. For native support, call `handlers.AccessManager.ConfigureMobileOAuth` (or
   the directly constructed handler's method) **once, before serving**, with
   `MobileOAuthConfig{Origin, RedirectURIs, Store}` and
   `accessmanager.NewRedisMobileOAuthStore(client, namespace)`. Check its error.
6. Attach the normal Access Manager routes; starter's default route attachment
   includes the optional discovery, linking and mobile handlers.

Use `nil`/no provider entry for a disabled provider. In your environment adapter,
all-empty fields may disable it; partially supplied fields must fail startup,
not silently disable it. The legacy `NewGoogleProvider` remains source-compatible
but is not accepted for Access Manager's secure sign-in. New hosts must use the
secure constructor.

The example derives every enabled provider callback from the same origin as
mobile handoff. If adapting it to separate callback settings, validate that their
scheme, host and exact `/api/v1/ams/oauth/{provider}/callback` paths agree with
`MobileOAuthConfig.Origin` before serving. `ConfigureMobileOAuth` validates its
own origin/allowlist/store, but cannot inspect the private callback configuration
of arbitrary provider implementations.

Use separate Redis namespaces and credentials per environment. Host reverse
proxies must preserve callback query strings and Apple's form-POST body. Exempt
the Apple callback from host middleware that would reject all cross-site form
POSTs; GHATD still validates the transaction cookie and one-use state. Don't
wrap or bypass its callback verification. The transient Apple cookie is
`HttpOnly; Secure; SameSite=None`; Google's uses `Lax`. Both are host-only and
scoped to the callback path. Your existing normal session-cookie policy applies
after success. Keep OAuth request bodies, callback query strings, cookies and
provider tokens out of application/proxy/telemetry logs.

## 4. Connect web and mobile clients

Browser clients discover enabled providers using
`GET /api/v1/ams/oauth/providers` (`data.providers`). Show those choices on both
signup and login, alongside email. Navigate the browser to, for example:

```text
/api/v1/ams/oauth/google/login?browser=true&request_url=%2Fapp
```

`browser=true` is essential for browser completion: GHATD sets ordinary session
cookies and redirects with HTTP 303. No custom callback wrapper is needed.
On return, confirm the session with `GET /api/v1/ums/me` before routing into the
app. Use credentialed requests when your API is on another origin. Display fixed
`oauth_error` messages, preserve only validated local return paths, and unlock
controls on cancellation/browser Back. Keep the email link and code fallback.
For connecting a provider to an existing account, use the explicit fresh-session
link endpoint described in [Access Manager](../../external/accessmanager/README.md#secure-google-and-apple-sign-in);
matching email never authorises a merge.

Native clients use the [native handoff contract](../../external/accessmanager/README.md#native-app-handoff):

1. Discover `data.providers` and `data.redirect_uris` from
   `GET /api/v1/ams/oauth/mobile/providers`. Hide unsupported/unconfigured choices.
2. Generate independent cryptographically random state and S256 verifier in the
   app, then create the provider's mobile login/link start ticket.
3. Open the returned `data.authorization_url` in the system authentication
   browser. This browser needs its own transaction cookie; it cannot share the
   app's HTTP-only cookie jar automatically.
4. Validate the exact returned app URI and original state, then exchange the
   one-use code with the original verifier through the app's HTTP client.
5. Persist the response's ordinary session cookies through the existing secure
   cookie jar, then confirm `/api/v1/ums/me`. Reuse normal refresh/logout logic.

Only a one-use code crosses the deep link. Do not copy browser cookies into the
app, put access/refresh tokens in the URL, or create a second account/token store.
Do not retry one-use exchanges automatically. Cancel stale attempts and avoid
late callbacks restoring a session after logout. Each client must implement the
same HTTP contract using its platform's secure credential storage.

Native handoff is disabled with an empty allowlist. Its current callback format
is exactly `com.example.yourapp:/oauth/callback` (one slash, no host/query/fragment).
Choose your own reverse-domain scheme. Register that scheme in Android's callback
activity and iOS URL types, and use the same full URI in the backend allowlist and
client. Universal/App Links require separate support; they are not accepted by
this private-scheme allowlist.

## 5. Test locally with HTTPS

Google can use an explicitly registered loopback callback such as
`http://localhost:5173/api/v1/ams/oauth/google/callback` for web development.
Apple [requires HTTPS and rejects localhost/IP callbacks](https://developer.apple.com/documentation/signinwithapple/configuring-your-webpage-for-sign-in-with-apple).
Use a tunnel such as ngrok to the local entry point that serves the UI **and**
proxies `/api` to the backend. For a Vite host on port 5173:

```sh
ngrok http 5173 --host-header=localhost:5173 --inspect=false
```

Configure the assigned HTTPS origin in the host and register the exact Google
and Apple HTTPS callbacks there. Open the web app through that HTTPS origin;
use it as the phone's API base URL too. The phone's `localhost` is the phone.
For mobile, every enabled provider callback must use the same HTTPS origin as
handoff. Keep cookies host-only (`CookieDomain` empty) for local tunnel testing.
Start the backend and frontend with your host application's documented
development commands; GHATD does not prescribe a frontend build tool or script.

If the hostname changes, update provider-console registrations, callback URLs,
allowed frontend origin and client API base URL, then restart the host. The ngrok `--host-header` flag is currently deprecated; use its documented
traffic-policy equivalent when upgrading the agent. Limit
Vite allowed hosts to your exact tunnel hostname if Host rewriting isn't used.
Disable tunnel inspection and callback/body logging. See the [ngrok CLI reference](https://ngrok.com/docs/gateway/agent/cli).
Keep each service's secrets outside Git; mount only the dedicated `.p8` file
read-only when using containers. A path on the host is not the container path.

## 6. Support Apple Hide My Email

Keep the signed relay email as the account email. Repeat Apple authorisation can
omit email and name; GHATD reuses a known issuer/subject without overwriting the
stored profile. A relay address and a Google address do not imply two identities
should be merged. Use explicit linking while signed into the intended account.

For email magic links/codes to reach the relay, register the actual sending
addresses/domains with Apple's private relay service and configure SPF/DKIM
alignment with your mail provider. Follow [Apple's relay configuration](https://developer.apple.com/help/account/capabilities/configure-private-email-relay-service/).
Test actual receipt at a relay address. A successful Apple login or an email
captured by a local development inbox does not validate outbound relay delivery.

## 7. Record acceptance and roll out

Acceptance belongs to each adopting application. Record the tested framework
revision, client build, environment, provider and platform. Success in one host
or device does not certify another deployment.

For each adopting application, record:

- First and repeat sign-in from login/signup; verify the same backend account ID
  across web/mobile, including first-only Apple fields and Hide My Email.
- Cancellation and retry, explicit linking from a fresh session, and rejection of
  stale/revoked/restricted sessions. Existing memberships/profile must survive linking.
- Normal session refresh, logout and native process restart with secure persistence.
- Real relay-email receipt, staging callback/secret configuration, and platform
  callback handling on both Android and iOS. iOS builds require macOS/Xcode.
- Keyboard/assistive-technology operation and provider button guidance from
  [Apple](https://developer.apple.com/design/human-interface-guidelines/sign-in-with-apple)
  and [Google](https://developers.google.com/identity/branding-guidelines).

Native repeat-ID/linking/refresh scenarios, iOS and actual relay email delivery
need their own evidence; successful Android login does not establish them.
Automated signed-provider tests with isolated MongoDB/Redis
cover the security lifecycle; commands are in the [OAuth package guide](../../external/oauth/README.md).
Do not use a production store or report credentials, tokens or provider subjects
in test evidence.

Merge the GHATD feature before downstream hosts and repin their Go dependency to
the merged revision. Ship the host-owned identity migration before enabling the
provider credentials. Use staging registrations/secrets first, then verify the
production origins, mobile allowlist and email sender configuration.


## Show connections and safely disconnect a provider

Public `GET /api/v1/ams/oauth/providers` describes deployment configuration.
Settings screens must use **authenticated** `GET /api/v1/ams/oauth/connections`
to show which Google/Apple identities are actually linked to the current user.
Its `data` contains `connected`, `available`, `email`, `disconnect_available`,
and `replacement_email_required_for` (provider names requiring a new email).
A provider can remain connected while its deployment configuration is disabled.
Do not infer connection state from discovery or an OAuth callback query string.

Enable verified disconnects during composition, before serving requests:

```go
err := services.AccessManager.ConfigureOAuthConnections(accessmanager.OAuthConnectionsConfig{
    Origin: origin, // exact frontend origin, matching handlersRequest.OAuthOrigin
    Store: oauth.NewRedisDisconnectChallengeStore(redisClient, namespace),
})
```

The compiled [composition example](../../examples/oauth/setup.go) includes this
setup. Use the ordinary GHATD auth signer, user/v2 repository, Redis runtime and
email manager. Custom adapters must support the separate optional connection
repository capability (including atomic `LinkOAuthIdentityAtRevision`) and
signed email revisions; unsupported adapters retain
provider login but do not advertise disconnect availability. No extra env vars
or migration are required: legacy account/token revisions are zero.

An Apple Sign in with Apple relay address cannot become the sole sign-in method
when disconnecting a provider. GHATD recognises the exact domains
`privaterelay.appleid.com` and `private.icloud.com`; ordinary `icloud.com`
addresses and independent privacy aliases remain acceptable. See Apple's
[relay-domain announcement](https://developer.apple.com/news/?id=1ptvdtcm).
A relay can be retained only if another provider is both linked and enabled by
the host. Otherwise request and verify an independent email, using the existing
current-email approval stage when necessary. This rule also applies to removing
Google later and to selecting a different Apple relay as the replacement.

GHATD checks this policy at start, review and confirmation. MongoDB checks that
an enabled alternative remains linked in the same atomic write as disconnection.
`user.DisconnectOAuthProviderRequest.AllowedRelayFallbackProviders` is a fresh,
server-derived allowlist, excluded from proof JSON. Custom repositories may
implement `user.OAuthRelayFallbackRepository` only if they enforce that atomic
guard. Without it GHATD requires an independent email for relay accounts, while
ordinary provider management remains available. No new migration is required.

Handle `OAuthReplacementEmailRequired` (409) by offering a new independent email
and restarting verification; a consumed proof cannot be reused. A database
snapshot race returns `OAuthConnectionConflict` (409), requiring refreshed
Settings. `OAuthEmailConflict` (409), checked after proof and by the unique index,
means another account owns the proposed address. Explain that both accounts are
unchanged; offer a different email or manual support. Never automatically merge
accounts or infer the destination mailbox behind a relay. Account merging is
outside this sign-in and disconnect flow; do not treat an email collision as
permission to transfer another account's identity or data.

The web flow uses session cookies and same-origin JSON POSTs:

1. `POST /api/v1/ams/oauth/connections/{google|apple}/disconnect` with
   `{"email":"me@example.com"}`. Omit the email to keep the current address when the fallback policy permits it.
   Requires a valid session. Keeping the current verified email needs no
   separate recent login: that inbox's proof supplies account authentication.
   For a replacement email, sign-in within five minutes permits direct
   verification; otherwise GHATD first sends approval to the current email.
   Returns `data.challenge_id`, `expires_in` (600), `resend_cooldown_seconds`
   (60), `verification_stage` (`current_email` or `sign_in_email`), `email`
   (the recipient for this stage) and `sign_in_email` (the intended final email).
2. Deliver a magic link **and** an eight-character alphanumeric code using the
   host's existing email manager. Every disconnect requires inbox proof, even
   with another provider connected; provider-asserted email verification alone
   does not establish current delivery/access, especially for Apple relay.
3. `POST /api/v1/ams/oauth/connections/{provider}/disconnect/confirm` with
   `{"challenge_id":"...","code":"A1B2C3D4"}` or `token` instead of `code`.
   Require exactly one proof and the initiating browser session. Confirming
   `current_email` returns HTTP 202 with `disconnected: false` and
   `next_challenge` in the start-response shape. No account fields or session
   cookies change: clear the old proof and prompt for the new address's separate
   code/link. The automatic transition bypasses the resend cooldown only after
   consuming the first one-use proof; explicit starts remain rate limited.
   Final confirmation returns HTTP 200, sets replacement access/refresh cookies,
   and returns `data: {disconnected: true, connected: [...], email: "..."}`.

Email links open
`{origin}/settings#oauth_disconnect=google&challenge_id=<id>&token=<token>`.
The host must remove this fragment **before analytics/router startup**, retain
it only in memory, and show explicit confirmation before posting it. Load the
stage and recipient with authenticated
`GET /api/v1/ams/oauth/connections/{provider}/disconnect/challenges/{challenge_id}`;
this sends no emailed proof and cannot consume it. The Redis store implements
the optional `oauth.DisconnectChallengeReader` interface; custom stores may
implement it to support this review UI. Unknown/foreign/expired challenges return
404, changed account/provider snapshots return 409, and a revoked session returns
401. Render server-owned metadata rather than guessing the stage from the link.
Never
mutate the account on GET, auto-confirm on page load, or put the proof in a
server URL query. If the link opens in another browser/session, use the code
in the initiating browser instead. These endpoints retain their web-only
mutation guards; native clients use the separate transport below and must not
impersonate a browser origin.

Until confirmation succeeds, the old email and provider remain intact. The
repository atomically verifies the full provider snapshot and account revision,
saves the verified replacement email, and removes that provider's identities.
A taken email returns 409 without merging accounts or changing either field.
A concurrent email/provider change also returns 409. All methods share the same
user ID, memberships, profile, and existing passwordless login mechanism.
Provider-created accounts with optional names remain valid after unlinking.

Challenges are purpose-isolated from login codes, single-use, expire after ten
minutes, allow five failed guesses, and have a per-account send cooldown.
Successful removal increments a server-owned email revision, invalidating old
JWTs/email proofs and pending provider links even if they race with session
cleanup. Linking checks the initiating revision in both its atomic write and
idempotent lookup. Stale full-user updates fail instead of restoring the old
email; ordinary no-op updates still succeed. The initiating
browser receives a new session; other sessions must sign in again. Same-email
proof uses its successful verification time as `auth_time`. Two-stage changes
carry the current-email approval time; direct replacement verification preserves
the original recent sign-in time. A new inbox alone never supplies proof of the
old account. Legacy queued final challenges remain redeemable only when the current fallback policy also permits them.

The entire UI can stay under authenticated Settings; public login guards need no
exception. Cancellation and expiry leave the account untouched. An access-token
rotation or new login changes the session binding: ask the user to restart the
request, and never silently carry proof across sessions. If current-email access
is unavailable, a normal sign-in with an existing connected provider permits
starting direct replacement verification. A failed second-stage delivery or
challenge publication consumes the approval proof but leaves the account intact;
show Start again for `OAuthDisconnectDeliveryFailed`. Infrastructure
failure after the atomic write returns `OAuthDisconnectSessionRequired` (503):
the provider is already disconnected, so ask the user to sign in using the
verified email. Never report an unconfirmed network request as an unchanged
account. A failed email delivery still consumes the one-minute send cooldown.

`OAuthConnections` is a safe read without disconnect configuration. A missing
configuration, foreign/missing Origin, non-JSON body, unknown fields, missing or
revoked session, or unavailable signing/persistence capability fails closed.
Include `AccessmanagerErrorMap` in custom handler error manifests.

Run the HTTP lifecycle/regression tests with disposable MongoDB and Redis:

```sh
GHATD_TEST_MONGO_URI=mongodb://127.0.0.1:27039 \
GHATD_TEST_REDIS_ADDR=127.0.0.1:6399 \
go test -race ./external/accessmanager ./external/oauth ./external/auth ./external/user/v2
```

Tests cover inbox fallback to the same account, two last-provider removals under
the sparse unique index, replay/expiry/attempt limits, duplicate email, changed
account/provider snapshots at each stage, concurrent confirmations, failed
delivery, stale profile writes, and credentials restored by overlapping
login/refresh work. Staged cases also verify older and legacy sessions, immediate
stage progression, review privacy/no-consumption, cross-stage/session rejection,
unchanged account after current-email approval, and authentication timestamps.

### Native Settings verification

Native clients reuse the same disconnect service and existing secure session
cookie store. Opt in with `OAuthConnectionsConfig.MobileRedirectURIs`, for
example `[]string{"com.example.app.settings:/oauth/disconnect"}`. Addresses must
use an exact private reverse-domain scheme, no authority/query/fragment, and
path `/oauth/disconnect`. Keep this scheme separate from an OAuth browser
callback; configure the corresponding Android/iOS app handler. Empty disables
native disconnection. Native support requires `DisconnectChallengeReader`.

- `GET /api/v1/ams/oauth/mobile/connections?redirect_uri=<address>` returns the
  authoritative connected providers and native `disconnect_available`. Connected
  identities remain visible when provider discovery is disabled.
- `POST /api/v1/ams/oauth/mobile/connections/{provider}/disconnect` accepts
  `email` and `redirect_uri`. Stage metadata and verification rules match web.
- `GET /api/v1/ams/oauth/mobile/connections/{provider}/disconnect/challenges/{id}?redirect_uri=<address>`
  reviews without accepting or spending emailed proof.
- `POST /api/v1/ams/oauth/mobile/connections/{provider}/disconnect/confirm`
  accepts `redirect_uri`, `challenge_id` and exactly one of `code` or `token`.
  First-stage success remains 202; only final 200 sets the renewed cookies.

Native requests reject any `Origin` header. Mutations are JSON-only and require
one unique live session cookie. Retain the web endpoints' exact Origin checks.
Disable automatic retries and session-refresh replay for proof requests. If the
session rotates while a challenge is pending, restart in Settings.

Emails carry both a code and an app-return link such as
`com.example.app.settings:/oauth/disconnect#oauth_disconnect=google&challenge_id=...&token=...`.
The link is bound to the initiating account/session and exact return address.
Web and other native apps cannot spend it, even with the same cookie. Keep proof
only in memory, exclude it from platform/router/network logs, and review metadata
before displaying explicit confirmation. Never redeem on app-open. Cancellation
keeps the provider connected. A new login/account switch must discard pending
proof; no token or cookie is transferred from the browser into the app.

### Native connection verification for older sessions

Connecting a provider requires authentication within the last five minutes.
An otherwise valid session must not be forced through the public login screen
just to satisfy that check. Native Settings clients can offer current-inbox
verification when linking returns `reauth_required`.

Native connection status advertises `connect_verification_available`. This is
opt-in through the existing exact Settings callback allowlist and requires a
store implementing `ConnectionVerificationStore() DisconnectChallengeStore`
with read support. The Redis implementation uses a separate purpose namespace;
custom stores without that capability remain compatible and advertise false.

- `POST /api/v1/ams/oauth/mobile/connections/{provider}/reauthenticate` accepts
  only `redirect_uri`; the server selects the current account email.
- `GET .../{provider}/reauthenticate/challenges/{id}?redirect_uri=<address>`
  reviews the pending challenge without spending proof.
- `POST .../{provider}/reauthenticate/confirm` accepts `redirect_uri`,
  `challenge_id` and exactly one `code` or `token`.

Start/review use the existing challenge shape with `verification_stage:
"connect_email"`. Emails contain an 8-character code and a link whose fragment
starts with `oauth_connect=<provider>` at the same registered Settings callback.
Keep that marker separate from `oauth_disconnect`; proof is bound to account,
initiating session, provider, exact callback and current email revision.
Disconnection and connection-verification proofs cannot spend each other.

Final confirmation returns HTTP 200 with `reauthenticated: true`,
`disconnected: false`, unchanged `connected`/`email`, and fresh ordinary session
cookies for the same account. Other sessions remain valid with their original
authentication time. No provider is connected or disconnected by this step.
Show an explicit Continue with Google/Apple action to launch the existing
native linking flow, which still rechecks session freshness and account revision.
Cancel keeps the account unchanged. If verification expires, offer verification
again without an automatic retry loop. Existing retry, logging and link-review
precautions apply. Ordinary login codes are deliberately not accepted here:
they can authenticate a different account and are not bound to this Settings
operation.

### Web Settings verification for older sessions

Web Settings offers the same reauthentication flow when connecting Google or
Apple returns `OAuthReauthenticationRequired`. It is available whenever web
connection verification is configured (origin, email delivery, revision-aware
sessions and a store exposing `ConnectionVerificationStore()` with read
support); `GET /api/v1/ams/oauth/connections` advertises
`connect_verification_available` for it. The return address is derived solely
from `OAuthConnectionsConfig.Origin` plus `/settings` and can never be
supplied by a caller; emailed links use
`https://<origin>/settings#oauth_connect=<provider>&challenge_id=...&token=...`.

- `POST /api/v1/ams/oauth/connections/{provider}/reauthenticate` accepts an
  empty JSON object `{}` only; the server selects the current account email.
- `GET .../{provider}/reauthenticate/challenges/{id}` reviews the pending
  challenge without accepting or spending proof.
- `POST .../{provider}/reauthenticate/confirm` accepts `challenge_id` and
  exactly one `code` or `token`.

Start/review reuse the `connect_email` metadata shape. Final confirmation
returns HTTP 200 with `reauthenticated: true`, unchanged `connected`/`email`,
and fresh session cookies for the same account; all other sessions stay valid
and unrefreshed, and no provider identity is mutated. Mutations require the
exact configured Origin and JSON-only bodies. Every request requires one unique
session cookie and matching handler/service origin configuration; review GETs
accept no query parameters or emailed proof.
Web, native and disconnect proofs remain bound to their own transports and
cannot spend each other.
