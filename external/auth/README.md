# JWT identity and credential context

`auth.Service` signs and verifies HS256 access, refresh, initial-login and
email-verification credentials. It provides signed metadata, not a complete
authorization system. Live session records, account state, current roles and
resource permissions must still be checked by the caller.

## Typed claims

`Claims` embeds JWT registered claims and adds `user_type`, `token_use`,
`email_revision` and optional `auth_time`. `AccessClaims` retains `access_uuid`,
`admin` and `authorized`; `RefreshClaims` retains `refresh_uuid`. The registered
`jti` equals the corresponding record UUID. `sub` remains the immutable user ID.

The optional `UserTypeProvider.GetUserType()` capability supplies the account's
stored classification. `user/v2.UniversalUser` returns its persisted `Type`;
its separate `GetType()` method still returns the resource kind `USER`. Existing
custom `UserModel` implementations need not add the optional method. Absent types
remain absent. Non-empty types must be valid UTF-8, at most 128 bytes, and contain
no whitespace or control characters. Types are neither roles nor permissions.

New credentials distinguish `access`, `refresh`, `login` and
`email_verification` purposes. Access-family extraction supports the first,
third and fourth kinds; call `IsSessionCredential()` before treating extracted
metadata as a session. Refresh extraction permits only refresh or legacy absent
purpose. Metadata validation does not prove that a live-store record exists.

Claims are signed, not encrypted. Never copy arbitrary request claims, private
user extensions, passwords or credentials into them. Read verification errors
through the package error manifest rather than returning parser diagnostics.

## Issuer and audience

Configure `NewServiceRequest.Issuer` and `Audience` explicitly when a service must
accept only its intended issuer and recipients. New tokens carry those values;
verification requires the configured issuer and at least one exact configured
audience match. Audience configuration is copied at construction. It is not an
all-audiences policy; hosts needing narrower recipients should use separate
service configurations or apply a stricter policy after verification.

Use distinct, cryptographically random access and refresh secrets of at least
32 bytes from a host-owned secret source. Callers must pass a non-nil constructor
request; passing nil panics. It does not provision keys or validate deployment
configuration.

Prefer `ExtractAccessTokenMetadataByString` and
`ExtractRefreshTokenMetadataByString` for untrusted input. The lower-level
`CheckAccessTokenValidityGetDetails` and `GetRefreshTokenUUID` accept a token
already verified by this service; a fabricated `jwt.Token{Valid: true}` is not
proof of signature verification.

`AuthenticationTime` is populated only from signed `auth_time`. `iat` must not
be treated as a recent login: refresh changes issuance time but should preserve
the original authentication time through `CreateTokenWithAuthenticationTime`.
The plain `CreateToken` method does not invent that freshness evidence.

## Deletion-only verification

`SessionRemovalVerifier.ExtractSessionRemovalMetadata(ctx, token, tokenUse)`
returns only the signed owner, record ID and selected access/refresh purpose.
It is a cleanup capability, **not** an authenticated-session result. Only a
well-formed elapsed expiry is relaxed: HS256, signature, required expiry,
configured issuer/audience, future `iat`/`nbf`, purpose and identity checks still
apply. Login/email proofs and malformed present claims are rejected. Legacy
absent purpose retains the ordinary parser's compatibility rule.

No account lookup, token issuance, refresh or request-context publication occurs.
The caller must restrict this result to deletion of the identified record. It
does not establish a live session or a relationship to another credential. Use
ordinary extraction plus live checks for every admission or management decision;
those parsers continue to reject expired credentials.

## Compatibility and rollout

- **Breaking:** verification accepts HS256 only, requires `exp` and rejects
  future `iat`. Previously accepted HS384/HS512 or no-expiry credentials must be
  replaced; review custom issuers before rollout.
- Empty subjects and access/refresh record identifiers are rejected during
  metadata extraction. Invalid stored type strings fail issuance instead of
  being silently rewritten by JSON encoding.
- Enabling issuer/audience constraints rejects older credentials lacking the
  configured values. Plan session rollover when opting in. Empty configuration
  retains unbound legacy compatibility; it does not establish external trust.
- Legacy absent `user_type`, `token_use`, `jti` and `auth_time` remain supported;
  malformed present identity values are not treated as missing. Missing context
  does not establish a type, fresh login or broader permission.
- `MatchesUserType` compares an asserted type against a current trusted account;
  it intentionally permits an absent legacy type. It does not fetch that account
  or enforce session revocation. A host needing mandatory types must explicitly
  reject missing values.

This package does not automatically add transport middleware, policy storage,
third-party token exchange or route enforcement. Those integration boundaries
remain explicit. See the [router contract](../router/README.md) and
[user model](../user/v2/README.md) for related primitives.
