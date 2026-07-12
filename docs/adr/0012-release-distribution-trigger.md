---
id: "0012"
title: "Release distribution — versioned static binaries published on v* tags"
date: 2026-07-12
status: accepted
deciders: ["claude (advisor)", "0x0abc123"]
retrospective: false
decision_key: release_trigger
supersedes: null
superseded_by: null
tags: [ci, release, deployment, distribution]
---

# ADR-0012: Release distribution — versioned static binaries published on v* tags

## Context

ADR-0006 decided the deliverable is **static Go binaries** (`byteswarm`,
`byteswarmctl`) for VM/container/Kubernetes, but left *how and when* release
binaries are cut and distributed unspecified — until now they were only built as
a throwaway CI artifact on every push to main. The CI-provider reference expects
a release/deploy trigger to be recorded "per deployment ADR", so the decision
needs its own record. Operators and downstream deployers want a predictable,
auditable way to obtain versioned, integrity-checkable binaries without building
from source, and maintainers want releases to be an intentional act rather than a
side effect of branch activity. PR #42 implements the mechanism this ADR records.

## Decision

We will publish releases **on git tags matching `v*`**: pushing a `v<semver>`
tag builds static (`CGO_ENABLED=0`) `byteswarm` and `byteswarmctl` binaries for
the supported Unix targets — **linux/amd64, linux/arm64, darwin/arm64,
darwin/amd64** — packages each as a `.tar.gz` with a combined `SHA256SUMS`,
stamps the tag into each binary's `main.version` via `-ldflags`, and publishes
them as a **GitHub Release** (a semver pre-release tag, e.g. `v1.2.0-rc.1`,
publishes as a GitHub pre-release). No release is cut on branch pushes.

## Consequences

- Releases are explicit and auditable: a release exists **iff** a maintainer
  pushed a `v*` tag onto a commit already merged to main. Publishing writes a
  Release object plus assets, never a branch commit, so branch protection and
  the "never commit to main directly" rule are untouched.
- Reproducible, verifiable downloads: `-trimpath` builds, per-arch tarballs, a
  `SHA256SUMS` manifest, and a version-stamped binary (`byteswarmctl version`;
  the server logs its version at startup) — the tag string is the single source
  of version truth for both binaries.
- **Windows is out of scope for releases**: the server binds the operator-local
  `/events` Unix domain socket (ADR-0011, `syscall.Umask`) and does not build for
  Windows; shipping it would require platform-stubbing the socket and would break
  ADR-0011's OS-permission security model. A Windows CLI alone has no local socket
  to reach, so it is not shipped either.
- Distribution is **GitHub Releases only** for now. The container image also named
  by ADR-0006 is a separate, later decision; this ADR does not cover image
  publishing to a registry.
- CI cost: each `v*` tag runs a 4-target matrix build plus a publish job; the
  publish job alone holds `contents: write` (least privilege) and uses the
  built-in token, so no third-party release action is introduced.

## Alternatives considered

- Release on every push to main — rejected: needs a synthetic version scheme and
  produces a release per merge; tags give intentional, semver-named releases.
- Manual `workflow_dispatch` as the primary trigger — rejected: not reproducible
  from a ref and easy to forget; could be added later purely for ad-hoc builds.
- A third-party release action (e.g. `softprops/action-gh-release`) — rejected:
  the built-in `GITHUB_TOKEN` + `gh` CLI publish releases with one fewer pinned
  external dependency to vet.
- Amend ADR-0006 in place — rejected: this project treats accepted ADRs as
  immutable and refines them with new, appended ADRs.
