# Architecture Brief — byteswarm

## Purpose

byteswarm is an extensible, event-driven framework for defining and running
automation workflows: chains of commands, external tool invocations, and telemetry.
It runs headless on a server, where pluggable consumers react to events, do work, and
emit further events — composing larger workflows from small, independent steps.

## Actors

- Operator — an engineer who runs and administers the framework and triggers work — via CLI.
- External trigger source — an upstream system or automation that fires events — via webhook.
- Consumer author — a developer who extends the framework with compiled-in Go event consumers (build time).
- Plugin author — a trusted operator/developer who adds runtime JavaScript plugin consumers via config, no rebuild (ADR-0008).

## External systems

- Event bus — internal backbone — durable publish/subscribe with replay.
- State repository — outbound — durable per-consumer and workflow state.
- External tools & processes — outbound — shell commands and long-running processes consumers launch
  (for script plugins, via a host-controlled `exec` allowlist).
- Plugin scripts & sandbox — a config-declared plugins directory holds script source; each plugin has a
  sandboxed working directory the host confines file access to (ADR-0008).
- External network services — outbound — services consumers read from / send data to.
- Business-event sinks — outbound — external destinations for emitted business telemetry.
- Logging / monitoring sink — outbound — structured error and exception records.
- Webhook callers — inbound — event triggers over HTTP.

## Key features

- Define and run automation workflows as chains of events.
- Publish/subscribe by event type, plus a global broadcast event type for systemwide notices.
- Extensible in-process consumer model; a consumer subscribes to one or a subset of event types.
- Two consumer kinds behind one port: compiled-in Go consumers, and runtime-loaded JavaScript (goja)
  script plugins declared in config, sandboxed with a host capability API (ADR-0008).
- Long-running work runs in a contained goja job-runner binary (`byteswarm-job`): operator-authored jobs,
  triggered by name via the plugin `exec` allowlist, run detached outside the plugin sandbox with a broad
  host API (ADR-0013).
- Consumers run synchronous shell commands, launch long-running processes, read/write durable
  repositories, and call external services.
- Consumers emit derived events destined for downstream consumers.
- `workflowID` tagging on events; an instance subscribes to one `workflowID` or to any.
- CLI to generate/publish events and operate the framework.
- Webhook ingress to trigger events, with configurable authentication.
- At-least-once delivery with idempotent consumers; event replay and audit.
- Durable, externalized per-consumer state.
- Business-event telemetry emission plus structured error/exception logging.

## Non-functionals & constraints

- Delivery: at-least-once; out-of-order tolerated; consumers idempotent.
- Replay/audit of past events is required.
- Scale: peak ~thousands events/s; dozens–100+ consumers; ≤10 concurrent workflows.
- Subscription scope: each instance subscribes to a single `workflowID` or to any; scope
  is a per-instance config parameter and filtering is broker-side, not per-consumer.
  Scale by one any-scope instance or by N disjoint per-`workflowID` instances (not both).
- Resilience: survive server reboot and external-network loss/reconnect; no critical
  in-process state.
- Security: single trusted operator; the operator-local /events ingress is served over a Unix
  domain socket bounded by OS filesystem/user permissions rather than the network (ADR-0011);
  the webhook ingress authenticates untrusted and cross-host callers with a shared secret now,
  pluggable for stronger mechanisms later; auth fails closed. Script plugins
  (ADR-0008) run in a defense-in-depth sandbox: host-mediated capabilities only, exec allowlist
  (argv, no shell), namespaced state, confined filesystem, per-invocation resource bounds; capability
  denials are logged without leaking payloads.
- Deployment: on-prem, cloud, and containers; serverless out of scope; single
  self-contained artifact preferred; configured via a config file with environment overrides.
- Longevity: long-lived product; minimal dependencies.
- Assumptions (to revisit): modest data volume; business-event sinks are consumer-defined;
  source hosted on GitHub.

## Data overview

- Event — type, `workflowID` (broadcast events are workflow-agnostic), parameters/payload,
  identifiers/correlation, timestamp.
- Durable event log — retained for replay and audit.
- Per-consumer state — durable, shape varies (key/value and document), keyed by consumer and workflow.
- Logs — structured error/exception records.

## Key flows

- CLI-triggered workflow — operator publishes an initiating event; subscribed consumers react.
- Webhook-triggered event — external caller fires an authenticated webhook; an event is injected.
- Consumer chain — a consumer handles an event, does its work, and emits derived event(s) downstream.
- Plugin invocation — a script plugin subscribed to an event type is invoked with the event payload; it
  uses the host capability API (exec/store/fs/publish) within its sandbox and resource bounds (ADR-0008).
- Long-running job — a plugin triggers the contained job-runner by name; it detaches, runs an
  operator-authored JS job with a broad host API (exec/fs/http/publish/log), and reports completion by
  publishing back to `/events` (ADR-0013).
- Global broadcast — a systemwide notification is delivered to all subscribed consumers.
- Replay / audit — past events for a `workflowID` are re-read from the durable log.
- Reconnect & recovery — after reboot or network loss, an instance reconnects, resumes from durable
  cursors, and unacknowledged events are redelivered.
