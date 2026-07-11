---
id: "0008"
title: "Plugin execution model — embedded JavaScript (goja) script consumers with a sandboxed capability API"
date: 2026-07-11
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: plugin_model
supersedes: "0001"
superseded_by: null
tags: [architecture, plugins, security, core]
---

# ADR-0008: Plugin execution model — embedded JavaScript script consumers

## Context

ADR-0001 decided that consumers are in-process plugins written in one language
and **compiled into** the server binary, and ADR-0003 chose Go with a
**compile-time registration** SDK ("no fragile runtime plugin loading"). Adding
or changing a consumer therefore means rebuilding and redeploying the binary.

The operator wants to author and change event handlers **without rebuilding** —
by dropping in a script and declaring, in the config file (ADR-0006), which event
types it handles. This is refactor-0001. The requirement is not to replace the
compiled-in model but to add a second, runtime-loaded kind of consumer beside it.

Plugins are authored by the **operator or a trusted team** (single-operator
on-prem tool, per the architecture brief). The sandbox is therefore
**defense-in-depth against mistakes and blast-radius, not an adversarial
multi-tenant boundary** — but because a plugin is handed the ability to run OS
commands, the capability surface is still the highest-risk part of the system and
is governed by reference/security-fundamentals.md.

## Decision

**Add runtime-loaded JavaScript script plugins as a second kind of consumer,
behind the existing `Consumer` port, coexisting with compiled-in Go consumers.**

- **Engine:** embed **`github.com/dop251/goja`** — a pure-Go ECMAScript 5.1/2015
  interpreter. Pure Go keeps the `CGO_ENABLED=0` static-binary build (ADR-0006)
  and native JSON matches byteswarm's JSON event payloads. cgo engines
  (V8/`v8go`, native C-Lua, QuickJS bindings) are rejected — they break the
  static build and enlarge the attack surface.
- **Port stability (adapter-only):** a `ScriptConsumer` **implements the existing
  `Consumer` port**; the goja host lives in a new `internal/plugin/` adapter. The
  `Consumer` and `Repository` (ADR-0005) port signatures do **not** change.
  Repository namespacing is enforced **inside the host shim**, not by a new port
  argument. Native Go consumers are untouched.
- **Configuration & code delivery:** plugins are declared in the existing JSON
  config file (ADR-0006) under a `plugins` array — each entry has a `name`, the
  `events` it subscribes to, and a `source`: a **file path resolved within a
  host-configured plugins directory** (primary), or an inline `script` string for
  trivial handlers. Scripts are **compiled at load and fail closed** on syntax
  error; a plugin that fails to load does not start (deny by default).
- **Capability API (host-injected):** the script sees no ambient authority — only
  a host-provided object exposing exactly four capabilities, each host-mediated:
  1. **`exec`** — run an OS command chosen from a **host allowlist keyed by
     logical name → fixed argv template**; arguments are passed as an **argv
     array, never a shell string** (no `sh -c`, no interpolation); returns
     stdout/stderr/exit status. Non-allowlisted names are denied and logged.
  2. **`store`** — read/write the `Repository` (ADR-0005) under a
     **host-controlled namespace** derived from the plugin name; keys are
     prefixed and validated so a plugin cannot address another's data.
  3. **`fs`** — read/write files **within a per-plugin sandboxed directory**;
     every path is resolved and confined (no `..` traversal, no symlink escape).
  4. **`publish`** — publish an event via the existing `Bus` port (ADR-0004).
- **Invocation & isolation:** the event payload is passed to the plugin's handler
  entrypoint on each event. Each invocation runs on a pooled goja `Runtime` in a
  **panic-recovered goroutine** with a **per-invocation timeout enforced via
  `Runtime.Interrupt` + a watchdog** and an instruction budget — satisfying and
  extending ADR-0001's "panic-recovered, resource-bounded" isolation clause to
  the script sandbox.

## Consequences

- Operators add/change handlers by editing config + a script file — no rebuild.
  Compile-time Go consumers remain for performance- or type-critical handlers.
- Adapter-only: the refactor adds `internal/plugin/` and composition-root wiring;
  it does not ripple into domain packages. If it ever needs to change a port, the
  refactor was misclassified — stop and re-run impact analysis.
- **Security (called out for PR review, per security-fundamentals.md):** the
  `exec` allowlist is argv-only and fails closed; the event payload is
  **untrusted input to the script** and script-supplied values (paths, keys, argv,
  event bodies) are **validated at the host boundary**; namespace and path
  containment are enforced host-side; denied capability calls (exec/namespace/
  path violations) are logged as security events **without logging payloads or
  secrets**.
- **Known limitation:** goja provides no hard **memory** cap and cannot preempt a
  tight loop without the cooperative interrupt. Mitigations: per-invocation
  timeout + instruction budget + watchdog, one runtime per invocation, and
  OS-level limits (ulimit/cgroups) on `exec`-launched children. If genuinely
  **untrusted** plugins ever become a requirement, revisit toward a WASM host
  (`wazero`, fuel-metered) or process isolation — recorded as the revisit trigger.
- New dependency: `github.com/dop251/goja` (pinned in go.mod; scanned by the
  existing `govulncheck` step). Justified under the minimal-dependency invariant:
  one pure-Go module is the whole cost of the runtime-plugin capability.

## Alternatives considered

- **Lua via `gopher-lua`** — pure-Go with a smaller sandbox TCB; rejected as
  primary because events are JSON (native in goja) and JS has a far larger author
  pool for a mixed-experience team. Retained as the fallback if the trust model
  turns adversarial.
- **WASM via `wazero`** — strongest isolation and fuel-based CPU metering, but
  costs authors a compile-to-WASM step and changes the "drop in a script" UX;
  over-engineered for trusted operator authors now. Named as the revisit path.
- **cgo engines (V8, C-Lua, QuickJS)** — rejected: break `CGO_ENABLED=0` and the
  minimal-dependency/attack-surface controls.
- **Changing the `Consumer`/`Repository` ports** to model script consumers —
  rejected: a `ScriptConsumer` satisfies the existing port; namespacing belongs in
  the shim. Changing ports would ripple to every Go consumer for no gain.
- **Cryptographic signing / verification of plugins at load** — deferred: authors
  are trusted; v1 enforces schema validation + path containment + fail-closed
  compile. Signing is the first control to add if trust weakens.
