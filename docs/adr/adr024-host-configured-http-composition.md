---
id: adrs-adr024
title: 'ADR024: Share HTTP Policy Assembly and Canonical Intake Dispatch'
date: 2026-09-29
status: accepted
---

## Context

Hosts repeated request-log proxy parsing, policy construction and optional
trace-suppression setup. They also copied a mux adapter that places browser
trace intake before application auth, cache and response transforms while
keeping it within the outer telemetry/logging/recovery boundary.

## Decision

Add `NewHTTPServerOptions(HTTPServerOptionsConfig)` and
`MountBrowserTraceIntake(next, intake, path)` to the existing observability
package. Reuse its validated policy constructors and route observation. Hosts
pass enable flags, redaction/suppression paths, proxy text and intake path
explicitly. There are no shared mutable path defaults or product identities.
Disabled policies do not validate unused settings. The mount rejects unsafe
literal paths, never cleans or redirects aliases and preserves fallback dispatch.

Keep the existing `BrowserTraceIntakeConfig` mapping in hosts: it already
expresses identity, allowed origins, bounded vocabularies and rate limits.
Avoid a getter interface over host settings, an additional runtime owner or a
provider-specific configuration layer around the existing traced HTTP client.

## Consequences

Hosts delete copied dispatch mechanics and pass a small configuration value
for policy assembly. Existing low-level constructors remain available. Shared
tests prove assembly, immutability, fixed diagnostics and canonical routing;
host tests continue to prove actual policy choices, SPA/cache behavior and TCP
read deadlines through their complete middleware stack.

Deployment remains operator-owned. Environment-variable documentation explains
runtime profiles, process roles, secret injection and browser build/runtime
separation without provisioning resources or changing deployment configuration.
This extends [ADR021](adr021-shared-observability-adapters.md).
