# Add Google and Apple sign-in to a GHATD application

Use this guide to add browser and native sign-in alongside the existing email
magic-link/code flow. It applies to any GHATD host, including projects that do
not use Bedrock. The [OAuth package](../../external/oauth/README.md) owns provider
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
late callbacks restoring a session after logout. The existing Flutter companion
illustrates the flow with `PersistCookieJar` and `FlutterSecureStorage`; another
client can implement the same HTTP contract using its own secure storage.

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
Start the host normally; in Bedrock this means `make start` and `yarn run dev`.

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

As of 2026-09-30, the Bedrock integration has live Google/Apple web sign-in,
returning web identity reuse and Apple Hide My Email sign-in evidence. The user
also confirmed both provider login flows on the connected Android companion
against the same backend. This is evidence for the browser-handoff integration,
not certification of every device or deployment.

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

The broader native repeat-ID/linking/refresh scenarios, iOS and actual relay
email delivery remain separate acceptance work; successful Android login does
not establish them. Automated signed-provider tests with isolated MongoDB/Redis
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
Its `data` contains `connected`, `available`, `email`, and `disconnect_available`.
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

The web flow uses session cookies and same-origin JSON POSTs:

1. `POST /api/v1/ams/oauth/connections/{google|apple}/disconnect` with
   `{"email":"me@example.com"}`. Omit the email to keep the current address.
   Requires sign-in within the last five minutes. Returns `data.challenge_id`,
   `expires_in` (600), and `resend_cooldown_seconds` (60).
2. Deliver a magic link **and** an eight-character alphanumeric code using the
   host's existing email manager. Every disconnect requires inbox proof, even
   with another provider connected; provider-asserted email verification alone
   does not establish current delivery/access, especially for Apple relay.
3. `POST /api/v1/ams/oauth/connections/{provider}/disconnect/confirm` with
   `{"challenge_id":"...","code":"A1B2C3D4"}` or `token` instead of `code`.
   Require exactly one proof and the initiating browser session. The response
   sets a replacement access/refresh cookie pair and returns
   `data: {disconnected: true, connected: [...], email: "..."}`.

Email links open
`{origin}/settings#oauth_disconnect=google&challenge_id=<id>&token=<token>`.
The host must remove this fragment **before analytics/router startup**, retain
it only in memory, and show explicit confirmation before posting it. Never
mutate the account on GET, auto-confirm on page load, or put the proof in a
server URL query. If the link opens in another browser/session, use the code
in the initiating browser instead. These mutation endpoints are web-only;
native clients must not impersonate a browser origin.

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
browser receives a new session; other sessions must sign in again. Infrastructure
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
account/provider snapshots, concurrent confirmations, failed delivery, stale
profile writes, and credentials restored by overlapping login/refresh work.
