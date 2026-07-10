# Security fundamentals

Baseline rules for all generated code, in all components, in all stacks.
Generated CLAUDE.md files point here; the implement-feature skill loads this
file before writing code that touches auth, input handling, secrets, or data.
Human-owned: skills read this file, never regenerate it.

## Secrets & configuration

- No secrets in source, config files, logs, error messages, or test fixtures —
  ever. Secrets arrive via environment variables or a secret manager.
- Commit a `.env.example` with variable names only; `.env` is gitignored.
- CI secrets live in the CI provider's secret store, referenced by name in
  pipeline files (see reference/ci-providers/).

## Input & output

- Validate and bound ALL external input (API payloads, query params, headers,
  file uploads, CLI args) at the boundary — type, size, range, format.
- Parameterised queries only; string-built SQL is prohibited.
- Encode output for its context (HTML templates auto-escape; never bypass).
- Set upload/size limits and request timeouts on every server.

## Authentication & authorization

- Never roll your own crypto or password hashing — stdlib/battle-tested
  libraries only (Go `golang.org/x/crypto`, Python `hashlib`/`secrets`, or the
  framework's vetted mechanism).
- Authorize on every request server-side; the client is always outside a security
  boundary. Deny by default.
- Tokens/sessions: short-lived, httpOnly + Secure cookies where applicable,
  invalidate on logout.

## Transport & storage

- TLS for anything crossing a machine boundary; internal-only is not an excuse.
- Encrypt sensitive data at rest; hash passwords with a modern KDF
  (argon2/bcrypt), never reversible encryption.
- Log security events (auth failures, permission denials) — but never log
  credentials, tokens, or personal data payloads.

## Dependencies & supply chain

- Pin dependency versions (go.mod, lockfiles). CI runs vulnerability scanning
  (see ci-providers step `security-scan`).
- New dependencies require justification per design-principles.md — smaller
  attack surface is a security control.

## Process

- Security-relevant changes (auth, crypto, input parsing, permissions) are
  called out explicitly in the PR "Impact" section for human attention.
- The 3-attempt guardrail does not apply to security fixes it cannot verify:
  if a security concern can't be resolved confidently, halt and raise a
  blocker immediately rather than iterating.
