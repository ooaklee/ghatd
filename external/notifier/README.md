# Notifier Package

The `external/notifier` package handles push notification delivery for GHATD.
It stores which devices a user has registered, remembers their notification
preferences, and delivers push messages through channel-specific adapters.

## Quick Start (13-year-old friendly)

Imagine you have a website and a mobile app. You want to send a message to
a user's phone or browser even when they're not looking at your app. That's
what push notifications do.

Here's how this package helps:

1. **Register a device** — When a user clicks "Enable notifications" in your
   app, the browser or phone gives you a special address (like a mailing
   address for push messages). You send that to `RegisterAddress()` and the
   package saves it.

2. **Save preferences** — Not everyone wants notifications on every channel.
   `GetPreferences()` and `UpdatePreferences()` let users turn notifications
   on or off per channel.

3. **Send a notification** — An admin or automated process calls `NotifyUser()`
   with a title and message. The package checks the user's preferences, finds
   their registered devices, and delivers the message.

**Important safety rule**: The package never sends the actual push endpoint
or token back to the browser or phone app — it only returns a "summary" that
tells you the device name, platform, and status. The secrets stay on the server.

## Package Structure

```
notifier/
├── model.go          # Data types: addresses, preferences, config
├── service.go        # Business logic: register, list, send, preferences
├── repository.go     # MongoDB persistence
├── sender.go         # Web Push and FCM delivery adapters
├── request.go        # API request types
├── response.go       # API response types
├── const.go          # Constants and error keys
├── errors.go         # Sentinel errors
├── errormap.go       # HTTP error code mapping
├── service_test.go   # Service tests with fakes
└── migrations/
    └── indexes_notifier.go  # Database index setup and rollback
```

## How to Use

### 1. Create a notifier service

```go
import "github.com/ooaklee/ghatd/external/notifier"

repo := notifier.NewRepository(myMongoStore)

service := notifier.NewService(&notifier.NewServiceRequest{
    Repository: repo,
    Senders: []notifier.ChannelSender{
        notifier.NewWebPushSender(notifier.WebPushSenderConfig{
            Enabled:         true,
            VAPIDPublicKey:  os.Getenv("VAPID_PUBLIC"),
            VAPIDPrivateKey: os.Getenv("VAPID_PRIVATE"),
        }),
    },
})
```

### 2. Wire it into UserManager

```go
umsService.WithNotifierService(service)
```

This registers the notification endpoints under `/api/v1/ums/me/notifications`
and the admin send route at `/api/v1/ums/users/{id}/notifications`.

### 3. Register a browser for push (client side)

```ts
// Fetch config to get the VAPID key
const config = await umsClient.get('me/notifications/config');

// Ask the browser for permission and subscribe
const subscription = await pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(config.data.vapid_public_key),
});

// Send the subscription to UMS
await umsClient.post('me/notifications/addresses', {
    channel: 'WEBPUSH',
    device_id: myDeviceId,
    device_name: 'Chrome on MacIntel',
    platform: 'WEB',
    webpush: {
        endpoint: subscription.endpoint,
        keys: { p256dh: ..., auth: ... },
    },
});
```

### 4. Send a notification (admin only)

```go
response, err := service.NotifyUser(ctx, &notifier.NotifyUserRequest{
    UserID:  "user-123",
    Title:   "New article published",
    Message: "Check out the latest changelog entry!",
    Data:    map[string]interface{}{"url": "/articles/latest"},
})
```

## Error Codes

| Code | Meaning | HTTP |
|------|---------|------|
| NTF00-001 | Database operation failed | 500 |
| NTF00-002 | Address payload is invalid | 400 |
| NTF00-003 | Channel is not supported | 400 |
| NTF00-004 | Address not found | 404 |
| NTF00-005 | Sender not enabled | 503 |
| NTF00-006 | No active addresses for user | 404 |
| NTF00-007 | Send failed | 500 |
| NTF00-008 | User ID is required | 400 |
| NTF00-009 | Preferences payload is invalid | 400 |

## Key Design Decisions

1. **Deduplication by hash** — SHA-256 of `channel:identity` in a unique
   MongoDB index. The same browser registering twice just updates the
   existing record.

2. **Sanitised output** — `RegisterAddress` and `ListUserAddresses` return
   summaries without endpoints/tokens. Only `GetActiveAddressesByUserID`
   (internal) returns full addresses for sending.

3. **Preference defaults** — New users default to "enabled for all channels."
   `GetPreferences` returns defaults when no document exists yet.

4. **FCM ready but disabled** — The FCM sender and mobile registration hooks
   are built. They stay disabled until Firebase credentials are provided.
