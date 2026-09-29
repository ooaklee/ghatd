---
id: adrs-adr023
title: 'ADR023: Own Migration Telemetry at the Database Action Boundary'
date: 2026-09-29
status: accepted
---

## Context

Hosts using the shared MongoDB migrator still copied the same telemetry settings
parser, subcommand-name loop and generic Cobra tests. Constructing a MongoDB
monitor alongside the command tree can bind it to a provider that is replaced
or shut down before a later invocation. Shared lifecycle behavior belongs with
the database actions that require it.

## Decision

Extend `external/migrator/mongo` with opt-in `WithTelemetry` and
`WithTelemetryFromEnvironment` options. Reuse `otelcobra.Run` for runtime startup,
command spans, safe completion logging and flush behavior. Avoid a second
command-wrapper package or a second implementation of that lifecycle.

The environment helper accepts a host component fallback and instrumentation
scope, preserves the standard environment variables, and owns logger sync.
Custom resolvers retain host policy and caller-owned loggers. Both resolve only
inside `up` and `down`; help, validation, hooks and offline file creation retain
their existing semantics. Command span names are fixed to the database action.

Create the default MongoDB monitor after runtime startup and bind both providers
explicitly for each invocation. An explicitly supplied monitor, including nil,
takes precedence regardless of option order. Its lifecycle stays caller-owned.
Database disconnect retains trace/logger context values but receives a fresh
independent timeout, followed by span completion, telemetry shutdown and finally
helper-owned logger sync. Action/disconnect errors remain joined; exporter flush
errors cannot turn committed database work into a failed action.

Hosts retain migration imports, registrations, paths, identity, exporter policy
and signal-aware root execution. Plain and generated commands remain opt-in;
existing users are not automatically connected to telemetry backends.

## Consequences and verification

Hosts can reduce their adapter to registration and policy. Shared tests exercise
actual up/down commands, fresh providers across invocations, MongoDB parentage,
custom-monitor precedence, cancellation/error/panic cleanup and offline command
behavior. Host tests retain registration and configuration wiring checks.

Runtime providers remain global: owning runtimes must not overlap. Custom
monitor users must create dependencies after starting their own runtime.
Automatic telemetry omits raw database errors, but returned errors keep their
identity and can contain data; host logging remains the host's responsibility.

This extends [ADR018](adr018-shared-mongodb-migrator-command.md) and
[ADR021](adr021-shared-observability-adapters.md) without changing migration
history, broad rollback semantics or schema ownership.
