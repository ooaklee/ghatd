---
id: adrs-adr026
title: 'ADR026: Share storage primitives, not product persistence policy'
date: 2026-10-02
status: accepted
---

## Context

Transactional domain adapters need native Mongo error causes, result counts for
revision checks, safe telemetry and dependable session cleanup. Older generic
helpers discarded some counts and error causes or logged query/document data.
That encouraged direct driver calls and duplicate silent logging/transaction
wrappers. Local test adapters also repeated copy-on-write state handling.

## Decision

Improve the existing repository helpers and add result-bearing operations
without expanding the legacy `CommonOperations` interface. Share managed-client
transaction execution, explicit collection/index initialization and an actual
transaction probe. Automatic helper logs carry only fixed operation/outcome
metadata; trusted callers still receive native error causes.

Extract authenticated payload sealing to `external/encryption`, preserving the
standard nonce-prefixed AES-256-GCM format. A host owns the stable key, schema,
unambiguous AAD, versioning and any eventual rotation migration.

Provide an opt-in `ephemeral.MemorySnapshot[T]` for process-local fixtures with
an explicit deep-clone function. It is not a Redis substitute, production
fallback, durable repository, or generic resource schema. Cloning read views and
committed snapshots deliberately trades O(state size) copying for alias safety.

Leave product records, query metadata, authority/fencing rules, state transitions,
receipt formats and retention with their owning domains. In particular, a
transaction runner cannot infer whether a snapshot authorization read needs a
write fence to serialize with revocation. Do not move an entire product store
into GHATD merely because its persistence code uses Mongo.

## Migration and consequences

Existing helper signatures remain available. Migrate CAS-sensitive callers to
result-bearing methods and verify counts. Remove private no-op logger adapters
once the caller uses privacy-safe helpers. Update log consumers for the reduced
metadata and do not log raw returned errors in public responses.

Hosts must preserve existing ciphertext/AAD and explicitly initialize required
collections/indexes before probing transactions. No schema rewrite, key rotation,
data deletion or standalone-Mongo fallback is implicit. Transaction callbacks
must use the supplied session/client and tolerate retries without external side
effects. An uncertain commit is not proof of rollback.

Validation combines table-driven unit tests, isolated replica-set tests for
counts/retries/rollback/probes, encryption compatibility checks and downstream
store contract tests. Passing a snapshot fixture test does not establish Mongo
concurrency or production durability; fault-injected uncertain commits and
production load remain separate validation work.

Canonical guides: [repository](../../external/repository/README.md),
[encryption](../../external/encryption/README.md),
[ephemeral state](../../external/ephemeral/README.md).
