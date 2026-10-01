# Access Manager

The `accessmanager` package handles authentication, authorisation, and user lifecycle management — including user creation, login, registration, email verification, OAuth integration, and API token management. It also provides middleware for JWT validation, API token validation, rate limiting, and hardened code-verification brute-force protection.

## Integration Model

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

Browser clients should use credentialed requests, such as Axios `withCredentials: true`, and native clients should use a persistent cookie jar. Clients do not need to store JWTs themselves. To resolve the current session, call `GET /api/v1/ums/me`; a `200` response means the cookie session is usable, while `401` or `403` should clear local user state and send the user through the login flow again.

## Refresh Rotation Tolerance

Access Manager treats refresh-token rotation as a one-winner operation. The first request that validates a refresh token claims a short-lived rotation lock, consumes the old refresh token, creates the replacement access and refresh tokens, and stores a short-lived replay result in ephemeral storage.

Near-concurrent duplicate refreshes for the same user and refresh token do not rotate a second time. They first check for an existing replay result, then wait briefly when another request already holds the lock. If the winning request completes, the duplicate receives the same replacement token pair. If no result appears before the wait expires, the duplicate is rejected and the client should resolve the session again through the normal `/me` probe or login flow.

Middleware refreshes also validate the retried request before writing replacement cookies to the response. This avoids committing cookies for a token pair that the protected route would immediately reject.

Clients should still avoid intentionally fanning out refresh calls, but host applications do not need every tab, worker, and retry path to serialize perfectly. The server tolerates the common browser race where multiple requests discover an expired access-token cookie at nearly the same time.

## Dual-Channel Verification

The access manager supports a dual-channel verification flow: users receive both a **magic link** (with a JWT token) and an **8-character alphanumeric verification code** in the same login or verification email. Some apps label this manual-entry value as a secret code.

- **Magic link flow**: The email includes `/v0/auth/verify?type=<verification-type>&__t=<jwt-token>`. The bridge redirects to `/api/v1/ams/login?t=<jwt-token>` or `/api/v1/ams/verify/email?t=<jwt-token>`, and the API validates the token before issuing the session cookies.
- **Code flow**: The email also includes an 8-character A-Z/0-9 code. The app submits the code to `/api/v1/ams/login?c=<code>` for login or `/api/v1/ams/verify/email?c=<code>` for signup/email verification. Access Manager resolves the code to the same ephemeral token, validates it, and then issues the same session cookies.

See the [Email Manager](../emailmanager/README.md) documentation for details on the email templates that deliver both channels.

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
8. Access Manager validates the login token, creates an authenticated session, stores it in ephemeral storage, invalidates the initial login token, sets auth cookies, and returns `200 OK` or redirects to `next_step`.

For an active user, duplicate login-initiation requests for the same user, dashboard flag, and requested return URL are suppressed for a short cooldown window. The API still preserves its non-enumerating response behavior, but only the first accepted request should send an email. If token setup or email delivery fails before the email is accepted, the cooldown is released so a retry can send a new email.

Client login forms should treat any successful `2xx` response from `POST /api/v1/ams/login` as "check your email", disable duplicate submits while the request is in flight, and keep the form locked once the request is accepted. This protects email quotas and gives users a stable transition to the check-email screen.

Use verification type `1` when the client is tracking a pending login email.

### Email Login With Verification Code

1. The app starts the same login flow with `POST /api/v1/ams/login`.
2. The user enters the 8-character code from the email.
3. The app normalises the code to uppercase and calls `GET /api/v1/ams/login?c=<code>`.
4. Access Manager resolves the code from ephemeral storage, validates the underlying token, creates the same cookie session as the magic-link flow, invalidates the initial login token, and returns `200 OK`.

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
| **One-time use** | Codes and tokens are invalidated after successful verification |
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

### Authenticated (JWT or API token required)
- `POST /api/v1/ams/users/{userID}/tokens` — Create an API token
- `GET /api/v1/ams/users/{userID}/tokens` — List API tokens
- `DELETE /api/v1/ams/users/{userID}/tokens/{apiTokenID}` — Delete an API token
- `PUT /api/v1/ams/users/{userID}/tokens/{apiTokenID}/activate` — Activate an API token
- `PUT /api/v1/ams/users/{userID}/tokens/{apiTokenID}/revoke` — Revoke an API token
- `GET /api/v1/ams/users/{userID}/tokens/thresholds` — Get token thresholds
- `GET /api/v1/ams/logout/other-sessions` — Revoke the user's other active sessions while keeping the current session

### Active users only
- `PATCH /api/v1/ams/users/{userID}/email` — Update user email address

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

> **Error maps:** The `NewHandler` and `NewMiddleware` constructors accept `ErrorMaps []reply.ErrorManifest` to translate domain errors into HTTP responses. Handlers auto-include their own domain error maps as a base layer; callers pass only cross-package/shared maps (e.g. `user`, `auth`) as overrides. Build them with the shared composer:
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
> See [package errormanifest](../errormanifest/) for the full convention docs.

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
