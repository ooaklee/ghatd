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

## Dual-Channel Verification

The access manager supports a dual-channel verification flow: users receive both a **magic link** (with a JWT token) and an **8-character alphanumeric verification code** in the same login or verification email. Some apps label this manual-entry value as a secret code.

- **Magic link flow**: The email includes `/v0/auth/verify?type=<verification-type>&__t=<jwt-token>`. The bridge redirects to `/api/v1/ams/login?t=<jwt-token>` or `/api/v1/ams/verify/email?t=<jwt-token>`, and the API validates the token before issuing the session cookies.
- **Code flow**: The email also includes an 8-character A-Z/0-9 code. The app submits the code to `/api/v1/ams/login?c=<code>` for login or `/api/v1/ams/verify/email?c=<code>` for signup/email verification. Access Manager resolves the code to the same ephemeral token, validates it, and then issues the same session cookies.

See the [Email Manager](../email-manager/) documentation for details on the email templates that deliver both channels.

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

Use verification type `1` when the client is tracking a pending login email.

### Email Login With Verification Code

1. The app starts the same login flow with `POST /api/v1/ams/login`.
2. The user enters the 8-character code from the email.
3. The app normalises the code to uppercase and calls `GET /api/v1/ams/login?c=<code>`.
4. Access Manager resolves the code from ephemeral storage, validates the underlying token, creates the same cookie session as the magic-link flow, invalidates the initial login token, and returns `200 OK`.

The code is a manual-entry alias for the underlying token. This keeps the email link and manual code paths equivalent after code resolution.

### SSO With Google Or Another OAuth Provider

OAuth support is provider-based. A host application creates one or more providers, such as `oauth.NewGoogleProvider`, and passes them to Access Manager as `OauthServices`.

1. The app starts SSO with `GET /api/v1/ams/oauth/google/login`, optionally including `request_url=<path>`.
2. Access Manager finds the `google` provider, creates a random CSRF protection state, stores that state in an `HttpOnly` provider cookie, appends the requested return path into the state value, and redirects to the provider authorization URL.
3. The provider redirects back to `GET /api/v1/ams/oauth/google/callback` with `code` and `state`.
4. Access Manager compares the returned `state` with the provider cookie.
5. Access Manager exchanges the provider code for provider tokens and fetches provider user information.
6. If a GHATD user already exists for the provider email, Access Manager creates a GHATD session for that user and records provider-verified email status when present. If no user exists, Access Manager creates a user without sending a verification email, runs the GHATD email-verification revisions, and creates a GHATD session.
7. Access Manager removes the provider state cookie, sets the GHATD auth cookies, returns a token response, and exposes `X-Web-Location` when a return URL was supplied.

Host applications that want a browser-only final redirect can wrap or customise the callback behavior. Applications that call the callback through an HTTP client can read `X-Web-Location` and route the user after the cookies have been stored.

For an app-facing checklist that applies these flows from a client perspective, see [Authenticating the App](../../how-to/authenticating-the-app.md).

## Security Measures

| Layer | Mechanism |
|---|---|
| **Code entropy** | 8-character A-Z/0-9 = ~2.8 trillion combinations |
| **Collision resistance** | Ephemeral storage check with up to 5 retry attempts |
| **Brute-force protection** | `HardenedRateLimitProtection` middleware tracks attempts per IP and per code within a configurable window (default: 5/hr per IP, 5/hr per code) |
| **Auto-blocking** | IPs exceeding the threshold are temporarily blocked (default: 1 hour) |
| **One-time use** | Codes and tokens are invalidated after successful verification |
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
- `GET /api/v1/ams/oauth/google/login` — Initiate Google OAuth login
- `GET /api/v1/ams/oauth/google/callback` — Google OAuth callback

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
> See [package errormanifest](../../../external/errormanifest/) for the full convention docs.

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
