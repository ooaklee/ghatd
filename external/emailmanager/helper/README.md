# Email manager composition

`BuildServices(Config)` constructs resolved Bird/Postmark/local accounts for the
owning [EmailManager](../README.md). Hosts choose capture policy, load secrets,
resolve per-account defaults and supply a borrowed instrumented HTTP client.
The helper reads no environment or files and makes no network request.

```go
services, err := emailmanagerhelper.BuildServices(emailmanagerhelper.Config{
    CaptureLocally: captureLocally,
    MaxStoredEmails: 100,
    HTTPClient: client,
    Providers: []emailmanagerhelper.ProviderConfig{
        {ID: "transactional", Vendor: "bird", Token: resolvedBirdKey},
        {ID: "broadcast", Vendor: "postmark", Token: resolvedPostmarkToken,
            MarketingStream: "broadcast"},
    },
    Routes: map[emailprovider.MailType]string{
        emailprovider.Transactional: "transactional",
        emailprovider.Marketing: "broadcast",
    },
})
if err != nil { return err }
// Pass services.Default and services.Routing to NewStandardEmailManager,
// together with the host's template, audit, send-enable and sender configuration.
```

The helper validates vendor construction and a maximum of 16 accounts. Empty
accounts select local capture or the explicit `FallbackBird` account; there is
no implicit credential discovery. Registry-free fallback ignores route/default-ID
settings because no named account routing is enabled. Hosts distinguish an absent registry from an
invalid empty serialized registry when decoding their own configuration.
Postmark stream names must be resolved before construction; an empty transactional
stream uses its owning default. Neither stream syntax nor a valid key proves
provider access/readiness. Credentials and raw configuration must never be logged.

Final account-ID, route, capability and preference validation belongs to
`NewStandardEmailManager`: complete that construction before admitting requests.
A default provider ID applies to transactional sends only when an explicit route
or preference has not selected an account. `Services.Default` is the first
preference-decorated registered provider; `Routing` remains the actual selection
policy. Input routes/preferences are copied rather than retained for mutation.

With `CaptureLocally=true`, one inbox is shared by interception and `local`
registrations. Inert tokens replace remote credentials; Bird's remote origin
assertion is ignored so local capture works with either production region.
Logging accounts support transactional and marketing purposes. Account identity
in receipts/inbox rows still names the selected route. Use the resulting
EmailManager for interception: direct provider handles are lower-level ports.
Capture does not grant marketing consent or create a remote audience. No automatic
send fallback, retries, readiness lookup, worker or listener is added.

Vendor constructors copy the borrowed client's policy, disable cookies and
redirects, and retain its caller-owned transport. This helper never closes that
transport. Hosts choose its lifetime and ensure it does not replay sends.
Construction errors return empty services and `ErrConfiguration` without raw
vendor diagnostics; manager routing failures retain their owning classification.

Run `go test -race ./external/emailmanager/... -count=1` for construction boundaries,
client/config ownership, local receipts/account isolation and synthetic vendor
requests. These controlled fixtures do not prove production delivery.
