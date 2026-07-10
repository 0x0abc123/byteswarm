---
id: "0004"
title: "Event bus — NATS JetStream behind a Bus port"
date: 2026-07-10
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: event_bus
supersedes: null
superseded_by: null
tags: [messaging, core, data]
---

# ADR-0004: Event bus — NATS JetStream behind a Bus port

## Context

The event bus is byteswarm's backbone. Requirements (interview): pub/sub by event
type; filter by `workflowID` (a specific one or any); a global broadcast event type;
**at-least-once** delivery with **out-of-order** tolerance; **replay/audit** of past
events; ~thousands events/s across ≤10 workflows and 100+ consumers; resilient
reconnect after reboot/network loss. The minimal-dependency and single-self-contained-
artifact invariants weigh heavily, and the stack is Go (ADR-0003). This is the one
genuinely contested technology choice, so it gets its own ADR.

## Decision

We will use **NATS with JetStream** as the default event bus, accessed through a
**`Bus` port** (publish / subscribe / ack) so the implementation is swappable.
Subjects encode type and workflow (e.g. `bw.evt.<type>.<workflowID>`), giving native
wildcard subscription by type, by workflow, or by any, plus a broadcast subject;
JetStream provides durable at-least-once delivery, per-consumer durable cursors,
redelivery on nack/crash, and replay by sequence or time.

## Consequences

- The nats-server is a single small Go binary that can be **embedded in-process**
  (dev / single-node) or **run standalone** (production) — a strong fit for the
  "engineer drops it on a server" on-prem story.
- The subject hierarchy matches the type-and-`workflowID` routing model directly, so
  routing is broker-side rather than hand-rolled client filtering.
- JetStream streams deliver replay/audit and survive reboot (durable cursors); the
  reconnect requirement is met by the client's built-in reconnection.
- Adds one core dependency (`nats.go`) — justified as the system's backbone.
- Cost: JetStream retention/replay is limit-based, not an infinite log by default —
  stream retention and limits must be configured deliberately for the audit window.
- The `Bus` port means a Redpanda/Kafka adapter can replace NATS later without
  touching consumer code.

## Alternatives considered

- Redpanda (Kafka API) — honest runner-up: best-in-class long-retention replay/audit
  and ecosystem tooling, but a heavier operational footprint than a single-operator
  on-prem tool needs, and topic-based routing makes the `workflowID` filter less
  natural. Kept as the documented alternative adapter for heavy-audit deployments.
- RabbitMQ — flexible routing but an Erlang runtime dependency, weaker native replay,
  less Go-aligned.
- Redis Streams — lighter, but weaker durability/replay guarantees for a
  system-of-record event log.
- In-process Go channels only — no durability or replay; fails the resilience and
  audit requirements.
