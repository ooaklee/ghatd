# Authenticating the App

Use this guide when wiring a web, native, or hybrid app to GHATD authentication. For the package-level API reference, see [Access Manager](../getting-started/access-manager/).

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
  headers: { 'X-Platform': 'web' },
});
```

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

## Google SSO

Configure a Google provider on the server and pass it to Access Manager as an OAuth service. The provider name becomes the route segment:

```http
GET /api/v1/ams/oauth/google/login?request_url=/app
```

The login endpoint creates an `HttpOnly` state cookie and redirects to Google. Google then calls:

```http
GET /api/v1/ams/oauth/google/callback?code=<provider-code>&state=<state>
```

On callback, Access Manager:

- verifies the state value against the provider cookie;
- exchanges the provider code and fetches provider user information;
- finds or creates the GHATD user by provider email;
- records provider-verified email status when present;
- creates the GHATD session cookies;
- clears the provider state cookie;
- includes `X-Web-Location` when a `request_url` was supplied.

If the app needs a pure browser redirect after the provider callback, wrap or customise the callback route so it follows `X-Web-Location` after the cookies are set. If the app handles the callback with an HTTP client, read `X-Web-Location` and route the user in the app.

## Refresh And Logout

Access Manager middleware can refresh an expired access-token cookie when the refresh-token cookie is still valid. It writes the rotated cookies to the response and lets the protected request continue.

Clients can also call the explicit refresh endpoint when they need to rotate before another API request:

```http
POST /api/v1/ams/tokens/refresh
```

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

If a client calls `/api/v1/ams/login` or `/api/v1/ams/verify/email` directly with `next_step`, the client or host application should apply the same same-origin rules before sending it.

## Related Docs

- [Access Manager](../getting-started/access-manager/)
- [Email Manager](../getting-started/email-manager/)
- [User Manager](../getting-started/user-manager/)
- [starter/v0](../getting-started/starter-v0.md)
