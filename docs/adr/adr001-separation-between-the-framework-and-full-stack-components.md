---
id: adrs-adr001
title: 'ADR001: Introduce a separation between the framework and full-stack components'
# prettier-ignore
description: |
Introduce a separation between the framework and full-stack components to simplify
the process of getting started with the Ghat(d) framework.
---

## Decision

A decision was made to split GHAT(D) into two types: `framework` and 
*independent self-running applications* called `Details`.

## Discussion

We should strive to make it simple for users to test and experiment with 
GHAT(D) framework until it becomes familiar and effortless to use. It would
be ideal to establish clear parameters for what constitutes a `Detail`-compatible
repository so that the community can maintain a consistent standard.

## Consequences

* It may be difficult for people to locate the `Details`.
* There is no guarantee `Details` will be updated once released/ created.
* People need to be persuaded to support GHAT(D)'s vision.
* We will split maintenance across multiple repos, growing inline with features/types. For a small team, it might become daunting.
   