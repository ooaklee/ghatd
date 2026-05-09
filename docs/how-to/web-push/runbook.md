# Web Push Notification Runbook

This guide walks you through every step needed to enable, test, and
troubleshoot Web Push notifications in a GHATD deployment.  It assumes
you are running a GHATD host application that wires `external/notifier` into UMS.

---

## Prerequisites

- a GHATD host application running locally (for example via its `Makefile` or Docker Compose setup)
- `mongo` and `redis` available (many local Docker Compose setups include both)
- A browser that supports the Push API: Chrome 50+, Firefox 44+,
  Edge 17+, or Safari 16+ (macOS only)
- `openssl` or `webpush-go` CLI for VAPID key generation

---

## 1. Generate VAPID Keys

VAPID (Voluntary Application Server Identification) keys let the browser
know which server is allowed to push.  You need one key pair per
deployment environment.

### Option A – openssl (no Go required)

```bash
# Generate an EC private key
openssl ecparam -genkey -name prime256v1 -out vapid_private.pem

# Extract the public key in uncompressed DER, then base64-url encode it
openssl ec -in vapid_private.pem -pubout -outform DER 2>/dev/null |
  tail -c 65 | base64 | tr '+/' '-_' | tr -d '=\n'
echo

# Extract the private key (raw bytes) and base64-url encode it
openssl ec -in vapid_private.pem -outform DER 2>/dev/null |
  tail -c +8 | head -c 32 | base64 | tr '+/' '-_' | tr -d '=\n'
echo
```

### Option B – Go (if you have the toolchain)

```bash
go run github.com/SherClockHolmes/webpush-go/cmd/vapid-gen@latest
```

**Save both keys.**  The public key is shared with browsers; the private
key stays on the server and **must never be committed to version control**.

---

## 2. Set Environment Variables

### Example envconfig mapping

| Setting struct field              | Env var                                 | Default  | Notes |
|-----------------------------------|-----------------------------------------|----------|-------|
| `NotifierWebPushEnabled`          | `NOTIFIER_WEBPUSH_ENABLED`             | `false`  | Set to `true` after keys are provided |
| `NotifierWebPushVAPIDPublicKey`   | `NOTIFIER_WEBPUSH_VAPID_PUBLIC_KEY`    | (empty)  | The public key from step 1 |
| `NotifierWebPushVAPIDPrivateKey`  | `NOTIFIER_WEBPUSH_VAPID_PRIVATE_KEY`   | (empty)  | The private key from step 1 |
| `NotifierFCMEnabled`              | `NOTIFIER_FCM_ENABLED`                 | `false`  | Leave `false` unless Firebase is configured |
| `NotifierFCMCredentialsFile`      | `NOTIFIER_FCM_CREDENTIALS_FILE`        | (empty)  | Path to Firebase service-account JSON |
| `NotifierFCMProjectID`            | `NOTIFIER_FCM_PROJECT_ID`             | (empty)  | Firebase project ID |

### Docker Compose (development)

Add these lines to the `x_environments` block in
`docker/compose/docker-compose.yaml`:

```yaml
# NOTIFICATIONS — WEB PUSH
- NOTIFIER_WEBPUSH_ENABLED=${NOTIFIER_WEBPUSH_ENABLED-true}
- NOTIFIER_WEBPUSH_VAPID_PUBLIC_KEY=${NOTIFIER_WEBPUSH_VAPID_PUBLIC_KEY-}
- NOTIFIER_WEBPUSH_VAPID_PRIVATE_KEY=${NOTIFIER_WEBPUSH_VAPID_PRIVATE_KEY-}
- NOTIFIER_FCM_ENABLED=${NOTIFIER_FCM_ENABLED-false}
- NOTIFIER_FCM_CREDENTIALS_FILE=${NOTIFIER_FCM_CREDENTIALS_FILE-}
- NOTIFIER_FCM_PROJECT_ID=${NOTIFIER_FCM_PROJECT_ID-}
```

Then export the keys before starting:

```bash
export NOTIFIER_WEBPUSH_ENABLED=true
export NOTIFIER_WEBPUSH_VAPID_PUBLIC_KEY="<your-public-key>"
export NOTIFIER_WEBPUSH_VAPID_PRIVATE_KEY="<your-private-key>"
make dev
```

### Production Deployment

Add these values through your deployment environment or secret manager. Do not commit production VAPID private keys.

---

## 3. Verify the Notifier Is Wired

Start the host application with the env vars set.  Check the **notifier config**
endpoint to confirm the server is advertising Web Push:

```bash
# This route is authenticated because it is under /me.
# Include your local app session cookies, or call it from the signed-in frontend.
curl -s http://localhost:4000/api/v1/ums/me/notifications/config \
  | jq .
```

**Expected response when enabled:**
```json
{
  "data": {
    "supported_channels": ["WEBPUSH"],
    "webpush": {
      "enabled": true,
      "vapid_public_key": "<your-public-key>"
    },
    "fcm": {
      "enabled": false
    }
  }
}
```

If `"supported_channels"` is empty or `"webpush.enabled"` is `false`,
the env vars are not being picked up — re-check step 2.

---

## 4. Sign In and Enable Browser Notifications

1. Open the frontend (for example: `http://localhost:5173`).
2. Sign in with an existing account or create one.
3. Navigate to **Settings** → **Notifications** tab.
4. In the **Web Push** card, click **Enable**.

**What happens behind the scenes:**
1. The browser fetches `/me/notifications/config` to get the VAPID
   public key and confirm the server is ready.
2. The browser asks for notification permission.
3. On grant, the browser calls `pushManager.subscribe()` with the
   VAPID key.  The browser generates a Push subscription with an
   endpoint URL and encryption keys.
4. The subscription is `POST`ed to `/me/notifications/addresses` as
   a `WEBPUSH` channel registration.
5. The server stores the subscription, keyed by a SHA-256 hash of
   `channel:endpoint` so the same browser always points to the
   current user.

**Visual signals in the UI:**
- An **Enabled** badge on the Web Push card
- The device name (e.g. "Chrome on MacIntel") and last-seen timestamp
- Permission status card shows "Granted"
- Backend status card shows "Enabled"

---

## 5. Send a Test Notification (Admin Route)

The `NotifyUser` route is admin-only. You can call it with a valid
admin API token or an admin JWT/session.

### Via curl (API token)

```bash
API_TOKEN="<admin-api-token>"
USER_ID="<target-user-id>"

curl -s -X POST \
  "http://localhost:4000/api/v1/ums/users/${USER_ID}/notifications" \
  -H "Content-Type: application/json" \
  -H "X-Api-Token: ${API_TOKEN}" \
  -d '{
    "title": "Hello from Web Push",
    "message": "If you see this, push is working!",
    "data": {
      "url": "/settings#notifications"
    }
  }' | jq .
```

**Expected success response (one active browser address):**
```json
{
  "data": [
    {
      "channel": "WEBPUSH",
      "attempted": 1,
      "sent": true,
      "cleaned": 0,
      "skipped": false
    }
  ]
}
```

**Expected response when no addresses are registered:**
```json
{
  "errors": [
    {
      "code": "NTF00-006",
      "title": "Not Found",
      "detail": "No active notification addresses are registered for this user."
    }
  ]
}
```

### What you should see in the browser

A notification pop-up with:
- Title: `Hello from Web Push`
- Body: `If you see this, push is working!`
- Clicking the notification opens `/settings#notifications`

Notifications appear even when the browser tab is in the background
or the browser window is minimised.

---

## 6. Error Codes

The notifier package defines these error codes.  They appear in the
`"code"` field of JSON error responses.

| Code       | HTTP | Meaning |
|------------|------|---------|
| NTF00-001  | 500  | Database operation failed |
| NTF00-002  | 400  | Notification address payload is invalid |
| NTF00-003  | 400  | Requested channel is not supported |
| NTF00-004  | 404  | Notification address not found |
| NTF00-005  | 503  | Sender is not enabled (no VAPID keys) |
| NTF00-006  | 404  | No active addresses for user |
| NTF00-007  | 500  | One or more sends failed |
| NTF00-008  | 400  | User ID is required |
| NTF00-009  | 400  | Preferences payload is invalid |

UMS also returns:

| Code      | HTTP | Meaning |
|-----------|------|---------|
| USM00-002 | 401 | Unauthenticated (no user ID in request context) |
| USM00-018 | 503 | Notification features not enabled (notifier not wired) |

---

## 7. Troubleshooting

### "Web Push is unsupported" in Settings

The browser doesn't expose `PushManager` or `serviceWorker`.  Check:
- Are you on `http://localhost` (not a plain IP address)?  The Push
  API requires a secure context (`https://` or `localhost`).
- Are you in an incognito/private window?  Firefox disables the Push
  API in private browsing.  Chrome allows it but service worker
  registration may behave differently.
- Is the Service Worker registered?  Open DevTools → Application →
  Service Workers.  You should see one active worker for the origin.

### "Permission denied" after clicking Enable

The user clicked Block on the browser permission prompt.  To reset:
- **Chrome**: Click the lock icon in the address bar → Site Settings →
  Reset Permissions.
- **Firefox**: Click the permissions icon next to the address bar →
  "X" next to Notifications → refresh the page.
- **Safari**: Safari → Settings for This Website → Notifications →
  Allow.

### Notification appears but clicking does nothing

The service worker's `notificationclick` handler opens the URL stored
in `event.notification.data.url`.  Verify:
1. The push payload included a `data.url` field (see step 5 example).
2. The service worker is the latest version.  Open DevTools →
   Application → Service Workers, click "Update" on the active worker.
3. Pop-up blockers may prevent `clients.openWindow()`.  Check browser
   settings.

### Stale service worker still running

After changing `service-worker.ts`, the old worker may still be active:
- DevTools → Application → Service Workers → "Unregister"
- Refresh the page; the new worker will register automatically.
- Or check "Update on reload" to force-reload the worker on each
  page refresh during development.

### "NTF00-005 — Sender not enabled" even with env vars set

The Web Push sender is only enabled when **all three** conditions are
true:
1. `NOTIFIER_WEBPUSH_ENABLED=true`
2. `NOTIFIER_WEBPUSH_VAPID_PUBLIC_KEY` is non-empty
3. `NOTIFIER_WEBPUSH_VAPID_PRIVATE_KEY` is non-empty

Double-check the values are actually making it to the server (see
step 3 — the `/config` response tells you).

### Expired subscription not cleaned up

When a browser unsubscribes (user clears site data, reinstalls
browser), the old Push subscription returns HTTP 410 Gone on next
send.  The notifier detects this via the webpush-go error message
pattern `unexpected status code: 410` and automatically disables
the address (sets `status: DISABLED`).  It does **not** delete the
address so the same device can re-register later without creating a
duplicate record.

If cleanup seems broken, check the server logs for
`"cleanup failed for address"` messages, which indicate the MongoDB
`DisableAddressByHash` call failed (likely a database connectivity
issue).

### Testing with multiple browser profiles

To simulate different users on the same machine:
- **Chrome**: Create separate profiles (Profile 1, Profile 2).
- **Firefox**: Use `about:profiles` to create and manage profiles.
- Each profile gets its own service worker, Push subscription, and
  localStorage — just like a separate device.

### Checking the service worker in production

The service worker must be served from the **root of the app domain**
(`/sw.js` or similar).  The `vite-plugin-pwa` generates this during
build.  In production, make sure the generated service worker is served from the same origin and scope as the frontend assets.

---

## Additional Context

- [Notifier package README](../../../external/notifier/README.md) —
  package-level overview, how to wire into UserManager
- [ADR005: Notifier Architecture](../../adr/adr005-add-external-notifier-for-push-notifications.md) —
  design decisions, address dedup, and sanitised output
- [Web Push spec (MDN)](https://developer.mozilla.org/en-US/docs/Web/API/Push_API) (external)
- [VAPID spec (RFC 8292)](https://datatracker.ietf.org/doc/html/rfc8292) (external)
- [nikoksr/notify](https://github.com/nikoksr/notify) (external) —
  the library GHATD uses to send push notifications
- [webpush-go](https://github.com/SherClockHolmes/webpush-go) (external) —
  the Go library for Web Push protocol messages
