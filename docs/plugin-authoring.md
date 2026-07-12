# Plugin authoring guide

byteswarm lets you add event handlers at runtime as **JavaScript script
plugins** — no rebuild — running on an embedded [goja](https://github.com/dop251/goja)
interpreter behind the same `Consumer` port as compiled-in Go consumers
(ADR-0008). This guide is the complete contract a script sees.

> **Read the [Security model and limitations](#security-model-and-limitations)
> section before shipping a plugin.** The sandbox is defence-in-depth for
> *trusted* authors, **not** a boundary for untrusted code. In particular, the
> filesystem sandbox does **not** constrain the paths a plugin passes to an
> allowlisted `exec` command.

## Declaring a plugin

Plugins are declared in the JSON config file (ADR-0006) under a `plugins`
array. Each entry has a `name`, the `events` it subscribes to, and a source —
either a `path` (a `.js` file resolved **within** the host `pluginsDir`) or an
inline `script` string for trivial handlers:

```json
{
  "pluginsDir": "plugins",
  "execAllow": {
    "gzip": ["/usr/bin/gzip"],
    "backup": ["/usr/bin/tar", "czf"]
  },
  "plugins": [
    { "name": "archiver", "events": ["order_created"], "path": "archiver.js" },
    { "name": "greeter",  "events": ["user_joined"],  "script": "host.publish('greeted', event.workflowID, {})" }
  ]
}
```

- `name` is the plugin's identity — it is also its **store namespace** and its
  **filesystem sandbox directory** (`<pluginsDir>/<name>`).
- The `execAllow` map is the **exec allowlist** (see [`host.exec`](#hostexecname-args)),
  keyed by a logical command name → a fixed argv template.
- Scripts are **compiled at load and fail closed**: a plugin whose script does
  not compile, or whose `path` escapes `pluginsDir`, does not start.

## Execution model

- The script runs **once per delivered event**, on a **fresh goja runtime each
  time** — there is no shared state between invocations (persist across events
  with [`host.store`](#hoststore)).
- Each invocation has a **per-invocation timeout** (default 5s, configurable by
  the operator via the `pluginTimeout` config field or `BYTESWARM_PLUGIN_TIMEOUT`);
  a runaway loop is interrupted and the invocation fails. **Your handler must
  finish within it** — see [Long-running work](#long-running-work).
- An instance processes its events **sequentially**: a slow handler delays every
  event queued behind it, and a handler that runs longer than the bus
  acknowledgement window (~30s) causes the event to be **redelivered** while it
  is still running. Keep handlers short; offload anything slow (see below).
- The runtime is **panic-recovered**. If the script **throws, times out, or
  panics**, the invocation returns an error and the event is left
  **unacknowledged for redelivery** rather than dropped. Write idempotent
  handlers.
- A script has exactly two globals in scope: `event` and `host`. There is no
  ambient authority — no `require`, no network, no filesystem except through
  `host`.

## The `event` global

The delivered event:

| Field | Type | Notes |
|---|---|---|
| `event.type` | string | the event type |
| `event.workflowID` | string | the workflow scope (may be empty) |
| `event.payload` | any | the payload parsed as JSON; if it is not valid JSON, the raw string |

`event.payload` is **untrusted input** — validate it in your handler.

## The `host` object

`host` exposes four capabilities plus a read-only `plugin` metadata object.
Every capability is host-mediated and **fails closed**: a denied or invalid
call **throws** (which, if uncaught, fails the invocation and triggers
redelivery).

### `host.exec(name, args)`

Run an OS command **from the allowlist**, chosen by its logical `name`. The
host-configured argv template runs with `args` appended as a **pure argv
array** — there is no shell, so arguments cannot inject a command.

```js
const res = host.exec("backup", ["/data/out.tar"]); // runs: /usr/bin/tar czf /data/out.tar
res.stdout; // string
res.stderr; // string
res.code;   // number — the process exit code
```

- `name` not on the allowlist → **throws** (deny by default).
- Bounds: at most **64** args, each at most **4096** bytes → throws if exceeded.
- A **non-zero exit is a normal result** reported in `res.code`, not a thrown
  error. Only a failure to *launch* the process throws.
- **Security:** see the limitation below — the arguments you pass are **not**
  confined to your sandbox.

### `host.store`

Durable key/value state, **namespaced to your plugin** automatically (you
cannot read or write another plugin's keys).

```js
host.store.set("last_id", String(event.payload.id)); // value must be a string
const last = host.store.get("last_id");              // string, or null if unset
```

### `host.fs`

File access **confined to your per-plugin sandbox directory**
(`<pluginsDir>/<name>`).

```js
host.fs.write("state/counter.txt", "42"); // path is relative to the sandbox
const s = host.fs.read("state/counter.txt");
const dir = host.fs.home();                // absolute path of your sandbox dir
```

- Paths are **relative**. An absolute path or any `..` that climbs above the
  sandbox → **throws** (`path escapes sandbox`). Symlink escapes are blocked at
  I/O time.
- A single read or write is bounded at **8 MiB** → throws if exceeded.
- `host.fs.home()` returns your sandbox's **absolute** path — useful for
  building an absolute path to a file you wrote, to hand to `host.exec`. It
  grants no new authority: `read`/`write` still reject absolute paths, so
  knowing `home()` cannot be used to escape the sandbox.

### `host.publish(type, workflowID, payload)`

Emit a derived event onto the bus.

```js
host.publish("order_archived", event.workflowID, { id: event.payload.id });
```

- `type`: a **single token** matching `[A-Za-z0-9_-]` — **no dots, no
  wildcards, no whitespace**. Invalid → throws.
- `workflowID`: same charset, at most 128 chars; may be `""`.
- `payload`: any JSON-serializable value (serialized for you), at most **1 MiB**.

### `host.plugin`

Read-only metadata about the running plugin:

| Field | Type | Notes |
|---|---|---|
| `host.plugin.name` | string | your plugin's name (= store namespace, sandbox dir) |
| `host.plugin.allowlist` | string[] | the **logical** `exec` command names you may call, sorted |

```js
if (host.plugin.allowlist.includes("gzip")) {
  host.exec("gzip", [host.fs.home() + "/report.csv"]);
}
```

`allowlist` lists the command **names only** — never the host argv templates —
so it discloses no host binary paths.

## Long-running work

A handler must return within the per-invocation timeout, and because an instance
processes events sequentially, it should return *quickly* so it does not delay
the events behind it. Anything slower than a second or two should not run inline.
There are two ways to stay within the budget.

### Option A — keep the work under the timeout

Prefer this whenever the work can be made fast:

- Do the minimum inline — validate `event.payload`, branch, record a little
  state — and avoid unbounded loops or large in-memory processing.
- If you `host.exec` a command, make sure the command itself is quick. `exec`
  **waits** for the process and the process is **killed when the invocation
  ends**, so it counts fully against your timeout.
- An operator can raise `pluginTimeout` for a few extra seconds of headroom, but
  keep it **comfortably below the ~30s acknowledgement window** — a handler that
  outruns that window gets its event redelivered (a duplicate invocation) while
  still running.

### Option B — daemonize the work and report completion with an event

For genuinely long jobs (minutes, or unbounded), run the work in a **detached
background process** and let its completion re-enter byteswarm as a *new event*
that another handler picks up. The mechanics that make this work:

- **The allowlisted command must daemonize itself.** `host.exec` runs a fixed
  argv with **no shell**, so you cannot background from JavaScript (there is no
  `&`), and `exec` both waits for the child and kills it when the invocation
  ends. So the wrapper has to detach into a **new session**, redirect its stdio,
  and **return immediately** — then `host.exec` returns in milliseconds and your
  handler finishes on time while the real work continues.
- **Completion comes back through the CLI, not `host.publish`.** The detached
  process is outside the goja runtime, so it reports done by running
  `byteswarmctl publish`, which dials the operator-local `/events` socket
  (ADR-0011). A plugin subscribed to that completion event then handles the
  result. The simplest shape is **one plugin subscribed to both** the trigger
  and the completion event, branching on `event.type` — it shares a single
  `store` namespace and sandbox across both.

**1. Allowlist the wrapper** (the script is the pinned binary; the plugin
supplies only trailing args):

```json
{
  "execAllow": { "run-job": ["/usr/local/bin/run-job.sh"] },
  "plugins": [
    { "name": "long-job", "events": ["report_requested", "job_done"], "path": "long-job.js" }
  ]
}
```

**2. The daemonizing wrapper** (`/usr/local/bin/run-job.sh`):

```bash
#!/usr/bin/env bash
# run-job.sh — a daemonizing wrapper for long byteswarm jobs.
# Invoked as: host.exec("run-job", [jobId, workflowID, ...args])
# On first entry it re-execs itself detached and exits, so host.exec returns
# within the invocation timeout; the detached copy does the work and reports back.
set -euo pipefail

JOB_ID="${1:?jobId required}"
WORKFLOW_ID="${2:-}"
shift 2 || true

OUT_DIR="${BYTESWARM_JOB_DIR:-/var/lib/byteswarm/jobs}"
SOCKET="${BYTESWARM_EVENTS_SOCKET:-/run/byteswarm/events.sock}"
CTL="${BYTESWARMCTL:-/usr/local/bin/byteswarmctl}"

# Phase 1 (launcher): detach into a new session with stdio closed, then exit.
# setsid + closed stdio is what lets host.exec return straight away and keeps
# the worker alive after the plugin invocation (and its context) ends.
if [[ "${_BW_WORKER:-}" != "1" ]]; then
  _BW_WORKER=1 setsid "$0" "$JOB_ID" "$WORKFLOW_ID" "$@" </dev/null >/dev/null 2>&1 &
  exit 0
fi

# Phase 2 (detached worker): do the real work, capture output, report completion.
mkdir -p "$OUT_DIR"
OUT_FILE="$OUT_DIR/$JOB_ID.out"
STATUS=ok

# ---- replace this block with your actual long-running command ----
if ! { sleep 45; echo "finished job $JOB_ID for args: $*"; } >"$OUT_FILE" 2>&1; then
  STATUS=failed
fi

# Report completion. Keep the payload small — send a *reference* to the output,
# not the output itself (publish caps at 1 MiB). jq escapes arbitrary strings.
PAYLOAD=$(jq -nc --arg id "$JOB_ID" --arg st "$STATUS" --arg out "$OUT_FILE" \
  '{jobId:$id, status:$st, output:$out}')

"$CTL" publish --socket "$SOCKET" --type job_done --workflow "$WORKFLOW_ID" \
  --payload "$PAYLOAD" \
  || logger -t byteswarm "run-job: failed to publish completion for $JOB_ID"
```

> The wrapper relies on two common host tools: **`setsid`** (util-linux) to
> detach into a new session, and **`jq`** to build the JSON payload safely. If
> either is unavailable, substitute an equivalent (`nohup`/a double-fork; a
> vetted `printf` template for simple, already-escaped values).

**3. The plugin** (`long-job.js`) — launches on the trigger, finishes on the
completion event:

```js
if (event.type === "report_requested") {
  const jobId = event.workflowID + "-" + String(event.payload.id);
  // Dedupe: the trigger can be redelivered (at-least-once). Launch once.
  if (!host.store.get("job:" + jobId)) {
    host.store.set("job:" + jobId, "started");
    // Returns immediately — run-job.sh daemonizes the real work.
    host.exec("run-job", [jobId, event.workflowID, String(event.payload.id)]);
  }
} else if (event.type === "job_done") {
  const p = event.payload;
  // Dedupe: completion events can be redelivered too.
  if (p && p.jobId && !host.store.get("done:" + p.jobId)) {
    host.store.set("done:" + p.jobId, p.status);
    host.publish(p.status === "ok" ? "report_ready" : "report_failed",
                 event.workflowID, { jobId: p.jobId, output: p.output });
  }
}
```

**Things to get right with this pattern:**

- **No durability once you return.** When your handler returns, the triggering
  event is **acknowledged** — the bus's at-least-once redelivery no longer covers
  the work. If the detached job crashes, nothing is redelivered. Make the job
  **report its own failures** (publish a `*_failed` event, or write a status file
  a supervisor plugin checks), so a dead job is visible.
- **Be idempotent on both sides.** The trigger *and* the completion event can each
  be redelivered, so dedupe both on a job id in `host.store` (as above) — otherwise
  a redelivered trigger launches a **duplicate** job.
- **The wrapper runs with the server's privileges** and its trailing argv is
  script-controlled (see [`exec` is not sandboxed](#exec-is-not-sandboxed-by-the-fs-sandbox)
  below). Validate the inputs you pass it and run it under a constrained OS user.

## Security model and limitations

**Read this before deploying a plugin.** Per ADR-0008, the plugin sandbox is
**defence-in-depth against mistakes and blast-radius — it is *not* an
adversarial, multi-tenant security boundary.** Plugins are expected to be
authored by **the operator or a trusted team**.

### `exec` is not sandboxed by the `fs` sandbox

This is the most important limitation. The **filesystem sandbox
(`host.fs`) only confines `host.fs.*` calls.** It does **not** constrain the
argument paths you pass to `host.exec`:

- An allowlisted command runs as a real OS process with **the server's
  privileges and the server's full filesystem access** — not confined to the
  plugin sandbox directory.
- The **trailing argv is fully script-controlled**, including **absolute paths
  and `..`**. The allowlist fixes *which binary runs* and its *leading
  arguments*; it does **not** restrict where that binary reads or writes.

So if `cat` were allowlisted, `host.exec("cat", ["/etc/passwd"])` would read a
file far outside the sandbox — the `fs` guards do nothing here.

**Operator guidance:** treat every entry in `execAllow` as if the plugin fully
controls the trailing arguments. Only allowlist commands whose **worst-case
behaviour with attacker-chosen trailing argv is acceptable**, and prefer argv
templates that pin the dangerous arguments. Constrain the `exec`-launched
process at the OS level (dedicated low-privilege user, `ulimit`/cgroups,
read-only mounts) rather than relying on the plugin sandbox.

### Other limits

- **In-process, no hard memory cap.** goja runs in the server process and has
  no hard memory bound; CPU is bounded only by the **cooperative** per-invocation
  timeout/interrupt, which cannot preempt certain tight native operations.
- **Not an isolation boundary.** A plugin shares the process with the server
  and other plugins. Store namespacing and path confinement prevent *accidental*
  cross-plugin access, not a determined in-process adversary.

### Do not run untrusted plugins

Because of the above, **running completely untrusted or third-party plugins is
not advised.** Plugin authors must be trusted operators or developers. If
genuinely untrusted plugins become a requirement, that is an **architecture
change**, not a config change: ADR-0008 records the revisit path — move to a
fuel-metered WASM host (`wazero`) or out-of-process isolation. Raise it through
`/refactor-architecture`.

## References

- ADR-0008 — plugin execution model, capability API, and threat model.
- `reference/security-fundamentals.md` — the host-boundary validation rules.
- `internal/plugin/` — the host adapter (capability shims and their bounds).
