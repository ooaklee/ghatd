# Access Manager

The `accessmanager` package handles authentication, authorisation, and user lifecycle management — including user creation, login, registration, email verification, OAuth integration, and API token management. It also provides middleware for JWT validation, API token validation, rate limiting, and hardened code-verification brute-force protection.

## Integration Model

For explicit token-policy provisioning, use the reusable
[access-policy manager](../accesspolicymanager/README.md). The
`PolicyManagementAuthorizer(system)` adapter checks the live session owner and
current account identity, email/type revisions, active status and administrator
role before returning an audit actor. It rejects API/anonymous/mixed contexts;
signed administrator claims alone never authorize changes. The HTTP manager also
requires the shared bearer-only middleware, with no cookie fallback or refresh.

A GHATD host application usually wires Access Manager alongside the router, email manager, user manager, Redis-backed ephemeral storage, and any OAuth providers.

The common setup is:

1. Create a `router.Router` and attach CORS for the frontend origins that will call GHATD with cookies.
2. Attach the default auth verification bridge at `/v0/auth/verify` with `router.AttachDefaultAuthVerifyRoute`.
3. Configure the email manager with `EmailVerificationFullEndpoint` set to the backend base URL plus `/v0/auth/verify`.
4. Create the Access Manager service with an ephemeral store, email manager, auth service, user service, API token service, audit service, and any OAuth services.
5. Create the Access Manager handler and middleware with the same cookie names, cookie domain, and environment.
6. Attach `/api/v1/ams` routes, either directly with `accessmanager.AttachRoutes` or through `starter/v0`.

The `/v0/auth/verify` bridge is intentionally separate from `/api/v1/ams`. Email links point at the bridge with `type`, `__t`, and optional `request_url` parameters. The bridge chooses the correct Access Manager endpoint, converts the frontend return path into a safe `next_step`, and then redirects to the API verification endpoint.

## Session Cookies And Client Contract

### Live session authority

`AuthenticateSession`, `MiddlewareJWTRequired`, and the authenticated branches
of the active/admin/optional service guards share one verifier. It checks signed
access-token identity, the exact live session owner, current stored user ID,
email revision and account type. Active guards then check current status;
administrator guards check current roles. Signed `IsAdmin` and `IsAuthorized`
values are historical metadata, not current authority. Promotions and demotions
take effect on the next check without minting a replacement access token.

Custom session stores should return `ephemeral.ErrAuthNotFound` for an absent
session; legacy `redis.Nil` remains supported. Storage outages and cancellations
are returned as operational errors, never treated as missing sessions. The
Redis session lookup logs fixed outcomes without keys, identities or raw driver
errors. Current-user ID/nano-ID lookups also preserve repository failures instead
of turning every failure into a missing account. Configure HTTP error manifests
and sanitize errors at the transport boundary; do not serialize raw causes.

This is a check-time guarantee, not a transaction lock or token-family revocation.
The cookie wrappers retain automatic refresh and optional-public fallback.
Deleting an access token alone does not invalidate its refresh token; the legacy
logout service currently removes only the access record. Cookie wrappers now
distinguish absent/expired access credentials from account denials and operational
failures; an outage never triggers refresh or anonymous fallback. The cookie
handler performs best-effort removal of the presented refresh token during
logout, but that is not atomic revocation of a rotating session family.
Use the [explicit bearer adapter](middleware/README.md#explicit-bearer-sessions)
where refresh or anonymous fallback is inappropriate. Recheck authority inside
sensitive domain operations and enforce resource ownership separately.

Successful login, email verification, token refresh, and OAuth callback responses set two `HttpOnly` cookies:

| Cookie | Purpose |
|---|---|
| `CookiePrefixAuthToken` | Short-lived access token cookie, commonly configured as `__aauth` |
| `CookiePrefixRefreshToken` | Longer-lived refresh token cookie, commonly configured as `__rauth` |

Access Manager also sets non-`HttpOnly` marker cookies named `access_token` and `refresh_token`. Their value is only `"true"`; they exist so browser clients can tell that auth cookies were recently issued without reading the token values.

Cookie attributes come from the handler configuration:

| Environment | `Secure` | `SameSite` |
|---|---:|---|
| `local` | `false` | `Lax` |
| non-`local` | `true` | `Strict` |

Browser clients should use credentialed requests, such as Axios `withCredentials: true`, and native clients should use a persistent cookie jar. Clients do not need to store JWTs themselves. To resolve the current session, call `GET /api/v1/ums/me`; a `200` user projection means the session is usable. The default probe returns `202` with `AM00-013` for a missing session; hosts can explicitly select an empty `202` or structured `401`. A probe `202` is not authentication success. Handle credential failures separately from dependency outages and permission denials; an outage should not erase a usable session. See the [session-probe response policy](middleware/README.md#session-probe-response-policy) before changing a client's wire contract.

## Refresh Rotation Tolerance

Access Manager treats refresh-token rotation as a one-winner operation. The first request that validates a refresh token claims a short-lived rotation lock, consumes the old refresh token, creates the replacement access and refresh tokens, and stores a short-lived replay result in ephemeral storage.

Near-concurrent duplicate refreshes first check for an existing replay result,
then wait briefly when another request holds the lock. If the winner completes,
the duplicate receives the same replacement pair. If no result appears before
the wait expires, the request fails with `ErrRefreshTemporarilyUnavailable`
(`503`, `AM00-040`), without clearing cookies. This is not evidence that the
credential is invalid. A later session probe can reconcile the outcome; avoid
unbounded automatic refresh loops. The lock/replay window is bounded, not an
atomic session-family transaction or an exactly-once guarantee across failures.

`ClassifySessionError` defines cookie behaviour independently of HTTP response
overrides. Custom adapters should wrap known credential sentinels, not synthesize
their text or rely on custom `Is` methods. Lifecycle decisions use the actual
leaf cause. Unknown, joined, cancellation and dependency errors fail closed and
preserve cookies. Current account-role/status denials also preserve cookies and
cannot be repaired by refresh. Known invalid credentials can clear cookies;
only explicitly optional routes may then use their rate-limited public branch.
The explicit refresh endpoint applies the same distinction and sends `no-store`.

The legacy `ErrUnauthorizedRefreshTokenCacheDeletionFailure` sentinel means a
successful delete confirmed no refresh record and no replay was available. An
actual storage deletion failure retains its original cause; it is not this
credential rejection. Refresh responses reject missing token data, and observed
cancellation stops later writes or publication. Manager refresh logs omit raw
adapter diagnostics and credential identifiers; hosts remain responsible for
the logging behavior of their injected dependencies.

Preserving cookies does not undo a completed rotation: a failure after consuming
the old refresh token may require replay reconciliation or reauthentication.
See [cookie timing and compatibility](middleware/README.md#refresh-cookie-timing).

Middleware refreshes also validate the retried request before writing replacement cookies to the response. This avoids committing cookies for a token pair that the protected route would immediately reject.

Clients should still avoid intentionally fanning out refresh calls, but host applications do not need every tab, worker, and retry path to serialize perfectly. The server tolerates the common browser race where multiple requests discover an expired access-token cookie at nearly the same time.

## Dual-Channel Verification

The access manager supports a dual-channel verification flow: users receive both a **magic link** (with a JWT token) and an **8-character alphanumeric verification code** in the same login or verification email. Some apps label this manual-entry value as a secret code.

- **Magic link flow**: The email includes `/v0/auth/verify?type=<verification-type>&__t=<jwt-token>`. The bridge redirects to `/api/v1/ams/login?t=<jwt-token>` or `/api/v1/ams/verify/email?t=<jwt-token>`, and the API validates the token before issuing the session cookies.
- **Code flow**: The email also includes an 8-character A-Z/0-9 code. The app submits the code to `/api/v1/ams/login?c=<code>` for login or `/api/v1/ams/verify/email?c=<code>` for signup/email verification. Access Manager resolves the code to the same ephemeral token, validates it, and then issues the same session cookies.

See the [Email Manager](../emailmanager/README.md) documentation for details on the email templates that deliver both channels.

### Proof admission and upgrade compatibility

Login accepts signed `login` or `email_verification` proofs; the email-verification
endpoint accepts only `email_verification`. Neither accepts access/refresh tokens
or credentials without a `token_use` claim. **On upgrade, users with older links
or codes must request a new email.** Ordinary legacy session verification is
unchanged; a missing purpose cannot establish a fresh login.

Before issuance, both flows check the live proof's exact stored owner and the
current account ID, email revision, signed user type and allowed status. A legacy
missing user type remains unbound. Custom signers must populate purpose for new
proofs, and custom stores must implement atomic exact-key `DeleteAuth` with an
accurate deleted count. Only one confirmed deletion admits account writes and
session minting. Concurrent reuse fails; a storage failure is not proof absence.
The manual code points to the same proof and cannot bypass consumption.

Consumption is not a transaction with account updates or session storage. If
anything fails after consumption, request a new proof; do not restore the old
one or assume that an error rolled back an account update. Authority checks are
check-time snapshots, not a lock against concurrent account changes. The trusted
`UserEmailVerificationRevisions` command is not a public proof verifier: internal
callers must already have validated and consumed a proof.

OAuth linking uses the shared live-session verifier plus current ACTIVE/verified
status and a signed login time within five minutes. Browser initiation rejects
duplicate access cookies. Server-owned link state carries the signed user type
and email revision for callback revalidation; native exchange also requires the
same initiating session snapshot. Restart pending linking after an incompatible
claim change. Operational failures retain their cause until the response boundary,
instead of asking users to sign in again during an outage.

### Route registry

`AttachRoutes` registers login, verification, refresh, OAuth and credential
management with the [shared registry](../router/README.md). Supply both
`ActiveOnlyMiddleware` and `HardenedRateLimitMiddleware`, then reject startup if
`ValidateRoutePolicies` fails. Missing required adapters invalidate all descriptors.
Optional OAuth handler interfaces still control which endpoints are mounted.
Existing paths, methods and first-match OPTIONS behaviour are preserved.

`HandlerVerified` names document a handler's proof obligation; route metadata
does not itself verify email codes, provider identities or session ownership.
Hosts must retain the corresponding service checks. Native OAuth errors keep
their fixed `error` vocabulary; unknown/multi-cause failures now return 500,
and unavailable session verification returns 503 rather than a client denial.

## Authentication Flows

### Signup And Email Verification

1. The app sends `POST /api/v1/ams/signup` with `email`, `first_name`, `last_name`, and an optional `request_url`.
2. Access Manager creates a provisioned user, creates a short-lived email verification token, creates an 8-character verification code mapped to that token, and asks Email Manager to send the verification email.
3. The API responds with `201 Created`.
4. If the user clicks the magic link, the link reaches `/v0/auth/verify?type=2&__t=<token>&request_url=<path>`.
5. The bridge redirects to `/api/v1/ams/verify/email?t=<token>&next_step=<frontend-url>`.
6. If the user enters the code instead, the app calls `/api/v1/ams/verify/email?c=<code>`.
7. Access Manager validates the token or code, marks the email verified, moves the account to `ACTIVE`, creates access and refresh tokens, stores the session in ephemeral storage, sets the auth cookies, and returns `200 OK` or redirects to `next_step`.

Use verification type `2` when the client is tracking a pending signup or email-verification email. The API route itself is selected by path; `type=2` is mainly used by the email bridge and client UI state.

### Email Login With Magic Link

1. The app sends `POST /api/v1/ams/login` with `email`, optional `dashboard` or `mobile`, and optional `request_url`.
2. Access Manager looks up the user by email.
3. If the user is `ACTIVE`, Access Manager creates a short-lived login token, creates an 8-character code mapped to that token, and sends the login email.
4. If the user is still `PROVISIONED`, Access Manager sends a verification email instead.
5. The API returns `202 Accepted` for both success and most lookup failures so callers do not leak whether an email address exists.
6. The user clicks `/v0/auth/verify?type=1&__t=<token>&request_url=<path>`.
7. The bridge redirects to `/api/v1/ams/login?t=<token>&next_step=<frontend-url>`.
8. Access Manager validates and consumes the login proof, creates an authenticated session, stores it in ephemeral storage, sets auth cookies, and returns `200 OK` or redirects to `next_step`.

For an active user, duplicate login-initiation requests for the same user, dashboard flag, and requested return URL are suppressed for a short cooldown window. The API still preserves its non-enumerating response behavior, but only the first accepted request should send an email. If token setup or email delivery fails before the email is accepted, the cooldown is released so a retry can send a new email.

Client login forms should treat any successful `2xx` response from `POST /api/v1/ams/login` as "check your email", disable duplicate submits while the request is in flight, and keep the form locked once the request is accepted. This protects email quotas and gives users a stable transition to the check-email screen.

Use verification type `1` when the client is tracking a pending login email.

### Email Login With Verification Code

1. The app starts the same login flow with `POST /api/v1/ams/login`.
2. The user enters the 8-character code from the email.
3. The app normalises the code to uppercase and calls `GET /api/v1/ams/login?c=<code>`.
4. Access Manager resolves the code from ephemeral storage, validates and consumes the underlying proof, creates the same cookie session as the magic-link flow, and returns `200 OK`.

The code is a manual-entry alias for the underlying token. This keeps the email link and manual code paths equivalent after code resolution.

### Sign in with Google and Apple

Create providers with `oauth.NewGoogleSecureProvider` and `oauth.NewAppleProvider`
and supply them as `OauthServices`. Apply the identity indexes before enabling
providers. The legacy `NewGoogleProvider` is not accepted for secure sign-in.

1. Discover configured providers, then start login with `browser=true` and a
   rooted local `request_url` for browser clients.
2. GHATD stores state, nonce, return path and PKCE/linking context server-side;
   the provider cookie holds only an opaque handle.
3. The provider returns to the Google GET or Apple form-POST callback. GHATD
   checks the transaction cookie/state, consumes the transaction, exchanges the
   code and validates the signed ID token.
4. Resolve the account by issuer/subject. A matching email on another account
   requires explicit fresh-session linking; it never silently selects that user.
5. Set the ordinary session cookies and clear the transaction cookie. Browser
   mode returns a 303 to the stored safe path; API mode retains the metadata
   response. No browser callback wrapper is required.

Native clients use the one-use [handoff](#native-app-handoff) to obtain these same
cookies in their own HTTP client. For registrations, composition and local
HTTPS testing, follow [Add Google and Apple sign-in](../../docs/how-to/add-google-apple-sign-in.md).
The precise route and linking contract is [below](#secure-google-and-apple-sign-in).

## Security Measures

| Layer | Mechanism |
|---|---|
| **Code entropy** | 8-character A-Z/0-9 = ~2.8 trillion combinations |
| **Collision resistance** | Ephemeral storage check with up to 5 retry attempts |
| **Brute-force protection** | `HardenedRateLimitProtection` middleware tracks attempts per IP and per code within a configurable window (default: 5/hr per IP, 5/hr per code) |
| **Auto-blocking** | IPs exceeding the threshold are temporarily blocked (default: 1 hour) |
| **One-time use** | The underlying proof is atomically consumed before account changes or session issuance |
| **Refresh rotation tolerance** | One request rotates a refresh token while short-lived replay results tolerate near-concurrent duplicate refreshes |
| **Login email cooldown** | Duplicate login email sends for the same active user/context are suppressed during a short cooldown window |
| **Audit logging** | All verification attempts and rate-limit blocks are logged for monitoring |
| **Rate-limit response** | Blocked IPs receive HTTP 429 with `EPH0-002` — no information leakage |

## API Endpoints

All endpoints are prefixed with `/api/v1/ams`.

### Open
- `POST /api/v1/ams/signup` — Create a new user account
- `POST /api/v1/ams/login` — Request a login or verification email
- `GET /api/v1/ams/login` — Verify a login token or code and authenticate
- `GET /api/v1/ams/logout` — Log out the current user
- `GET /api/v1/ams/verify/email` — Verify an email verification token or code
- `POST /api/v1/ams/tokens/refresh` — Refresh access and refresh tokens
- Google/Apple provider discovery, sign-in, callbacks and linking — see [secure provider endpoints](#secure-google-and-apple-sign-in)
- Optional native discovery, start and exchange — see [native app handoff](#native-app-handoff)

### Active owner session required

These credential-management routes no longer accept API-token-only requests.
Use a verified active JWT/cookie session for the target account; this prevents a
delegated credential from creating or reactivating account-wide credentials.

- `POST /api/v1/ams/users/{userID}/tokens` — Create an API token
- `GET /api/v1/ams/users/{userID}/tokens` — List API tokens
- `DELETE /api/v1/ams/users/{userID}/tokens/{apiTokenID}` — Delete an API token
- `PUT /api/v1/ams/users/{userID}/tokens/{apiTokenID}/activate` — Activate an API token
- `PUT /api/v1/ams/users/{userID}/tokens/{apiTokenID}/revoke` — Revoke an API token
- `GET /api/v1/ams/users/{userID}/tokens/thresholds` — Get token thresholds
- `GET /api/v1/ams/logout/other-sessions` — Revoke the user's other active sessions while keeping the current session

### Active users only
- `PATCH /api/v1/ams/users/{userID}/email` — Update user email address

## Conditional email changes

**Breaking:** `UpdateUserEmail` now takes `ActorID`, `TargetUserID` and `Email`,
and returns `(*UpdateUserEmailResponse, error)` rather than `(bool, error)`.
Update custom service adapters and clients that expected an empty response.
The HTTP payload remains `{"email":"new@example.test"}`. The mapper binds the
actor from authenticated context and the target from the URI; body identity
fields cannot select either. Cookie tokens and the HTTP request are no longer
part of this command.

The manager requires explicit authenticated context, then loads current ACTIVE
actor authority. It permits self-service or a current administrator acting on
another account. Trusted in-process callers must publish verified identity with
the context helpers before invoking it; passing an `ActorID` on a bare context
is rejected. Context publication is trusted wiring, not authentication itself.
The standard route requires an active session. This command does not add a
recent-login or old-inbox-proof requirement; hosts needing step-up authentication
must enforce that before dispatch. Authorization of a different administrator
is a pre-write check, not a transaction lock against later role changes.

The [user domain](../user/v2/README.md#conditional-email-changes) owns the atomic
mailbox, verification-state and revision update. Configure its explicit unique
email index and optional `ChangeUserEmail` adapter capability first. No legacy
full-profile update fallback is allowed. Old access/refresh/login/verification
proofs fail their live revision checks even if Redis cleanup fails. Provider
identity links and API credentials are not removed by this operation.

A confirmed change returns HTTP 200 with `Cache-Control: no-store` and this
object in the shared reply response's `data` field:

```json
{
  "changed": true,
  "sign_out_required": true,
  "session_cleanup_complete": true,
  "verification_email_sent": true,
  "previous_address_notified": true,
  "audit_recorded": true
}
```

The handler clears the two authentication cookies and two marker cookies only
for a confirmed self-change. An administrator changing another account keeps
their own cookies. Cleanup selects only the target's sessions, without exemptions
copied from the administrator. The security notice goes to the previous address
only after the database change, with escaped HTML fields; audit records use the
actual actor, target and new revision, without email addresses.

`changed: true` remains true when a post-commit action fails. Mail flags mean the
adapter accepted the request, not that a message reached an inbox. A false flag
requires recovery, not repeating the committed mutation: request verification
through the normal sign-in flow at the new address; investigate cleanup, notice
or audit failures operationally. There is no durable outbox or automatic retry
queue here. Mongo, Redis, mail and audit are not a distributed transaction.

An error or lost response can have an unknown database outcome. Reconcile current
account state before retrying; do not assume rollback. Native failures reach
the shared error manifest, where unknown/multiple causes stay opaque and host
overrides remain supported. Manager logs use fixed phase outcomes. Injected
adapters remain responsible for their own privacy-safe logging.

Verification-proof creation rejects incomplete dependencies and malformed token
receipts, snapshots delivery fields, and stops between phases on cancellation.
Earlier token/code writes are not rolled back after a later mail failure.
`helpers.GenerateUniqueCode` now preserves native storage failures instead of
replacing them with `ErrCodeGenerationFailure`; exhaustion/invalid inputs retain
that sentinel. Its legacy existence-check/store contract is not an atomic code
reservation, despite retrying observed collisions. One-use proof consumption is
separate from code allocation.

## Transactional API-token policy

### Session-bound management commands

The create, list, threshold, delete, activate and revoke commands require an
explicit `ActorID` and a target `UserID`. Both must match the verified session
owner; an administrator cannot use these self-service commands for another user.
The manager requires authenticated session context with a nonempty access-session
ID, rejects competing API-credential context, and reads the current matching
`ACTIVE` account. Its email revision and any signed user type must still match.
Previously verified legacy sessions without a signed user type remain supported.

**Go API migration:** populate `ActorID` from trusted authentication, not from the
target or request payload, and rename threshold request `UserId` to `UserID`.
An owner ID or `helpers.TransitWith` alone is no longer sufficient. Custom
in-process adapters must verify credentials and live session admission before
publishing context through the [session authentication helper](middleware/README.md#explicitly-selected-sessions). Publishing
context is trusted wiring, not authentication. Use the preloaded session
middleware where possible.

HTTP mappers bind owner and token selectors from the URI and status from the
route. JSON/query payloads cannot override those fields or the actor. List
filters are copied; embedded identity and count selectors are ignored. List
responses validate ownership and copy rows without plaintext secrets or digests,
including responses from custom manager adapters. Only successful creation
returns the new secret. Nil or malformed adapter results fail closed.

The six handlers set `Cache-Control: no-store` and retain their existing success
contracts: create `201`, reads `200`, and delete/activate/revoke `202` with a blank
response. Native errors go through shared reply manifests without raw diagnostic
logging by these handlers. This does not change logout endpoints.

Live-account checks are point-in-time observations, not account locks. Policy
creation repeats the check inside each transaction attempt; custom transaction
adapters must preserve verified context as well as the managed database session.
Other management operations are not transactional with concurrent account
suspension or session revocation. Cancellation after a mutation is not rollback.

### Policy admission and rollout

Credential management requires `ActiveOnlyMiddleware` backed by a live, active
session verifier, such as the preloaded middleware suite's `ActiveOnly`. API tokens
cannot use the built-in routes to issue or manage credentials or revoke other
sessions. The deprecated `ActiveValidApiTokenOrJWTMiddleware` route field is
ignored; missing session middleware returns 503 rather than exposing a handler.
Handlers still check the requested owner against verified identity. Custom
middleware is trusted wiring: a pass-through function is not authentication.

Set `NewServiceRequest.TokenPolicy` (or `starter.NewServicesRequest.TokenPolicy`) to
`accesspolicy.TokenPolicy{Service: policyService, System: "example-system"}` to
enforce stored limits. Configure the system once on the server; do not derive it
from a request. The threshold endpoint then reads that same current grant.
Missing, expired, disabled or invalid grants deny creation without falling back
to a role, even for an administrator.

Create and initialize the [Mongo policy store](../accesspolicy/README.md#mongo-startup)
at startup. Call the API-token repository's `InitializeTokenInventory` to create
its collections and verify transaction support, then explicitly call
`PrepareTokenInventory` for each provisioned owner before enabling issuance.
Preparation is idempotent and separate from grants; it creates no credential.
The policy store, user repository and API-token repository must share the same
managed Mongo client and database. The built-in API-token service provides the
required `CountTokenInventoryFenced` capability; custom adapters must provide an
equivalent owner-wide lock and preserve the callback context. No unfenced-count
or paginated-list fallback is permitted. See [inventory setup](../apitoken/README.md#transactional-inventory-setup).

Admission reads the current target account, counts all stored owner credentials
and inserts the new record inside `WithTokenCreation`. The grant write fence
serializes changes to that owner/system policy, while an API-token-owned lock
serializes counts/inserts across every system sharing the owner's inventory.
Each system compares total inventory against its own live allowance; the limit
is not the minimum across every system's grants. Missing preparation, a foreign
client or a session without an active transaction fails closed. Aborted
callbacks may retry; no external effects or secret publication may occur inside
them. A creation response is returned only after success. An uncertain commit can
leave a stored token whose secret was never delivered: inspect inventory and
explicitly delete unwanted records rather than blindly retrying issuance.
Issuance is not an idempotent secret-recovery API.

Inventory includes revoked, expired and malformed-expiry records until explicit
deletion. GETs do not clean up records or free slots. Missing/null/empty expiry
means permanent; every other stored expiry belongs to ephemeral inventory.
Authentication independently rejects expired or malformed expiry. Reactivation
does not extend a credential's lifetime or change its inventory class.

**Migration boundary:** an omitted `TokenPolicy` temporarily retains legacy role
limits, now using exact counts. That old count-then-insert path is still
non-atomic. All writers to a shared inventory must migrate together and use its
owner-wide fence; mixing legacy/unfenced writers invalidates the concurrency
guarantee. Different system grants may safely share the built-in owner fence.
Direct low-level token creation is trusted infrastructure,
not an alternative public admission endpoint.

Review and seed grants explicitly before enabling the port. Existing credentials
are neither deleted nor granted scopes automatically. Token-specific route grants
remain separate: this port does not copy user scopes onto the issued credential.
Host rollout, migration/rollback tooling and legacy-tier removal remain required
before the overall permissions upgrade is complete. Custom HTTP integrations
must retain owner/session checks and consistent reply error manifests.

Policy failures retain their original causes through admission. The handler's
dependency manifest includes `accesspolicy` errors; host overrides still take
precedence. Joined operational failures must not be rewritten as ordinary
policy denials. Cancellation is checked between adapter calls and before secret
delivery, but a late cancellation does not prove that a write was rolled back.

## Configuration and Initialisation

```go
import (
    "github.com/ooaklee/ghatd/external/accessmanager"
    "github.com/ooaklee/ghatd/external/router"
)

func main() {
    // ... initialise ghatdRouter, ephemeral store, email manager,
    // auth service, user service, api token service, audit service

    accessManagerService := accessmanager.NewService(&accessmanager.NewServiceRequest{
        EphemeralStore:  ephemeralRedisStore,
        EmailManager:    emailManager,
        AuthService:     authService,
        UserService:     userService,
        ApiTokenService: apitokenService,
        OauthServices:   []accessmanager.OauthService{oauthGoogleProvider},
        AuditService:    auditService,
        StaticPlaceholderUuid: staticPlaceholderUUID,
    })

    accessmanagerHandler := accessmanager.NewHandler(&accessmanager.NewHandlerRequest{
        Service:                  accessManagerService,
        Validator:                validator,
        ErrorMaps:                errorMaps,
        Environment:              "production",
        CookiePrefixAuthToken:    "__aauth",
        CookiePrefixRefreshToken: "__rauth",
        CookieDomain:             "example.com",
        OAuthOrigin:              "https://app.example.com",
    })

    accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{
        Router:                             ghatdRouter,
        Handler:                            accessmanagerHandler,
        ActiveOnlyMiddleware:               activeMiddleware,
        ActiveValidApiTokenOrJWTMiddleware: apiTokenOrJWTMiddleware,
        HardenedRateLimitMiddleware:        hardenedRateLimitMiddleware,
    })
}
```

When using `starter/v0`, pass the same dependencies to `starter.NewServices`, `starter.NewHandlers`, and `starter.NewMiddleware`, then call `starter.AttachDefaultRoutes`. The starter path attaches the same `/api/v1/ams` routes as the direct `accessmanager.AttachRoutes` call.

> **Error maps:** The `NewHandler` and `NewMiddleware` constructors accept `ErrorMaps []reply.ErrorManifest` to translate domain errors into HTTP responses. The Access Manager handler includes its domain and dependency manifests (including `user`, `auth`, `apitoken` and `accesspolicy`), so expected lifecycle and policy failures remain structured without a host bundle. Caller-supplied manifests override these defaults. Build them with the shared composer:
>
> ```go
> import "github.com/ooaklee/ghatd/external/errormanifest"
>
> errorMaps := errormanifest.NewComposer().
>     Add(
>         user.UserErrorMap,
>         auth.AuthErrorMap,
>     ).
>     Build()
> ```
>
> Handler error responses resolve unambiguous `%w` causes through
> `Handler.NewHTTPErrorResponse`. Unknown or multi-cause errors receive an opaque
> generic response; host overrides still win for the same domain key. Keep the
> original error for redacted diagnostics. The current reply dependency also
> resolves wrapped errors. This handler adds the stricter authentication policy:
> unknown or multi-cause failures remain opaque, rather than selecting a nested
> client denial. Use it consistently for authentication error responses.
>
> See [package errormanifest](../errormanifest/README.md) for the full convention docs.

For a complete setup guide, see the [Router documentation](../router/README.md).

## Troubleshooting

### Safari Authentication Cookies Persist After Logout

If Safari still presents the authentication cookies (`__aauth`, `__rauth`) or auth info cookies (`refresh_token`, `access_token`) after logging out — while Chrome and Firefox work correctly — the cause is typically **duplicate cookies with different scopes** in Safari's cookie jar.

Safari is strict about cookie attribute matching for deletion. If the cookie jar contains two copies of `__rauth` set under different `Domain`, `Path`, or `Secure` scopes (e.g., across server restarts or configuration changes), the `Set-Cookie` deletion header only matches and removes one copy. The other persists.

**Symptoms**:
- `ephemeral-delete-failed-after-successful-refresh-token-validation` in server logs after logout
- `GET /api/v1/ums/me` returns `202` instead of `401`/`403` after logout
- Works correctly in Chrome/Firefox but not Safari
- HAR or request inspector shows **duplicate entries** for `__rauth` or `refresh_token` in the request's cookie list

**Diagnosis**:
1. Open Safari → **Develop** → **Show Web Inspector** (⌥⌘I)
2. Go to the **Storage** tab → **Cookies** → select the relevant domain (e.g., `localhost`)
3. Look for **duplicate entries** of the same cookie name (e.g., two rows both named `__rauth`) with different values or attributes

**Fix**:
1. In the Storage tab, select and delete **all** duplicate cookie entries by hand
2. After clearing, run through the full login → logout lifecycle again — the `Set-Cookie` deletion headers will now correctly match and remove the single remaining copy
3. If the issue recurs after server restarts, verify the `CookieDomain` configuration is consistent

**Prevention**:
- Maintain a consistent `CookieDomain` setting in the server configuration across restarts (e.g., always use `"localhost"` for local development, never mix empty and explicit domain values)
- Avoid switching between secure/non-secure configurations that produce cookies with different `Secure` attribute scopes


## Secure Google and Apple sign-in

Supply secure providers in `OauthServices`, identity-capable `user/v2` service,
the standard auth service and Redis ephemeral storage. Configure `OAuthOrigin`
on the handler (or starter `NewHandlersRequest`) to the exact trusted browser
origin for explicit account-link POSTs. Apply the user OAuth indexes first.

| Method | Route under `/api/v1/ams` | Behaviour |
| --- | --- | --- |
| GET | `/oauth/providers` | Available secure provider names, without credentials |
| GET | `/oauth/{google\|apple}/login` | Starts a server-held transaction and redirects |
| GET | `/oauth/google/callback` | Completes Google sign-in |
| POST | `/oauth/apple/callback` | Completes Apple's bounded form-post callback |
| POST | `/oauth/{google\|apple}/link` | Starts explicit, fresh-session linking |

Login accepts `request_url` as a rooted same-origin path and optional
`browser=true`. Completion mode and return path are stored at initiation.
Browser success sets normal session cookies and returns a 303; API success
retains the token-metadata response. Browser failures redirect to
`/auth/login?oauth_error=<fixed-code>&request_url=<safe-path>`.

Linking requires a JSON body containing only `request_url` and `browser`, an
exact matching Origin header and the configured access cookie. The service
uses the signed user ID, access UUID and recent `auth_time`, never a user ID
supplied by the caller. A successful POST returns `data.redirect_url` and a
transient cookie. Callback rechecks the original session and current verified
ACTIVE account before atomic linking. Success preserves session cookies and
adds `oauth_linked=google|apple` to the trusted continuation URL. Old or revoked
sessions must reauthenticate. Refresh preserves the original signed login time.

The fixed browser errors are `cancelled`, `unavailable`, `invalid`,
`unverified_email`, `restricted`, `link_required`, `identity_conflict`,
`reauth_required` and `failed`.

`identity_conflict` means the verified provider identity is already linked to
another account. Keep the current session and explain that the user can sign in
to that account, disconnect the provider there after email verification, then
connect it to the intended account; alternatively, use another provider account.
Do not disclose the owning account's email or ID, merge accounts or transfer the
link automatically. `link_required` remains the separate login case where a
matching email needs an explicit connection in Settings.
Provider accounts are found by signed issuer/subject, never automatically by
matching email. Apple repeat sign-in can omit first-only profile data. Every
restricted user status is denied before session issuance.

## Native app handoff

Native clients reuse the provider callbacks, identity resolution and normal
session issuance above. Configure `Handler.ConfigureMobileOAuth` once at
startup with `MobileOAuthConfig{Origin, RedirectURIs, Store}` and
`NewRedisMobileOAuthStore(redisClient, namespace)`. An empty redirect allowlist
disables native discovery and handoff. `Origin` must be the public HTTPS origin
hosting the provider callbacks; register exact private app URIs such as
`com.example.yourapp:/oauth/callback`. Each app should use its own reverse-domain
scheme. These app URIs are separate from Google/Apple's HTTPS redirect URIs.

| Method | Route under `/api/v1/ams` | Behaviour |
| --- | --- | --- |
| GET | `/oauth/mobile/providers` | Available providers and exact allowed app callback URIs |
| POST | `/oauth/{google\|apple}/mobile/login` | Creates an app-bound, two-minute browser start ticket |
| POST | `/oauth/{google\|apple}/mobile/link` | Creates a start ticket bound to a fresh signed app session |
| GET | `/oauth/mobile/start?ticket=…` | Consumes the ticket and starts the existing browser-cookie-bound provider flow |
| POST | `/oauth/mobile/exchange` | Consumes a proven one-minute handoff code and sets normal session cookies in the app response |

Initiation uses JSON `redirect_uri`, `state`, `code_challenge`, and
`code_challenge_method: "S256"`. Generate independent random 32-byte state and
verifier values in the app; the challenge is base64url(SHA256(verifier)). Native
POSTs reject browser Origin headers and require JSON. Link initiation also
requires the existing fresh access cookie. The response's
`data.authorization_url` is opened in the platform system authentication
browser. The browser retains the existing provider state cookie requirement.

A successful provider callback returns only `code` and the original app `state`
to the allowlisted app URI. It does not create an account, link an identity or
set browser session cookies. The app validates the exact callback and state,
then posts `code`, `code_verifier`, `redirect_uri` and `state` to exchange.
Matching proof consumes the code atomically; incorrect proof leaves it usable
by the initiating app. Account status is checked at exchange. Linking requires
the same still-fresh signed app session before the identity is attached.

Exchange returns `data.provider` and `data.linked`. Login sets the existing
access/refresh cookies; linking preserves the current session cookies. Reuse
the native client's cookie manager and secure cookie storage, then confirm the
user via `/api/v1/ums/me`. Existing refresh/logout behaviour remains shared.
Native errors use the same fixed vocabulary in `data.error`; browser cancellation
returns `error=cancelled` with app state. Do not log callback URLs, bodies,
codes, verifiers or cookies, or automatically retry one-use exchanges.

Validation includes the signed-provider HTTP lifecycle test with real isolated
MongoDB/Redis. Set `GHATD_TEST_MONGO_URI` and `GHATD_TEST_REDIS_ADDR` to test-only
stores to exercise both browser and native identity/session lifecycles; normal
unit tests run without those services.


### Connected providers and verified removal

`GET /api/v1/ams/oauth/connections` reports the current account's linked providers,
separately from deployment discovery. Hosts can opt into the two same-origin
JSON disconnect endpoints with `Service.ConfigureOAuthConnections`; they verify
an email by link/code before atomically replacing the sign-in email and removing
a provider. See [the complete host flow](../../docs/how-to/add-google-apple-sign-in.md#show-connections-and-safely-disconnect-a-provider),
including early URL-proof removal, session rotation, optional-adapter contracts,
and the framework integration tests.

A valid session can verify the account's current email without another recent
login. For a new email, recent authentication allows direct verification;
otherwise the current inbox is verified first. First-stage confirmation returns
202 with `disconnected: false` and `next_challenge`, leaving the account untouched.
Only the final 200 response has `disconnected: true` and replacement session
cookies. Both stages support a purpose-specific magic link or eight-character
code; ordinary login tokens cannot authorize disconnection.

`GET /oauth/connections/{provider}/disconnect/challenges/{challenge_id}` reviews
stage/recipient metadata without accepting or consuming a proof. It requires the
same live session and current account/provider snapshot. Optional read-capable
stores implement `oauth.DisconnectChallengeReader`; existing store/client and
challenge-constructor interfaces remain compatible. The host can keep the entire
flow in protected Settings without relaxing public login route guards.

Native Settings can opt in with `OAuthConnectionsConfig.MobileRedirectURIs`.
The `/oauth/mobile/connections` routes use the same account, stage and session
logic, reject browser Origin headers, and bind email proof to the exact native
return address. They renew cookies through the existing secure mobile cookie
jar. See the adoption guide for code/link review and platform registration.

## Logout command boundaries

**Breaking:** `Service.LogoutUser` now accepts `*LogoutUserRequest`, not an HTTP
request. Pass the selected `AccessToken` and/or `RefreshToken`; do not decode this
command from JSON or pass an unverified actor ID. The manager derives the owner
from signed credentials through [auth.SessionRemovalVerifier](../auth/README.md#deletion-only-verification).
Custom auth adapters must implement that narrow capability; missing adapters
fail closed. Replace `RemoveRefreshTokenWithCookieValue` with
`LogoutUser(ctx, &LogoutUserRequest{RefreshToken: value})`. The old helper, which
conflated storage failures with absence, has been removed.

Ordinary logout is cleanup-only: it never issues or refreshes tokens and does not
require a current ACTIVE account or matching email revision. Correctly signed
expired credentials may remove their own records but cannot authenticate.
All supplied credentials must verify and have the same owner before any write.
The HTTP mapper accepts a bearer or access cookie, rejects conflicting values
and duplicate credentials, and leaves incoming headers unchanged. It does not
read query/body identity. No credentials is an idempotent client-cleanup success.

Client auth cookies are cleared on every ordinary logout response. Successful
API responses remain blank 200 with an access credential, or blank 202 without
one; successful web/HTMX requests retain their home redirect. Native failures
use the shared manifest and never become a redirect or blank success. All
handler responses are `no-store`. A failed request may have removed one record:
do not infer rollback or confirmed complete revocation from an error. Successful
cleanup emits best-effort audit attribution to the signed owner; audit failure
does not undo removal.
Partial/uncertain cleanup emits a fixed operation warning, not a successful
logout audit event. Store/audit warnings omit native diagnostic text, which may
contain secrets; the original error remains available to trusted callers.
Multiple cleanup failures retain their joined native causes internally. The
existing strict authentication response policy renders every multi-cause tree
as generic 500, even when each cause is mapped; a single cause retains its
canonical status and host override. No raw diagnostic text reaches the client.

**Breaking:** `LogoutUserOthersRequest.UserId` becomes `UserID`, alongside a
transport-excluded `ActorID`. In-process adapters must preserve explicitly
verified, unmixed session context and bind both IDs to its owner. The manager
rechecks the live ACTIVE account, signed type/revision, supplied access UUID
against the published session, refresh owner/type/revision and both live records.
It then calls the lower store with exactly those two owned exemptions. API-only
credentials, forged owners and unrelated credentials cannot select a sweep.
Errors preserve native causes; confirmed missing records are unauthorized.

The other-sessions route retains its existing cookie-based ACTIVE middleware,
including its credential selection and refresh policy. A refreshed downstream
request now contains the replacement cookies as well as the new bearer, so
management commands do not receive the consumed predecessor. Ordinary logout
does not use that middleware. Other-sessions success remains blank 202 and does
not clear the initiating cookies.

### Revocation limits

These commands remove supplied records or perform an owner-scoped SCAN sweep;
they do **not** provide atomic session-family revocation. Same-owner access and
refresh possession does not establish a signed pair because current tokens have
no family ID. Concurrent login/rotation may escape the sweep; rotation replay
records are not swept, and a successor already issued under an old refresh
credential may remain live. Bearer-only logout cannot remove an unknown refresh
credential. Live account checks do not close those races. Clients should discard
their local credentials on logout, but must not describe this as guaranteed
revocation of every device or family. See [store semantics](../ephemeral/README.md#target-only-session-cleanup).
