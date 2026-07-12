# byteswarm

byteswarm is an extensible, event-driven framework for defining and running
automation workflows — chains of commands, external tool invocations, and
telemetry. It runs headless on a server, where pluggable consumers subscribe to
event types, do work, and emit further events, composing larger workflows from
small, independent steps. Operators drive it from a CLI over a local Unix socket
or trigger it over an authenticated webhook; consumers persist durable state and
support replay and audit of past events.

## Architecture at a glance

byteswarm is a modular monolith built ports-and-adapters style as a single Go
module. Consumers come in two kinds behind a single Consumer port: Go consumers
compiled into the server binary, and JavaScript plugins loaded at runtime from
config on an embedded goja engine (ADR-0008) — so operators can add or change
handlers without rebuilding. Script plugins run in a host-mediated sandbox that
exposes exactly four capabilities (an allowlisted `exec`, a namespaced store, a
confined filesystem, and event publish). The main containers are the **server**
(event routing and consumer host), the **CLI** (`byteswarmctl`, the primary
operator client), and a webhook ingress; these sit alongside a NATS JetStream
event bus and a PostgreSQL/SQLite state repository, each reached through a port.

The server exposes two ingress paths with different trust boundaries (ADR-0011):
the operator-local `POST /events` is served over a **Unix domain socket** (mode
`0660`, operator group) bounded by OS filesystem permissions, while the
authenticated `POST /webhook` and the health endpoints are served over **TCP**
for cross-host and untrusted callers. See the container diagram at
[docs/c4/l2-container.mmd](docs/c4/l2-container.mmd) and the decision records in
[docs/adr/](docs/adr/) for the full picture.

## Getting started

**Prerequisites:** Go ≥ 1.22 (`gofmt` and the test toolchain ship with Go).

The whole project is a single Go module — run every command at the repo root.

```
# setup
go mod download

# run the server
go run ./cmd/byteswarm

# run the CLI
go run ./cmd/byteswarmctl

# test
go test -race -cover ./...
```

**Configuration** is via a config file with environment overrides. The event
ingress split (ADR-0011) drives the current config surface:

- **`/events` Unix domain socket** — the server binds and owns the socket
  (unlink-before-bind, removed on graceful shutdown). Config covers the socket
  **path**, file **mode** (default `0660`), and owning **group** (the operator
  group). `byteswarmctl` reaches the server by **dialing this socket path**, not
  an `http://` address — the HTTP request/response contract is unchanged.
- **TCP address** — the listen address for the authenticated `/webhook` ingress
  and the health endpoints; also how cross-host and remote publishers submit
  events.
- **Event bus** (ADR-0004), **state store** (ADR-0005), and **webhook shared
  secret** (fail-closed).

See [.env.example](.env.example) for the available settings and their variable
names.

## Repository map

| Path | Purpose | Design |
|---|---|---|
| cmd/byteswarm/ | Server binary — composition root (Server container) | docs/c4/l2-container.mmd |
| cmd/byteswarmctl/ | CLI — primary operator client (CLI container) | ADR-0011 |
| internal/event/ | Core event model & routing ports (domain) | ADR-0001, ADR-0004 |
| internal/consumer/ | Consumer port (native + script) & Repository port (domain) | ADR-0001, ADR-0005, ADR-0009, ADR-0008 |
| internal/store/ | Repository-port adapters — PostgreSQL (BYTEA) + SQLite (BLOB); consumer state is opaque bytes | ADR-0005, ADR-0009 |
| internal/plugin/ | Script-plugin host adapter — goja JS runtime + sandboxed capability API (exec/store/fs/publish) | ADR-0008 |
| internal/workflow/ | Workflow lifecycle: subscription, reconnect, redelivery | ADR-0001, ADR-0004 |
| internal/bus/ | Event-bus adapter — NATS JetStream publish + durable subscription (event.Bus port) | ADR-0004 |
| internal/auth/ | Authentication port (default shared-secret) | ADR-0011 |
| internal/server/ | Inbound HTTP adapter — dual-listener: /events over Unix domain socket (operator-local, OS-permission bounded), /webhook + health over TCP | ADR-0011 |
| internal/telemetry/ | Outbound business-event emitter port + slog | ADR-0001 |

External containers (no code here): NATS JetStream bus (ADR-0004),
PostgreSQL/SQLite store (ADR-0005).

## Contributing

- All code changes flow through the `/implement-feature` skill; architecture
  changes go through `/refactor-architecture`.
- Pull requests require a green verification run and human review; never commit
  to main directly or self-merge.
- See [docs/template-guide.md](docs/template-guide.md) for the full development
  lifecycle.

<!-- CUSTOM: additions below this line are preserved on regeneration -->

## Writing plugins

Authoring runtime JavaScript plugins? See the
[plugin authoring guide](docs/plugin-authoring.md) for the full `host` API
contract and — importantly — its security model and limitations (the `fs`
sandbox does not constrain `exec` argument paths; untrusted plugins are not
advised).

## Security: the two ingress trust models

The server exposes two event ingress paths with **different trust boundaries**
(ADR-0011):

- **`POST /events`** — the operator-local ingress used by `byteswarmctl`, served
  over a **Unix domain socket** (mode `0660`, owned by the operator group). It
  performs input validation and bounding; it has no application-layer
  authentication because the **OS filesystem/user permissions on the socket are
  the access control** — only processes running as the operator user or the
  socket's group can connect. This is defence in depth: unlike a network
  boundary, it is not defeated by an accidental `0.0.0.0` bind or a loopback
  SSRF, and there is no long-lived secret to leak.
- **`POST /webhook`** — the ingress for untrusted, external, and **cross-host**
  callers, served over **TCP**. It **requires** the shared-secret authenticator
  (`BYTESWARM_WEBHOOK_SECRET`) and fails closed when no secret is configured.

**Operational notes:**

- The Unix socket is host-local. A `byteswarmctl` running in a **separate
  pod/container/host** cannot reach `/events`; it must publish via the
  authenticated `/webhook`, or share the socket through a mounted volume /
  sidecar in the same pod.
- Set the socket's owning group to the operator group and keep the mode at
  `0660` (or tighter). On Windows/macOS the group-permission model differs;
  treat those as development-only for the operator ingress.
- External event sources always belong on `/webhook`, never on `/events`.
