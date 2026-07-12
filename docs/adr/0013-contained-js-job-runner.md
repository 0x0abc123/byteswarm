---
id: "0013"
title: "Contained goja job-runner binary for long-running JavaScript jobs"
date: 2026-07-12
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: job_runner
supersedes: null
superseded_by: null
tags: [plugin, execution, deployment, security, job-runner]
---

# ADR-0013: Contained goja job-runner binary for long-running JavaScript jobs

## Context

Script plugins (ADR-0008) run one invocation per event on a fresh goja runtime
under a short per-invocation timeout — the enforced CPU bound. Handlers that
cannot finish within it must offload the work; today the only in-model path is a
hand-written daemonizing wrapper (bash) that re-enters completion via
`byteswarmctl` (docs/plugin-authoring.md, "Long-running work"). Operators asked
to write that long-running logic in **JavaScript on the already-bundled goja
engine** — no Node.js, no bash. The tempting shortcut — allowlisting a general
unsandboxed interpreter that runs any script path — was rejected: it would
collapse the ADR-0008 sandbox into arbitrary RCE from any plugin and reopen the
`sh -c` hole ADR-0008 deliberately closed (a plugin could `host.fs.write` a
script into its sandbox and then `host.exec` the interpreter on it).

## Decision

We will add a third static Go binary, **`byteswarm-job`** — a goja interpreter
for **operator-authored** long-running jobs — triggered through the existing
plugin `exec` allowlist under a **name-not-path containment** rule, and granting
a deliberately **broad but bounded** capability API that sits **outside** the
plugin sandbox.

- **Trigger & containment.** The `execAllow` template pins `byteswarm-job
  --jobs-dir <dir>`; a plugin passes only a job **name**, which the runner
  resolves **within** `<dir>` using the same fail-closed guard as
  `internal/plugin/host.go` `source()` (reject absolute paths and `..`). A
  plugin can therefore only trigger **pre-vetted, operator-placed** job scripts —
  never arbitrary code. This is why ADR-0008 **stands**: the plugin still sees
  exactly its four capabilities, its `exec` is still argv-only with no `sh -c`
  (launching the runner is an ordinary allowlisted argv call), and the
  per-invocation timeout still bounds the plugin. The runner is not added to the
  sandbox and is not a general interpreter on the allowlist.
- **Lifecycle.** The runner self-daemonizes in Go (setsid/re-exec, `--foreground`
  for debugging) so the launching `host.exec` returns immediately; the detached
  job then runs to completion under an operator-configured **max wall-clock**
  (breach → kill and publish a `*_failed`/`*_timeout` event). Jobs are linear
  synchronous scripts, so goja needs no event loop.
- **Capability surface (phase 1).** `job` (read-only: `id`, `name`,
  `workflowID`, `args[]`); `host.publish(type, workflowID?, payload)` via the
  operator-local `/events` Unix socket (ADR-0011), reusing `event.ValidType` and
  the ≤128-char workflowID / 1 MiB payload bounds (ADR-0010); `host.exec` with
  **unrestricted argv, no implicit shell**; `host.fs` **open** (absolute allowed,
  relatives rooted at a per-job workdir); `host.http.request(...)` (HTTP(S)
  client, egress open); `host.log(...)` (slog-JSON to a job log; uncaught errors
  auto-logged and auto-published `*_failed`). **Excluded:** `host.store` (the
  server owns the state DB; a second process contending on SQLite is a locking
  hazard — jobs round-trip state via events/files) and any direct NATS access.
- **Phasing.** Raw TCP/UDP sockets (`host.net.dial`) are **deferred to phase 2** —
  the largest and most error-prone shim and the biggest outbound surface; phase 1
  ships `host.http` only.
- **Concurrency/resources.** Bounded at the **OS level** (systemd slice /
  cgroups), documented — not runner-enforced.

## Consequences

- Operators write long-running jobs in JS on the bundled engine — no Node, no
  bash wrapper — with a `host` API that mirrors the plugin's where it overlaps
  (`publish`) and extends it where the escape hatch needs power (`exec`/`fs`/
  `http`). Adds a **third static binary** (extends ADR-0006's two-binary artifact
  list; the release workflow, ADR-0012, must build and package it — Unix-only,
  consistent with 0012's Windows exclusion).
- **Accepted trade-off — untrusted data in a powerful process.** Containment
  stops arbitrary *new code*, not untrusted *data*: `job.args[]` originates from a
  plugin that processed an untrusted event payload. A job that forwards `args`
  into `host.exec`/`host.http` is an injection/pivot path. The safety case rests
  on **operator-authored jobs treating `job.args` as hostile** — a discipline we
  document and give patterns for, not one the runner can enforce.
- **Accepted trade-off — `/events` boundary widened (ADR-0011).** To
  `host.publish`, the job's OS user must join the socket's `0660` group, giving it
  authority to forge any event type/workflowID within bounds. "Locked-down job
  user" and "reaches `/events`" are in tension; deployment guidance is a
  constrained user that is *in the socket group*, and nothing more.
- **No delivery guarantee past the trigger.** The triggering event is acked when
  `host.exec` returns, so a crashed detached job is not redelivered; jobs own
  their failure reporting (the auto-`*_failed` event and job log exist for this).
  Self-daemonized jobs are not lifecycle-owned by the server — restart can orphan
  them; OS supervision (cgroups/systemd) is the mitigation.
- Domain ports (`event.*`, `consumer.*`, `auth.*`) are untouched — the runner is a
  client-like side process dialing `/events` (like the CLI), so ADR-0001's
  modular-monolith style stands. The build is almost entirely new adapter/edge
  code plus two shared extractions (`internal/eventclient`, `internal/pathguard`).

## Alternatives considered

- General unsandboxed interpreter on the `exec` allowlist — rejected: arbitrary
  RCE from any plugin; reopens the `sh -c` hole ADR-0008 closed.
- Async-exec capability inside the plugin host (goroutine + completion event) —
  rejected earlier: voids ADR-0008's "invocation timeout is the enforced bound"
  invariant and needs host-owned background lifecycle + concurrency bounds.
- Keep the bash daemonizing wrapper only — the status quo; the ask was explicitly
  to remove bash and the Node dependency.
- Give the runner `host.store` / direct NATS — rejected: second-process DB
  contention, and NATS creds would have to be distributed to spawned jobs (the
  exact leak ADR-0011 avoids by putting `/events` on a socket).
