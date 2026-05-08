# Access Manager

The `accessmanager` package handles authentication, authorisation, and user lifecycle management — including user creation, login, registration, email verification, OAuth integration, and API token management. It also provides middleware for JWT validation, API token validation, rate limiting, and hardened code-verification brute-force protection.

## Dual-Channel Verification

The access manager supports a dual-channel verification flow: users receive both a **magic link** (with a JWT token) and an **8-character alphanumeric code** in the same login or verification email.

- **Magic link flow** (`?t=<jwt-token>`): User clicks the link → token is validated → authenticated
- **Code flow** (`?c=ABCD1234`): User enters the code in the app or web interface → code is resolved to its token via ephemeral storage → token is validated → authenticated

See the [Email Manager](../email-manager/) documentation for details on the email templates that deliver both channels.

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
