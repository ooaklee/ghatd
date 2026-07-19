---
id: adrs-adr015
title: 'ADR015: Harden Go Toolchain and Dependencies'
# prettier-ignore
description: Architecture Decision Record (ADR) for hardening the Go toolchain and dependency baseline
---

## Context

GHATD is a public Go framework repository with packages that handle authentication, authorisation, email, payments, notifications, MongoDB persistence, Redis-backed ephemeral storage, and local development workflows.

The repository previously pinned Go 1.25.9 in `.tool-versions` while `go.mod` declared `go 1.25` and `toolchain go1.25.0`. A vulnerability scan against that baseline identified standard-library vulnerabilities fixed in later Go 1.25 patch releases and the current Go 1.26 patch line. It also identified module vulnerabilities in `golang.org/x/net`, `github.com/go-jose/go-jose/v4`, and `go.opentelemetry.io/otel/sdk`.

The direct dependency graph also still used `github.com/dgrijalva/jwt-go`, which is an abandoned JWT module with a known authorisation-bypass advisory. The maintained `github.com/golang-jwt/jwt/v5` module is already present indirectly and is the safer direct dependency for GHATD auth code.

Some dependencies have important provenance or migration considerations:

- `github.com/ooaklee/http-cache` was pinned to a forked pseudo-version because the upstream module had not yet accepted the skip-cache controls GHATD needs. The upstream maintainer later accepted the behaviour and [reimplemented it in `github.com/victorspringer/http-cache`](https://github.com/victorspringer/http-cache/pull/21#issuecomment-4499679232), preserving the response-writer path while adding tests for skipping storage by response header and skipping lookup and storage by URI path regex.
- `go.mongodb.org/mongo-driver` v1 was deprecated by MongoDB in favour of `go.mongodb.org/mongo-driver/v2`. GHATD subsequently migrated its repository packages and MongoDB migrator to v2.

## Decision

We will pin GHATD to Go 1.26.4 in `.tool-versions`, `go.mod`, and the module `toolchain` directive.

We will update major-version-compatible direct and indirect Go dependencies to current safe versions, including the module updates required to clear the vulnerability findings from the baseline scan.

We will replace direct `github.com/dgrijalva/jwt-go` usage with `github.com/golang-jwt/jwt/v5` and update auth parsing to use JWT v5 sentinel errors instead of matching error strings.

We will update `github.com/xakep666/mongo-migrate` and adapt the `cmd/mongo-migrator` command to the package's context-aware API and MongoDB driver v2 connection type.

The original hardening pass retained `go.mongodb.org/mongo-driver` v1.17.x. The later repository-wide migration moved GHATD's MongoDB integrations to v2.

We will replace `github.com/ooaklee/http-cache` with upstream `github.com/victorspringer/http-cache` now that upstream provides the required skip-cache response-header and URI-path-regex options.

## Consequences

GHATD maintainers can install and validate the repository with `asdf install` followed by `asdf exec` commands, using one explicit Go patch version.

The baseline vulnerability findings from Go 1.25.9, `golang.org/x/net`, `github.com/go-jose/go-jose/v4`, and `go.opentelemetry.io/otel/sdk` are addressed by the toolchain and dependency updates.

Auth code no longer depends directly on the abandoned `dgrijalva/jwt-go` module, reducing long-term security and maintenance risk.

The Mongo migrator now uses MongoDB driver v2 through `mongo-migrate`, while the rest of the repository continues to use MongoDB driver v1. This means both driver major versions are present temporarily.

The MongoDB v1 package deprecation remains a known follow-up. Moving all repository packages to driver v2 should be planned separately because it affects many public package imports, repository helpers, tests, examples, and host application integration expectations.

The `http-cache` dependency now resolves from the upstream module path. The fork can be archived after downstream applications have migrated, while preserving it for provenance of older builds.
