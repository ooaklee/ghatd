# HTTP composition

`serverhttp` provides startup composition shared across hosts. It reads no
environment variables, starts no telemetry SDK and owns no connection lifetime.

## Outer handler

`NewHandler(HandlerConfig, next)` places telemetry outside request logging and
panic recovery. Install it once around the complete router, outside route caches,
so matched requests, misses, method errors and recovered panics share one server
span, metric observation and completion record.

The host supplies `observability.HTTPServerOptionsConfig` for redaction, proxy
trust, parameter logging and trace noise policy. No path is inferred from the
service name. `IgnoreCompletionPaths` suppresses only selected completion logs;
request context, metrics, application logs and recovery remain active. Inputs are
snapshotted during construction. Trace suppression needs the owning
`HTTPTraceSampler`, which `observability.StartRuntime` configures.

Response caching/compression/minification already belongs to
[`middleware/helper`](../../middleware/helper/README.md). Use its
`NewResponseMinifier` and `NewResponseMiddlewares` directly with host cache
exclusions. Browser trace intake likewise stays with
[`observability`](../../observability/README.md); hosts choose labels and allowed
origins explicitly.

## CORS and handle preflight

`CORS(CORSConfig)` selects Partners by its exact base path or directory boundary,
and optionally selects the shared handle paths. Other paths use the inherited
CORS middleware. Partners exposes revision, replay, retry and CSRF headers; handle
routes expose ETag. Credentialed family origins must be exact HTTP(S) origins,
without wildcards, userinfo, paths, queries or fragments. Empty lists deny those
cross-origin requests. Hosts migrating a wildcard configuration for Partners or
handle routes must list their exact frontend origins before adopting this helper.
Ordinary routes keep their separately configured inherited policy. Untrusted origins receive no CORS permission headers;
CORS is not authentication and does not promise a particular denial status.

`PartnersMethods: nil` uses GET, POST, PATCH, PUT, DELETE and OPTIONS. An explicit
slice can narrow this vocabulary without enabling new methods. A non-nil empty
slice denies every preflight method. Partners responses and preflight responses
carry `Cache-Control: no-store`. The host still excludes these private routes
from its response cache before the cache is entered.

`AttachHandlePreflight(routes, operationPrefix)` attaches only public OPTIONS for
the two owning user-handle endpoints. The stable operation prefix is host input,
so an existing inventory identifier can be retained. Authentication and mutation
methods remain with User Manager. Abort startup on route validation errors.
Product route families can select their own middleware outside this shared chain.

## Local email review

`AttachLocalEmailReview` requires the exact `local` environment and a manager whose
output is locally captured. Build that manager with
[`emailmanager/helper`](../../emailmanager/helper/README.md), then optionally attach
the owning inbox through `emailprovider.AttachLocalInboxRoutes`.

The review route is `/_ghatd/local/emails/review`. Loopback peers can access it by
default. `AllowPrivateNetwork` explicitly permits directly connected private IPs
for local container/proxy workflows; it defaults to false. Forwarded headers never
grant access. POST also requires a loopback HTTP
origin matching the request host or configured frontend origin. The bounded form
selects one of five fixed message processes. Recipients, content, credentials and
provider IDs cannot be supplied by the browser. No rendered message or credential
is logged. Construction rejects a manager connected to remote output even when
the environment flag says local.

## Validation

`go test ./external/serverhelpers/http` exercises telemetry cardinality, sampled
parent decisions, no-secret telemetry assertions, cache hit/miss preservation,
proxy trust, CORS boundaries and local email capture/denial. It makes no remote
provider requests. Sampling tests temporarily install in-memory global telemetry
providers and deliberately do not run in parallel.
