---
id: "0001"
title: "Architecture style — modular monolith"
date: 2026-07-10
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: architecture_style
supersedes: null
superseded_by: "0008"       # consumer-execution model only; modular-monolith style stands
tags: [architecture, core]
---

# ADR-0001: Architecture style — modular monolith

## Context

byteswarm is an event-driven automation framework whose consumers are
**in-process plugins written in one language and compiled into the framework
binary** (interview). One process therefore owns both the plugin set and the
bus connection. Load is modest and bounded (≤10 concurrent workflows, dozens–100+
consumers, ~thousands events/s), the product is long-lived, and the standing
invariants require deployment portability and ports-and-adapters structure.

## Decision

We will build byteswarm as a **modular monolith**: a single deployable framework
server binary with consumers compiled in, organised around explicit module
boundaries that could later become separable. Horizontal scale is achieved by
running **multiple instances**, each subscribing to a single `workflowID` or to
any `workflowID`.

## Consequences

- Simplest thing that satisfies the in-process-plugin requirement; one artifact
  to build, ship, and operate — right for a single-operator on-prem tool.
- Module boundaries are the future L2/L3 seams (for c4-designer): event model &
  routing core; **Bus** port + adapters; consumer registry / plugin SDK;
  **Repository** port + adapters; ingress adapters (CLI publisher, webhook
  receiver); telemetry (outbound business-event emitter port + structured
  logging); auth port; workflow lifecycle & resilience (subscription, reconnect,
  redelivery). Ports-and-adapters is mandatory in spirit (design-principles.md).
- A misbehaving plugin shares the process: consumer execution must be isolated
  (panic-recovered, resource-bounded) so one consumer cannot down the instance.
- Rules out independent scaling/deployment of a single consumer — accepted, since
  the in-process model is a requirement, and instance-per-workflowID gives a
  coarse scaling knob.

## Alternatives considered

- Microservices / one service per consumer — rejected: contradicts the in-process
  plugin requirement and imposes distributed-systems ops on a single-operator tool.
- Distributed actor runtime — over-engineered for ≤10 concurrent workflows.

## Amendment (refactor-0001, see ADR-0008)

The **modular-monolith style, module boundaries, and multi-instance scaling
above stand unchanged.** Superseded on **one dimension only**: consumers are no
longer *exclusively* compiled-in. As of ADR-0008, a second consumer kind —
runtime-loaded JavaScript (goja) script plugins behind the same `Consumer` port —
coexists with the compiled-in Go consumers. The "misbehaving plugin shares the
process → panic-recovered, resource-bounded" isolation clause now applies with
equal force to the script sandbox (per-invocation timeout, instruction budget,
recovered goroutine).
