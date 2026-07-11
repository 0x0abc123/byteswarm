---
id: "0010"
title: "Event-type token rule — a single dot-free subject token"
date: 2026-07-12
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: null            # partial supersede of 0004; no new active key
supersedes: "0004"
superseded_by: null
tags: [messaging, routing, refactor-0003]
---

# ADR-0010: Event-type token rule — a single dot-free subject token

## Context

ADR-0004 encodes an event on the bus as the NATS subject
`bw.evt.<type>.<workflowID>` and requires two things the subject must support:
filtering **by workflowID** (a specific one, or any) and a **global broadcast**
event type. But the event `Type` was never constrained: `internal/bus` and
`internal/server` allowed `.` in a type (rejecting only whitespace and NATS
wildcards), while `internal/plugin` already forbade it (charset
`[A-Za-z0-9_-]`) — a live split-brain where a JS plugin cannot publish
`order.created` yet the CLI/webhook ingress accepts it.

More importantly, a dotted `Type` is **multiple** subject tokens, so
`<workflowID>` sits at a variable depth and **no NATS wildcard can pin it** —
which blocks broker-side workflowID scoping (roadmap F4.4). The premise of
ADR-0004's workflowID filtering is unrealisable while `Type` can contain dots.

## Decision

An event **`Type` is a single subject token**: non-empty, bounded, charset
`[A-Za-z0-9_-]` — **no dots**, whitespace, or NATS wildcards. One shared
validator, **`event.ValidType`**, is the single source of truth, applied by the
bus (`subjectFor`), the HTTP ingress (`/events`, `/webhook` → 400 on a bad
type), and the plugin publish shim (replacing its private check). This
**supersedes ADR-0004 on the event-type token dimension only** — the subject
shape, NATS JetStream choice, durable delivery, and Bus port all stand.

Consequently `bw.evt.<type>.<workflowID>` keeps `<type>` at a fixed single
token, so an instance can scope to a workflow via `bw.evt.*.<workflowID>`
(unblocking F4.4, which remains a separate feature).

**Reserved sentinel:** `consumer.BroadcastType` (`@broadcast`) is exempt —
`event.ValidType` permits it in addition to the charset, so broadcast stays
unforgeable by ordinary types and F4.5 keeps working (`@` is a legal NATS token
character).

**workflowID** is left unconstrained for now: only `Type` must be single-token
to pin the workflowID for `bw.evt.*.<workflowID>` scoping. The symmetric
"any-workflowID for a fixed type" subscription would additionally require a
dot-free workflowID — deferred until a feature needs it.

## Consequences

- Broker-side workflowID scoping becomes expressible; F4.4 is an ordinary
  feature, not a subject-scheme reorder.
- The bus/server/plugin type-validation split-brain is resolved by one rule.
- **Backward-incompatible (pre-1.0, accepted):** callers posting a dotted type
  to `/events` or `/webhook` now get 400; examples move to `order_created`.
- Type is opaque and flat — no hierarchical `order.*` matching. Consumers/
  plugins already match by exact type, so nothing in use is lost.

## Alternatives considered

- **Reorder the subject to `bw.evt.<workflowID>.<type…>`** — also pins
  workflowID, but is a larger, invasive change to the subject encoding and
  keeps the type/plugin split-brain. Rejected: constraining the type is smaller
  and unifies the validators.
- **In-process workflowID filtering only** — no subject change, but every
  instance still receives every event; defeats the point of scoping. Rejected.
- **Re-slug `@broadcast` to a charset-valid token** — rejected: `@` keeps the
  sentinel unforgeable; exempting it is simpler than reserving a plain word.

## Reopening trigger

Hierarchical / namespaced event types return only via a dedicated payload or
namespace field (not by loosening the subject token) — a future ADR, mirroring
ADR-0009's deferred-JSONB precedent.
