// Package plugin is the runtime script-plugin host adapter (ADR-0008). It lets
// an operator author event handlers as JavaScript without rebuilding the
// binary: a ScriptConsumer implements the existing internal/consumer.Consumer
// port and runs on an embedded goja runtime, coexisting with native Go
// consumers behind the same port.
//
// This package is an ADAPTER — it depends on the domain ports (consumer,
// event) and is wired from the composition root; the domain never depends on
// it. It exposes exactly four host-mediated capabilities to a script — exec
// (allowlisted, argv-only), store (namespaced Repository), fs (sandboxed
// directory), and publish (Bus) — each validated at the host boundary because
// the event payload is untrusted input (reference/security-fundamentals.md).
//
// Walking skeleton: the goja runtime itself (github.com/dop251/goja) is wired
// by the code-migration step. Everything here builds on the standard library —
// the host-boundary guards (allowlist deny-by-default, namespace prefixing,
// path confinement) are real; the goja-, OS-, and I/O-touching bodies are
// placeholders returning ErrNotImplemented until the engine is attached.
package plugin

import "errors"

// ErrNotImplemented marks a placeholder whose real body is attached with the
// goja runtime (code-migration step, ADR-0008). It fails closed: an
// unwired capability refuses rather than silently succeeding.
var ErrNotImplemented = errors.New("plugin: not implemented until goja runtime is wired")
