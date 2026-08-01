---
id: adrs-adr017
title: 'ADR017: Colocate Package Documentation'
# prettier-ignore
description: Architecture Decision Record for colocating canonical package guides beside their source code
date: 2026-07-31
status: accepted
---

## Status

Accepted.

## Context

GHATD package documentation was previously split across two locations:
package-level `README.md` files inside `external/<package>/` and getting-started
guides under `docs/getting-started/<package>/`. This created several problems:

- **Drift and duplication**: Two documents described the same package, and
  neither was clearly canonical. Updates to one were not always reflected in the
  other.
- **Poor discoverability**: Contributors working inside a package directory had
  to navigate to a separate `docs/` tree to find or update the guide that
  described it.
- **Unclear ownership**: Package guides in `docs/getting-started/` were not
  co-located with the code they described, making it unclear who owned them or
  where review should happen.
- **Inconsistent linking**: Top-level and how-to links pointed at
  `docs/getting-started/` paths, while package-level READMEs existed alongside
  the code, leading to mixed and sometimes broken navigation.

The project also maintains `doc.go` files in some packages. These serve as
concise GoDoc API and convention summaries, distinct from the richer narrative
guides a README provides. No standard existed to clarify the boundary between
`doc.go`, `README.md`, and package-owned `docs/` directories.

## Decision

We will colocate canonical package documentation beside the source code it
describes. Each `external/<package>/README.md` is the single canonical package
guide.

### Classification rules

| Location | Purpose |
|---|---|
| `external/<package>/README.md` | Canonical package guide: overview, architecture, setup, API reference, configuration, and usage examples. One per package. |
| `external/<package>/doc.go` | Concise GoDoc summary: package purpose, key types, and conventions. Intended for `go doc` and IDE hover. Does not duplicate the README narrative. |
| `external/<package>/docs/` | Multiple substantial guides owned primarily by one package and too long for its README (e.g., a package-specific host-application walkthrough). Used only when a single README is insufficient. |
| `docs/how-to/` | Cross-package, task-oriented guides whose subject is a complete workflow rather than one package. |
| `docs/adr/` | Architecture Decision Records. |
| `docs/product-requirements/` | Product-level requirements and specifications. |
| `docs/assets/` | Shared visual and static assets. |
| `docs/about-details.md` | Project-level context and detail documents. |

### Linking policy

- Top-level `README.md` and central indexes link to the canonical
  `external/<package>/README.md` without copying or duplicating package
  content.
- How-to guides reference package READMEs for API-level detail and remain
  focused on cross-package workflows.
- Package READMEs may link to central `docs/` resources (ADRs, how-tos) when
  relevant context exists there.

## Rationale

- **Discoverability beside code**: A contributor inside a package directory sees
  the guide immediately. New contributors find documentation where the code
  lives.
- **Contributor ownership**: Package maintainers own their documentation in the
  same location as their code, making review local and accountability clear.
- **Review locality**: Documentation changes appear in the same diff as related
  code changes, improving review quality.
- **Reduced drift and duplication**: One canonical guide per package eliminates
  the split-brain problem where two documents describe the same thing.
- **Link and navigation clarity**: All links point to one location per package.
  Central indexes link outward without copying content.

## Consequences

- **Positive**: Single source of truth per package; documentation lives with
  code; fewer broken links; clearer review boundaries.
- **Negative**: Package directories grow with documentation files; contributors
  must remember that `external/<package>/docs/` is for package-owned
  substantial guides only, not for cross-package content.
- **Trade-off**: Central `docs/` no longer contains package guides, so a
  contributor browsing `docs/` will not find package-specific setup there.
  Top-level and how-to indexes compensate by linking outward.

## Alternatives Considered

1. **Keep `docs/getting-started/` as the canonical location.** Rejected because
   it separates documentation from code, worsens discoverability, and
   encourages drift.
2. **Use only `doc.go` for all package documentation.** Rejected because
   GoDoc is not suitable for rich narrative guides, tables, and multi-section
   walkthroughs. `doc.go` remains a concise API summary.
3. **Duplicate content in both locations with a sync script.** Rejected because
   it adds tooling overhead and still risks drift when the script is not run.
4. **Move all documentation into a single monolithic `docs/` tree.** Rejected
   because it loses the co-location benefit and makes package ownership
   unclear.

## Migration Policy

- Package guides previously in `docs/getting-started/<package>/` have been
  moved to `external/<package>/README.md` using git-aware moves to preserve
  history.
- Where a package-level README already existed, the richer content was merged
  into it and duplicate sections were removed.
- The starter/v0 host-application walkthrough moved to
  `external/starter/v0/docs/host-application-style.md` because starter/v0 owns
  that substantial guide and its README already serves as the package entry
  point.
- The `docs/getting-started/` directory has been removed; no redirect stubs
  remain.
- Existing `doc.go` files are preserved as-is. New `doc.go` files are not added
  in this relocation pass; the standard is recorded here for future
  incremental adoption.
- No speculative package documentation is created where none previously
  existed.
