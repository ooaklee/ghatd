# Request-local proof admission

`accessproof` evaluates trusted, credential-free evidence against compiled
admission requirements. It supports conditional rules such as **a verified
member OR a current guest capability bound to this document**, without giving
the guest a member account or borrowing another user's permissions.

This package is opt-in. It does not verify JWTs, read cookies, persist grants,
perform resource ownership queries, or meter usage. Proof categories are
request-local facts, not persisted administrative grants or account roles.

## Usage and trust boundary

Compile once during startup and propagate configuration errors:

```go
policy, err := accessproof.Compile(
    accessproof.Requirement{
        Kind: "member", Assurances: []string{"verified-account"},
    },
    accessproof.Requirement{
        Kind: "guest", Capabilities: []string{"document.read"},
        ResourceKind: "document", RequireExpiry: true,
    },
)
if err != nil {
    return err
}
```

For **each request**, a trusted host resolver authenticates the selected proof,
checks its source, current revocation, deployment binding and validity, and
produces `Evidence`. Never decode evidence from request JSON, trust unsigned
claims, or treat a nonempty bearer string as a resolved identity. Use the real
source principal ID, capabilities and binding. Do not copy the requested
resource into the evidence to manufacture permission.

Pass the independently validated request target and a nonzero trusted clock:

```go
err = policy.Authorize(evidence, accessproof.Resource{
    Kind: "document", ID: documentID,
}, now)
```

The host maps `ErrDenied` through its reviewed response/error manifest. No
evidence or private failed constraint is included in the error. A denial must
stop dispatch. A successful admission is **not** a completed domain authority
check: recheck live resource relationships and revocation inside the command's
transaction, including before returning an idempotent replay.

## Exact semantics

- Alternatives are OR; requirements within an alternative are AND, all from
  **one** evidence value. The evaluator cannot union facts from different proofs
  or identities. Capability and assurance names are exact, case-sensitive and
  have no wildcard, role-ranking or assurance-inheritance semantics.
- `ResourceKind` requires an exact match of both resource namespace and ID.
  Omitting it imposes no resource check; the domain must still verify ownership
  or participation. An unbound proof never satisfies a bound requirement.
- `RequireExpiry` rejects an omitted source deadline. Every supplied deadline
  is exclusive: equality with `now` is expired. A branch that does not require
  expiry may accept a resolver that exposes no deadline, **only after that
  resolver independently verified current validity for this request**. Never
  invent a future deadline to compensate for missing proof validation.
- Empty/zero policies deny. Public routes need an explicit anonymous branch in
  the host; an empty list is not an anonymous allow rule. Unknown categories
  cannot match a configured alternative.
- Compilation copies mutable slices. The resulting policy supports concurrent
  reads. Evidence remains caller-owned and must not be mutated during a call;
  do not retain it as a session or cross-request authorization cache.
- Policy names are bounded to 128 visible ASCII bytes without whitespace or
  `*`/`?`; subject/resource IDs to 512 visible ASCII bytes. At most 32 alternatives
  and 128 distinct assurances/capabilities per list are supported. Malformed or
  duplicate facts fail closed rather than being silently normalized.

## Handler-owned routes and combined proofs

Use this evaluator inside the actual proof-owning handler registered with
[`router.HandlerVerified`](../router/README.md#proof-owned-application-flows).
The outer guard does not verify these identities. The route's `Policy.Proof`
name remains documentation, not an automatic invocation of this package.
Preserve the host's proof → browser-CSRF → command validation → transactional
domain checks; do not move a post-proof requirement into an earlier guard.

Combined identity transitions need a domain-specific protocol. For example, a
first account-linking command can require a verified member plus a current,
resource-bound guest ticket, consume that ticket and revoke the guest session
atomically. Its completed retry may then be authorized by the linked member and
stored receipt, even though the original guest credential is revoked. Do not
blindly require a fresh secondary guest proof before every replay. A secondary
credential's presence is input, not a verified capability; this evaluator never
converts it into one. A new command must still pass the domain's ticket rules.

No token format, grant migration, quota identity, public-registration setting,
or storage schema change is required by the evaluator itself. Adopting hosts
must supply their live resolver and explicitly review those separate concerns.

## Validation

```sh
asdf exec go test -race ./external/accessproof
```

Table-driven tests cover configuration rejection, exact alternatives and
bindings, no partial-branch merging, missing/expired deadlines, immutable
configuration and redacted denials. Host integration tests must additionally
exercise real resolution, command/replay revocation and error mapping.
