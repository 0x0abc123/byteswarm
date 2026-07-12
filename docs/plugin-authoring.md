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
- Each invocation has a **per-invocation timeout** (default 5s); a runaway loop
  is interrupted and the invocation fails.
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
