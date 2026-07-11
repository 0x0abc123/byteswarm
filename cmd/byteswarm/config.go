package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/0x0abc123/byteswarm/internal/plugin"
)

// storeConfig selects the state store. Only "sqlite" is supported today; the
// PostgreSQL adapter is roadmap F2.2. Path is the SQLite database file.
type storeConfig struct {
	Driver string `json:"driver"`
	Path   string `json:"path"`
}

// appConfig is byteswarm's committable configuration (ADR-0006). The JSON file
// is the reviewable base; environment variables override the scalar fields per
// deployment. Secrets never belong in the committed file — pass them via env
// (reference/security-fundamentals.md).
type appConfig struct {
	HTTPAddr   string                `json:"httpAddr"`
	NATSURL    string                `json:"natsURL"`
	Store      storeConfig           `json:"store"`
	PluginsDir string                `json:"pluginsDir"`
	ExecAllow  map[string][]string   `json:"execAllow"`
	Plugins    []plugin.PluginConfig `json:"plugins"`

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
	if v := os.Getenv("BYTESWARM_NATS_URL"); v != "" {
		c.NATSURL = v
	}
	if v := os.Getenv("BYTESWARM_STORE_PATH"); v != "" {
		c.Store.Path = v
	}
	if v := os.Getenv("BYTESWARM_PLUGINS_DIR"); v != "" {
		c.PluginsDir = v
	}
	if v := os.Getenv("BYTESWARM_WEBHOOK_SECRET"); v != "" {
		c.WebhookSecret = v
	}
}

func validateConfig(c appConfig) error {
	if c.Store.Driver != "sqlite" {
		return fmt.Errorf("config: unsupported store driver %q (only \"sqlite\" is available; PostgreSQL is roadmap F2.2)", c.Store.Driver)
	}
	if err := (plugin.Config{Plugins: c.Plugins}).Validate(); err != nil {
		return fmt.Errorf("config: invalid plugin declarations: %w", err)
	}
	return nil
}
