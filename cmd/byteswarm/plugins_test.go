package main

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/0x0abc123/byteswarm/internal/plugin"
	"github.com/0x0abc123/byteswarm/internal/store"
)

func pluginTestHost(t *testing.T) *plugin.Host {
	t.Helper()
	repo, err := store.NewSQLite(":memory:")
	if err != nil {
		t.Fatalf("NewSQLite(:memory:): %v", err)
	}
	t.Cleanup(func() { _ = repo.Close() })
	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	// noBusPublisher is only invoked if a script calls host.publish, which
	// buildPluginConsumers (parse+compile only) never does.
	return plugin.NewHost(repo, noBusPublisher{}, plugin.ExecAllowlist{}, t.TempDir(), log)
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plugins.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestBuildPluginConsumersLoadsInlineScript(t *testing.T) {
	host := pluginTestHost(t)
	cfgPath := writeConfig(t, `{"plugins":[{"name":"greet","events":["order.created"],"script":"1"}]}`)

	consumers, err := buildPluginConsumers(host, cfgPath)
	if err != nil {
		t.Fatalf("buildPluginConsumers: %v", err)
	}
	if len(consumers) != 1 {
		t.Fatalf("got %d consumers, want 1", len(consumers))
	}
	if ev := consumers[0].Events(); len(ev) != 1 || ev[0] != "order.created" {
		t.Fatalf("Events() = %v, want [order.created]", ev)
	}
}

func TestBuildPluginConsumersFailsClosed(t *testing.T) {
	host := pluginTestHost(t)

	// Missing config file.
	if _, err := buildPluginConsumers(host, filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("missing config path should return an error")
	}

	// Malformed JSON.
	if _, err := buildPluginConsumers(host, writeConfig(t, "{not json")); err == nil {
		t.Fatal("malformed config should return an error")
	}

	// Uncompilable script (fails at load).
	bad := writeConfig(t, `{"plugins":[{"name":"broken","events":["x"],"script":"function ("}]}`)
	if _, err := buildPluginConsumers(host, bad); err == nil {
		t.Fatal("uncompilable plugin script should return an error (fail closed)")
	}
}
