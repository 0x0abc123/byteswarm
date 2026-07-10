---
id: "0002"
title: "Client approach — CLI primary, webhook ingress"
date: 2026-07-10
status: proposed
deciders: ["claude (advisor)"]
retrospective: false
decision_key: client_approach
supersedes: null
superseded_by: null
tags: [client, cli, ingress]
---

# ADR-0002: Client approach — CLI primary, webhook ingress

## Context

The system runs headless on a server. Its primary client is a **CLI tool** an
engineer uses on that machine to generate/publish events and operate the
framework; events can also be triggered by an inbound **webhook** (interview).
No browser, mobile, or desktop UI is required. Standing invariants govern client
technology.

## Decision

We will provide a **CLI tool as the primary client** (stdlib flag-based command
parsing, shipped as a static binary) and expose an **HTTP webhook ingress** on
the server. Both are thin **ingress adapters** that produce events into the core
through an ingress port; there is no web UI. The webhook authenticates via a
**pluggable auth port** (shared-secret adapter by default).

## Consequences

- A single self-contained CLI binary distributes trivially on-prem; no JavaScript
  build pipeline enters the project.
- CLI and server share one codebase/module as two entry points — keeps client and
  server logic in one language (see ADR-0003).
- Webhook auth is configurable: the shared-secret adapter today, stronger
  mechanisms (SSO/JWT/mTLS) later, without touching the core; the auth path
  fails closed.
- The exact transport by which the CLI produces events (server HTTP ingress vs.
  direct bus publish) is left to component design (c4-designer).
- If an interactive multi-user UI is ever needed, a vanilla Svelte SPA can be
  added against the same core — noted, not built.

## Alternatives considered

- Svelte SPA now — no interactive web client is required; would add a build
  pipeline against the minimal-dependency invariant.
- Interactive TUI (e.g. bubbletea) — CLI subcommands suffice; revisit if an
  operations console is wanted.
- gRPC API for the CLI — JSON/HTTP is simpler and stdlib-first; revisit only if
  streaming or strict typing across a network boundary is needed.
