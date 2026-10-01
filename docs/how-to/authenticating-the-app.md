# Authenticating the App

Use this guide when wiring a web, native, or hybrid app to GHATD authentication. For the package-level API reference, see [Access Manager](../../external/accessmanager/README.md).

## Server Setup

1. Configure absolute `BACKEND_BASE_URL` and `FRONTEND_BASE_URL` values. The backend URL is used in email links; the frontend URL is used for safe post-auth redirects.
2. Configure CORS so the frontend origin can call the API with credentials.
3. Configure consistent cookie settings: `COOKIE_PREFIX_AUTH_TOKEN`, `COOKIE_PREFIX_REFRESH_TOKEN`, and `COOKIE_DOMAIN`.
4. Attach the default auth verification bridge with `router.AttachDefaultAuthVerifyRoute`. This registers `/v0/auth/verify`.
5. Configure `emailmanager.NewStandardEmailManager` with `EmailVerificationFullEndpoint` set to `${BACKEND_BASE_URL}/v0/auth/verify`.
6. Create the Access Manager service with an ephemeral store, email manager, auth service, user service, API token service, audit service, and any OAuth services.
7. Attach the Access Manager and User Manager route groups. Access Manager owns `/api/v1/ams`; User Manager owns `/api/v1/ums/me`, which clients use to resolve the session.

With `starter/v0`, this usually means passing the dependencies to `starter.NewServices`, passing cookie settings to `starter.NewHandlers` and `starter.NewMiddleware`, then calling `starter.AttachDefaultRoutes`.

## Client Contract

Browser clients should use credentialed requests:

```ts
axios.create({
  baseURL: '/api/v1/ams/',
  withCredentials: true,
  headers: {
    'X-Platform': 'web',
    'X-Timezone': Intl.DateTimeFormat().resolvedOptions().timeZone,
  },
});
```

`X-Platform` is a client-surface hint. Access Manager uses `web` for browser
logout flows that should redirect back to `/`. Native or extension clients
should send their own platform value, such as `mobile` or `browser-extension`,
and expect API responses rather than navigation redirects.

`X-Timezone` should be an IANA timezone, such as `Europe/London`. GHATD does
not authenticate with this value, but host applications can use it when passing
request context into timezone-aware packages such as Streaker and Reminder.

Native clients should use a persistent cookie jar. The app should not store token response values as its primary session state; GHATD sets `HttpOnly` access and refresh token cookies, and the client sends those cookies back on later requests.

Use `GET /api/v1/ums/me` as the session probe:

| Response | Client action |
|---|---|
| `200` | Store the returned user profile in app state and continue |
| `202` | Treat as unauthenticated when the custom `/me` middleware is wired |
| `401` or `403` | Clear local user state and route to login |
| Network failure | Treat separately from authentication; show offline or retry UI |

The standard starter wiring uses `CustomMeEndpointValidApiTokenOrJWTMiddleware` for `GET /api/v1/ums/me`. That middleware intentionally changes one missing-credentials auth failure from `401 Unauthorized` to `202 Accepted`, so public or indexable pages do not receive a hard auth error when the session probe runs without cookies. Client code should not treat `202` from `/api/v1/ums/me` as an authenticated session.

Also keep the auth-initiation statuses separate from session-probe statuses:

| Endpoint | Success status | Meaning |
|---|---:|---|
| `POST /api/v1/ams/signup` | `201` | User was created and should verify email |
| `POST /api/v1/ams/login` | `202` | Login or verification email was accepted for delivery |

Neither status means the client is authenticated yet. Only the later magic-link or code verification response establishes the cookie session.

For login initiation, treat any successful `2xx` response from `POST /api/v1/ams/login` as an accepted request and navigate the user to the check-email screen. Access Manager may suppress duplicate email sends for the same active user and request context during a short cooldown window, so retrying the same request repeatedly is not a useful user action. Keep the submit button disabled while the request is in flight, and keep it locked after the accepted response while the app transitions to the check-email route.

For protected browser routes, preserve the intended destination in a `request_url` query value, such as `/auth/login?request_url=/app/projects`. Pass the same path to `POST /api/v1/ams/login` or `POST /api/v1/ams/signup` so email links can return the user to the right place.

## Email Magic Link

1. Start login with `POST /api/v1/ams/login`.
2. Include `email` and, when useful, `request_url`.
3. Treat `202 Accepted` as "check your email".
4. The email link points to `/v0/auth/verify?type=1&__t=<token>&request_url=<path>`.
5. The bridge redirects to `/api/v1/ams/login?t=<token>&next_step=<frontend-url>`.
6. Access Manager validates the token, sets the auth cookies, and redirects to `next_step` when present.
7. After the browser returns to the app, call `GET /api/v1/ums/me` to hydrate the user profile.

For signup or email verification, the same bridge is used with `type=2`, and the bridge targets `/api/v1/ams/verify/email?t=<token>`.

## Email Verification Code

The same email includes an 8-character alphanumeric verification code, sometimes presented as a secret code, for users who cannot or do not want to click the link.

For login:

```http
GET /api/v1/ams/login?c=ABCD1234
```

For signup or email verification:

```http
GET /api/v1/ams/verify/email?c=ABCD1234
```

Client guidance:

- Strip whitespace and uppercase the code before submitting it.
- Keep the UI-specific verification type in app state: `1` for login, `2` for signup/email verification.
- Use the verification type to choose the endpoint; the API route is what determines whether the code is treated as a login or email-verification code.
- On success, route through the same post-auth path as the magic-link flow and re-check `/api/v1/ums/me`.

Access Manager resolves the code to its underlying ephemeral token and then runs the same validation path as the magic-link flow. Hardened rate limiting protects the code endpoints.

## Google and Apple sign-in

Follow [Add Google and Apple sign-in](add-google-apple-sign-in.md) to register
providers, apply identity indexes and wire the secure constructors. Both
providers use the normal GHATD accounts and cookie sessions.

Browser clients discover configured providers with
`GET /api/v1/ams/oauth/providers`, then navigate to:

```http
GET /api/v1/ams/oauth/google/login?browser=true&request_url=%2Fapp
```

Use `apple` for Apple. Google returns by GET; Apple uses a form-POST callback.
GHATD validates the transaction cookie/state and signed identity, resolves the
account by issuer/subject, and sets normal session cookies. `browser=true`
selects an HTTP 303 to the stored safe path; no custom callback wrapper is
needed. Confirm `/api/v1/ums/me` after return. Matching email on another account
requires explicit linking from that account's fresh session.

Native clients use the [one-use handoff](../../external/accessmanager/README.md#native-app-handoff)
instead of trying to share the system browser's cookie store. Exchange the code
using the initiating app's verifier, persist the normal response cookies through
the existing secure cookie manager, then confirm `/api/v1/ums/me`. Refresh and
logout remain the same. Keep the magic-link/code option alongside provider buttons.

## Refresh And Logout

Access Manager middleware can refresh an expired access-token cookie when the refresh-token cookie is still valid. It writes the rotated cookies to the response and lets the protected request continue.

Clients can also call the explicit refresh endpoint when they need to rotate before another API request:

```http
POST /api/v1/ams/tokens/refresh
```

Refresh-token rotation is tolerant of near-concurrent duplicate requests. The first valid request consumes the old refresh token and stores a short-lived replay result; duplicate requests for the same token can reuse that result instead of failing simply because another tab or request won the race. Middleware writes replacement cookies only after the retried protected request accepts the refreshed access token.

Clients should still use `/api/v1/ums/me` as the source of truth after refresh uncertainty. If a refresh call fails or a protected request still returns an auth error, clear local session state and route through login rather than looping refresh attempts.

For logout, clear local user state first, then call:

```http
GET /api/v1/ams/logout
```

The server deletes the auth cookies and removes the current refresh token from ephemeral storage when it can. To keep the current session but revoke other active sessions for the same user, call:

```http
GET /api/v1/ams/logout/other-sessions
```

## Redirect Safety

Use `request_url` for user intent before authentication and `next_step` only for the API redirect after verification.

The default email-link path normalises `request_url` before it becomes `next_step`:

- relative paths like `/app/settings` are allowed;
- same-origin absolute frontend URLs are reduced to frontend paths;
- malformed or external URLs fall back to the configured frontend root.

If a client calls `/api/v1/ams/login` or `/api/v1/ams/verify/email` directly with `next_step`, the client or host application should apply the same-origin rules before sending it.

## Related Docs

- [Access Manager](../../external/accessmanager/README.md)
- [Email Manager](../../external/emailmanager/README.md)
- [User Manager](../../external/usermanager/README.md)
- [starter/v0](../../external/starter/v0/README.md)
