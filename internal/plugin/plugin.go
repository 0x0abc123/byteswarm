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
// The goja runtime (github.com/dop251/goja) executes each plugin on a fresh,
// panic-recovered runtime with a per-invocation timeout; the host-boundary
// guards (allowlist deny-by-default, key namespacing, path confinement, event
// validation) are enforced in the capability shims.
package plugin

import "errors"

// ErrNotImplemented is returned by a ScriptConsumer that has no compiled
// program — an unloaded consumer fails closed rather than silently succeeding.
var ErrNotImplemented = errors.New("plugin: script consumer has no compiled program")
