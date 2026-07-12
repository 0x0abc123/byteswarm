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

### Option B — hand the work to a `byteswarm-job` job

For genuinely long jobs (minutes, or unbounded), don't do the work in the
handler. Trigger a **`byteswarm-job` job** (ADR-0013): a separate, operator-
deployed binary — the same bundled goja engine — that **detaches itself** and
runs an **operator-authored JavaScript job** with a broader host API
(`publish`/`exec`/`fs`/`http`/`log`) *outside* the plugin sandbox. Your plugin
just launches it by **name** and returns; the job publishes its own completion
event, which any plugin can subscribe to. No bash, no manual daemonizing, no
Node.

The pieces:

- **The runner detaches for you.** `host.exec("run-job", …)` returns in
  milliseconds because `byteswarm-job` re-execs itself into a new session and
  the launcher exits — the real work runs on in the background. (Under the hood
  this is the same detach the old bash wrapper did by hand; now it's built in.)
- **Containment is by name, not path.** The `execAllow` template pins the runner
  **and its jobs directory**; your plugin passes only a job *name*, which the
  runner resolves *within* that directory (absolute paths and `..` rejected). A
  plugin can only trigger scripts the operator placed there.
- **Completion is a normal `host.publish`** from inside the job — straight to
  `/events`, no `byteswarmctl`, no NATS credentials.

**1. Operator wiring** — pin the runner (with its dirs) in `execAllow`; the
plugin supplies only `--job-id`, `--workflow`, the job name, and its args:

```json
{
  "execAllow": {
    "run-job": ["byteswarm-job", "run",
      "--jobs-dir", "/opt/byteswarm/jobs",
      "--socket", "/run/byteswarm/events.sock",
      "--log-dir", "/var/log/byteswarm/jobs",
      "--workdir-base", "/var/lib/byteswarm/jobs",
      "--max-wall-clock", "30m"]
  },
  "plugins": [
    { "name": "reports", "events": ["report_requested"], "path": "reports.js" }
  ]
}
```

**2. The job** (`/opt/byteswarm/jobs/report.js`) — plain JavaScript, run by
`byteswarm-job` **outside** the plugin sandbox (full host API; bounded by
`--max-wall-clock`, not the per-invocation timeout):

```js
// job.args is UNTRUSTED — it came from a plugin that handled an untrusted
// event payload. Validate before using it anywhere powerful.
const orderId = job.args[0];
if (!/^[0-9]+$/.test(orderId)) throw new Error("bad orderId: " + orderId);

host.log("info", "building report", { orderId: orderId });
const pdf = host.fs.workdir() + "/report.pdf";
const r = host.exec("/usr/bin/generate-report", [orderId, pdf], { timeoutMs: 20 * 60 * 1000 });
if (r.code !== 0) throw new Error("generator exited " + r.code); // -> job_failed safety net

host.http.request({ method: "PUT", url: "https://store.internal/reports/" + orderId, body: host.fs.read(pdf) });

// Report completion back into byteswarm — host.publish, straight to /events.
host.publish("report_ready", job.workflowID, { orderId: orderId, jobId: job.id });
```

**3. The trigger plugin** (`reports.js`) — a normal sandboxed plugin that
launches the job by name and returns immediately:

```js
const orderId = String(event.payload.id);
const jobId = event.workflowID + "-" + orderId;
// Dedupe: the trigger can be redelivered (at-least-once). Launch once.
if (!host.store.get("job:" + jobId)) {
  host.store.set("job:" + jobId, "started");
  // Returns immediately — byteswarm-job daemonizes; "report.js" is resolved
  // within the operator's jobs dir; the rest are the job's argv.
  host.exec("run-job", ["--job-id", jobId, "--workflow", event.workflowID, "report.js", orderId]);
}
```

A downstream plugin subscribing to `report_ready` then handles the result — a
plain plugin, nothing special.

**Things to get right with this pattern:**

- **No durability once the trigger returns.** `host.exec` returns as soon as the
  job detaches, so the triggering event is **acknowledged** — the bus's
  at-least-once redelivery no longer covers the work. `byteswarm-job` auto-
  publishes a `job_failed` event and writes a per-job log if the job throws or
  hits the wall-clock, so a dead job is visible; design your job to report its
  own outcome.
- **Be idempotent.** A redelivered trigger launches a **duplicate** job, so dedupe
  on a job id in `host.store` (as above).
- **The job runs with the runner's privileges** — unrestricted `exec`, open `fs`,
  and its `job.args` are plugin-supplied (see [`exec` is not sandboxed](#exec-is-not-sandboxed-by-the-fs-sandbox)
  below). Run `byteswarm-job` as a **constrained OS user that is a member of the
  `/events` socket group** (so `host.publish` works while `exec`/`fs` are OS-
  contained). See the README for the full job-runner host API and deployment.

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
