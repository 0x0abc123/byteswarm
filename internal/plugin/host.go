package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dop251/goja"

	"github.com/0x0abc123/byteswarm/internal/consumer"
	"github.com/0x0abc123/byteswarm/internal/event"
)

// defaultInvocationTimeout bounds a single script invocation. goja cannot
// preempt a tight loop except via a cooperative interrupt, so this watchdog is
// the enforced CPU/time bound (ADR-0008).
const defaultInvocationTimeout = 5 * time.Second

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
// mints a per-plugin ScriptConsumer wired with a confined Capabilities set and
// a compiled program. It is constructed once from the composition root.
type Host struct {
	repo  consumer.Repository
	pub   event.Publisher
	allow ExecAllowlist
	root  string // plugins root; scripts resolve under it, each plugin sandboxes at root/<name>
	log   *slog.Logger

	// Timeout is the per-invocation wall-clock bound. Defaulted by NewHost;
	// the composition root may override it before loading plugins.
	Timeout time.Duration
}

// NewHost constructs the host from composition-root dependencies.
func NewHost(repo consumer.Repository, pub event.Publisher, allow ExecAllowlist, root string, log *slog.Logger) *Host {
	return &Host{
		repo:    repo,
		pub:     pub,
		allow:   allow,
		root:    filepath.Clean(root),
		log:     log,
		Timeout: defaultInvocationTimeout,
	}
}

// Load compiles one plugin declaration into a ScriptConsumer, wiring its
// per-plugin capabilities. Compilation happens here, at load, and fails closed
// (ADR-0008): a plugin whose script does not compile does not start.
func (h *Host) Load(p PluginConfig) (*ScriptConsumer, error) {
	src, err := h.source(p)
	if err != nil {
		return nil, err
	}
	prog, err := goja.Compile(p.Name, src, true)
	if err != nil {
		return nil, fmt.Errorf("plugin %q: compile: %w", p.Name, err)
	}
	return &ScriptConsumer{
		name:    p.Name,
		events:  append([]string(nil), p.Events...),
		caps:    h.capsFor(p),
		prog:    prog,
		timeout: h.Timeout,
		log:     h.log,
	}, nil
}

// LoadAll compiles every declared plugin, failing closed on the first error so
// a broken plugin cannot leave the set partially loaded.
func (h *Host) LoadAll(cfg Config) ([]*ScriptConsumer, error) {
	consumers := make([]*ScriptConsumer, 0, len(cfg.Plugins))
	for _, p := range cfg.Plugins {
		sc, err := h.Load(p)
		if err != nil {
			return nil, err
		}
		consumers = append(consumers, sc)
	}
	return consumers, nil
}

// capsFor builds the confined capability set for one plugin: exec against the
// shared allowlist, a store namespaced to the plugin name, an fs sandbox at
// root/<name>, and publish onto the shared bus.
func (h *Host) capsFor(p PluginConfig) Capabilities {
	return Capabilities{
		Exec:    NewExecCapability(h.allow),
		Store:   NewNamespacedStore(h.repo, p.Name),
		FS:      NewSandboxedFS(filepath.Join(h.root, p.Name)),
		Publish: NewPublishCapability(h.pub),
	}
}

// source returns the plugin's JavaScript: the inline Script, or the contents of
// Path resolved within the plugins root. Absolute paths and ".." traversal are
// rejected (fail-closed), the same containment the fs sandbox applies.
func (h *Host) source(p PluginConfig) (string, error) {
	if p.Script != "" {
		return p.Script, nil
	}
	if filepath.IsAbs(p.Path) {
		return "", fmt.Errorf("plugin %q: script path must be relative to the plugins root", p.Name)
	}
	full := filepath.Join(h.root, p.Path)
	if full != h.root && !strings.HasPrefix(full, h.root+string(filepath.Separator)) {
		return "", fmt.Errorf("plugin %q: script path escapes the plugins root", p.Name)
	}
	b, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("plugin %q: reading script: %w", p.Name, err)
	}
	return string(b), nil
}

// ScriptConsumer is a runtime-loaded consumer whose handler is a JavaScript
// program executed on a goja runtime (ADR-0008). It implements the same
// consumer.Consumer port as native Go consumers, so the registry treats both
// alike. The program runs with two globals in scope: `event` (the delivered
// event) and `host` (the four capabilities).
type ScriptConsumer struct {
	name    string
	events  []string
	caps    Capabilities
	prog    *goja.Program
	timeout time.Duration
	log     *slog.Logger
}

// compile-time proof that the script host implements the domain dispatch port.
var _ consumer.Consumer = (*ScriptConsumer)(nil)

// Name is the plugin's host-controlled identity (also its store namespace and
// sandbox directory name).
func (c *ScriptConsumer) Name() string { return c.name }

// Events are the event types this plugin subscribes to (from its config
// declaration); the registry uses them to route deliveries.
func (c *ScriptConsumer) Events() []string { return append([]string(nil), c.events...) }

// Handle runs the compiled script against a delivered event on a fresh,
// panic-recovered goja runtime with a per-invocation timeout (ADR-0008). A
// fresh runtime per invocation gives hard inter-invocation isolation (no state
// leakage between events); the compiled program is shared and reused. On
// timeout, panic, or an uncaught script error, Handle returns an error and the
// event is left unacknowledged for redelivery rather than silently dropped.
func (c *ScriptConsumer) Handle(parent context.Context, e event.Event) (err error) {
	if c.prog == nil {
		return ErrNotImplemented // fail closed: an unloaded consumer never runs
	}

	ctx := parent
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, c.timeout)
		defer cancel()
	}

	rt := goja.New()
	c.inject(ctx, rt, e)

	// Watchdog: cooperatively interrupt the VM if the context is cancelled or
	// the invocation times out.
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			rt.Interrupt(ctx.Err())
		case <-stop:
		}
	}()

	defer func() {
		close(stop)
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q: recovered panic: %v", c.name, r)
		}
	}()

	if _, runErr := rt.RunProgram(c.prog); runErr != nil {
		return fmt.Errorf("plugin %q: %w", c.name, runErr)
	}
	return nil
}

// inject binds the `host` capability object and the `event` value into the
// runtime. Go functions that return a non-nil error surface to the script as a
// thrown exception, so a denied capability (exec allowlist, path escape,
// invalid event) fails closed and, if uncaught, fails the whole invocation.
func (c *ScriptConsumer) inject(ctx context.Context, rt *goja.Runtime, e event.Event) {
	host := map[string]interface{}{
		"exec": func(name string, args []string) (map[string]interface{}, error) {
			res, rerr := c.caps.Exec.Run(ctx, name, args)
			if rerr != nil {
				return nil, rerr
			}
			return map[string]interface{}{
				"stdout": string(res.Stdout),
				"stderr": string(res.Stderr),
				"code":   res.ExitCode,
			}, nil
		},
		"store": map[string]interface{}{
			"get": func(key string) (interface{}, error) {
				b, gerr := c.caps.Store.Load(ctx, key)
				if gerr != nil {
					return nil, gerr
				}
				if b == nil {
					return nil, nil // -> null in the script
				}
				return string(b), nil
			},
			"set": func(key, value string) error {
				return c.caps.Store.Save(ctx, key, []byte(value))
			},
		},
		"fs": map[string]interface{}{
			"read": func(path string) (string, error) {
				b, rerr := c.caps.FS.ReadFile(path)
				return string(b), rerr
			},
			"write": func(path, data string) error {
				return c.caps.FS.WriteFile(path, []byte(data))
			},
		},
		"publish": func(typ, workflowID string, payload interface{}) error {
			var pb []byte
			if payload != nil {
				b, merr := json.Marshal(payload)
				if merr != nil {
					return merr
				}
				pb = b
			}
			return c.caps.Publish.Publish(ctx, event.Event{Type: typ, WorkflowID: workflowID, Payload: pb})
		},
	}
	_ = rt.Set("host", host)

	// The event payload is untrusted input: parse it as JSON for ergonomics,
	// falling back to the raw string if it is not JSON.
	var payload interface{}
	if len(e.Payload) > 0 {
		if uerr := json.Unmarshal(e.Payload, &payload); uerr != nil {
			payload = string(e.Payload)
		}
	}
	_ = rt.Set("event", map[string]interface{}{
		"type":       e.Type,
		"workflowID": e.WorkflowID,
		"payload":    payload,
	})
}
