---
id: "0009"
title: "Consumer state representation — opaque bytes (Postgres BYTEA / SQLite BLOB)"
date: 2026-07-11
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: null            # partial supersede of 0005; no new active key
supersedes: "0005"
superseded_by: null
tags: [data, persistence, refactor-0002]
---

# ADR-0009: Consumer state representation — opaque bytes

## Context

ADR-0005 decided the PostgreSQL Repository adapter would store consumer state as
**JSONB** ("to serve both key/value and document shapes"). Since then the
`consumer.Repository` port has settled as **opaque bytes** —
`Save(ctx, id string, state []byte)` / `Load(ctx, id) ([]byte, error)` — and the
merged SQLite adapter (F2.1) stores state as an opaque `BLOB`. So ADR-0005's
JSONB decision already diverged from the code as built: a JSONB column rejects
non-JSON input and cannot hold arbitrary bytes, which would fail the shared
Repository contract test (it round-trips non-JSON values such as `"v1"`). This
surfaced building the PostgreSQL adapter (roadmap F2.2) and was routed through
refactor-0002 rather than resolved by stealth.

The premise that made JSONB attractive — server-side query of document-shaped
state — is not actually delivered by the current port, because the port exposes
opaque bytes with no JSON-typed accessor. JSONB would be storage the domain
cannot exploit.

## Decision

Consumer state is **opaque bytes**. The PostgreSQL adapter stores it in a
**`BYTEA`** column; the SQLite adapter's `BLOB` (already shipped) is conformant.
This **supersedes ADR-0005 on the storage-representation dimension only** — the
per-aggregate `Repository` port, PostgreSQL-default / SQLite-embedded split, and
`database/sql` + `pgx` driver choice all stand.

**JSONB / document-query capability is deferred.** It returns only when the
`Repository` port itself exposes JSON-typed state — a future change that crosses
the domain boundary and warrants its own ADR — not as a silent column swap.

## Consequences

- The two adapters are behaviorally identical behind the port (both store opaque
  bytes), so one shared contract test proves parity; F2.2 is unblocked.
- `pgx` is used through its `database/sql` (stdlib) registration, keeping the
  `CGO_ENABLED=0` single-static-binary invariant (ADR-0006), matching the pure-Go
  `modernc.org/sqlite` choice (re-examined from ADR-0006: stands).
- No server-side JSON/document queries in Postgres until a port change reopens it
  (recorded trigger above). Acceptable: no current or near-term roadmap item
  needs it.
- Records and resolves the untracked drift between ADR-0005 (JSONB) and the
  as-built opaque-bytes port; no `divergence-register.md` was in use.

## Alternatives considered

- **Keep JSONB, constrain the port to JSON-only state** — rejected: changes the
  domain port semantics, the contract test, and the plugin store shim for a
  document-query capability nothing needs yet.
- **JSONB storing a JSON envelope around base64 bytes** — rejected: pays JSONB's
  cost (validation, storage) for none of its benefit (the bytes still aren't
  queryable), and complicates the adapter.
- **Edit ADR-0005 in place** — rejected: ADRs are immutable records; a new ADR
  with a scoped supersede mirrors the ADR-0008-amends-0001 precedent.
