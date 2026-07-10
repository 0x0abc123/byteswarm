# Architecture Brief — byteswarm

## Purpose

byteswarm is an extensible, event-driven framework for defining and running
automation workflows: chains of commands, external tool invocations, and telemetry.
It runs headless on a server, where pluggable consumers react to events, do work, and
emit further events — composing larger workflows from small, independent steps.

## Actors

- Operator — an engineer who runs and administers the framework and triggers work — via CLI.
- External trigger source — an upstream system or automation that fires events — via webhook.
- Consumer author — a developer who extends the framework with new event consumers (build time).

## External systems

- Event bus — internal backbone — durable publish/subscribe with replay.
- State repository — outbound — durable per-consumer and workflow state.
- External tools & processes — outbound — shell commands and long-running processes consumers launch.
- External network services — outbound — services consumers read from / send data to.
- Business-event sinks — outbound — external destinations for emitted business telemetry.
- Logging / monitoring sink — outbound — structured error and exception records.
- Webhook callers — inbound — event triggers over HTTP.

## Key features

- Define and run automation workflows as chains of events.
- Publish/subscribe by event type, plus a global broadcast event type for systemwide notices.
- Extensible in-process consumer model; a consumer subscribes to one or a subset of event types.
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
- Security: single trusted operator; publish/consume unauthenticated; webhook auth is
  shared-secret now and pluggable for stronger mechanisms later; auth fails closed.
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
- Global broadcast — a systemwide notification is delivered to all subscribed consumers.
- Replay / audit — past events for a `workflowID` are re-read from the durable log.
- Reconnect & recovery — after reboot or network loss, an instance reconnects, resumes from durable
  cursors, and unacknowledged events are redelivered.
