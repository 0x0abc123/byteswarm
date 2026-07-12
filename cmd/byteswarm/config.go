package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/0x0abc123/byteswarm/internal/plugin"
)

// defaultSocketPath is the operator-local /events Unix socket (ADR-0011),
// relative to the working directory to match the store's default (byteswarm.db);
// production deployments set an absolute path via config/env. byteswarmctl
// defaults to the same path.
const defaultSocketPath = "byteswarm-events.sock"

// parseMode parses the octal socket mode string (e.g. "0660"). It fails closed:
// a malformed mode is a config error, not a silent default.
func (s socketConfig) parseMode() (os.FileMode, error) {
	m, err := strconv.ParseUint(s.Mode, 8, 32)
	if err != nil {
		return 0, err
	}
	return os.FileMode(m), nil
}

// pluginTimeout parses the optional per-invocation script timeout override. It
// returns set=false when unset (empty) so the caller can leave the host's
// built-in default in place, and fails closed on a malformed or non-positive
// value — a script must always run under a positive wall-clock bound (ADR-0008).
func (c appConfig) pluginTimeout() (d time.Duration, set bool, err error) {
	if c.PluginTimeout == "" {
		return 0, false, nil
	}
	d, err = time.ParseDuration(c.PluginTimeout)
	if err != nil {
		return 0, false, err
	}
	if d <= 0 {
		return 0, false, fmt.Errorf("must be positive")
	}
	return d, true, nil
}

// storeConfig selects the state store. Only "sqlite" is supported today; the
// PostgreSQL adapter is roadmap F2.2. Path is the SQLite database file.
type storeConfig struct {
	Driver string `json:"driver"`
	Path   string `json:"path"`
}

// socketConfig configures the operator-local /events Unix domain socket
// (ADR-0011). The socket's file permissions are the access control, so Mode and
// Group are security-relevant: keep the socket owned by the operator group at
// mode 0660 (or tighter). Mode is an octal string ("0660"); Group is a group
// name resolved at bind time (empty → leave the socket's default group).
type socketConfig struct {
	Path  string `json:"path"`
	Mode  string `json:"mode"`
	Group string `json:"group"`
}

// appConfig is byteswarm's committable configuration (ADR-0006). The JSON file
// is the reviewable base; environment variables override the scalar fields per
// deployment. Secrets never belong in the committed file — pass them via env
// (reference/security-fundamentals.md).
type appConfig struct {
	HTTPAddr   string                `json:"httpAddr"`
	Socket     socketConfig          `json:"socket"` // operator-local /events Unix socket (ADR-0011)
	NATSURL    string                `json:"natsURL"`
	WorkflowID string                `json:"workflowID"` // scope this instance to one workflowID; empty = any (ADR-0004, F4.4)
	Store      storeConfig           `json:"store"`
	PluginsDir string                `json:"pluginsDir"`
	ExecAllow  map[string][]string   `json:"execAllow"`
	Plugins    []plugin.PluginConfig `json:"plugins"`

	// PluginTimeout overrides the per-invocation script wall-clock bound
	// (ADR-0008). It is a Go duration string ("5s", "500ms"); empty leaves the
	// host's built-in default in place. A malformed or non-positive value is a
	// config error (fail closed) — a plugin must always run under a bound.
	PluginTimeout string `json:"pluginTimeout,omitempty"`

	// WebhookSecret is the shared secret authenticating POST /webhook. It is a
	// secret, so it comes ONLY from the environment (BYTESWARM_WEBHOOK_SECRET),
	// never the committed config file (json:"-"). Empty → the webhook denies
	// all callers (fail closed).
	WebhookSecret string `json:"-"`
}

// loadConfig reads the JSON config at path (empty path → defaults only),
// applies defaults, overlays environment overrides (env wins, ADR-0006), and
// validates. It fails closed: malformed config or invalid plugin declarations
// return an error so the caller can refuse to start.
func loadConfig(path string) (appConfig, error) {
	var cfg appConfig
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return appConfig{}, fmt.Errorf("config: opening %q: %w", path, err)
		}
		defer func() { _ = f.Close() }()
		dec := json.NewDecoder(f)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&cfg); err != nil {
			return appConfig{}, fmt.Errorf("config: parsing %q: %w", path, err)
		}
	}
	applyConfigDefaults(&cfg)
	applyConfigEnvOverrides(&cfg)
	if err := validateConfig(cfg); err != nil {
		return appConfig{}, err
	}
	return cfg, nil
}

func applyConfigDefaults(c *appConfig) {
	if c.HTTPAddr == "" {
		c.HTTPAddr = ":8080"
	}
	if c.Socket.Path == "" {
		c.Socket.Path = defaultSocketPath
	}
	if c.Socket.Mode == "" {
		c.Socket.Mode = "0660"
	}
	if c.Store.Driver == "" {
		c.Store.Driver = "sqlite"
	}
	if c.Store.Path == "" {
		c.Store.Path = "byteswarm.db"
	}
	if c.PluginsDir == "" {
		c.PluginsDir = "plugins"
	}
}

// applyConfigEnvOverrides lets environment variables win over file values for
// the scalar, per-deployment settings (ADR-0006). Plugin declarations and the
// exec allowlist come only from the file.
func applyConfigEnvOverrides(c *appConfig) {
	if v := os.Getenv("BYTESWARM_HTTP_ADDR"); v != "" {
		c.HTTPAddr = v
	}
	if v := os.Getenv("BYTESWARM_EVENTS_SOCKET"); v != "" {
		c.Socket.Path = v
	}
	if v := os.Getenv("BYTESWARM_EVENTS_SOCKET_MODE"); v != "" {
		c.Socket.Mode = v
	}
	if v := os.Getenv("BYTESWARM_EVENTS_SOCKET_GROUP"); v != "" {
		c.Socket.Group = v
	}
	if v := os.Getenv("BYTESWARM_NATS_URL"); v != "" {
		c.NATSURL = v
	}
	if v := os.Getenv("BYTESWARM_WORKFLOW_ID"); v != "" {
		c.WorkflowID = v
	}
	if v := os.Getenv("BYTESWARM_STORE_PATH"); v != "" {
		c.Store.Path = v
	}
	if v := os.Getenv("BYTESWARM_PLUGINS_DIR"); v != "" {
		c.PluginsDir = v
	}
	if v := os.Getenv("BYTESWARM_PLUGIN_TIMEOUT"); v != "" {
		c.PluginTimeout = v
	}
	if v := os.Getenv("BYTESWARM_WEBHOOK_SECRET"); v != "" {
		c.WebhookSecret = v
	}
}

func validateConfig(c appConfig) error {
	if c.Store.Driver != "sqlite" {
		return fmt.Errorf("config: unsupported store driver %q (only \"sqlite\" is available; PostgreSQL is roadmap F2.2)", c.Store.Driver)
	}
	if c.Socket.Path == "" {
		return fmt.Errorf("config: socket path must not be empty (ADR-0011: /events is served over a Unix socket)")
	}
	if _, err := c.Socket.parseMode(); err != nil {
		return fmt.Errorf("config: invalid socket mode %q (want octal, e.g. \"0660\"): %w", c.Socket.Mode, err)
	}
	if err := (plugin.Config{Plugins: c.Plugins}).Validate(); err != nil {
		return fmt.Errorf("config: invalid plugin declarations: %w", err)
	}
	if _, _, err := c.pluginTimeout(); err != nil {
		return fmt.Errorf("config: invalid pluginTimeout %q (want a positive Go duration, e.g. \"5s\"): %w", c.PluginTimeout, err)
	}
	return nil
}
