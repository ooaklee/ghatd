# @ghatd/browser-observability

Reusable, consent-scoped browser telemetry for GHATD host applications. Ships
TypeScript sources; the host bundles them. The core has no framework dependency.

## Install

```sh
npm install git+https://github.com/ooaklee/ghatd.git#<exact-commit-sha>
```

The package is not published to a registry and has no install/build hook. Pin a
full commit SHA. Axios, Vue and Vue Router are optional peers used only by their
respective subpaths. Import the controller lazily to keep the OpenTelemetry SDK
out of initial application code.

## Design invariants

- One private OpenTelemetry provider per consent generation. No global provider
  or context manager is installed.
- Revocation closes the export gate synchronously and discards queued and active
  spans before provider shutdown. Business requests are never cancelled.
- Consent callback failures deny telemetry. If `getExpiresAt` is supplied, it
  must return a finite future epoch timestamp in milliseconds; missing, invalid
  or expired values deny consent even when `isAllowed()` still returns true.
  Hosts without expiry semantics can supply only `isAllowed()`.
- All eligible spans are sampled. Active spans, duration, attributes and both
  SDK and exporter queues have hard bounds; this is not a delivery guarantee.
- Exports use a canonical absolute same-origin path without credentials,
  retries or redirects. Ambiguous paths fail configuration before transport.
- Route and API groups each contain at most 32 distinct values including the
  `other` fallback. Group values match `^[a-z][a-z0-9_-]{0,31}$`; unknown runtime
  values map to `other`. Outcomes and error sources use fixed vocabularies.
- Product route names, API prefixes, consent storage, identity and deployment
  policy remain in the host application. No URLs, query strings, raw errors or
  storage contents become span attributes.

## Usage

The group configuration is copied at construction. Use the same configuration
for the controller and request integration; pass the configuration object to
`createBrowserTelemetry`, and the resolver returned by `createGroups` to Axios.

```ts
import { browserTelemetry, createGroups, setBrowserTelemetry }
  from '@ghatd/browser-observability';
import type { GroupConfig } from '@ghatd/browser-observability/groups';
import { observeAxiosInstance } from '@ghatd/browser-observability/axios';
import { observeRouterWith } from '@ghatd/browser-observability/router';
import { createVueIntegration } from '@ghatd/browser-observability/vue';
import * as axiosModule from 'axios';

const intakePath = '/api/v1/telemetry/browser/traces';
const groupConfig: GroupConfig = {
  routes: { home: 'public', settings: 'account' },
  apis: [['/api/v1/ums', 'accounts'], ['/api/v1/billing', 'billing']],
  intakePath,
};
const groups = createGroups(groupConfig);

// Storage policy belongs to the host. It must notify on revoke/change.
const consent = {
  isAllowed: () => myConsentStore.analyticsAllowed(),
  getExpiresAt: () => myConsentStore.analyticsExpiresAtMs(),
};
const changes = {
  subscribe: (listener: () => void) => myConsentStore.onChange(listener),
};

const { createBrowserTelemetry } =
  await import('@ghatd/browser-observability/controller');
const telemetry = createBrowserTelemetry({
  consent,
  changes,
  serviceName: 'my-web-app',
  scope: 'example.com/my-web-app',
  groups: groupConfig,
  intakePath,
  framework: createVueIntegration(app),
});
setBrowserTelemetry(telemetry);

observeAxiosInstance(axiosModule, client, { telemetry: browserTelemetry, groups });
observeRouterWith(router, browserTelemetry);

// On application teardown:
setBrowserTelemetry(undefined);
await telemetry.stop();
```

The default `/api/v1/telemetry` subtree is always excluded from request
instrumentation. `intakePath` additionally excludes a custom intake subtree;
`telemetryPaths` can exclude more paths. The controller automatically includes
its own intake path. Independent request resolvers must use the same custom
intake setting. Paths must start with one `/` and contain no repeated slash,
encoded bytes, backslash, whitespace/control characters, query, fragment or
`.`/`..` segments. Request URLs outside same-origin `/api/` are not traced.

The optional Vue integration preserves the existing error handler arguments,
restores it only while it still owns the handler, and retains Vue's production
rethrow mode. Other frameworks can implement `BrowserFrameworkIntegration`:
`subscribeErrors(record)` returns a cleanup function; `postRender()` resolves
after pending rendering. Omit `framework` for window/rejection errors and a
microtask render tick.

The facade is an optional single-controller holder. Applications with multiple
controllers should provide separate getters to the Axios and router
integrations. The package never installs a singleton OpenTelemetry provider.

## Development

From the repository root:

```sh
npm test
npm run typecheck
```

Tests cover native span parentage, cancellation and expiry, bounded export,
finite vocabulary, request propagation, framework hooks and consent isolation.
