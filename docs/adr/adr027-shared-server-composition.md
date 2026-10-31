# ADR027: Shared server composition

Status: Accepted for implementation

## Context

Applications repeat orchestration around domain managers: transactional policy
startup, HTTP boundaries, optional providers, public content and worker ownership.
Domain packages already own behavior, but copying their assembly into each host
creates drift in startup validation and cleanup.

## Decision

Keep cross-domain composition in `external/serverhelpers`, with focused `access`,
`http`, `providers`, `publication` and `partners` subpackages. The root owns an
explicit resource lifecycle. Reuse existing owner helpers directly where they
already supply a complete integration; avoid alias-only or re-export packages.

Hosts resolve environment values and secrets, select stable authority namespaces,
provide identity admission, consent and presentation, and retain their explicit
`server.go` composition root. Helpers receive typed configuration and borrowed
owners. Constructors prepare only; worker execution is an explicit later step.
Startup failure and normal shutdown use the same reverse resource cleanup, with
dependent drains before releases and telemetry last.

## Consequences

Applications adopt helper APIs without copying shared mechanics. Package moves
do not change wire namespaces, persistent checkpoint identities, user authority
or publication policy. Product-specific rules and operator target choices remain
in applications. This is an additive composition surface, not an automatic
service container, configuration framework or replacement for domain managers.

The canonical API and migration constraints live in the
[package guide](../../external/serverhelpers/README.md); the
[offline example](../../examples/serverhelpers/README.md) demonstrates ownership.
