package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "byteswarm.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg, err := loadConfig("") // no file
	if err != nil {
		t.Fatalf("loadConfig(\"\"): %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.Store.Driver != "sqlite" || cfg.Store.Path != "byteswarm.db" {
		t.Errorf("Store = %+v, want sqlite/byteswarm.db", cfg.Store)
	}
	if cfg.PluginsDir != "plugins" {
		t.Errorf("PluginsDir = %q, want plugins", cfg.PluginsDir)
	}
	if cfg.Socket.Path != "byteswarm-events.sock" || cfg.Socket.Mode != "0660" {
		t.Errorf("Socket = %+v, want byteswarm-events.sock / 0660", cfg.Socket)
	}
}

func TestLoadConfigParsesFile(t *testing.T) {
	path := writeConfigFile(t, `{
		"httpAddr": ":9000",
		"natsURL": "nats://cfg:4222",
		"store": {"driver": "sqlite", "path": "/data/state.db"},
		"pluginsDir": "/srv/plugins",
		"execAllow": {"backup": ["/usr/bin/tar", "czf"]},
		"plugins": [{"name": "greet", "events": ["order_created"], "script": "1"}]
	}`)

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.HTTPAddr != ":9000" || cfg.NATSURL != "nats://cfg:4222" {
		t.Errorf("scalars = %q / %q", cfg.HTTPAddr, cfg.NATSURL)
	}
	if cfg.Store.Path != "/data/state.db" || cfg.PluginsDir != "/srv/plugins" {
		t.Errorf("store/pluginsDir = %q / %q", cfg.Store.Path, cfg.PluginsDir)
	}
	if got := cfg.ExecAllow["backup"]; len(got) != 2 || got[0] != "/usr/bin/tar" {
		t.Errorf("execAllow[backup] = %v", got)
	}
	if len(cfg.Plugins) != 1 || cfg.Plugins[0].Name != "greet" {
		t.Errorf("plugins = %+v", cfg.Plugins)
	}
}

func TestLoadConfigEnvOverridesFile(t *testing.T) {
	path := writeConfigFile(t, `{"httpAddr": ":9000", "natsURL": "nats://file:4222", "store": {"path": "file.db"}}`)
	t.Setenv("BYTESWARM_HTTP_ADDR", ":7000")
	t.Setenv("BYTESWARM_NATS_URL", "nats://env:4222")
	t.Setenv("BYTESWARM_STORE_PATH", "env.db")
	t.Setenv("BYTESWARM_EVENTS_SOCKET", "/run/byteswarm/events.sock")
	t.Setenv("BYTESWARM_EVENTS_SOCKET_GROUP", "byteswarm")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.HTTPAddr != ":7000" {
		t.Errorf("HTTPAddr = %q, want env override :7000", cfg.HTTPAddr)
	}
	if cfg.Socket.Path != "/run/byteswarm/events.sock" || cfg.Socket.Group != "byteswarm" {
		t.Errorf("Socket env override = %+v", cfg.Socket)
	}
	if cfg.NATSURL != "nats://env:4222" {
		t.Errorf("NATSURL = %q, want env override", cfg.NATSURL)
	}
	if cfg.Store.Path != "env.db" {
		t.Errorf("Store.Path = %q, want env override env.db", cfg.Store.Path)
	}
}

func TestLoadConfigDefaultPluginTimeoutUnset(t *testing.T) {
	cfg, err := loadConfig("") // no file
	if err != nil {
		t.Fatalf("loadConfig(\"\"): %v", err)
	}
	// Unset by default so the host keeps its built-in bound; override "if desired".
	if _, set, err := cfg.pluginTimeout(); err != nil || set {
		t.Errorf("pluginTimeout() = (set=%v, err=%v), want unset with no error", set, err)
	}
}

func TestLoadConfigParsesPluginTimeout(t *testing.T) {
	path := writeConfigFile(t, `{"pluginTimeout": "250ms"}`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	d, set, err := cfg.pluginTimeout()
	if err != nil {
		t.Fatalf("pluginTimeout(): %v", err)
	}
	if !set || d != 250*time.Millisecond {
		t.Errorf("pluginTimeout() = (%v, set=%v), want 250ms set", d, set)
	}
}

func TestLoadConfigEnvOverridesPluginTimeout(t *testing.T) {
	path := writeConfigFile(t, `{"pluginTimeout": "1s"}`)
	t.Setenv("BYTESWARM_PLUGIN_TIMEOUT", "3s")
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	d, set, err := cfg.pluginTimeout()
	if err != nil || !set || d != 3*time.Second {
		t.Errorf("pluginTimeout() = (%v, set=%v, err=%v), want env override 3s", d, set, err)
	}
}

func TestLoadConfigFailsClosed(t *testing.T) {
	tests := map[string]string{
		"malformed json":               `{"httpAddr":`,
		"unknown field":                `{"nope": 1}`,
		"unsupported store driver":     `{"store": {"driver": "postgres"}}`,
		"invalid socket mode":          `{"socket": {"mode": "99z"}}`,
		"invalid plugin (no events)":   `{"plugins": [{"name": "x", "script": "1"}]}`,
		"invalid plugin (two sources)": `{"plugins": [{"name": "x", "events": ["e"], "path": "p.js", "script": "1"}]}`,
		"malformed plugin timeout":     `{"pluginTimeout": "5 minutes"}`,
		"zero plugin timeout":          `{"pluginTimeout": "0s"}`,
		"negative plugin timeout":      `{"pluginTimeout": "-1s"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := loadConfig(writeConfigFile(t, body)); err == nil {
				t.Fatalf("loadConfig(%s) = nil error, want failure", name)
			}
		})
	}
}
