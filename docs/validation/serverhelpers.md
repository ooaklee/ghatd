# Server composition validation

Validated 2026-10-31 against integration base
`4321e1c04930d979e40bdba799513af1c087386e`.
The staged Go source/test and module diff SHA-256 is
`2f703db0dfebee02721a0f13b1f73c3ecf01f032899af40553d38c6985627d84`.
Compute it from `git diff --binary <base> -- '*.go' go.mod go.sum`.
Documentation-only changes are excluded from this fingerprint.

## Executed checks

- Go 1.26.10: full `go test -p 4 ./...`, full `go vet ./...`, and `go mod verify` passed.
- Go 1.26.10 with GCC 14: `go test -race -p 4 ./external/serverhelpers/... ./external/emailmanager/helper ./examples/serverhelpers/...` passed.
- The race run supplied an isolated Mongo replica set and Redis. Access-policy
  provisioning used actual transactions and unique disposable databases. Other
  optional native suites outside this command remain outside this evidence.
- `go run ./examples/serverhelpers` returned the documented offline health and
  telemetry output. No provider or search-engine notification was made.
- Independent bounded Codex source review covered public SSR/sitemap/eligibility,
  Partners authority/worker lifecycle and host adoption. A separate plumbing
  review identified typed-nil dependencies, now rejected with negative tests.
- OpenCode GLM 5.3 reviewed a manifest-hashed snapshot of lifecycle, its tests,
  email provider composition and explicit SparkPost tests. It found no blocking
  defects. This was static review; OpenCode did not execute the test suites.

The examples and tests do not prove production delivery, indexing, rollout or
external-provider readiness. Credentialed special-route CORS requires exact
origins, documented in the HTTP package. Domain persistence and authority remain
owned by the original packages. Host adoption retains native acceptance tests;
those results belong in each consumer's own validation record.

## Test-file disposition

| File | Disposition |
| --- | --- |
| `external/serverhelpers/access/runtime_integration_test.go` | Table-driven invalid/typed-nil composition and native live-session provisioning; fixture databases are generated per case. |
| `external/serverhelpers/access/runtime_test.go` | Table-driven invalid/typed-nil composition and native live-session provisioning; fixture databases are generated per case. |
| `external/serverhelpers/http/cors_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/http/email_review_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/http/handler_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/http/http_sampling_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/http/observability_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/http/request_logging_test.go` | Table-driven HTTP/CORS/email denial and privacy assertions; stateful cache and global telemetry cases retain explicit cleanup. |
| `external/serverhelpers/lifecycle_test.go` | New table-driven order/rollback/error/panic checks and an explicit concurrent close sequence. |
| `external/serverhelpers/partners/http_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/lifecycle_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/preparation_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/refund_snapshot_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/refund_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/runtime_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/partners/worker_test.go` | Extracted runtime/command/worker assertions and new typed composition boundaries. Stateful worker drains and retry sequences remain explicit; native financial acceptance stays in consumers. |
| `external/serverhelpers/providers/oauth_test.go` | Table-driven optional/partial provider configurations and credential-preserving authentication; no live provider requests. |
| `external/serverhelpers/providers/payments_test.go` | Table-driven optional/partial provider configurations and credential-preserving authentication; no live provider requests. |
| `external/serverhelpers/publication/composition_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/serverhelpers/publication/frontend_articles_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/serverhelpers/publication/indexnow_eligibility_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/serverhelpers/publication/indexnow_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/serverhelpers/publication/markdown_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/serverhelpers/publication/public_sitemap_test.go` | Extracted public-content assertions; SSR, status/privacy, build policy, markdown, independent schedules or scope identity remain tested. Stateful HTTP/clock cases retain their sequence. |
| `external/emailmanager/helper/sparkpost_test.go` | Explicit named provider and intercepted local-capture cases; failing transport proves no send. |
| `examples/serverhelpers/main_test.go` | Offline composition, disabled integration and cleanup output boundaries. |
