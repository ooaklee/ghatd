# Error manifests

`errormanifest` composes the maps consumed by `reply/v2`. Handlers and hosts
explicitly compose their required base and dependency maps. Later entries for the **same error identity** win. Matching error messages do not make
two distinct errors interchangeable.

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

Direct mapped sentinels work in the pinned `reply/v2` integration version.
Ordinary `%w` wrappers were not resolved consistently by `v2.0.0`, and its joined-error handling
could discard unknown branches. GHATD now pins the reply integration branch's
structural resolver for validation collections. Use the shared writer for domain
HTTP responses:

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
Manifest keys must have safe, stable `Error()` text. Register domain sentinels,
not per-request diagnostics containing credentials or personal information.
Adoption is explicit: existing or custom handlers must call the shared writer
or use `ResponseErrors` with their own replier. Adding this package does not
automatically migrate handler response paths or dependency maps.

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

## Coverage and adoption

Table-driven tests cover direct and wrapped errors, validation joins, host
overrides, unknown and malformed causes, bounded traversal, diagnostic privacy,
response isolation, and encoding/writer failures. They do not certify every
framework handler or application error map.

For each adopting boundary, compose all expected dependency maps, retain the
original error for internal inspection, and test its actual HTTP response. Use
`CanonicalError` only when the boundary intentionally rejects multi-cause errors;
use `WriteHTTPError` for mapped validation collections. Neither grants access.
