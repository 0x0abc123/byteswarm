package plugin

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// Capabilities is the exact, host-injected authority a script sees — no
// ambient authority, only these four host-mediated shims (ADR-0008).
type Capabilities struct {
	Exec    *ExecCapability
	Store   *NamespacedStore
	FS      *SandboxedFS
	Publish *PublishCapability
}

// Host is the goja script-plugin host adapter (ADR-0008). It owns the shared
// dependencies (repository, event publisher, exec allowlist, plugins root) and
// mints a per-plugin ScriptConsumer wired with a confined Capabilities set.
// The goja runtime pool that actually executes handlers is attached by the
// code-migration step; this skeleton wires the capabilities and satisfies the
// Consumer port so the registry can dispatch to script plugins.
type Host struct {
	repo  consumer.Repository
	pub   event.Publisher
	allow ExecAllowlist
	root  string // plugins sandbox root; each plugin gets root/<name>
	log   *slog.Logger
}

// NewHost constructs the host from composition-root dependencies.
func NewHost(repo consumer.Repository, pub event.Publisher, allow ExecAllowlist, root string, log *slog.Logger) *Host {
	return &Host{repo: repo, pub: pub, allow: allow, root: root, log: log}
}

// NewConsumer builds a ScriptConsumer for one loaded plugin declaration,
// wiring its per-plugin capabilities: exec against the shared allowlist, a
// store namespaced to the plugin name, an fs sandbox at root/<name>, and
// publish onto the shared bus. The compiled goja program is attached with the
// runtime; the returned consumer is wired but inert until then.
func (h *Host) NewConsumer(p PluginConfig) *ScriptConsumer {
	return &ScriptConsumer{
		name:   p.Name,
		events: append([]string(nil), p.Events...),
		caps: Capabilities{
			Exec:    NewExecCapability(h.allow),
			Store:   NewNamespacedStore(h.repo, p.Name),
			FS:      NewSandboxedFS(filepath.Join(h.root, p.Name)),
			Publish: NewPublishCapability(h.pub),
		},
	}
}

// ScriptConsumer is a runtime-loaded consumer whose handler is a JavaScript
// script executed on a goja runtime (ADR-0008). It implements the same
// consumer.Consumer port as native Go consumers, so the registry treats both
// alike.
type ScriptConsumer struct {
	name   string
	events []string
	caps   Capabilities
}

// compile-time proof that the script host implements the domain dispatch port.
var _ consumer.Consumer = (*ScriptConsumer)(nil)

// Name is the plugin's host-controlled identity (also its store namespace and
// sandbox directory name).
func (c *ScriptConsumer) Name() string { return c.name }

// Events are the event types this plugin subscribes to (from its config
// declaration); the registry uses them to route deliveries.
func (c *ScriptConsumer) Events() []string { return append([]string(nil), c.events...) }

// Handle runs the script against a delivered event on a pooled, panic-recovered
// goja runtime with a per-invocation timeout and instruction budget (ADR-0008).
// The runtime is attached by the code-migration step; until then Handle fails
// closed rather than silently dropping the event.
func (c *ScriptConsumer) Handle(_ context.Context, _ event.Event) error {
	return ErrNotImplemented
}
