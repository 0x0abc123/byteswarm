---
id: "0003"
title: "Backend language — Go"
date: 2026-07-10
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: backend_language
supersedes: null
superseded_by: null
tags: [backend, language]
---

# ADR-0003: Backend language — Go

## Context

The core is a high-concurrency event runtime: dozens–100+ in-process consumers
executing concurrently, running synchronous shell commands and launching
long-running child processes, at ~thousands events/s. Consumers are compiled-in
plugins in one language (ADR-0001). The artifact must be a self-contained binary
runnable on-prem/cloud/containers, and the minimal-dependency invariant favours a
strong standard library. The standing invariant restricts the choice to Python or
Go.

## Decision

We will use **Go** for the entire system — framework server and CLI — as a single
module producing static binaries.

## Consequences

- Goroutines, channels, and worker pools model concurrent consumers and long-running
  processes directly; `os/exec` covers shell/child-process work.
- Static binaries satisfy the self-contained / deployment-portability invariants;
  `net/http` (Go 1.22+ routing) fully covers the webhook without a framework;
  `log/slog`, `database/sql`, `testing` come from the stdlib.
- Consumers use a **compile-time registration** SDK (type-safe, no fragile runtime
  plugin loading); adding a consumer means rebuilding the binary — accepted, since a
  single-language compiled-in model is a requirement.
- The chosen bus and its embeddable server are Go-native (see ADR-0004), letting the
  same language span client, server, and bus tooling.
- Cost: no Python data/ML ergonomics — not needed by the requirements.

## Alternatives considered

- Python — rejected: the GIL undercuts the high-concurrency core, there is no single
  static binary, the runtime plugin model is weaker, and the stdlib lacks a production
  HTTP server.
- Mixed Go + Python — rejected: ADR-0001 is a monolith and no ML/data split exists to
  justify a second language.
