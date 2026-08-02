---
id: adrs-adr016
title: 'ADR016: External SEO Sitemap Package'
# prettier-ignore
description: Architecture Decision Record for reusable sitemap ownership and host application extension points
date: 2026-07-12
status: accepted
---

## Status

Accepted.

## Context

GHATD host applications need the same core sitemap capabilities: persistent URL
entries, deterministic XML generation, a public `/sitemap.xml` route,
administrative CRUD and batch ingestion, safe file generation, and MongoDB
migrations. Keeping independent copies of that backend in each host application
causes fixes and API behaviour to drift.

The framework already exposes content-specific sitemap candidates through
packages such as `external/contentmanager`. Candidate discovery is different
from sitemap ownership: content packages know which records are public, while a
sitemap service knows how entries are validated, stored, filtered, rendered,
and served.

Crawler policy is application-specific. A framework cannot decide which product
routes deserve indexing, which dynamic records have sufficient canonical
content, or what priority and change frequency a product should assign.

## Decision

GHATD will own the reusable sitemap backend in `external/seo`.

The package will provide:

- sitemap item models, validation, repository, service, handler, and routes;
- deterministic and de-duplicated XML generation;
- exclusion of protected route groups from generated XML;
- safe writes and downloads below a configured writable root;
- an embedded-file fallback for the public sitemap;
- MongoDB index and configurable starter-route seed migrations; and
- service methods that host applications can use for batch ingestion and
  product-specific orchestration.

Host applications will compose the package with their repository, validator,
frontend domain, filesystem, writable path, and admin-only middleware. They may
customise starter seed paths when registering migrations.

Content packages and host applications remain responsible for discovering
public URL candidates. They submit candidates through the SEO service instead
of duplicating sitemap persistence or XML generation. Product-specific repair
migrations, crawl-budget decisions, metadata, robots directives, and frontend
administration remain outside the framework package.

## Consequences

Core sitemap fixes and API behaviour now have one framework implementation.
Host applications can add content ingestion without forking the sitemap domain.

Applications adopting the package must update imports and register the external
SEO migrations. Existing collections, document shape, and route paths remain
compatible, so the migration does not require copying stored sitemap data. A
host with duplicate URI records must clean them up before creating the package's
unique URI index.

The framework provides conservative protected-route filtering and starter seed
defaults, but each host application must still review its public routes and
crawler policy. Moving the backend into GHATD does not make product-specific
content automatically indexable.

The package currently uses the framework's MongoDB driver v1 repository surface.
Migration to a newer driver must be coordinated with the shared repository and
migration APIs rather than performed by an individual host application.

## Follow-up status (2026-08-01)

The coordinated repository migration has since completed. The SEO package,
its migrations, and the shared repository surface now use MongoDB driver v2.
The original consequence above records the constraint at the time this ADR was
accepted; it is not the package's current dependency state. Host applications
retain ownership of SEO registration and seed customisation, and execute those
registrations through the shared MongoDB command recorded in
[ADR018](./adr018-shared-mongodb-migrator-command.md).
