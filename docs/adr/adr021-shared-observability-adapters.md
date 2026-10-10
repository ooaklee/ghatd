---
id: adrs-adr021
title: 'ADR021: Centralize Observability Mechanics Behind Host-Owned Policy'
date: 2026-09-29
status: accepted
---

## Context

Integrating a shared telemetry runtime still required host applications to copy
provider HTTP-client adapters, cache counters, worker instrumentation, shutdown
coordinators and their regression tests. Repeating these implementations makes
upgrades large and lets lifecycle, privacy and context-propagation fixes drift.

[ADR001](adr001-separation-between-the-framework-and-full-stack-components.md)
separates the framework from host applications.
[ADR002](adr002-separation-of-emailer-package.md) assigns delivery mechanics to
the email provider. Replacing that provider in every host merely to inject a
client or propagate context contradicts those boundaries.

## Decision

GHATD owns reusable instrumentation and lifecycle mechanics and tests them at
the package boundary. Hosts retain product behavior and explicit configuration.

- `emailprovider.NewSparkPostClient` accepts a full HTTP client, including a
  fluent constructor-request option, while retaining its transport override
  and SDK return type. The provider copies client policy before customization,
  never mutating a caller's or process-global client. Context-aware sending is
  an optional client capability, preserving existing legacy implementations.
- `external/http/server` inherits request context values, detaches process
  cancellation during drain, and force-closes connections on failed shutdown.
  Hosts can retain custom hooks or own a server directly.
- `observability.ShutdownResources` orders drain, work cancellation, dependency
  cleanup and telemetry flush. Each phase receives the appropriate fresh
  deadline. Hosts still own dependency callbacks and the logger's lifetime.
- `otelcache` owns finite cache-event metrics and the HTTP cache adapter.
  `otelqueue` owns finite consumer operations, settlement observations and a
  worker runtime. Neither owns business payloads, retries or broker admission.
- Existing metric names are configurable so adoption can preserve dashboards.
  Configured vocabularies are finite, validated and copied. Request-derived
  values cannot add dimensions during recording.

Generic regression tests move with their implementation. Hosts retain tests
that prove their wiring, policy choices, routes, process identities and shipped
queries. Passing a generic package suite does not prove correct host adoption.
Duplicate wrapper packages are deleted after imports are updated.

## Compatibility and consequences

Existing SparkPost constructor calls and legacy `Send` clients continue to work.
The fluent option configures a constructor request, not a live shared provider;
mutating a provider after requests begin would create concurrency hazards.
Transports and cookie jars remain shared references governed by their own
concurrency contracts; GHATD never mutates them.

HTTP handler contexts now inherit runtime values and remain live during drain.
Custom server hooks that replace `BaseContext` own their replacement behavior.
Hijacked connections and callbacks that ignore context still need host cleanup.

Consumer spans remain independent roots until a host defines a versioned trace
carrier. Shutdown deadlines do not imply exactly-once effects or guaranteed
delivery. Exporter selection, sampling, credentials, deployment identities,
retention and budget remain explicit host policy.

Adoption should pin an exact reviewed module revision, preserve metric identity
and validate one host before wider propagation. Remove development-only local
replacement directives before publishing the host dependency update. Package tests, host integration tests and deployment observation
provide different evidence; none substitutes for the others.

## Alternatives considered

Copying more adapters keeps each host self-contained but repeats fixes and
tests. Generating wrappers only relocates that duplication. Hardcoding a host's
routes, broker or metric prefix inside the framework would hide product policy.
The chosen APIs expose static configuration while sharing execution behavior.
