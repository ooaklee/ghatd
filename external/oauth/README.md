# Provider sign-in

Start with [Add Google and Apple sign-in](../../docs/how-to/add-google-apple-sign-in.md)
for registrations, migrations, browser/mobile integration, ngrok and Apple relay
setup. The [composition example](../../examples/oauth/README.md) and its
[environment placeholders](../../examples/oauth/.env.example) show the existing
APIs without depending on Bedrock. This README is the provider API/security guide.

`NewGoogleSecureProvider` and `NewAppleProvider` implement the optional
`ContextualFlowProvider` capability. Supply a private HTTP client (or use the
traced, 30-second default), a `SecureTransactionStore`, and complete provider
configuration. Keep client secrets and Apple's PKCS#8 P-256 `.p8` key in the
host's secret store, outside source control. Apple uses the web Services ID as
its client ID, with the associated team ID and Sign in with Apple key ID.
`PrivateKeyPEM` accepts decoded PEM bytes. A host may decode an optional base64
secret in memory rather than mounting a file; the
[compile-checked adapter](../../examples/oauth/apple_key.go) demonstrates strict
precedence and redacted errors. The framework does not load environment
variables, create key files or configure Kubernetes volumes.

Google requests `openid email profile`, nonce and S256 PKCE. Apple requests
`name email` with `response_mode=form_post`; its client secret is signed with
ES256 for each exchange and expires after five minutes. Both providers verify
RS256 ID tokens against bounded, cached provider JWKS, then check issuer,
audience, expiry, subject and the server-held nonce. Google's documented
`accounts.google.com` issuer alias and multi-audience `azp` are handled. Token
time claims allow 30 seconds of clock skew; state transactions do not.
Unknown signing keys share a throttled refresh, including failed refreshes.
Keep host clocks synchronised. A rotation within the 30-second refresh window
can briefly reject a new key; retry starts a fresh transaction.

`NewRedisTransactionStore(client, namespace)` uses Redis SET NX with a bounded
TTL and an atomic Lua GET/DEL consumption. State, nonce, PKCE verifier, validated
return path and authenticated linking proof remain server-side. The browser
cookie carries only a 256-bit opaque state handle. Use an application and
environment namespace when sharing Redis. Match the cookie to the single
callback state before consuming; duplicate or ambiguous callback parameters
fail closed. Transactions are single-use, including cancellation and failed
code/token exchanges. Client-supplied nonce or PKCE verifier is ignored.

Apple's `user` object is optional first-authorisation profile data. Its email
is ignored; only the signed email claim is eligible for new account creation.
Repeat Apple sign-in can omit email and names entirely when the signed
issuer/subject already belongs to a known account. Names remain optional for
provider accounts. Relay email is supported without email-based account
linking.

The access manager requires the secure capability and identity-aware user
persistence. The legacy `NewGoogleProvider` and `OauthService` interface remain
source-compatible, but access-manager login/callback no longer fall back to
the legacy email-only flow. Migrate hosts to the secure constructor and apply
the user identity indexes before enabling a provider.

Run meaningful provider and real-store checks with:

```sh
GHATD_TEST_REDIS_ADDR=127.0.0.1:6379 go test -race ./external/oauth
GHATD_TEST_MONGO_URI=mongodb://127.0.0.1:27017 \
GHATD_TEST_REDIS_ADDR=127.0.0.1:6379 \
go test -race ./external/user/v2 ./external/accessmanager
```

Tests use isolated MongoDB databases and random Redis handles. The service
lifecycle tests simulate provider HTTPS responses with real signed JWTs;
live Google/Apple account testing still requires registered callback URLs,
credentials and browser consent.
