---
id: adrs-adr014
title: 'ADR014: Local Email Inbox for Development'
# prettier-ignore
description: >
  Architecture Decision Record (ADR) for capturing local EmailManager output in
  an opt-in development inbox instead of writing raw rendered email bodies to
  structured logs.
date: 2026-05-27
status: proposed
---

## Status

Proposed.

## Context

GHATD host applications need usable local email flows. During development,
login emails, verification emails, and custom transactional emails often
contain magic links or security codes that developers must open or copy in
order to complete the flow under test.

Before this decision, the local email provider wrote enough email content to
logs that developers could recover those links and codes. That conflicted with
the structured logging strategy in ADR013, because rendered email bodies can
contain tokens, codes, personal data, and arbitrary host application content.
Those values should not appear in structured logs or log shipping systems.

At the same time, simply redacting the email body from logs would make local
development worse. Developers would no longer be able to sign in, verify
accounts, inspect templates, or debug outbound email content without wiring a
real provider or adding ad hoc host application code.

## Decision

GHATD will keep raw rendered email bodies out of structured logs and provide a
separate local email inbox for development.

`emailprovider.LoggingEmailProvider` will be the local output provider. It will
capture full local emails in a bounded in-memory `LocalEmailStore` and continue
to log only safe metadata such as provider, message ID, address domains, body
presence, subject length, and local inbox count.

The local inbox routes are opt-in through:

```go
err := emailprovider.AttachLocalInboxRoutes(&emailprovider.AttachLocalInboxRoutesRequest{
    Router:   ghatdRouter,
    Provider: localEmailProvider,
})
```

The default route prefix is `/_ghatd/local/emails`. Host applications can set a
custom `Prefix` when they need a different local route.

The inbox will expose:

- an HTML list of captured emails;
- an HTML detail page with metadata, rendered preview, raw HTML, optional text
  body, and extracted web links;
- a raw rendered-email endpoint for opening the email in a browser;
- a JSON summary endpoint for local tools;
- a clear action for resetting the in-memory inbox.

The routes are local-development surfaces and must not be enabled in production
or untrusted environments. By default, the route middleware rejects non-loopback
clients. `AllowRemote` exists only for trusted local proxy setups where another
local layer controls access.

Rendered email previews must be constrained. The detail page renders email HTML
inside a sandboxed iframe. The raw rendered-email endpoint sets a CSP sandbox
policy. Extracted shortcut links are limited to `http`, `https`, and local
relative web URLs so magic-link flows remain convenient without exposing
non-web schemes as action buttons.

`emailmanager.EmailManager` will treat local output providers specially when
`ShouldSendEmail` is false. For those providers, both templated email paths and
the pre-rendered `SendEmail` path may still call `Send` so the local inbox is
populated. For non-local providers, `ShouldSendEmail=false` continues to prevent
real provider sends.

## Consequences

Local developers can test login, verification, and custom-email flows without
using a real provider allowance and without searching raw HTML in logs.

Structured logs remain safe for host applications to ship and retain. They
identify that email was captured locally and provide operational metadata, but
they do not contain rendered email bodies, tokens, security codes, or full email
addresses.

The inbox is intentionally in-memory. It is simple to operate, resets on
process restart, and avoids adding storage dependencies to local development.
Developers who need persistence can build a host application integration around
the provider/store API.

Host applications must explicitly attach the local inbox routes. This keeps
production deployments from receiving the route by default, but it does mean
local startup code needs a small integration step.

Because captured emails may contain sensitive local credentials, host
applications should only attach the routes for trusted local development and
should avoid exposing them through public tunnels unless an additional trusted
access layer protects the route.

## Rollout

GHATD will document the local inbox workflow in the Email Manager guide and
examples. Host applications that currently rely on raw local email logs should
switch to `LoggingEmailProvider`, set real provider sending to disabled in local
development, attach `AttachLocalInboxRoutes`, and open
`/_ghatd/local/emails` while testing local email flows.

After adopting this change, host applications should verify:

- local login and verification emails appear in the inbox;
- magic links can be opened from the inbox;
- security codes can be copied from rendered or raw email content;
- structured logs only contain safe email metadata;
- inbox routes are not attached in production or untrusted environments.
