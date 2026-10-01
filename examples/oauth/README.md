# Compose provider sign-in in your host

[setup.go](setup.go) is a compile-checked example of the existing public GHATD
APIs. Copy/adapt it into your host; it is not a standalone server or a new
configuration API. Follow the [adoption guide](../../docs/how-to/add-google-apple-sign-in.md)
for provider registrations, indexes, proxy/cookie setup and client flows.

Supply already-populated `starter.NewServicesRequest` and
`starter.NewHandlersRequest` values from your application (repositories, existing
email/ephemeral/auth dependencies, signing secrets, validator and cookie policy).
`Compose` adds secure providers, creates the services and handlers, then configures
optional mobile handoff. It returns both containers: use the services with
`starter.NewMiddleware`, keep cookie settings consistent with the handlers, then
attach the handlers and middleware with your normal starter routes.
Hosts not using starter can pass the providers to
`accessmanager.NewServiceRequest.OauthServices` and configure their handler directly.

The host owns Redis lifecycle, environment parsing and key loading:

- Map [.env.example](.env.example) through your own configuration loader. GHATD
  does not read these variable names automatically. Bedrock uses
  `FRONTEND_BASE_URL` for the example's `OAUTH_ORIGIN` concept.
- All-empty provider fields map to `nil`. A partial configuration must not be
  silently converted to `nil`: fail startup. A supplied request with missing
  credentials is rejected by the secure constructor.
- Google input supplies `ClientID`, `ClientSecret` and optionally `HTTPClient`.
  Apple input supplies `ClientID` (Services ID), `TeamID`, `KeyID`,
  `PrivateKeyPEM` and optionally `HTTPClient`. Read the PEM bytes through your
  secret store or a narrowly mounted file outside Git.
- The example copies each provider request and supplies its `Store` and
  `RedirectURL`. It derives callbacks from one exact public origin, keeping
  Google's and Apple's callbacks on the native handoff's origin. Register
  those complete URLs in the provider consoles; deriving them does not register them.
- Use a distinct Redis namespace per service/environment. Keep
  `mobileRedirects` empty for web-only hosts; otherwise pass an exact private
  app callback such as `com.example.yourapp:/oauth/callback`. GHATD validates the
  HTTPS origin and native callback format before the listener starts.

Apply the OAuth identity migration before enabling providers. This example does
not provision databases, register providers, run migrations, load `.env`, read
secrets, or start listeners. Existing email authentication remains host-configured.

Check the example against the framework's current API without provider calls,
using the Go version pinned in the repository's `.tool-versions`:

```sh
go test ./examples/oauth
go vet ./examples/oauth
```

For asdf-managed toolchains, prefix these commands with `asdf exec`.

Live validation still needs your own provider registrations and user consent.
