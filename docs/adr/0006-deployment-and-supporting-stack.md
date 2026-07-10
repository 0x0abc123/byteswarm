---
id: "0006"
title: "Deployment posture & supporting stack"
date: 2026-07-10
status: proposed
deciders: ["claude (advisor)"]
retrospective: false
decision_key: deploy_targets
supersedes: null
superseded_by: null
tags: [deployment, stack, observability]
---

# ADR-0006: Deployment posture & supporting stack

## Context

byteswarm must run on-prem, in cloud, and in containers; **serverless is out of
scope** (interview — long-running processes and persistent bus connections fit it
poorly). A single self-contained artifact is preferred. The deployment-portability
invariant requires externalized state, portable configuration, thin per-target
adapters, and one artifact everywhere. Operators running the tool on-prem want a
committable, reviewable **configuration file** rather than assembling many
environment variables by hand (interview). The remaining stdlib-first
supporting-stack choices are recorded here.

## Decision

We will package the server and CLI as **static Go binaries plus a container image**,
targeting **VM, container, and Kubernetes** (not serverless). Supporting stack,
stdlib-first: **`net/http`** (Go 1.22+ routing) for the webhook ingress and
`/healthz` + readiness endpoints; **`log/slog`** for structured error/exception
logging; **in-process goroutine worker pools** with **`os/exec`** for consumer
execution and long-running child processes — no external job queue beyond the bus;
**configuration from a config file with environment-variable overrides** (the file
is the base/committable configuration; env vars override per-deployment secrets and
target-specific values); correlation carried by `workflowID` / event IDs through the
ports. Business-event telemetry is an **outbound emitter port**
with consumer-defined external sinks; OpenTelemetry is added only if a later ADR
records the need.

## Consequences

- One build artifact runs across all supported targets by swapping only thin entry
  adapters — the portability invariant made real.
- A config file gives operators a reviewable, version-controllable base configuration;
  env-var overrides preserve container/Kubernetes portability and keep secrets out of
  the committed file. A stdlib format (JSON) needs no dependency; a friendlier format
  (YAML/TOML) would justify one small parsing library at bootstrap.
- No web/logging framework dependency; background work needs no Celery-class system.
- Health/readiness endpoints make the artifact portable across orchestrators.
- Cost: individual consumers cannot be autoscaled (in-process by ADR-0001); scale by
  running more instances on disjoint `workflowID`s.
- Serverless exclusion is deliberate and recorded, should it ever be revisited.

## Alternatives considered

- Include serverless — ruled out by the interview; poor fit for long-running work and
  persistent bus connections.
- A web framework (Gin/Echo) — rejected: `net/http` suffices under the
  minimal-dependency invariant.
- An external job queue for background work — rejected: the bus plus goroutine worker
  pools already cover it.
