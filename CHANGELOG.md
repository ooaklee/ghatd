# Changelog

Notable changes to GHATD are recorded here for application authors and
contributors. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
with release versions expressed using [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

This record starts with the current upgrade. Earlier releases have not been
retrospectively catalogued. An unreleased entry is not a release announcement or
evidence that a feature has been deployed.

## Maintaining this file

- Update the current unreleased section in the same change as a notable feature,
  fix, security improvement, deprecation or compatibility change. Describe the
  effect on users rather than copying commit messages.
- Keep plans and unimplemented features in the project tracker. State limitations
  of partial or opt-in implementations and link to their canonical package guides.
- Mark incompatible changes with **Breaking:** and include the required migration
  action. Use public issue, PR or documentation links where useful; never include
  private project details, credentials or machine-specific paths.
- Use one `Unreleased` section until a version is selected. Then name it
  `[x.y.z] - Unreleased`. On publication, replace `Unreleased` with the actual
  `YYYY-MM-DD` release date and start a fresh unreleased section above it.
- Keep newest releases first and omit empty categories. Do not invent versions,
  dates or historical entries. Preserve published entries; clarify inaccuracies
  explicitly rather than silently changing the recorded release scope.

Before `1.0.0`, advance the minor version for features or incompatible API
changes and the patch version for fixes-only releases. Pre-release identifiers
such as `alpha`, `beta` and `rc` distinguish candidates from stable releases.
Choosing or documenting a version does not authorise creating a tag or publishing
a release.

## Changelog entry template

Use the categories that apply:

- `Added`: newly available capabilities.
- `Changed`: adjustments to existing behaviour.
- `Deprecated`: functionality scheduled for retirement.
- `Removed`: functionality no longer available.
- `Fixed`: corrected behaviour.
- `Security`: security-related changes.

The version below is a placeholder, not the next GHATD release. Replace it only
when the release version has been selected, and remove unused subsections.

```markdown
## [x.y.z] - Unreleased

### Added

- Describe the new capability and any opt-in requirements.

### Changed

- **Breaking:** Describe the compatibility impact and migration action, if any.

### Deprecated

- Identify the deprecated API and its replacement.

### Removed

- Identify the removed behaviour and migration path.

### Fixed

- Describe the corrected behaviour and affected users.

### Security

- Describe the security improvement and any required operator action.
```

---

## Unreleased

### Added

- Opt-in [declarative routes](external/router/README.md#declarative-route-policies)
  with startup validation, defensive inventories and structured, fail-closed
  policy responses. Hosts supply enforcing middleware and authorizers; metadata
  is not a grant, and raw Mux routes remain outside the registry.
- [Request-local proof admission](external/accessproof/README.md) evaluates exact
  alternatives, capabilities, assurances, bindings and expiry. Hosts authenticate
  evidence and retain live transactional ownership and replay checks.
- [Strong-revision validation](external/router/README.md#shared-strong-revision-validation)
  parses singular If-Match headers with explicit size, whitespace and byte rules.
  It does not compare revisions or authorize writes. Unknown or ambiguous route
  authorizer failures produce 503; deliberate denials must use a route sentinel.
- Result-bearing Mongo mutation helpers, shared managed-client transactions,
  explicit index/collection setup and a transactional startup probe. Domains
  retain their schemas, authorization, revision checks and retention policy; see
  [repository migration guidance](external/repository/README.md#transaction-safe-operations).
- Reusable [AES-256-GCM payload encryption](external/encryption/README.md),
  preserving the standard nonce-prefixed byte format and requiring explicit AAD
  and a host-owned stable key. No automatic key rotation or data rewrite occurs.
- Opt-in [copy-on-write memory snapshots](external/ephemeral/README.md#process-local-transactional-snapshots)
  for local/test adapters, with cancellation-aware entry and deep-copy isolation.
  These are not a Redis replacement or production persistence fallback.
- Contributor guidance establishing table-driven tests as the default and
  requiring changelog updates for notable changes. The whole-suite test-style
  audit remains separate work.
- Reusable [manifest-driven HTTP error helpers](external/errormanifest/README.md#wrapped-errors-at-http-boundaries)
  preserve mapped wrappers and validation joins while rejecting unknown independent
  causes. A separate strict resolver supports single-cause authentication
  boundaries. Handler adoption and dependency-map wiring remain explicit.

### Changed

- **Breaking:** the reply integration rejects invalid or shared custom response
  prototypes; custom factories must return independent state. Unknown error
  diagnostics are no longer printed to the standard logger, and joins containing
  an unmapped cause now return a generic 500. Review the
  [reply migration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md)
  and use its safe observer when failure telemetry is required.
- Redis session lookups now distinguish missing records from operational
  failures. Use `ephemeral.ErrAuthNotFound` or `errors.Is`; legacy `redis.Nil`
  remains detectable through wrapping, but direct equality is no longer safe.
  See [lookup semantics](external/ephemeral/README.md#live-session-lookup).
- Automatic repository logs now emit only fixed operation/outcome metadata;
  filters, documents, names and raw database errors are omitted even for custom
  loggers. Missing-document lookups use debug-level telemetry. Update log
  consumers for this privacy-oriented change; explicit application `Log*` calls
  and driver monitors still need their own redaction policy.

### Fixed

- Pin the reply integration upgrade for structural error resolution.
  This is an immutable, unreleased integration commit, not a tagged release.
  Reply uses request-local response state, safe opt-in unmapped diagnostics and
  deterministic error ordering; see
  its [migration guide](https://github.com/ooaklee/reply/blob/38c9f4107f3dce3e9f0c09c8cc919c20d02fa967/UPGRADING.md).
- Repository find/count/cursor failures retain native error causes underneath
  existing error codes, enabling retry-label and cancellation inspection.
  Single-result cursor mapping closes its cursor and distinguishes iteration
  errors from missing documents.
