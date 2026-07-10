---
id: "0005"
title: "Data stores — PostgreSQL default, SQLite embedded, behind a Repository port"
date: 2026-07-10
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: data_stores
supersedes: null
superseded_by: null
tags: [data, persistence]
---

# ADR-0005: Data stores — PostgreSQL default, SQLite embedded

## Context

Stateful consumers and workflow state need **durable, externalized** persistence
that survives a full server rebuild (interview); nothing critical is held in-process.
The data shape **varies per consumer** — some want key/value, others document
objects. Volume is modest, the product is long-lived, and the minimal-dependency and
deployment-portability invariants apply. design-principles.md prescribes the
repository pattern and a SQLite-local / PostgreSQL-production seam.

## Decision

We will define a **`Repository` port per consumer aggregate** and provide two default
adapters: **PostgreSQL** (`database/sql` + pgx) as the production store, using **JSONB**
to serve both key/value and document shapes; and **SQLite** (`database/sql`) as the
zero-extra-infrastructure embedded option for small / single-node / development
deployments.

## Consequences

- JSONB covers the key/value + document mix without standing up multiple store engines.
- The `Repository` port keeps consumers persistence-agnostic and makes the
  SQLite↔PostgreSQL swap a composition-root choice, per design principles.
- State is external, so an instance reboot loses nothing; recovery pairs with the bus's
  durable cursors (ADR-0004).
- Both adapters sit on stdlib `database/sql` — minimal dependencies (one PostgreSQL
  driver).
- Cost: a consumer needing an exotic store (graph/vector/time-series) writes its own
  adapter behind the port — allowed, not provisioned up front.
- No Redis: there is no strong caching signal, and consumer state must be
  durable-of-record.

## Alternatives considered

- PostgreSQL only — rejected: the embedded SQLite option matters for the
  drop-on-a-single-server case.
- A document database (e.g. MongoDB) — rejected: JSONB already covers document needs
  without a new engine or dependency.
- Redis as primary state — rejected: not durable-of-record enough for consumer state.
- Free-for-all per-consumer stores with no port — rejected: the port permits variety
  without mandating infrastructure or losing testability.
