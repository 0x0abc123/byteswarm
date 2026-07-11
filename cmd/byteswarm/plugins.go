package main

import (
	"fmt"
	"os"

	"github.com/0x0abc123/byteswarm/internal/plugin"
)

// buildPluginConsumers reads the plugin config at configPath, parses it, and
// loads (compiles) every declared plugin into a ScriptConsumer via the host.
// It fails closed: any read/parse/compile error is returned so the caller can
// refuse to start rather than run with missing or broken plugins (ADR-0008).
func buildPluginConsumers(host *plugin.Host, configPath string) ([]*plugin.ScriptConsumer, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading plugin config %q: %w", configPath, err)
	}
	cfg, err := plugin.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing plugin config %q: %w", configPath, err)
	}
	consumers, err := host.LoadAll(cfg)
	if err != nil {
		return nil, fmt.Errorf("loading plugins from %q: %w", configPath, err)
	}
	return consumers, nil
}

// storePath resolves the SQLite state-store path (BYTESWARM_STORE_PATH), with a
// sane default for single-node deployments.
func storePath() string {
	if p := os.Getenv("BYTESWARM_STORE_PATH"); p != "" {
		return p
	}
	return "byteswarm.db"
}

// pluginsDir resolves the per-plugin sandbox root (BYTESWARM_PLUGINS_DIR).
func pluginsDir() string {
	if d := os.Getenv("BYTESWARM_PLUGINS_DIR"); d != "" {
		return d
	}
	return "plugins"
}
