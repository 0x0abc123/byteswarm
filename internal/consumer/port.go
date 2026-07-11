package consumer

import (
	"context"

	"github.com/0x0abc123/byteswarm/internal/event"
)

// Consumer is the inbound dispatch port for an event handler (ADR-0001,
// ADR-0008). The registry maps an event type to a Consumer and calls Handle
// for each delivered event. Both native compile-time Go consumers and the
// runtime-loaded ScriptConsumer (internal/plugin) satisfy this one port, so
// adding script plugins is an adapter-only change: the signature does not vary
// by consumer kind.
//
// Implementations must be safe for concurrent invocation and must not panic
// across the port — the script host recovers panics on its own side per
// ADR-0008. Repository namespacing and other capability confinement live in
// the host shim, not in this port's signature.
type Consumer interface {
	Handle(ctx context.Context, e event.Event) error
}
