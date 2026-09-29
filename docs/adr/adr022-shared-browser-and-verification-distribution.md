---
id: adrs-adr022
title: 'ADR022: Share Browser Telemetry and Observability Verification'
date: 2026-09-29
status: accepted
---

Related: [ADR021](adr021-shared-observability-adapters.md)

## Context

Host applications were carrying substantially identical browser controllers,
exporters, HTTP adapters and infrastructure regression suites. Copying fixes
between services made privacy and lifecycle behavior difficult to keep aligned.
Product route names, consent storage and deployment topology still differ.

## Decision

GHATD owns a browser package and a profile-driven verification command alongside
its Go adapters. Shared tests live with those implementations. Host applications
consume a published exact commit and retain policy and deployment assets.

The root npm package, `@ghatd/browser-observability`, exports TypeScript sources
from `browser/observability`. Hosts need a TypeScript-capable bundler. The
package is installed from the Git repository at a full commit SHA; no registry
release or installation-time build is required. Pin the Go module and browser
package to the same reviewed revision and commit their dependency locks.

The facade and group vocabulary remain independent of the SDK. Applications
load the controller lazily behind their build flag. Axios, router and Vue
integrations are separate subpath imports with optional peers; applications
import only the integrations they use. Framework hooks belong at the boundary
so the controller does not require a particular UI runtime.

Each controller owns private providers for each consent generation. Hosts
supply consent decisions, optional expiry and change notifications; GHATD does
not read a particular cookie or storage schema. Revoke and expiry close the
export gate synchronously before shutdown. Pending telemetry is discarded;
business requests retain their normal cancellation ownership. The exporter
accepts only a canonical same-origin intake path, rejects redirects, omits
credentials and has bounded queues, batch sizes and transport deadlines.

Hosts supply finite route/API groups; raw URLs, identifiers and exception text
are not span attributes. Both browser vocabulary and server intake admission
must be updated together. The host's consent integration and group contract
remain service tests rather than being absorbed into generic fixtures.

Verification suites are distributed with their runner and synthetic fixtures.
A host profile describes local asset paths, identity, process roles, metric
names and dashboard panel IDs. Shared assertions exercise shipped queries and
rendered routing, rather than relying on screenshots or duplicated snapshots.
The runner uses local tools and pinned fixture containers; it does not deploy
resources. Profile validation precedes tool execution. Required checks must
fail visibly when configuration or tools are missing.

## Consequences

- Shared fixes can propagate through a dependency update; hosts should delete
  obsolete copies rather than retain forwarding wrappers.
- Applications still own consent UX, product vocabulary, feature instrumentation,
  exporter destinations, secrets, dashboards and deployment configuration.
- Exact dependency pins make adoption reproducible. Temporary local replacements
  support development but must be removed before publishing host updates.
- CI must run the browser tests/typecheck and Go adapter tests, plus the host's
  real profile and integration checks. Upstream unit tests do not prove a
  particular application's chart or consent wiring.
- Copied deployment assets retain separate provenance hashes. Their source
  revision need not equal the current library pin when the assets are unchanged.
- Roll out to one host first, verify its deployment and adoption surface, then
  propagate to additional applications deliberately.
