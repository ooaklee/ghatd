# Error manifests

`errormanifest` composes the maps consumed by `reply/v2`. Package handlers own
their base maps and built-in dependency maps; hosts supply additional maps and
explicit overrides. Later
entries for the **same error identity** win. Matching error messages do not make
two distinct errors interchangeable.

Hosts adopting the integration should pin a reviewed module revision and remove
development-only replacement directives before publishing; see
[ADR021's adoption guidance](../../docs/adr/adr021-shared-observability-adapters.md#compatibility-and-consequences).

```go
manifests := errormanifest.NewComposer().
    Add(PackageErrorMap).
    AddOverrides(hostErrorMaps...).
    Build()
replier := reply.NewReplier(manifests)
```

`Composer.Duplicates()` is an optional diagnostic for duplicate error messages;
it does not change runtime matching. Compose a fresh slice for endpoint-specific
overrides instead of mutating a shared bundle during a request.

## Wrapped errors at HTTP boundaries

Every expected domain failure needs a manifest entry. Compose dependency maps
at the manager/host boundary and translate native validation or storage outcomes
into the appropriate domain classification. A generic fallback is not a
substitute for those mappings. Keep original causes available for internal
inspection without putting their text in public manifest fields.

`WriteHTTPError` resolves wrapped errors and joined validation collections
structurally; classification never depends on error text or newlines. Use the
shared writer for domain HTTP responses:

```go
return errormanifest.WriteHTTPError(w, err, manifests,
    reply.WithHeaders(map[string]string{"X-Request-ID": requestID}),
)
```

`WriteHTTPError` pins its error-only contract and sends the roots to reply's
explicit multi-error input. `ResponseErrors` delegates to `Replier.ResolveErrors`
for callers needing canonical identities without HTTP output. Neither classifies
errors by their messages or by newlines:

- Wrapped errors retain the mapped status, code and host override.
- All-mapped validation joins retain every distinct key in first-seen order.
  Singleton joins work too. An unknown independent branch makes the entire
  response a generic 500, rather than silently disappearing.
- An exactly registered error node, or a custom `Is` method matching exactly
  one registered key, explicitly classifies its own underlying cause. That
  cause remains available through `Unwrap` to internal callers, but is not an
  additional public failure. A sibling joined failure must still resolve.
  Custom `Is` implementations must compare their own classification, not search
  other joined branches; custom `Is` and `Unwrap` methods must terminate.
- The traversal permits at most 64 nodes in total. Nil/typed-nil errors, empty
  or malformed joins, unknown leaves, ambiguous classifications and excessive
  depth/breadth fail closed with one opaque error. A nil error supplied to this
  failure-only writer does **not** produce a successful response.
- Reply retains its 5xx dominance: a mapped server error replaces the client
  validation collection. Otherwise the first resolved key determines the HTTP
  status, defaulting to 400 if its manifest omits a status. Deliberate host
  overrides such as 202 remain supported.
- Each call owns a fresh replier. Attributes may add headers and metadata, but
  cannot replace the writer/errors or inject data/tokens. The manifest controls
  status. Encoding and writer failures are returned without retrying a response.

The original error is never passed to reply's fallback logger. Manifest fields,
headers and metadata must themselves be safe to publish. These helpers do not
sanitize earlier service/repository logging or metadata, and shared manifest
maps must not be mutated during requests.
The domain handlers listed below log resolved identities; manifest keys must
therefore have safe, stable `Error()` text too. Register domain sentinels, not
per-request diagnostics containing credentials or personal information. Access
Manager's response sanitization does not redact its existing authentication-path
log statements; review those separately.

The blueprint and billing, contact, content, group, policy, pricing, sitemap,
user, user-manager and vision handlers use this writer through
`Handler.NewHTTPErrorResponse`. Existing public `GetBaseResponseHandler`
signatures and success responses are unchanged. Direct calls to reply from
custom handlers do not automatically acquire these guarantees. Hosts with a
custom reply transfer object can use `ResponseErrors` with their own replier.

## Strict single-cause authentication boundaries

`CanonicalError` is a conservative resolver for authentication/policy boundaries
that require single-cause errors. It rejects joins rather than formatting a
validation collection:

```go
publicErr := errormanifest.CanonicalError(err, manifests)
return replier.NewHTTPErrorResponse(w, publicErr)
```

- One unambiguous manifest key in a single-cause chain retains its configured
  status, code and host override. Resolution uses `errors.Is`, never error text.
- Unknown, multi-cause, ambiguous, typed-nil or excessively deep chains become an opaque,
  unmapped error. Reply emits its generic server response rather than choosing a
  potentially misleading client error. This also keeps original diagnostics
  out of reply's fallback logger and handles uncomparable dynamic error values.
- `nil` remains `nil`; callers should only write an error response when they
  actually have an error.
- Keep the original error for internal retry/cause inspection. Redact any
  application-owned diagnostics separately; this helper does not sanitize
  earlier logging, metadata or custom manifest fields.

Typed-nil nodes are rejected before calling their `Is` or `Unwrap` methods;
typed-nil manifest keys are ignored. Non-nil custom methods must terminate and
be safe to call. The helper does not recover arbitrary adapter panics.

Unlike `ResponseErrors`, it checks the complete single-cause chain before
matching and rejects multi-cause chains even under a registered wrapper. If both
a wrapper and its inner sentinel are registered, matching both distinct keys is
ambiguous: the strict resolver does not prefer the exact wrapper. Register one
public classification per authentication failure chain. Do not
replace an authorization decision with response resolution: neither helper
authenticates a caller or grants access.

Access Manager's `Handler.NewHTTPErrorResponse` uses this resolver and includes
its own and built-in dependency manifests by default. Custom Access Manager
service adapters must classify validation failures as one reviewed domain error;
even all-mapped joins are rejected with a generic 500 at this strict boundary.
Built-in request mappers translate invalid fields to a single mapped client
error before response resolution. Existing authentication middleware is a
separate boundary; handler adoption does not change its credential decisions.
The declarative router uses the same resolver, then explicitly maps unresolved
authorization failures to its `ROUTE_UNAVAILABLE` 503 contract rather than the
generic fallback. A known denial never masks an independent joined failure.

## Coverage and migration checks

Keep table-driven coverage for direct errors, wrappers, validation joins, host
overrides, native-to-domain translation and unknown failures. The package tests
check declared `errors.New` sentinels in the eleven migrated packages and runtime
responses for their manifests. OAuth user-domain errors are explicitly checked
in Access Manager's map. This scoped check is not a whole-framework audit of
every dynamic error or dependency path.

Access, User, Content and Billing Manager handlers always include their
`DependencyErrorMaps()` inventories, even without a host-supplied bundle. The
legacy `bundles` functions delegate to these manager-owned factories. Ordering is
the manager's own map, dependency maps in historical bundle order, then host
overrides. Known lower-domain errors therefore no longer depend on the host
remembering the manager's collaborators. Adding a new collaborator still requires
updating the owning inventory; custom application errors need explicit mappings.

`CloneManifests` copies slices and map entries, preserving order and nil maps.
Referenced metadata is not deep-copied: treat it as immutable or replace it with
an independently owned value before editing. `Composer.Build` creates a slice
but does not clone entries. Do not mutate configuration while serving requests.

Reply now keeps response state request-local and supports `reply.WithContext`
and an opt-in unmapped-error observer. Custom transfer objects must return
independent state; manifest metadata and observers must be concurrency-safe.
GHATD's writer still allocates a response-local replier for caller-specific
manifests. See reply's [integration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md)
for behavior changes and immutable commit pinning. A dependency's local `replace`
is not inherited by host applications.
