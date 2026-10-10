# Browser transport security

`browsersecurity` supplies principal-bound CSRF protection, explicitly verified
native bearer admission and bounded process-local rate limiting. It does not
authenticate a cookie, resolve accounts, grant capabilities or authorize a
financial action. The host resolves current identity before calling this
package; owning managers must still recheck session and action authority,
including when replaying a command.

## Host composition

Construct `Security` with an explicit valid `CSRFCookieName`, an exact origin
allow-list and a stable independent signing key of at least 32 bytes. Keys and
allow-lists are copied. `CSRFTTL` defaults to one hour and permits one minute to
24 hours. Construction performs no network/storage I/O and starts no goroutines.

```go
guard, err := browsersecurity.New(browsersecurity.Config{
    AllowedOrigins: []string{"https://host.example"},
    Key: csrfKey,
    CSRFCookieName: "__Host-host-csrf",
    CSRFTTL: time.Hour,
})
if err != nil {
    return err
}
// These values come from the live host session verifier, never request JSON.
identity, err := browsersecurity.NewIdentity(browsersecurity.IdentityConfig{
    BindingParts: []string{"session", actorID, sessionCredential},
    ActorID: actorID,
})
if err != nil {
    return err
}
```

Include a stable purpose/kind in `BindingParts`. Order matters, and changing it
invalidates previously issued cookies. At most 16 parts, 8 KiB per part and
64 KiB total are accepted. The constructor immediately hashes JSON string-array
encoding with SHA-256; raw credentials are neither retained nor put in cookies.
`Identity` has private fields and its zero value fails closed. It remains the
host's responsibility to construct it from trustworthy inputs. `ActorID` is a
verified member's stable rate identity only; it does not choose a command actor
or selected account. `Public: true` creates an anonymous nonce binding and
cannot be combined with member/native inputs.

Origins must be HTTP(S), have a hostname, and contain no wildcard, userinfo,
query (including an empty `?`), fragment or non-root path. A trailing `/` is
normalized to the origin. HTTP is loopback-only. `InsecureLocalCookies` is an
explicit loopback-only development exception; names beginning `__Host-` or
`__Secure-` are refused in this mode. Production cookies use Secure, HttpOnly,
SameSite Strict, path `/` and no Domain. Prefer a `__Host-` name in production.
Do not infer public HTTPS from `r.TLS`: hosts may terminate TLS at a trusted
proxy. This package does not configure that proxy or CORS.

## Request flow

After resolving the principal, call `Issue` on a GET response to set
`X-CSRF-Token` and, when needed, the signed CSRF cookie. Do not cache the response.
Host CORS must use the same exact origins, allow this header on requests and
expose it on responses. GETs with Authorization, a foreign/ambiguous Origin, or
cross-site/invalid/duplicate `Sec-Fetch-Site` receive no token or cookie.
Ordinary GETs without fetch metadata remain supported.

Call `Verify` before unsafe browser operations. It requires one allowed Origin
(or, when absent, one Referer with an allowed origin), exactly one valid signed
cookie bound to the current principal, and exactly one matching token header.
Duplicate cookies and security headers fail closed. A changed/revoked session
requires current host admission and a new binding; cookie possession alone
cannot establish authority.

GET rebinding preserves a valid cookie's nonce and **absolute expiry**. It does
not renew the CSRF lifetime. `PublicIdentity` derives a pseudonymous identity
from that nonce, or the immediate peer address if no valid cookie exists; it is
not a verified person. `RateIdentity` uses verified member IDs across session
rotation, immediate peer addresses for public traffic, and opaque binding for
other authenticated traffic. It ignores forwarded-IP headers. Hosts requiring
trusted proxy or shared admission must provide that boundary separately.

For native requests, supply `NativeAuthenticated: true` and `NativeClientID`
only after the host verifies the token, current session, accepted principal kind
and client audience. `Native` checks the configured audience allow-list, one
printable `Bearer` header (token at most 8 KiB, no commas/spaces), and **no Cookie
header at all**, including malformed or empty headers. Client-submitted names
and unverified claims cannot populate this identity. Native transport admission
does not authorize any domain action. A host may leave the allow-list empty to
disable this mode.

## Errors and rate limiting

Safe sentinels are `ErrConfigurationInvalid`, `ErrAuthenticationRequired`,
`ErrForbidden` and `ErrInvalidRequest`. Map them to the transport's documented
codes/statuses and return generic messages; do not serialize raw dependency
diagnostics. `UniqueCookie` is only an ambiguity check, not a session verifier.

`NewWindowLimiter` accepts 1 to 10,000 admissions per window and windows from one
second to 24 hours. It bounds state to 10,000 identities and keys to 512 bytes,
serializes concurrent admission and cleans expired buckets when capacity is
reached. A full live identity table refuses new identities for the window.
Cancellation/nil contexts fail instead of admitting work. This limiter is local
to one process, resets on restart and does not establish a cluster-wide quota.
Inject a different `Limiter` for distributed enforcement. A positive retry
duration means refuse admission; `RetrySeconds` rounds it up without overflow
for a `Retry-After` header.

## Compatibility and verification

The signed cookie uses HMAC-SHA256 over `cookie\n<base64url-json>`; the response
token uses `token\n<nonce>\n<binding>`. Retain the key, cookie name and exact
binding order when adopting without rotating browser cookies. The
[`testdata/legacy-v1.json`](testdata/legacy-v1.json) fixture pins the cookie/token
and pseudonymous identity format using dummy inputs.
Guest/draft session cookies and raw-token-to-cookie response transformation are
host-specific concerns and remain outside this package.

Run `go test -race ./external/http/browsersecurity`. Table tests cover valid and
rejected configuration, browser proof/session changes, issuance provenance,
native admission, wire-contract vectors, expiry/rebinding, key/config ownership,
identity bounds and concurrent/capacity recovery. The single construction
ownership sequence verifies one dependent lifetime contract. Hosts must also
test their principal mapping, error projection, session exchange and HTTP/CORS
assembly; shared tests do not establish those integrations.
