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
module. Consumers are in-process plugins compiled into the server binary. The
main containers are the **server** (event routing and consumer host), the
**CLI** (`byteswarmctl`, the primary operator client), and a webhook ingress;
these sit alongside a NATS JetStream event bus and a PostgreSQL/SQLite state
repository, each reached through a port. See the container diagram at
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
| internal/consumer/ | Consumer registry & plugin SDK; Repository port | ADR-0001, ADR-0005 |
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
