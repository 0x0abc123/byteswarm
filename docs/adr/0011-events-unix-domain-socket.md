---
id: "0011"
title: "Client approach — operator-local /events over a Unix domain socket"
date: 2026-07-12
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: client_approach
supersedes: "0002"
superseded_by: null
tags: [client, cli, ingress, security]
---

# ADR-0011: Client approach — operator-local /events over a Unix domain socket

## Context

The system runs headless on a server with a **CLI as its primary client** and an
inbound **webhook** for external triggers (the client approach set by ADR-0002,
which this ADR supersedes). ADR-0002 left the operator-local `POST /events`
ingress **unauthenticated by design** (recorded in PR #35), relying solely on
network-level controls to keep it reachable only from the trusted operator. A
defence-in-depth review (refactor-0004) found that this places the entire trust
burden on network configuration: one accidental `0.0.0.0` bind, a container
port-publish, an SSRF from another local service, or simply another local user
on a shared host defeats it. We want a second, stronger boundary that does not
depend on network configuration and introduces no long-lived secret.

## Decision

We will serve the operator-local event ingress (`POST /events`) **exclusively
over a Unix domain socket** — path from config, file mode `0660`, owned by the
operator group — and continue to serve the authenticated webhook (`POST
/webhook`) and the health endpoints over **TCP**. The `/events` trust boundary
becomes the **OS filesystem/user model** (socket-file permissions); remote and
cross-host publishers use the authenticated `/webhook` ingress. `byteswarmctl`
produces events by dialing the socket over HTTP — the HTTP request/response
contract is unchanged.

## Consequences

- Defence in depth: reaching `/events` now requires local filesystem access to
  the socket (operator user/group), not merely network reachability — an
  accidental TCP exposure or loopback SSRF can no longer submit events. No
  app-layer secret to store, rotate, or leak.
- `/webhook` remains the single **authenticated** path for untrusted, external,
  and cross-host callers. ADR-0006 deploy targets (VM, container, Kubernetes)
  **stand**: cross-pod/cross-host event submission keeps working via `/webhook`.
- Host-local constraint: `/events` is unreachable across pods/hosts. A CLI in a
  separate container must publish via `/webhook`, or share the socket through a
  mounted volume / sidecar in the same pod.
- Operational surface grows: new config for socket path, mode, and group; the
  server owns socket lifecycle (unlink-before-bind, remove on graceful
  shutdown); the composition root runs two `http.Server` instances.
- Platform: the group-permission model is POSIX. Windows/macOS operators fall
  back to the socket's default permissions (documented, not enforced).
- Adapter-only change: the domain ports (`event.Publisher`, `auth.Authenticator`)
  are untouched; transport wiring stays in `cmd/*/main.go`.
- Backward-incompatible for existing CLI users: `BYTESWARM_HTTP_ADDR` /
  `http://localhost` no longer reaches `/events`; the CLI target becomes a
  socket path.

## Alternatives considered

- Shared-secret cookie file over TCP — reuses the existing `Authenticator` port
  and works cross-host, but keeps a secret to manage and an open TCP surface;
  retained as the `/webhook` mechanism, not chosen for the local ingress.
- Leave `/events` unauthenticated with network controls only (ADR-0002 status
  quo) — a single boundary that one misconfiguration defeats.
- mTLS on `/events` — strong, but PKI/rotation cost is disproportionate for a
  host-local operator client.
- Env-var secret for `/events` — rejected: script-plugin `exec` children
  (ADR-0008) inherit `environ`, which would leak the credential.
