# byteswarm

byteswarm is an extensible, event-driven framework for defining and running
automation workflows — chains of commands, external tool invocations, and
telemetry. It runs headless on a server, where pluggable consumers subscribe to
event types, do work, and emit further events, composing larger workflows from
small, independent steps. Operators drive it from a CLI or trigger it over an
authenticated webhook; consumers persist durable state and support replay and
audit of past events.

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
See the container diagram at
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

Configuration is via a config file with environment overrides; see
[.env.example](.env.example) for the available settings.

## Repository map

| Path | Purpose | Design |
|---|---|---|
| cmd/byteswarm/ | Server binary — composition root (Server container) | docs/c4/l2-container.mmd |
| cmd/byteswarmctl/ | CLI — primary operator client (CLI container) | ADR-0002 |
| internal/event/ | Core event model & routing ports (domain) | ADR-0001, ADR-0004 |
| internal/consumer/ | Consumer port (native + script) & Repository port (domain) | ADR-0001, ADR-0005, ADR-0008 |
| internal/plugin/ | Script-plugin host adapter — goja JS runtime + sandboxed capability API (exec/store/fs/publish) | ADR-0008 |
| internal/workflow/ | Workflow lifecycle: subscription, reconnect, redelivery | ADR-0001, ADR-0004 |
| internal/auth/ | Authentication port (default shared-secret) | ADR-0002 |
| internal/server/ | Inbound HTTP adapter — mux, middleware, health | ADR-0002 |
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

## Security: the `/events` ingress is unauthenticated by design

The server exposes two event ingress paths, and they have **different trust
models**:

- **`POST /events`** — the operator-local ingress used by `byteswarmctl`. It
  performs input validation and bounding but **no authentication**. This is a
  deliberate trade-off (architecture brief: *"single trusted operator;
  publish/consume unauthenticated"*): anyone who can reach this endpoint can
  publish arbitrary events.
- **`POST /webhook`** — the ingress for untrusted external triggers. It
  **requires** the shared-secret authenticator (`BYTESWARM_WEBHOOK_SECRET`) and
  fails closed when no secret is configured (ADR-0002).

**Operational requirement:** `/events` must only be reachable from the trusted
operator boundary (e.g. loopback, a private management network, or a
reverse-proxy that enforces access control). Do **not** expose it to untrusted
callers — its safety depends entirely on network-level access control, not on
anything the application enforces. External event sources belong on `/webhook`.
