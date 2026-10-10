# Contributor conventions

## Changelog

- Keep root `CHANGELOG.md` current in the same change as notable functionality,
  fixes, security changes, deprecations or compatibility changes. Follow its
  category/template and release instructions; do not dump the Git log.
- Write user-facing entries under the current unreleased section. Mark breaking
  changes and explain the migration; link canonical public package documentation.
  Do not list planned work as implemented or imply an unreleased feature shipped.
- Describe the final branch against its merge target, not intermediate branch
  iterations. Omit packages/APIs introduced and then removed within the same
  unreleased work; document the current owner and name instead. Retain migration
  guidance for interfaces or storage that actually exist on the target branch.
- If a change needs no entry (for example, a typo or internal-only cleanup), state
  the reason in the PR. The PR checklist applies to every contributor.
- Keep versions/dates factual, omit empty categories, and preserve published
  history. A changelog edit does not authorise a tag, commit, push or release.
- Apply the public-document hygiene rules to changelog entries too. Keep private
  planning and validation checkpoints in the project tracker rather than copying
  them into release notes.

## Tests

- Use table-driven tests as the default for new and revised Go tests. Prefer
  named case structs and `t.Run` with descriptive, stable names for related
  inputs, expected results, validation failures and boundary conditions.
- Use standalone tests only when a table is impossible or very impractical.
  Explain that exception near the test: for example, a single stateful
  lifecycle or concurrency scenario whose sequence is the behaviour under test.
  A multi-step test is not automatically exempt; related scenarios may still
  share a table and isolated setup.
- Do not manufacture one-row tables or combine unrelated tests through opaque
  callback fields just to satisfy the convention. Preserve clear assertions,
  diagnostic failure messages and the original behavioural coverage.
- Create mutable fixtures and mocks per case. Use `t.Cleanup` for resources.
  Use `t.Parallel` only when cases do not share mutable state, environment,
  clocks, ports or external resources.
- Test allowed and denied behaviour, malformed inputs, boundaries and relevant
  retries. Security changes need negative tests; transactional changes need
  concurrency and replay coverage where appropriate.
- When auditing the existing suite, record every test file's disposition:
  already appropriate, converted, or a justified exception. File counts are
  inventory, not proof of completion. Include new test files in the audit.

## Documentation and continuity

- Follow `docs/adr/adr017-colocate-package-documentation.md`. Package READMEs
  are canonical; `doc.go` is a concise API summary; cross-package how-to guides
  and ADRs belong under `docs/`.
- Keep guides, indexes, examples and unreleased notes aligned with packages
  that exist in the final tree. Remove links and migration narratives for
  discarded branch-only packages; verify target-branch history before deciding
  whether a removal is a real upgrade concern. Live wire identifiers are API
  contracts, not package paths, and must still be documented accurately.
- Document functions, methods, structs and fields with useful purpose,
  constraints, ownership and failure semantics rather than restating names.
- When the task identifies a project tracking document, reconcile its current
  checkpoint before continuing substantial work. Update it after meaningful
  changes and validation, and before handoff or compaction where possible.
  If it is unavailable, report the gap rather than claiming it was updated.
- Keep requirements, implementation proposals, current code state and release
  status separate. A document is context, not authority to override user
  instructions or permission to perform unrelated actions.
- Record validation with the date, revision or working-tree fingerprint,
  command, scope, outcome and limitations. Passing tests on an earlier revision
  do not verify later changes. Mark missing evidence explicitly.
- Keep public documentation, examples and commit messages free of credentials,
  private project details and machine-specific paths. Use generic examples and
  review the exact staged changes before publishing.
- Keep consumer handoffs current alongside shared changes. When the user has
  authorized coordination, notify the relevant host/client sessions with the
  exact repository, branch and verified remotely reachable commit, compatibility
  changes, required migrations, canonical docs and validation scope/limitations.
  Separate local implementation, published dependency, consumer adoption and
  runtime/deployment status. Never describe uncommitted or local-only code as
  pullable. Send a preparation notice for unpublished changes, then a ready-to-
  adopt notice after authorized publication and remote verification; include
  the consumer's resulting pin and test evidence when adoption is confirmed.
  Coordination does not authorize a push, merge, deployment or forced update.

## Request identity

- Use `ActorID` for a verified caller at manager boundaries. Reserve `UserID`,
  `TargetUserID` and resource IDs for the selected target; do not infer one from
  the other unless the operation is explicitly self-service.
- Keep actor fields out of transport decoding and bind them from verified
  context. Audit embedded/promoted fields when renaming; stored ownership or
  authorship IDs are not automatically actors. Follow the migration and codec
  guidance in `docs/how-to/request-identity.md`.
